package main

import (
	"strings"
	"sync"
)

const scrollbackMaxLines = 5000

// Scrollback captures everything printed to the terminal as plain text so it
// can be grabbed later. It is an io.Writer fed by the PTY mirror; ANSI
// escape sequences are stripped and carriage-return overwrites collapse to
// the final state of the line (progress bars keep only their last frame).
type Scrollback struct {
	mu    sync.Mutex
	lines []string
	cur   []rune
	col   int // cursor column in cur: \r rewinds it, printables overwrite

	// escape-stripping state
	state    sbState
	oscEsc   bool // inside an OSC sequence, saw ESC (terminator is ESC \)
	utf8Left int
	utf8Buf  []byte
}

type sbState int

const (
	sbNormal sbState = iota
	sbEsc            // saw ESC
	sbCSI            // inside ESC [ ... sequence
	sbOSC            // inside ESC ] ... sequence
)

func NewScrollback() *Scrollback {
	return &Scrollback{}
}

func (sb *Scrollback) Write(p []byte) (int, error) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	for _, b := range p {
		sb.consume(b)
	}
	return len(p), nil
}

func (sb *Scrollback) consume(b byte) {
	switch sb.state {
	case sbEsc:
		switch b {
		case '[':
			sb.state = sbCSI
		case ']':
			sb.state = sbOSC
		default:
			// two-byte sequence like ESC 7; drop it
			sb.state = sbNormal
		}
		return
	case sbCSI:
		if b >= 0x40 && b <= 0x7e {
			sb.state = sbNormal
		}
		return
	case sbOSC:
		if b == 0x07 { // BEL terminator
			sb.state = sbNormal
		} else if b == 0x1b {
			sb.oscEsc = true
		} else if sb.oscEsc {
			sb.oscEsc = false
			sb.state = sbNormal // ESC \ terminator
		}
		return
	}

	switch {
	case b == 0x1b:
		sb.state = sbEsc
	case b == '\n':
		sb.pushLine()
	case b == '\r':
		// cursor to column 0; following characters overwrite in place, which
		// collapses progress-bar redraws to their final frame
		sb.col = 0
	case b == 0x08: // backspace
		if sb.col > 0 {
			sb.col--
		}
	case b == '\t':
		sb.putRune(' ')
	case b < 0x20:
		// other control bytes: drop
	case b < 0x80:
		sb.putRune(rune(b))
	default:
		// multi-byte UTF-8
		sb.utf8Buf = append(sb.utf8Buf, b)
		if sb.utf8Left == 0 {
			switch {
			case b&0xe0 == 0xc0:
				sb.utf8Left = 1
			case b&0xf0 == 0xe0:
				sb.utf8Left = 2
			case b&0xf8 == 0xf0:
				sb.utf8Left = 3
			default:
				sb.utf8Buf = sb.utf8Buf[:0] // stray continuation byte
			}
		} else {
			sb.utf8Left--
			if sb.utf8Left == 0 {
				for _, r := range string(sb.utf8Buf) {
					sb.putRune(r)
				}
				sb.utf8Buf = sb.utf8Buf[:0]
			}
		}
	}
}

func (sb *Scrollback) putRune(r rune) {
	if sb.col < len(sb.cur) {
		sb.cur[sb.col] = r
	} else {
		sb.cur = append(sb.cur, r)
	}
	sb.col++
}

func (sb *Scrollback) pushLine() {
	sb.lines = append(sb.lines, string(sb.cur))
	sb.cur = sb.cur[:0]
	sb.col = 0
	if len(sb.lines) > scrollbackMaxLines {
		sb.lines = sb.lines[len(sb.lines)-scrollbackMaxLines:]
	}
}

// Lines returns captured lines, oldest first, including the partial current
// line if any.
func (sb *Scrollback) Lines() []string {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	out := append([]string{}, sb.lines...)
	if len(sb.cur) > 0 {
		out = append(out, string(sb.cur))
	}
	return out
}

// Tokens returns unique whitespace-separated words from the scrollback,
// newest first — the candidate list for the grab picker.
func (sb *Scrollback) Tokens(limit int) []string {
	lines := sb.Lines()
	seen := map[string]bool{}
	var tokens []string
	for i := len(lines) - 1; i >= 0; i-- {
		for _, tok := range strings.Fields(lines[i]) {
			if seen[tok] {
				continue
			}
			seen[tok] = true
			tokens = append(tokens, tok)
			if limit > 0 && len(tokens) >= limit {
				return tokens
			}
		}
	}
	return tokens
}
