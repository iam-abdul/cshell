package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHistory_AddSkipsBlanksAndRepeats(t *testing.T) {
	h := &History{}

	h.Add("echo one")
	h.Add("echo one") // consecutive duplicate
	h.Add("   ")      // blank
	h.Add("")
	h.Add("echo two")
	h.Add("echo one") // not consecutive: kept

	want := []string{"echo one", "echo two", "echo one"}
	if len(h.Items) != len(want) {
		t.Fatalf("expected %d items, got %d: %v", len(want), len(h.Items), h.Items)
	}
	for i, w := range want {
		if h.Items[i] != w {
			t.Errorf("item %d: expected %q, got %q", i, w, h.Items[i])
		}
	}
}

func TestHistory_SearchBackward(t *testing.T) {
	h := &History{Items: []string{
		"echo hello",
		"ls -la",
		"echo world",
		"cat file.txt",
	}}

	if idx := h.SearchBackward("echo", len(h.Items)-1); idx != 2 {
		t.Errorf("expected newest echo at 2, got %d", idx)
	}
	// repeat search continues from before the previous match
	if idx := h.SearchBackward("echo", 1); idx != 0 {
		t.Errorf("expected older echo at 0, got %d", idx)
	}
	// case-insensitive
	if idx := h.SearchBackward("ECHO", len(h.Items)-1); idx != 2 {
		t.Errorf("expected case-insensitive match at 2, got %d", idx)
	}
	if idx := h.SearchBackward("nomatch", len(h.Items)-1); idx != -1 {
		t.Errorf("expected -1, got %d", idx)
	}
	if idx := h.SearchBackward("", len(h.Items)-1); idx != -1 {
		t.Errorf("empty query should not match, got %d", idx)
	}
}

func TestHistory_PersistAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history")

	h := &History{path: path}
	h.Add("echo persisted")
	h.Add("ls | wc -l")
	h.Add("echo 'multi\nline'") // newline must survive the round-trip

	h2 := LoadHistory(path)
	if len(h2.Items) != 3 {
		t.Fatalf("expected 3 items after reload, got %d: %v", len(h2.Items), h2.Items)
	}
	if h2.Items[0] != "echo persisted" {
		t.Errorf("item 0: got %q", h2.Items[0])
	}
	if h2.Items[2] != "echo 'multi\nline'" {
		t.Errorf("multi-line item corrupted: got %q", h2.Items[2])
	}
}

func TestHistory_LoadMissingFileIsEmpty(t *testing.T) {
	h := LoadHistory(filepath.Join(t.TempDir(), "does-not-exist"))
	if len(h.Items) != 0 {
		t.Errorf("expected empty history, got %v", h.Items)
	}
}

func TestHistory_EncodeDecodeRoundTrip(t *testing.T) {
	cases := []string{
		"plain",
		"has\nnewline",
		`has\backslash`,
		"both\\and\nnewline",
		`trailing\`,
	}
	for _, c := range cases {
		got := decodeHistoryLine(encodeHistoryLine(c))
		if got != c {
			t.Errorf("round trip of %q: got %q", c, got)
		}
	}
}

func TestHistory_FilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history")
	h := &History{path: path}
	h.Add("secret command")

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("history file should be 0600, got %v", info.Mode().Perm())
	}
}
