package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// syncMarker is an OSC sequence written to the pty after each command; when
// it comes back out of the master, every byte the command printed has been
// mirrored to the screen and captured. It doubles as a workaround for macOS
// discarding buffered pty output when the slave side closes — the slave
// never closes. Terminals ignore unknown OSC sequences, so even a leak would
// be invisible.
const syncMarker = "\x1b]7373;cshell-sync\x07"

// posixVDisable disables a termios control character (_POSIX_VDISABLE).
const posixVDisable = 0xff

// ptyRunner runs commands on a session-long pseudo-terminal that acts as
// their real terminal, the same way tmux and script(1) do:
//
//	keyboard → real tty (raw) → pump → pty master → command
//	command → pty slave → master → mirror → screen + scrollback
//
// The command owns the pty as its controlling terminal, so /dev/tty works —
// pagers and editors read their keys from it — while isatty, colors and
// window size all behave. Ctrl+C becomes a byte we forward, and the pty's
// line discipline turns it into SIGINT for the command alone. Redirected
// output (`> file`) bypasses the pty, exactly like it bypasses the screen.
type ptyRunner struct {
	ptmx *os.File
	tty  *os.File
	fd   int // the real terminal (stdin)
	pump *stdinPump
	sb   *Scrollback
	sync chan struct{}
}

func newPtyRunner(sb *Scrollback, fd int, pump *stdinPump) (*ptyRunner, error) {
	ptmx, tty, err := pty.Open()
	if err != nil {
		return nil, err
	}

	// adopt the pty as the shell's controlling terminal (ensureOwnSession
	// made us a ctty-less session leader). Children inherit it, so their
	// /dev/tty — pagers, editors, password prompts — is the capture pty,
	// where forwarded keystrokes actually arrive.
	if err := unix.IoctlSetInt(int(tty.Fd()), unix.TIOCSCTTY, 0); err != nil {
		fmt.Fprintln(os.Stderr, "cshell: warning: no controlling terminal for commands:", err)
	}

	pr := &ptyRunner{ptmx: ptmx, tty: tty, fd: fd, pump: pump, sb: sb, sync: make(chan struct{}, 1)}
	pr.disableSuspend()
	go pr.mirror()
	return pr, nil
}

// disableSuspend turns off ^Z/^Y on the pty. A suspended command would leave
// the shell waiting forever with no job control to resume it, so the bytes
// are passed through to the command instead.
func (pr *ptyRunner) disableSuspend() {
	tio, err := unix.IoctlGetTermios(int(pr.tty.Fd()), ioctlGetTermios)
	if err != nil {
		return
	}
	tio.Cc[unix.VSUSP] = posixVDisable
	disableDelayedSuspend(tio) // ^Y, Darwin/BSD only
	unix.IoctlSetTermios(int(pr.tty.Fd()), ioctlSetTermios, tio)
}

// mirror pumps master → (scrollback, screen), stripping sync markers and
// signaling each one. Capture happens before mirroring: once output is
// visible on screen it must already be grabbable.
func (pr *ptyRunner) mirror() {
	marker := []byte(syncMarker)
	var carry []byte
	buf := make([]byte, 32*1024)

	for {
		n, err := pr.ptmx.Read(buf)
		if n > 0 {
			data := append(carry, buf[:n]...)
			for {
				idx := bytes.Index(data, marker)
				if idx < 0 {
					break
				}
				pr.emit(data[:idx])
				data = data[idx+len(marker):]
				select {
				case pr.sync <- struct{}{}:
				default:
				}
			}
			// hold back a possible marker prefix at the end of the chunk
			hold := markerPrefixLen(data, marker)
			pr.emit(data[: len(data)-hold : len(data)-hold])
			carry = append(carry[:0], data[len(data)-hold:]...)
		}
		if err != nil {
			return
		}
	}
}

func (pr *ptyRunner) emit(p []byte) {
	if len(p) == 0 {
		return
	}
	pr.sb.Write(p)
	os.Stdout.Write(p)
}

