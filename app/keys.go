package main

import (
	"io"
	"time"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

type KeyKind int

const (
	KeyRune KeyKind = iota
	KeyCtrl         // Rune holds the letter: Ctrl+A → 'a'
	KeyAlt          // Rune holds the key pressed with Alt/Meta
	KeyEnter
	KeyTab
	KeyBackspace
	KeyEsc
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyHome
	KeyEnd
	KeyDelete
	KeyUnknown // unrecognized escape sequence, safe to ignore
)

type Key struct {
	Kind KeyKind
	Rune rune
}

// keySource abstracts terminal input so the decoder can be tested with
// scripted bytes. pending reports whether more bytes are immediately
// available — that is how a lone Esc press is told apart from an escape
// sequence, which always arrives as a burst.
type keySource interface {
	ReadByte() (byte, error)
	pending() bool
}

// decodeKey reads one logical key from raw terminal input.
func decodeKey(src keySource) (Key, error) {
	b, err := src.ReadByte()
	if err != nil {
		return Key{}, err
	}

	switch {
	case b == '\r' || b == '\n':
		return Key{Kind: KeyEnter}, nil
	case b == '\t':
		return Key{Kind: KeyTab}, nil
	case b == 0x7f || b == 0x08:
		return Key{Kind: KeyBackspace}, nil
	case b == 0x1b:
		return decodeEscape(src)
	case b < 0x20:
		return Key{Kind: KeyCtrl, Rune: rune('a' + b - 1)}, nil
	case b < utf8.RuneSelf:
		return Key{Kind: KeyRune, Rune: rune(b)}, nil
	}

	// multi-byte UTF-8: collect continuation bytes until a rune decodes
	buf := []byte{b}
	for !utf8.FullRune(buf) && len(buf) < utf8.UTFMax {
		nb, err := src.ReadByte()
		if err != nil {
			return Key{}, err
		}
		buf = append(buf, nb)
	}
	r, _ := utf8.DecodeRune(buf)
	return Key{Kind: KeyRune, Rune: r}, nil
}

func decodeEscape(src keySource) (Key, error) {
	if !src.pending() {
		return Key{Kind: KeyEsc}, nil
	}
	b, err := src.ReadByte()
	if err != nil {
		return Key{}, err
	}

	if b != '[' && b != 'O' {
		// ESC+<key> is how terminals send Alt/Meta chords
		return Key{Kind: KeyAlt, Rune: rune(b)}, nil
	}

	// CSI sequence: parameters (digits/;) followed by a final byte
	var params []byte
	for {
		nb, err := src.ReadByte()
		if err != nil {
			return Key{}, err
		}
		if nb >= 0x40 && nb <= 0x7e {
			return csiKey(nb, string(params)), nil
		}
		params = append(params, nb)
		if len(params) > 16 {
			return Key{Kind: KeyUnknown}, nil
		}
	}
}

func csiKey(final byte, params string) Key {
	switch final {
	case 'A':
		return Key{Kind: KeyUp}
	case 'B':
		return Key{Kind: KeyDown}
	case 'C':
		return Key{Kind: KeyRight}
	case 'D':
		return Key{Kind: KeyLeft}
	case 'H':
		return Key{Kind: KeyHome}
	case 'F':
		return Key{Kind: KeyEnd}
	case '~':
		switch params {
		case "1", "7":
			return Key{Kind: KeyHome}
		case "3":
			return Key{Kind: KeyDelete}
		case "4", "8":
			return Key{Kind: KeyEnd}
		}
	}
	return Key{Kind: KeyUnknown}
}

// stdinPump is the single owner of the real terminal's input. Exactly one
// goroutine reads the fd forever and publishes chunks; consumers alternate
// between the line editor (at the prompt) and the pty forwarder (while a
// command runs). One reader means a keystroke can never be stolen by the
// wrong side — whatever is not consumed now waits in the channel for the
// next consumer.
type stdinPump struct {
	ch chan []byte
}

func newStdinPump(fd int) *stdinPump {
	p := &stdinPump{ch: make(chan []byte, 64)}
	go func() {
		for {
			buf := make([]byte, 4096)
			n, err := unix.Read(fd, buf)
			if err == unix.EINTR {
				continue
			}
			if n > 0 {
				p.ch <- buf[:n]
			}
			if err != nil || n == 0 {
				close(p.ch)
				return
			}
		}
	}()
	return p
}

// termSource feeds the key decoder from the pump.
type termSource struct {
	pump *stdinPump
	buf  []byte
}

func newTermSource(pump *stdinPump) *termSource {
	return &termSource{pump: pump}
}

func (t *termSource) ReadByte() (byte, error) {
	for len(t.buf) == 0 {
		chunk, ok := <-t.pump.ch
		if !ok {
			return 0, io.EOF
		}
		t.buf = append(t.buf, chunk...)
	}
	b := t.buf[0]
	t.buf = t.buf[1:]
	return b, nil
}

// pending waits briefly for a follow-up byte; escape sequences arrive
// together, a human pressing Esc does not type another key within 25ms.
func (t *termSource) pending() bool {
	if len(t.buf) > 0 {
		return true
	}
	select {
	case chunk, ok := <-t.pump.ch:
		if !ok {
			return false
		}
		t.buf = append(t.buf, chunk...)
		return len(t.buf) > 0
	case <-time.After(25 * time.Millisecond):
		return false
	}
}
