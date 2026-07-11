package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
)

// These tests drive the real compiled shell under a pseudo-terminal, typing
// keystrokes exactly as a user would: line editing, tab completion, Ctrl+R
// history search, and the Ctrl+G grab picker are exercised end to end.

var shellBinary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "cshell-test")
	if err != nil {
		fmt.Fprintln(os.Stderr, "tempdir:", err)
		os.Exit(1)
	}
	bin := dir + "/cshell"
	out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "building shell binary: %v\n%s", err, out)
		os.Exit(1)
	}
	shellBinary = bin

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type ptySession struct {
	t    *testing.T
	cmd  *exec.Cmd
	ptmx *os.File
	ch   chan []byte
	buf  bytes.Buffer
	pos  int // consumed offset into the normalized output
}

// startShell launches the shell in a fresh pty with an isolated HOME so
// history and rc files do not leak between tests or from the real user.
func startShell(t *testing.T) *ptySession {
	t.Helper()
	s := startShellInHome(t, t.TempDir())
	s.expect("$ ")
	return s
}

// startShellInHome starts the shell with HOME pointing at dir (which may
// contain a .cshrc) and does not wait for a prompt — rc files can change it.
func startShellInHome(t *testing.T, home string) *ptySession {
	t.Helper()

	cmd := exec.Command(shellBinary)
	// Pin a deterministic default prompt; the shipped default is
	// environment-dependent (\u@\h \W % ). Tests that need a custom PS1
	// still override this via .cshrc or an interactive assignment.
	cmd.Env = append(os.Environ(), "HOME="+home, "PS1=$ ")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Skipf("pty unavailable: %v", err)
	}
	pty.Setsize(ptmx, &pty.Winsize{Rows: 24, Cols: 120})

	s := &ptySession{t: t, cmd: cmd, ptmx: ptmx, ch: make(chan []byte, 256)}
	go func() {
		for {
			b := make([]byte, 4096)
			n, rerr := ptmx.Read(b)
			if n > 0 {
				s.ch <- b[:n]
			}
			if rerr != nil {
				close(s.ch)
				return
			}
		}
	}()

	t.Cleanup(func() {
		ptmx.Close()
		cmd.Process.Kill()
		cmd.Wait()
	})

	return s
}

func (s *ptySession) send(text string) {
	s.t.Helper()
	if _, err := s.ptmx.WriteString(text); err != nil {
		s.t.Fatalf("writing %q: %v", text, err)
	}
}

// ansiSGR matches color/attribute escape sequences (ESC [ ... m).
var ansiSGR = regexp.MustCompile("\x1b\\[[0-9;]*m")

// normalized is the output so far with carriage returns and SGR color codes
// stripped: the pty stack inserts \r unpredictably (ONLCR at two layers), and
// the prompt and typed input are colored, so tests match on plain \n-only
// text. Escape-sequence expansion itself is covered by TestExpandPrompt.
func (s *ptySession) normalized() string {
	out := strings.ReplaceAll(s.buf.String(), "\r", "")
	return ansiSGR.ReplaceAllString(out, "")
}

// expect waits until the not-yet-consumed output contains sub, then consumes
// through the end of the match. Consuming makes assertions order-sensitive:
// a marker in a later expect cannot be satisfied by an earlier echo of the
// keystrokes that typed it.
func (s *ptySession) expect(sub string) {
	s.t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		norm := s.normalized()
		if idx := strings.Index(norm[s.pos:], sub); idx >= 0 {
			s.pos += idx + len(sub)
			return
		}
		select {
		case chunk, ok := <-s.ch:
			if !ok {
				s.t.Fatalf("shell exited while waiting for %q\nunconsumed output: %q", sub, norm[s.pos:])
			}
			s.buf.Write(chunk)
		case <-deadline:
			s.t.Fatalf("timeout waiting for %q\nunconsumed output: %q", sub, norm[s.pos:])
		}
	}
}

// drain pulls in whatever output is already buffered and returns all of it
// (consumed and unconsumed), for negative assertions.
func (s *ptySession) drain() string {
	for {
		select {
		case chunk, ok := <-s.ch:
			if !ok {
				return s.normalized()
			}
			s.buf.Write(chunk)
		default:
			return s.normalized()
		}
	}
}

