package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// IOStreams carries the stdin/stdout/stderr a node runs with. Pipelines and
// redirects work by swapping these before handing them to a command.
type IOStreams struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

func (cs *Cshell) execNode(n Node, s IOStreams) int {
	switch v := n.(type) {
	case *List:
		status := 0
		for _, item := range v.Items {
			status = cs.execNode(item, s)
		}
		return status

	case *AndOr:
		left := cs.execNode(v.Left, s)
		if v.Op == LOGICAL_AND && left == 0 {
			return cs.execNode(v.Right, s)
		}
		if v.Op == LOGICAL_OR && left != 0 {
			return cs.execNode(v.Right, s)
		}
		return left

	case *Pipeline:
		return cs.execPipeline(v, s)

	case *SimpleCommand:
		return cs.execSimple(v, s)
	}
	return 0
}

// syncWriter serializes writes from concurrent pipeline stages that share a
// writer (e.g. every stage writes the same stderr).
type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (sw *syncWriter) Write(p []byte) (int, error) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	return sw.w.Write(p)
}

// sharedWriter makes a writer safe to hand to several pipeline stages at
// once. *os.File stays as-is: children then inherit the fd directly and the
// kernel handles interleaving, exactly like a real shell.
func sharedWriter(w io.Writer) io.Writer {
	if _, ok := w.(*os.File); ok {
		return w
	}
	return &syncWriter{w: w}
}

// execPipeline wires the commands together with OS pipes and runs them all
// concurrently, like a real shell: `a | b` starts b before a finishes.
// The pipeline's status is the last command's status.
func (cs *Cshell) execPipeline(p *Pipeline, s IOStreams) int {
	if len(p.Cmds) == 1 {
		return cs.execSimple(p.Cmds[0], s)
	}

	// stderr (and stdout, via 2>&1 or for the last stage) is shared by all
	// concurrently running stages
	s.Out = sharedWriter(s.Out)
	s.Err = sharedWriter(s.Err)

	statuses := make([]int, len(p.Cmds))
	var wg sync.WaitGroup
	var prevRead *os.File

	for i, cmd := range p.Cmds {
		streams := s
		if prevRead != nil {
			streams.In = prevRead
		}

		var pipeWrite, nextRead *os.File
		if i < len(p.Cmds)-1 {
			r, w, err := os.Pipe()
			if err != nil {
				fmt.Fprintln(s.Err, "cshell: pipe:", err)
				wg.Wait()
				return 1
			}
			streams.Out = w
			pipeWrite = w
			nextRead = r
		}

		wg.Add(1)
		go func(i int, cmd *SimpleCommand, streams IOStreams, readEnd, writeEnd *os.File) {
			defer wg.Done()
			statuses[i] = cs.execSimpleIn(cmd, streams, true)
			// closing our write end gives the downstream command EOF;
			// closing the read end after we finish unblocks an upstream
			// writer if we exited early
			if writeEnd != nil {
				writeEnd.Close()
			}
			if readEnd != nil {
				readEnd.Close()
			}
		}(i, cmd, streams, prevRead, pipeWrite)

		prevRead = nextRead
	}

	wg.Wait()
	return statuses[len(statuses)-1]
}

func (cs *Cshell) execSimple(cmd *SimpleCommand, s IOStreams) int {
	return cs.execSimpleIn(cmd, s, false)
}

