package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Completion is what Tab produced for the word being completed:
// Candidates are full replacements for line[WordStart:cursor].
type Completion struct {
	Candidates []string
	WordStart  int
}

// Complete computes completions at the cursor. The first word of a command
// completes from builtins and PATH executables; every other position (and
// anything containing a slash) completes as a file path.
func (cs *Cshell) Complete(line string, cursor int) Completion {
	wordStart := cursor
	for wordStart > 0 && line[wordStart-1] != ' ' && line[wordStart-1] != '\t' {
		wordStart--
	}
	word := line[wordStart:cursor]

	isFirstWord := strings.TrimSpace(line[:wordStart]) == ""

	var cands []string
	if isFirstWord && !strings.Contains(word, "/") {
		cands = cs.completeCommand(word)
	} else {
		cands = completePath(word)
	}
	sort.Strings(cands)
	return Completion{Candidates: cands, WordStart: wordStart}
}

// completeCommand matches builtins and executables on PATH. A trailing space
// is appended to each candidate since the command word is complete.
func (cs *Cshell) completeCommand(prefix string) []string {
	seen := map[string]bool{}

	for name := range cs.Commands {
		if strings.HasPrefix(name, prefix) {
			seen[name] = true
		}
	}

	for _, dir := range strings.Split(os.Getenv("PATH"), string(os.PathListSeparator)) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if !strings.HasPrefix(name, prefix) || seen[name] {
				continue
			}
			info, err := os.Stat(filepath.Join(dir, name))
			if err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
				continue
			}
			seen[name] = true
		}
	}

	cands := make([]string, 0, len(seen))
	for name := range seen {
		cands = append(cands, name+" ")
	}
	return cands
}

// completePath matches directory entries against the last path segment.
// Directories complete with a trailing / so Tab can keep descending; files
// complete with a trailing space. A leading ~ is expanded for the directory
// read but candidates keep the ~ form the user typed.
func completePath(word string) []string {
	// bare ~ can only sensibly become the home directory
	if word == "~" {
		return []string{"~/"}
	}

	dir, base := filepath.Split(word)
	searchDir := expandTilde(dir)
	if searchDir == "" {
		searchDir = "."
	}

	entries, err := os.ReadDir(searchDir)
	if err != nil {
		return nil
	}

	var cands []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, base) {
			continue
		}
		// hide dotfiles unless the user typed the dot
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(base, ".") {
			continue
		}
		if e.IsDir() {
			cands = append(cands, dir+name+"/")
		} else {
			cands = append(cands, dir+name+" ")
		}
	}
	return cands
}

// longestCommonPrefix of all candidates; used to extend an ambiguous word.
func longestCommonPrefix(cands []string) string {
	if len(cands) == 0 {
		return ""
	}
	prefix := cands[0]
	for _, c := range cands[1:] {
		for !strings.HasPrefix(c, prefix) {
			prefix = prefix[:len(prefix)-1]
			if prefix == "" {
				return ""
			}
		}
	}
	return prefix
}