func TestInteractive_EchoRoundTrip(t *testing.T) {
	s := startShell(t)
	s.send("echo round-trip-marker\r")
	// the bare marker followed by newline only appears in command output;
	// the typed line always has escape codes between the text and the \n
	s.expect("round-trip-marker\n")
	s.expect("$ ")
}

func TestInteractive_CommandsSeeATTY(t *testing.T) {
	s := startShell(t)
	// external `test -t 1` succeeds only when stdout is a terminal — proving
	// commands run under the pty, not a plain pipe
	s.send("test -t 1 && echo is-a-tty\r")
	s.expect("is-a-tty\n")
}

func TestInteractive_TabCompletesCommand(t *testing.T) {
	s := startShell(t)
	// "ech" + Tab must complete to "echo " (builtin); if completion failed
	// the line would run as "echcompletion-ok" and error
	s.send("ech\t")
	s.expect("echo ")
	s.send("completion-ok\r")
	s.expect("completion-ok\n")
	if strings.Contains(s.drain(), "command not found") {
		t.Fatalf("completion did not produce a runnable command:\n%q", s.drain())
	}
}

func TestInteractive_HistoryUpArrow(t *testing.T) {
	s := startShell(t)
	s.send("echo history-entry-one\r")
	s.expect("history-entry-one\n")
	s.expect("$ ") // raw mode is on once the prompt renders

	// Up arrow recalls the previous command into the editor
	s.send("\x1b[A")
	s.expect("$ echo history-entry-one")

	// and Enter reruns it
	s.send("\r")
	s.expect("history-entry-one\n")
}

func TestInteractive_CtrlRSearch(t *testing.T) {
	s := startShell(t)
	s.send("echo needle-in-haystack\r")
	s.expect("needle-in-haystack\n")
	s.expect("$ ")
	s.send("echo something-else\r")
	s.expect("something-else\n")
	// wait for the prompt: it renders only after raw mode is on, and in
	// cooked mode the kernel line discipline would swallow Ctrl+R (VREPRINT)
	s.expect("$ ")

	// Ctrl+R, type a fragment: the search line shows the older match
	s.send("\x12needle")
	s.expect("(reverse-i-search)`needle': echo needle-in-haystack")

	// Enter submits the found command
	s.send("\r")
	s.expect("needle-in-haystack\n")
}

func TestInteractive_GrabPicker(t *testing.T) {
	s := startShell(t)
	// produce output containing a token worth grabbing; printf so the token
	// itself is never typed — it exists only in command output
	s.send("printf 'grab-target-%s\\n' abc123\r")
	s.expect("grab-target-abc123\n")
	s.expect("$ ") // raw mode is on once the prompt renders

	// start a new command, open the picker, filter, check the highlighted row
	s.send("echo ")
	s.send("\x07") // Ctrl+G
	s.expect("grab> ")
	s.send("abc123")
	s.expect("> grab-target-abc123") // the highlighted picker row

	// accept: the grabbed token lands in the command line
	s.send("\r")
	s.expect("$ echo grab-target-abc123")

	// and runs
	s.send("\r")
	s.expect("grab-target-abc123\n")
}

func TestInteractive_PS2Continuation(t *testing.T) {
	s := startShell(t)
	// unclosed quote: shell asks for more input instead of erroring
	s.send("echo 'first-part\r")
	s.expect("> ")
	s.send("second-part'\r")
	s.expect("first-part\nsecond-part\n")
}

func TestInteractive_CtrlCAbortsLine(t *testing.T) {
	s := startShell(t)
	s.send("garbage-that-should-never-run")
	s.send("\x03") // Ctrl+C
	s.expect("^C")
	s.expect("$ ")

	s.send("echo after-abort\r")
	s.expect("after-abort\n")
	if strings.Contains(s.drain(), "command not found") {
		t.Fatal("aborted line was executed")
	}
}