// execSimpleIn runs one simple command. inPipeline marks commands running as
// pipeline stages, where POSIX gives assignments subshell semantics: they
// must not (and, since stages run concurrently, safely cannot) mutate the
// parent shell.
func (cs *Cshell) execSimpleIn(cmd *SimpleCommand, s IOStreams, inPipeline bool) int {
	streams, closers, err := applyRedirects(cmd.Redirects, s)
	defer func() {
		for _, c := range closers {
			c.Close()
		}
	}()
	if err != nil {
		fmt.Fprintln(s.Err, "cshell: "+err.Error())
		return 1
	}

	// leading NAME=value words are variable assignments
	words := cmd.Args
	var assigns []string
	for len(words) > 0 {
		if _, _, ok := splitAssignment(words[0]); !ok {
			break
		}
		assigns = append(assigns, words[0])
		words = words[1:]
	}

	// assignment-only command: set shell variables (PS1=..., PATH=...)
	if len(words) == 0 && len(assigns) > 0 {
		if !inPipeline {
			for _, a := range assigns {
				name, value, _ := splitAssignment(a)
				cs.setVar(name, value)
			}
		}
		return 0
	}

	// redirect-only command like `> file`: the open above already did the work
	if len(words) == 0 {
		return 0
	}

	name := words[0]
	args := words[1:]

	if fn, ok := cs.Commands[name]; ok {
		return fn(args, streams)
	}

	// a name containing a slash bypasses PATH lookup (POSIX)
	path := name
	if strings.Contains(name, "/") {
		if _, err := os.Stat(name); err != nil {
			fmt.Fprintln(streams.Err, name+": No such file or directory")
			return 127
		}
	} else {
		found, ferr := cs.findExecutable(name)
		if ferr != nil {
			fmt.Fprintln(streams.Err, name+": command not found")
			return 127
		}
		path = found
	}

	c := exec.Command(path, args...)
	c.Stdin = streams.In
	c.Stdout = streams.Out
	c.Stderr = streams.Err
	if len(assigns) > 0 {
		// FOO=bar cmd: the assignment lives only in that command's environment
		c.Env = append(os.Environ(), assigns...)
	}
	if err := c.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			if code := ee.ExitCode(); code >= 0 {
				return code
			}
			return 1 // killed by a signal
		}
		fmt.Fprintln(streams.Err, "cshell: "+err.Error())
		return 126
	}
	return 0
}

// applyRedirects opens the files a command's redirects name and returns the
// adjusted streams plus the files to close once the command finishes.
// Redirects apply left to right, so `> out 2>&1` sends both to the file while
// `2>&1 > out` sends stderr to the original stdout — same as POSIX shells.
func applyRedirects(redirects []Redirect, s IOStreams) (IOStreams, []io.Closer, error) {
	var closers []io.Closer

	for _, r := range redirects {
		switch r.Op {
		case REDIRECT_IN:
			if r.FD != 0 {
				return s, closers, fmt.Errorf("input redirection for fd %d is not supported", r.FD)
			}
			f, err := os.Open(r.Target)
			if err != nil {
				return s, closers, fmt.Errorf("%s: No such file or directory", r.Target)
			}
			closers = append(closers, f)
			s.In = f

		case REDIRECT_OUT, REDIRECT_OUT_APPEND:
			flags := os.O_WRONLY | os.O_CREATE
			if r.Op == REDIRECT_OUT_APPEND {
				flags |= os.O_APPEND
			} else {
				flags |= os.O_TRUNC
			}
			f, err := os.OpenFile(r.Target, flags, 0644)
			if err != nil {
				return s, closers, fmt.Errorf("%s: %v", r.Target, err)
			}
			closers = append(closers, f)
			switch r.FD {
			case 1:
				s.Out = f
			case 2:
				s.Err = f
			default:
				return s, closers, fmt.Errorf("output redirection for fd %d is not supported", r.FD)
			}

		case REDIRECT_DUP_OUT:
			var target io.Writer
			switch r.Target {
			case "1":
				target = s.Out
			case "2":
				target = s.Err
			default:
				return s, closers, fmt.Errorf("%s: bad file descriptor", r.Target)
			}
			switch r.FD {
			case 1:
				s.Out = target
			case 2:
				s.Err = target
			default:
				return s, closers, fmt.Errorf("output redirection for fd %d is not supported", r.FD)
			}

		case REDIRECT_DUP_IN:
			return s, closers, fmt.Errorf("input fd duplication (<&) is not supported yet")
		}
	}

	return s, closers, nil
}
