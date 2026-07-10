package main

import (
	"io"
	"testing"
)

// fakeSource feeds scripted bytes to the decoder.
type fakeSource struct {
	data []byte
	pos  int
}

func (f *fakeSource) ReadByte() (byte, error) {
	if f.pos >= len(f.data) {
		return 0, io.EOF
	}
	b := f.data[f.pos]
	f.pos++
	return b, nil
}

func (f *fakeSource) pending() bool {
	return f.pos < len(f.data)
}

func TestDecodeKey(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected Key
	}{
		{"plain rune", []byte("a"), Key{Kind: KeyRune, Rune: 'a'}},
		{"utf8 rune", []byte("é"), Key{Kind: KeyRune, Rune: 'é'}},
		{"enter CR", []byte{'\r'}, Key{Kind: KeyEnter}},
		{"enter LF", []byte{'\n'}, Key{Kind: KeyEnter}},
		{"tab", []byte{'\t'}, Key{Kind: KeyTab}},
		{"backspace DEL", []byte{0x7f}, Key{Kind: KeyBackspace}},
		{"backspace BS", []byte{0x08}, Key{Kind: KeyBackspace}},
		{"ctrl-a", []byte{0x01}, Key{Kind: KeyCtrl, Rune: 'a'}},
		{"ctrl-r", []byte{0x12}, Key{Kind: KeyCtrl, Rune: 'r'}},
		{"ctrl-g", []byte{0x07}, Key{Kind: KeyCtrl, Rune: 'g'}},
		{"bare esc", []byte{0x1b}, Key{Kind: KeyEsc}},
		{"arrow up", []byte("\x1b[A"), Key{Kind: KeyUp}},
		{"arrow down", []byte("\x1b[B"), Key{Kind: KeyDown}},
		{"arrow right", []byte("\x1b[C"), Key{Kind: KeyRight}},
		{"arrow left", []byte("\x1b[D"), Key{Kind: KeyLeft}},
		{"home CSI H", []byte("\x1b[H"), Key{Kind: KeyHome}},
		{"end CSI F", []byte("\x1b[F"), Key{Kind: KeyEnd}},
		{"home O variant", []byte("\x1bOH"), Key{Kind: KeyHome}},
		{"delete", []byte("\x1b[3~"), Key{Kind: KeyDelete}},
		{"home tilde", []byte("\x1b[1~"), Key{Kind: KeyHome}},
		{"end tilde", []byte("\x1b[4~"), Key{Kind: KeyEnd}},
		{"alt-b", []byte{0x1b, 'b'}, Key{Kind: KeyAlt, Rune: 'b'}},
		{"alt-f", []byte{0x1b, 'f'}, Key{Kind: KeyAlt, Rune: 'f'}},
		{"unknown CSI", []byte("\x1b[99Z"), Key{Kind: KeyUnknown}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := decodeKey(&fakeSource{data: tt.input})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if key != tt.expected {
				t.Errorf("expected %+v, got %+v", tt.expected, key)
			}
		})
	}
}

func TestDecodeKey_SequentialKeys(t *testing.T) {
	src := &fakeSource{data: []byte("hi\r")}

	expected := []Key{
		{Kind: KeyRune, Rune: 'h'},
		{Kind: KeyRune, Rune: 'i'},
		{Kind: KeyEnter},
	}
	for i, want := range expected {
		key, err := decodeKey(src)
		if err != nil {
			t.Fatalf("key %d: %v", i, err)
		}
		if key != want {
			t.Errorf("key %d: expected %+v, got %+v", i, want, key)
		}
	}

	if _, err := decodeKey(src); err != io.EOF {
		t.Errorf("expected EOF at end, got %v", err)
	}
}