// Regression: quitting an interactive pager used to wedge the terminal —
// the pager's /dev/tty pointed at the capture pty, which nothing wrote
// keyboard input into. Commands now own the capture pty as their
// controlling terminal and keystrokes are forwarded through it.
func TestInteractive_PagerLessQuit(t *testing.T) {
	dir := t.TempDir()
	content := strings.Repeat("pager content line\n", 200)
	if err := os.WriteFile(dir+"/file.txt", []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	s := startShell(t)
	s.send("less " + dir + "/file.txt\r")
	s.expect("pager content line")
	time.Sleep(300 * time.Millisecond) // let less finish terminal setup

	s.send(" ") // page down: proves the pager receives keys at all
	time.Sleep(200 * time.Millisecond)
	s.send("q")
	s.expect("$ ") // prompt is back — input typed before it would be flushed

	s.send("echo pager-closed-fine\r")
	s.expect("pager-closed-fine\n")
}

func TestInteractive_PagerManQuit(t *testing.T) {
	s := startShell(t)
	s.send("man echo\r")
	s.expect("ECHO") // man page rendered
	time.Sleep(500 * time.Millisecond)

	s.send("q")
	s.expect("$ ")
	s.send("echo man-closed-fine\r")
	s.expect("man-closed-fine\n")
}

func TestInteractive_CommandReadsKeyboard(t *testing.T) {
	s := startShell(t)
	// cat with no args reads the terminal: typed lines must reach it
	s.send("cat\r")
	time.Sleep(300 * time.Millisecond)
	s.send("typed-into-cat\r")
	s.expect("typed-into-cat") // echoed by the pty and/or printed by cat
	s.send("\x04")             // Ctrl+D: EOF ends cat
	s.expect("$ ")
	s.send("echo cat-done\r")
	s.expect("cat-done\n")
}

func TestInteractive_CtrlCKillsCommandNotShell(t *testing.T) {
	s := startShell(t)
	s.send("sleep 30\r")
	time.Sleep(300 * time.Millisecond)
	s.send("\x03") // Ctrl+C → pty line discipline → SIGINT to sleep only
	s.expect("$ ")

	// if sleep survived, this times out; if the shell died, expect fails
	s.send("echo shell-survived\r")
	s.expect("shell-survived\n")
}

func TestInteractive_CtrlDExits(t *testing.T) {
	s := startShell(t)
	s.send("\x04") // Ctrl+D on empty line

	done := make(chan error, 1)
	go func() { done <- s.cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected clean exit, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shell did not exit on Ctrl+D")
	}
}

func TestInteractive_CshrcAndPS1(t *testing.T) {
	home := t.TempDir()
	rc := `# cshell test config
echo rc-file-loaded
export RCVAR=from-the-rc-file
PS1='\e[32mcustom\e[0m%'
PS2=more%
`
	if err := os.WriteFile(home+"/.cshrc", []byte(rc), 0644); err != nil {
		t.Fatal(err)
	}

	s := startShellInHome(t, home)

	// rc commands ran before the first prompt, which uses the custom PS1
	// (its \e color escapes are stripped by normalized(); expansion itself is
	// covered by TestExpandPrompt)
	s.expect("rc-file-loaded")
	s.expect("custom%")

	// exported rc variables reach child processes
	s.send("printenv RCVAR\r")
	s.expect("from-the-rc-file\n")
	s.expect("custom") // next prompt

	// PS2 from the rc file shows on continuation lines
	s.send("echo 'part-one\r")
	s.expect("more%")
	s.send("part-two'\r")
	s.expect("part-one\npart-two\n")
}

func TestInteractive_AssignmentPersistsAcrossCommands(t *testing.T) {
	s := startShell(t)
	// set PS1 interactively: the very next prompt uses it
	s.send("PS1=changed%% \r")
	s.expect("changed%% ")
	s.send("echo still-works\r")
	s.expect("still-works\n")
}

func TestInteractive_LineEditing(t *testing.T) {
	s := startShell(t)
	// type a broken command, then fix it with editing keys:
	// "Xcho edit-test" → Ctrl+A, Delete the X, type "e"
	s.send("Xcho edit-test")
	s.send("\x01")    // Ctrl+A: home
	s.send("\x1b[3~") // Delete: removes the X
	s.send("e")       // now the line reads "echo edit-test"
	s.expect("$ echo edit-test")
	s.send("\r")
	s.expect("edit-test\n")
}