// markerPrefixLen reports how many trailing bytes of data could be the start
// of the marker and must be withheld until more bytes arrive.
func markerPrefixLen(data, marker []byte) int {
	longest := len(marker) - 1
	if longest > len(data) {
		longest = len(data)
	}
	for k := longest; k > 0; k-- {
		if bytes.HasSuffix(data, marker[:k]) {
			return k
		}
	}
	return 0
}

// Run executes one parsed command line on the pty and returns once all of
// its output has reached the screen, so the next prompt renders after it.
func (pr *ptyRunner) Run(cs *Cshell, ast Node) int {
	// drop a stale token a previous timed-out run may have left behind
	select {
	case <-pr.sync:
	default:
	}

	_ = pty.InheritSize(os.Stdin, pr.ptmx)

	// raw mode on the real terminal: every keystroke (including Ctrl+C)
	// passes through to the pty, whose line discipline handles signals,
	// echo, and line editing for the command
	oldState, rawErr := term.MakeRaw(pr.fd)
	if rawErr == nil {
		defer term.Restore(pr.fd, oldState)
	}

	// keep the command's window size in sync with the real terminal
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)
	go func() {
		for range winch {
			pty.InheritSize(os.Stdin, pr.ptmx)
		}
	}()

	// forward keyboard input into the pty for the command's lifetime
	stop := make(chan struct{})
	forwarderDone := make(chan struct{})
	go func() {
		defer close(forwarderDone)
		for {
			select {
			case chunk, ok := <-pr.pump.ch:
				if !ok {
					return
				}
				pr.ptmx.Write(chunk)
			case <-stop:
				return
			}
		}
	}()

	status := cs.execNode(ast, IOStreams{In: pr.tty, Out: pr.tty, Err: pr.tty})

	close(stop)
	<-forwarderDone

	// discard typed-ahead input the command never read, so stale bytes
	// don't leak into the next command's stdin
	pr.flushInput()

	pr.tty.WriteString(syncMarker)
	select {
	case <-pr.sync:
	case <-time.After(2 * time.Second):
		// something is still holding the terminal (e.g. an orphaned
		// background write); don't wedge the prompt over it
	}
	return status
}

// flushInput drops unread bytes from the pty's input queue by re-applying
// the current termios with the set-and-flush variant.
func (pr *ptyRunner) flushInput() {
	tio, err := unix.IoctlGetTermios(int(pr.tty.Fd()), ioctlGetTermios)
	if err != nil {
		return
	}
	unix.IoctlSetTermios(int(pr.tty.Fd()), ioctlSetTermiosFlush, tio)
}

// execute runs the session's parsed AST through the pty runner, falling back
// to plain streams (still captured, stdin forwarded via a pipe) when no pty
// is available.
func (ts *TermSession) execute() {
	shell := ts.shell
	if shell.AST == nil {
		return
	}
	if ts.runner != nil {
		shell.LastStatus = ts.runner.Run(shell, shell.AST)
		return
	}
	shell.LastStatus = ts.runWithoutPty(shell.AST)
}

// runWithoutPty is the degraded path: commands see pipes instead of a tty,
// but keyboard input still reaches them (through the pump, which owns the
// real stdin and must not be bypassed).
func (ts *TermSession) runWithoutPty(ast Node) int {
	r, w, err := os.Pipe()
	if err != nil {
		return ts.shell.execNode(ast, IOStreams{
			In:  bytes.NewReader(nil),
			Out: io.MultiWriter(os.Stdout, ts.sb),
			Err: io.MultiWriter(os.Stderr, ts.sb),
		})
	}

	stop := make(chan struct{})
	go func() {
		defer w.Close()
		for {
			select {
			case chunk, ok := <-ts.pump.ch:
				if !ok {
					return
				}
				if _, err := w.Write(chunk); err != nil {
					return
				}
			case <-stop:
				return
			}
		}
	}()

	status := ts.shell.execNode(ast, IOStreams{
		In:  r,
		Out: io.MultiWriter(os.Stdout, ts.sb),
		Err: io.MultiWriter(os.Stderr, ts.sb),
	})

	close(stop)
	r.Close()
	return status
}
