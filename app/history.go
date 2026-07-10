package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

const historyMax = 10000

// History holds past command lines, newest last, persisted one entry per
// line. Multi-line commands are stored with newlines encoded so the file
// stays line-oriented.
type History struct {
	Items []string
	path  string
}

// LoadHistory reads the history file if it exists; a missing file is a
// normal first run.
func LoadHistory(path string) *History {
	h := &History{path: path}
	f, err := os.Open(path)
	if err != nil {
		return h
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		h.Items = append(h.Items, decodeHistoryLine(line))
	}
	if len(h.Items) > historyMax {
		h.Items = h.Items[len(h.Items)-historyMax:]
	}
	return h
}

func defaultHistoryPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cshell_history")
}

// Add records a command line, skipping blanks and immediate repeats, and
// appends it to the history file.
func (h *History) Add(line string) {
	line = strings.TrimRight(line, "\n")
	if strings.TrimSpace(line) == "" {
		return
	}
	if len(h.Items) > 0 && h.Items[len(h.Items)-1] == line {
		return
	}
	h.Items = append(h.Items, line)
	if len(h.Items) > historyMax {
		h.Items = h.Items[len(h.Items)-historyMax:]
	}

	if h.path == "" {
		return
	}
	f, err := os.OpenFile(h.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(encodeHistoryLine(line) + "\n")
}

// SearchBackward returns the index of the newest item at or before `from`
// whose text contains query, or -1. An empty query matches nothing.
func (h *History) SearchBackward(query string, from int) int {
	if query == "" {
		return -1
	}
	if from >= len(h.Items) {
		from = len(h.Items) - 1
	}
	q := strings.ToLower(query)
	for i := from; i >= 0; i-- {
		if strings.Contains(strings.ToLower(h.Items[i]), q) {
			return i
		}
	}
	return -1
}

func encodeHistoryLine(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, "\n", `\n`)
}

func decodeHistoryLine(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				b.WriteByte('\n')
				i++
				continue
			case '\\':
				b.WriteByte('\\')
				i++
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
