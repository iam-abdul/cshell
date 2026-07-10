package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// --- shell variables ------------------------------------------------------

// splitAssignment recognizes NAME=value words. Only leading words of a
// command are treated as assignments, mirroring POSIX.
func splitAssignment(w string) (name, value string, ok bool) {
	i := strings.IndexByte(w, '=')
	if i <= 0 || !validVarName(w[:i]) {
		return "", "", false
	}
	return w[:i], w[i+1:], true
}

func validVarName(s string) bool {
	for i, r := range s {
		switch {
		case r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
		case r >= '0' && r <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return len(s) > 0
}

// getVar looks up a shell variable, falling back to the environment.
func (cs *Cshell) getVar(name string) (string, bool) {
	if v, ok := cs.Vars[name]; ok {
		return v, true
	}
	return os.LookupEnv(name)
}

// setVar stores a shell variable. If the name already lives in the
// environment (like PATH), the environment is updated too, so lookups and
// child processes see the new value — bash behaves the same way for
// already-exported variables.
func (cs *Cshell) setVar(name, value string) {
	cs.Vars[name] = value
	if _, exported := os.LookupEnv(name); exported {
		os.Setenv(name, value)
	}
}

// --- PS1/PS2 expansion ----------------------------------------------------

// expandPrompt renders bash-style prompt escapes:
//
//	\u user   \h short host   \H full host   \w cwd (~)   \W cwd basename
//	\$ $ or # for root   \n newline   \t HH:MM:SS   \e escape   \\ backslash
//
// \[ and \] are accepted and dropped: promptWidth already skips ANSI
// sequences, so the readline-style markers are unnecessary.
func (cs *Cshell) expandPrompt(format string) string {
	var b strings.Builder
	runes := []rune(format)

	for i := 0; i < len(runes); i++ {
		if runes[i] != '\\' || i+1 >= len(runes) {
			b.WriteRune(runes[i])
			continue
		}
		i++
		switch runes[i] {
		case 'u':
			b.WriteString(os.Getenv("USER"))
		case 'h':
			host, _ := os.Hostname()
			host, _, _ = strings.Cut(host, ".")
			b.WriteString(host)
		case 'H':
			host, _ := os.Hostname()
			b.WriteString(host)
		case 'w':
			b.WriteString(promptCwd(false))
		case 'W':
			b.WriteString(promptCwd(true))
		case '$':
			if os.Geteuid() == 0 {
				b.WriteByte('#')
			} else {
				b.WriteByte('$')
			}
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteString(time.Now().Format("15:04:05"))
		case 'e':
			b.WriteByte(0x1b)
		case '\\':
			b.WriteByte('\\')
		case '[', ']':
			// non-printing markers: nothing to emit
		default:
			// unknown escape stays literal, like bash
			b.WriteByte('\\')
			b.WriteRune(runes[i])
		}
	}
	return b.String()
}

// promptCwd is the working directory for \w (home shown as ~) or its
// basename for \W.
func promptCwd(baseOnly bool) string {
	dir, err := os.Getwd()
	if err != nil {
		return "?"
	}
	home, herr := os.UserHomeDir()
	if herr == nil && home != "" {
		if dir == home {
			return "~"
		}
		if strings.HasPrefix(dir, home+string(os.PathSeparator)) {
			dir = "~" + dir[len(home):]
		}
	}
	if baseOnly {
		return filepath.Base(dir)
	}
	return dir
}

// promptWidth is the number of terminal columns the prompt occupies: ANSI
// escape sequences (colors etc.) take no space. The renderer needs this to
// place the cursor when PS1 is colored.
func promptWidth(s string) int {
	width := 0
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] != 0x1b {
			width++
			continue
		}
		i++
		if i >= len(runes) {
			break
		}
		switch runes[i] {
		case '[': // CSI: parameters then a final byte in 0x40..0x7e
			for i++; i < len(runes); i++ {
				if runes[i] >= 0x40 && runes[i] <= 0x7e {
					break
				}
			}
		case ']': // OSC: until BEL or ESC \
			for i++; i < len(runes); i++ {
				if runes[i] == 0x07 {
					break
				}
				if runes[i] == 0x1b && i+1 < len(runes) && runes[i+1] == '\\' {
					i++
					break
				}
			}
		default:
			// two-character sequence like ESC 7: the char is consumed
		}
	}
	return width
}
