package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"golang.org/x/term"
)

// errAborted is returned by ReadLine when the user hits Ctrl+C.
var errAborted = errors.New("aborted")

// setTerminalTitle writes an OSC 0 sequence so the terminal tab/window shows
// the given name. Without this the emulator falls back to the foreground
// process name (e.g. "go" when launched via `go run`).
func setTerminalTitle(w io.Writer, title string) {
	fmt.Fprintf(w, "\x1b]0;%s\x07", title)
}

// TermSession owns the interactive terminal: raw mode, rendering, and the
// key loop. All editing state lives in the pure Editor/History/Picker types;
// this file is the thin I/O shell around them.
type TermSession struct {
	shell  *Cshell
	fd     int
	pump   *stdinPump
	src    *termSource
	out    *bufio.Writer
	ed     *Editor
	hist   *History
	sb     *Scrollback
	runner *ptyRunner

	prompt    string
	promptW   int // display columns the prompt occupies (ANSI-aware)
	width     int
	histIdx   int
	histSaved string
}

func NewTermSession(shell *Cshell) (*TermSession, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return nil, errors.New("stdin is not a terminal")
	}
	sb := NewScrollback()
	pump := newStdinPump(fd)
	runner, err := newPtyRunner(sb, fd, pump) // nil runner falls back to plain streams
	if err != nil {
		runner = nil
	}

	// `exit` may run while the real terminal is raw: restore the state we
	// found at startup on the way out
	if initial, err := term.GetState(fd); err == nil {
		shell.Cleanup = func() { term.Restore(fd, initial) }
	}

	return &TermSession{
		shell:  shell,
		fd:     fd,
		pump:   pump,
		src:    newTermSource(pump),
		out:    bufio.NewWriter(os.Stdout),
		ed:     &Editor{},
		hist:   LoadHistory(defaultHistoryPath()),
		sb:     sb,
		runner: runner,
		width:  80,
	}, nil
}

// ReadLine edits one line in raw mode. Returns io.EOF on Ctrl+D at an empty
// line and errAborted on Ctrl+C.
func (ts *TermSession) ReadLine(prompt string) (string, error) {
	oldState, err := term.MakeRaw(ts.fd)
	if err != nil {
		return "", err
	}
	defer term.Restore(ts.fd, oldState)

	// multi-line PS1: print every line but the last once; editing happens on
	// the final line, which is all the redraw logic needs to track
	if i := strings.LastIndexByte(prompt, '\n'); i >= 0 {
		ts.out.WriteString(strings.ReplaceAll(prompt[:i+1], "\n", "\r\n"))
		prompt = prompt[i+1:]
	}
	ts.prompt = prompt
	ts.promptW = promptWidth(prompt)

	if w, _, err := term.GetSize(ts.fd); err == nil && w > 0 {
		ts.width = w
	}

	ts.ed.Reset()
	ts.histIdx = len(ts.hist.Items)
	ts.histSaved = ""

	for {
		ts.render()

		key, err := decodeKey(ts.src)
		if err != nil {
			ts.out.WriteString("\r\n")
			ts.out.Flush()
			return "", err
		}

		switch {
		case key.Kind == KeyEnter:
			ts.ed.MoveEnd()
			ts.render()
			ts.out.WriteString("\r\n")
			ts.out.Flush()
			return ts.ed.String(), nil

		case key.Kind == KeyCtrl && key.Rune == 'c':
			ts.out.WriteString("^C\r\n")
			ts.out.Flush()
			return "", errAborted

		case key.Kind == KeyCtrl && key.Rune == 'd':
			if len(ts.ed.Buf) == 0 {
				ts.out.WriteString("\r\n")
				ts.out.Flush()
				return "", io.EOF
			}
			ts.ed.Delete()

		case key.Kind == KeyBackspace:
			ts.ed.Backspace()
		case key.Kind == KeyDelete:
			ts.ed.Delete()

		case key.Kind == KeyLeft, key.Kind == KeyCtrl && key.Rune == 'b':
			ts.ed.MoveLeft()
		case key.Kind == KeyRight, key.Kind == KeyCtrl && key.Rune == 'f':
			ts.ed.MoveRight()
		case key.Kind == KeyHome, key.Kind == KeyCtrl && key.Rune == 'a':
			ts.ed.MoveHome()
		case key.Kind == KeyEnd, key.Kind == KeyCtrl && key.Rune == 'e':
			ts.ed.MoveEnd()

		case key.Kind == KeyAlt && (key.Rune == 'b' || key.Rune == 'B'):
			ts.ed.WordLeft()
		case key.Kind == KeyAlt && (key.Rune == 'f' || key.Rune == 'F'):
			ts.ed.WordRight()
		case key.Kind == KeyAlt && (key.Rune == 'd' || key.Rune == 'D'):
			ts.ed.DeleteWordForward()

		case key.Kind == KeyCtrl && key.Rune == 'w':
			ts.ed.DeleteWordBack()
		case key.Kind == KeyCtrl && key.Rune == 'u':
			ts.ed.KillToStart()
		case key.Kind == KeyCtrl && key.Rune == 'k':
			ts.ed.KillToEnd()
		case key.Kind == KeyCtrl && key.Rune == 'y':
			ts.ed.Yank()

		case key.Kind == KeyCtrl && key.Rune == 'l':
			ts.out.WriteString("\x1b[H\x1b[2J")

		case key.Kind == KeyUp, key.Kind == KeyCtrl && key.Rune == 'p':
			ts.histPrev()
		case key.Kind == KeyDown, key.Kind == KeyCtrl && key.Rune == 'n':
			ts.histNext()

		case key.Kind == KeyTab:
			ts.completeAtCursor()

		case key.Kind == KeyCtrl && key.Rune == 'r':
			if line, submit := ts.searchHistory(); submit {
				ts.ed.Set(line)
				ts.render()
				ts.out.WriteString("\r\n")
				ts.out.Flush()
				return line, nil
			}

		case key.Kind == KeyCtrl && key.Rune == 'g':
			ts.grabFromScreen()

		case key.Kind == KeyRune:
			ts.ed.Insert(key.Rune)
		}
	}
}

