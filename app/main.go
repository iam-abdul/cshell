package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/term"
)

type Cshell struct {
	Commands   map[string]Builtin
	Vars       map[string]string // shell variables (PS1, PS2, ...)
	Input      string
	Tokens     []Token
	Position   int
	AST        Node
	LastStatus int
	Cleanup    func() // restores the terminal; run before the process exits
}

// processInput lexes and parses one line of input into cs.AST.
func (cs *Cshell) processInput(input string) error {
	cs.AST = nil
	if err := cs.lexInput(input); err != nil {
		return err
	}
	ast, err := Parse(cs.Tokens)
	if err != nil {
		return err
	}
	cs.AST = ast
	return nil
}

// execute runs the parsed AST against the shell's real streams and records
// the exit status.
func (cs *Cshell) execute() int {
	if cs.AST == nil {
		return cs.LastStatus
	}
	cs.LastStatus = cs.execNode(cs.AST, IOStreams{
		In:  os.Stdin,
		Out: os.Stdout,
		Err: os.Stderr,
	})
	return cs.LastStatus
}

func (cs *Cshell) findExecutable(name string) (string, error) {
	allPaths := os.Getenv("PATH")
	if allPaths != "" {
		paths := strings.Split(allPaths, string(os.PathListSeparator))
		for _, dir := range paths {
			files, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, file := range files {
				if file.Name() == name {
					stat, err := os.Stat(dir + string(os.PathSeparator) + file.Name())
					if err != nil {
						continue
					}
					mode := stat.Mode()
					if mode.IsRegular() {
						if (mode & 0111) != 0 {
							return dir + string(os.PathSeparator) + file.Name(), nil
						}
					}
				}
			}
		}
	}
	return "", errors.New("executable not found")
}

func NewCshell() *Cshell {
	cs := &Cshell{
		Commands: make(map[string]Builtin),
		Vars:     make(map[string]string),
	}

	cs.Register("exit", cs.exit)
	cs.Register("echo", cs.echo)
	cs.Register("type", cs.typeCmd)
	cs.Register("pwd", cs.pwd)
	cs.Register("cd", cs.cd)
	cs.Register("export", cs.export)

	return cs
}

func (cs *Cshell) Register(name string, fn Builtin) {
	cs.Commands[name] = fn
}

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version", "-v", "version":
			fmt.Println("cshell", version)
			return
		}
	}

	shell := NewCshell()

	// full line editing needs a terminal on both ends; piped input (scripts,
	// tests) gets the plain read-a-line loop
	if term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) {
		ensureOwnSession()
		runInteractive(shell)
		return
	}
	runBatch(shell)
}

// ensureOwnSession makes the shell a session leader with no controlling
// terminal, so it can adopt the capture pty as the controlling terminal for
// itself and every command it spawns (the tmux model). The session leader
// must be the long-lived shell: on macOS, a session leader's death hangs up
// and revokes its pty, which is why commands themselves cannot take that
// role. If setsid is impossible from this process (it is already a group or
// session leader), the shell re-execs itself once as a child, which can.
func ensureOwnSession() {
	if os.Getenv("CSHELL_SESSION") == "1" {
		return // re-exec'd child: the parent's Setsid already applied
	}
	if _, err := syscall.Setsid(); err == nil {
		return
	}

	exe, err := os.Executable()
	if err != nil {
		return // degraded: commands may not have a working /dev/tty
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "CSHELL_SESSION=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return
	}

	// relay the signals a terminal sends to its foreground job — the child
	// is in another session now, so the kernel won't deliver them there
	sigs := make(chan os.Signal, 8)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGWINCH)
	go func() {
		for s := range sigs {
			cmd.Process.Signal(s)
		}
	}()

	err = cmd.Wait()
	if ee, ok := err.(*exec.ExitError); ok {
		os.Exit(ee.ExitCode())
	}
	os.Exit(0)
}

func runBatch(shell *Cshell) {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print("$ ")
		input, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				fmt.Println()
				os.Exit(shell.LastStatus)
			}
			fmt.Fprintln(os.Stderr, "cshell: error reading input:", err)
			os.Exit(1)
		}

		// a bad line must never kill the shell: report and prompt again
		if err := shell.processInput(input); err != nil {
			fmt.Fprintln(os.Stderr, "cshell: "+err.Error())
			shell.LastStatus = 1
			continue
		}
		shell.execute()
	}
}
