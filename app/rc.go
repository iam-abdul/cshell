package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// defaultRCPath is ~/.cshrc, overridable with $CSHRC.
func defaultRCPath() string {
	if p := os.Getenv("CSHRC"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cshrc")
}

// RunScript executes a file line by line with the shell's normal pipeline
// (lex → parse → exec). Unclosed quotes continue onto the next line, like
// they do at the PS2 prompt. Errors are reported but never stop the script
// or the shell — a broken rc file must not lock the user out.
func (cs *Cshell) RunScript(path string, s IOStreams) {
	data, err := os.ReadFile(path)
	if err != nil {
		return // missing rc file is a normal first run
	}

	report := func(err error) {
		fmt.Fprintf(s.Err, "cshell: %s: %s\n", filepath.Base(path), err.Error())
	}

	var acc string
	for _, line := range strings.Split(string(data), "\n") {
		if acc != "" {
			acc += "\n" + line
		} else {
			acc = line
		}

		perr := cs.processInput(acc)
		if isIncomplete(perr) {
			continue // quote still open: absorb the next line
		}
		acc = ""

		if perr != nil {
			report(perr)
			continue
		}
		if cs.AST != nil {
			cs.LastStatus = cs.execNode(cs.AST, s)
		}
	}

	if acc != "" {
		report(fmt.Errorf("unexpected end of file (unclosed quote)"))
	}
}
