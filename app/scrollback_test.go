package main

import (
	"reflect"
	"testing"
)

func TestScrollback_PlainLines(t *testing.T) {
	sb := NewScrollback()
	sb.Write([]byte("hello world\nsecond line\n"))

	want := []string{"hello world", "second line"}
	if !reflect.DeepEqual(sb.Lines(), want) {
		t.Errorf("expected %v, got %v", want, sb.Lines())
	}
}

func TestScrollback_PartialLineVisible(t *testing.T) {
	sb := NewScrollback()
	sb.Write([]byte("no newline yet"))

	want := []string{"no newline yet"}
	if !reflect.DeepEqual(sb.Lines(), want) {
		t.Errorf("expected %v, got %v", want, sb.Lines())
	}
}

func TestScrollback_StripsANSIColors(t *testing.T) {
	sb := NewScrollback()
	sb.Write([]byte("\x1b[31mred text\x1b[0m plain\n"))

	want := []string{"red text plain"}
	if !reflect.DeepEqual(sb.Lines(), want) {
		t.Errorf("expected %v, got %v", want, sb.Lines())
	}
}

func TestScrollback_StripsOSCTitleSequence(t *testing.T) {
	sb := NewScrollback()
	// OSC terminated by BEL
	sb.Write([]byte("\x1b]0;window title\x07visible\n"))

	want := []string{"visible"}
	if !reflect.DeepEqual(sb.Lines(), want) {
		t.Errorf("expected %v, got %v", want, sb.Lines())
	}
}

func TestScrollback_CRLFAndProgressBars(t *testing.T) {
	sb := NewScrollback()
	// pty output uses \r\n; progress bars redraw with bare \r
	sb.Write([]byte("progress 10%\rprogress 50%\rprogress done\r\n"))

	want := []string{"progress done"}
	if !reflect.DeepEqual(sb.Lines(), want) {
		t.Errorf("expected %v, got %v", want, sb.Lines())
	}
}

func TestScrollback_SplitAcrossWrites(t *testing.T) {
	sb := NewScrollback()
	// escape sequence and utf-8 rune split across Write calls
	sb.Write([]byte("col\x1b["))
	sb.Write([]byte("32mored\x1b[0m caf\xc3"))
	sb.Write([]byte("\xa9\n"))

	want := []string{"colored café"}
	if !reflect.DeepEqual(sb.Lines(), want) {
		t.Errorf("expected %v, got %v", want, sb.Lines())
	}
}

func TestScrollback_Tokens(t *testing.T) {
	sb := NewScrollback()
	sb.Write([]byte("first alpha beta\nsecond beta gamma\n"))

	// newest first, deduped
	want := []string{"second", "beta", "gamma", "first", "alpha"}
	if !reflect.DeepEqual(sb.Tokens(0), want) {
		t.Errorf("expected %v, got %v", want, sb.Tokens(0))
	}

	// limit caps the result
	if got := sb.Tokens(2); len(got) != 2 {
		t.Errorf("expected 2 tokens, got %v", got)
	}
}

func TestScrollback_LineCap(t *testing.T) {
	sb := NewScrollback()
	for range scrollbackMaxLines + 100 {
		sb.Write([]byte("line\n"))
	}
	if len(sb.Lines()) != scrollbackMaxLines {
		t.Errorf("expected cap at %d lines, got %d", scrollbackMaxLines, len(sb.Lines()))
	}
}