// render redraws the prompt line. Long lines scroll horizontally so the
// cursor stays visible (single-row strategy, like linenoise).
func (ts *TermSession) render() {
	w := ts.width
	if w <= 0 {
		w = 80
	}
	avail := w - ts.promptW - 1
	if avail < 1 {
		avail = 1
	}

	buf := ts.ed.Buf
	cur := ts.ed.Cursor
	start := 0
	if cur > avail {
		start = cur - avail
	}
	end := start + avail
	if end > len(buf) {
		end = len(buf)
	}

	fmt.Fprintf(ts.out, "\r%s%s\x1b[K", ts.prompt, displayable(buf[start:end]))
	col := ts.promptW + (cur - start)
	ts.out.WriteString("\r")
	if col > 0 {
		fmt.Fprintf(ts.out, "\x1b[%dC", col)
	}
	ts.out.Flush()
}

// displayable makes control characters visible; a recalled multi-line
// history entry shows its newlines as ⏎ but submits with real newlines.
func displayable(rs []rune) string {
	var b strings.Builder
	for _, r := range rs {
		switch {
		case r == '\n':
			b.WriteRune('⏎')
		case r < 0x20:
			b.WriteRune('?')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (ts *TermSession) bell() {
	ts.out.WriteString("\a")
	ts.out.Flush()
}

// --- history navigation -------------------------------------------------

func (ts *TermSession) histPrev() {
	if ts.histIdx == 0 {
		ts.bell()
		return
	}
	if ts.histIdx == len(ts.hist.Items) {
		ts.histSaved = ts.ed.String()
	}
	ts.histIdx--
	ts.ed.Set(ts.hist.Items[ts.histIdx])
}

func (ts *TermSession) histNext() {
	if ts.histIdx >= len(ts.hist.Items) {
		return
	}
	ts.histIdx++
	if ts.histIdx == len(ts.hist.Items) {
		ts.ed.Set(ts.histSaved)
	} else {
		ts.ed.Set(ts.hist.Items[ts.histIdx])
	}
}

// searchHistory is the Ctrl+R mode: an incremental reverse substring search.
// Returns (line, true) when Enter submits a match; false when the user exits
// back to normal editing (with the match, on Esc) or cancels (Ctrl+G/C).
func (ts *TermSession) searchHistory() (string, bool) {
	var query []rune
	matchIdx := -1
	orig := ts.ed.String()

	renderSearch := func() {
		match := ""
		if matchIdx >= 0 {
			match = ts.hist.Items[matchIdx]
		}
		line := fmt.Sprintf("(reverse-i-search)`%s': %s", string(query), displayable([]rune(match)))
		if len(line) > ts.width-1 && ts.width > 1 {
			line = line[:ts.width-1]
		}
		fmt.Fprintf(ts.out, "\r%s\x1b[K", line)
		ts.out.Flush()
	}

	for {
		renderSearch()

		key, err := decodeKey(ts.src)
		if err != nil {
			ts.ed.Set(orig)
			return "", false
		}

		switch {
		case key.Kind == KeyCtrl && key.Rune == 'r':
			from := matchIdx - 1
			if matchIdx < 0 {
				from = len(ts.hist.Items) - 1
			}
			if idx := ts.hist.SearchBackward(string(query), from); idx >= 0 {
				matchIdx = idx
			}

		case key.Kind == KeyRune:
			query = append(query, key.Rune)
			from := matchIdx
			if from < 0 {
				from = len(ts.hist.Items) - 1
			}
			matchIdx = ts.hist.SearchBackward(string(query), from)

		case key.Kind == KeyBackspace:
			if len(query) > 0 {
				query = query[:len(query)-1]
			}
			matchIdx = ts.hist.SearchBackward(string(query), len(ts.hist.Items)-1)

		case key.Kind == KeyEnter:
			if matchIdx >= 0 {
				return ts.hist.Items[matchIdx], true
			}
			ts.ed.Set(orig)
			return "", false

		case key.Kind == KeyEsc, key.Kind == KeyLeft, key.Kind == KeyRight,
			key.Kind == KeyUp, key.Kind == KeyDown:
			if matchIdx >= 0 {
				ts.ed.Set(ts.hist.Items[matchIdx])
			} else {
				ts.ed.Set(orig)
			}
			return "", false

		case key.Kind == KeyCtrl && (key.Rune == 'g' || key.Rune == 'c'):
			ts.ed.Set(orig)
			return "", false
		}
	}
}

// --- tab completion -----------------------------------------------------

func (ts *TermSession) completeAtCursor() {
	line := ts.ed.String()
	cursorBytes := len(string(ts.ed.Buf[:ts.ed.Cursor]))

	comp := ts.shell.Complete(line, cursorBytes)
	if len(comp.Candidates) == 0 {
		ts.bell()
		return
	}

	// convert byte offsets back to rune offsets for the editor
	runeStart := len([]rune(line[:comp.WordStart]))
	word := line[comp.WordStart:cursorBytes]

	if len(comp.Candidates) == 1 {
		ts.ed.ReplaceRange(runeStart, ts.ed.Cursor, comp.Candidates[0])
		return
	}

	if lcp := longestCommonPrefix(comp.Candidates); len(lcp) > len(word) {
		ts.ed.ReplaceRange(runeStart, ts.ed.Cursor, lcp)
		return
	}

	// no progress to make: show the choices
	ts.listBelow(comp.Candidates)
}

// listBelow prints candidates in columns under the prompt, then editing
// continues on a fresh prompt line (bash behavior).
func (ts *TermSession) listBelow(items []string) {
	const maxShown = 100
	shown := items
	if len(shown) > maxShown {
		shown = shown[:maxShown]
	}

	colWidth := 0
	for _, it := range shown {
		if len(it) > colWidth {
			colWidth = len(it)
		}
	}
	colWidth += 2
	cols := ts.width / colWidth
	if cols < 1 {
		cols = 1
	}

	ts.out.WriteString("\r\n")
	for i, it := range shown {
		fmt.Fprintf(ts.out, "%-*s", colWidth, strings.TrimRight(it, " "))
		if (i+1)%cols == 0 {
			ts.out.WriteString("\r\n")
		}
	}
	if len(shown)%cols != 0 {
		ts.out.WriteString("\r\n")
	}
	if len(items) > maxShown {
		fmt.Fprintf(ts.out, "…and %d more\r\n", len(items)-maxShown)
	}
	ts.out.Flush()
}

// --- the grab picker (Ctrl+G) -------------------------------------------

const pickerRows = 8

// grabFromScreen opens the fuzzy picker over tokens captured from previous
// command output and inserts the chosen one at the cursor.
func (ts *TermSession) grabFromScreen() {
	tokens := ts.sb.Tokens(500)
	if len(tokens) == 0 {
		ts.bell()
		return
	}

	pk := NewPicker(tokens)
	defer func() {
		// wipe the overlay; the caller's render() repaints the prompt line
		ts.out.WriteString("\r\x1b[J")
		ts.out.Flush()
	}()

	for {
		ts.renderPicker(pk)

		key, err := decodeKey(ts.src)
		if err != nil {
			return
		}

		switch {
		case key.Kind == KeyEnter, key.Kind == KeyTab:
			if sel, ok := pk.Selection(); ok {
				ts.ed.InsertString(sel)
			}
			return

		case key.Kind == KeyEsc,
			key.Kind == KeyCtrl && (key.Rune == 'g' || key.Rune == 'c'):
			return

		case key.Kind == KeyUp, key.Kind == KeyCtrl && key.Rune == 'p':
			pk.Up()
		case key.Kind == KeyDown, key.Kind == KeyCtrl && key.Rune == 'n':
			pk.Down()

		case key.Kind == KeyBackspace:
			pk.Backspace()

		case key.Kind == KeyRune:
			pk.Input(key.Rune)
		}
	}
}

func (ts *TermSession) renderPicker(pk *Picker) {
	filtered := pk.Filtered()

	// window the list so the selection stays visible
	start := 0
	if pk.Sel >= pickerRows {
		start = pk.Sel - pickerRows + 1
	}
	end := start + pickerRows
	if end > len(filtered) {
		end = len(filtered)
	}

	// prompt line, then the overlay
	fmt.Fprintf(ts.out, "\r%s%s\x1b[K", ts.prompt, displayable(ts.ed.Buf))
	fmt.Fprintf(ts.out, "\r\ngrab> %s\x1b[K", string(pk.Query))

	rows := 1 // the grab> header
	for i := start; i < end; i++ {
		item := filtered[i]
		if len(item) > ts.width-4 && ts.width > 4 {
			item = item[:ts.width-4]
		}
		if i == pk.Sel {
			fmt.Fprintf(ts.out, "\r\n\x1b[7m> %s\x1b[0m\x1b[K", item)
		} else {
			fmt.Fprintf(ts.out, "\r\n  %s\x1b[K", item)
		}
		rows++
	}
	ts.out.WriteString("\x1b[J") // clear leftovers from a longer previous frame

	// return the cursor to the prompt line
	fmt.Fprintf(ts.out, "\x1b[%dA\r", rows)
	col := ts.promptW + ts.ed.Cursor
	if col > 0 {
		fmt.Fprintf(ts.out, "\x1b[%dC", col)
	}
	ts.out.Flush()
}

// --- interactive main loop ----------------------------------------------

// promptString expands the PS1/PS2 shell variable, re-evaluated every prompt
// so \w and \t stay current.
func (ts *TermSession) promptString(varName, fallback string) string {
	v, ok := ts.shell.getVar(varName)
	if !ok {
		return fallback
	}
	return ts.shell.expandPrompt(v)
}

func isIncomplete(err error) bool {
	return errors.Is(err, ErrUnclosedSingleQuote) || errors.Is(err, ErrUnclosedDoubleQuote)
}

func runInteractive(shell *Cshell) {
	ts, err := NewTermSession(shell)
	if err != nil {
		runBatch(shell)
		return
	}

	// Handle (not ignore) SIGINT: handled signals revert to default in child
	// processes, so Ctrl+C kills the foreground command while the shell,
	// which shares the terminal's process group, just drains the signal.
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt)
	go func() {
		for range sigc {
		}
	}()

	shell.RunScript(defaultRCPath(), IOStreams{In: os.Stdin, Out: os.Stdout, Err: os.Stderr})

	for {
		// re-assert at every prompt so the title returns to "cshell" after a
		// full-screen program (man, vim, ...) set its own
		setTerminalTitle(os.Stdout, "cshell")
		line, err := ts.ReadLine(ts.promptString("PS1", "$ "))
		if errors.Is(err, errAborted) {
			shell.LastStatus = 130
			continue
		}
		if err != nil {
			os.Exit(shell.LastStatus)
		}

		perr := shell.processInput(line)
		// unclosed quote: keep reading continuation lines (PS2), the newline
		// becomes part of the quoted string
		for isIncomplete(perr) {
			more, rerr := ts.ReadLine(ts.promptString("PS2", "> "))
			if rerr != nil {
				shell.AST = nil
				perr = nil
				break
			}
			line = line + "\n" + more
			perr = shell.processInput(line)
		}

		ts.hist.Add(line)

		if perr != nil {
			fmt.Fprintln(os.Stderr, "cshell: "+perr.Error())
			shell.LastStatus = 1
			continue
		}
		ts.execute()
	}
}
