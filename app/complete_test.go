package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestComplete_Builtins(t *testing.T) {
	cs := NewCshell()

	comp := cs.Complete("ech", 3)
	found := false
	for _, c := range comp.Candidates {
		if c == "echo " {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'echo ' among candidates, got %v", comp.Candidates)
	}
	if comp.WordStart != 0 {
		t.Errorf("expected word start 0, got %d", comp.WordStart)
	}
}

func TestComplete_CommandsAreUnique(t *testing.T) {
	cs := NewCshell()

	// echo is both a builtin and a PATH executable; it must appear once
	comp := cs.Complete("echo", 4)
	count := 0
	for _, c := range comp.Candidates {
		if c == "echo " {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly one 'echo ' candidate, got %d in %v", count, comp.Candidates)
	}
}

func TestComplete_Paths(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"alpha.txt", "alphabet.txt", "beta.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "alphadir"), 0755); err != nil {
		t.Fatal(err)
	}

	cs := NewCshell()
	line := "cat " + dir + "/alpha"
	comp := cs.Complete(line, len(line))

	if len(comp.Candidates) != 3 {
		t.Fatalf("expected 3 candidates, got %v", comp.Candidates)
	}
	joined := strings.Join(comp.Candidates, "|")
	if !strings.Contains(joined, "alpha.txt ") {
		t.Errorf("file should complete with trailing space: %v", comp.Candidates)
	}
	if !strings.Contains(joined, "alphadir/") {
		t.Errorf("directory should complete with trailing slash: %v", comp.Candidates)
	}
	if comp.WordStart != 4 {
		t.Errorf("expected word start 4, got %d", comp.WordStart)
	}
}

func TestComplete_SecondWordIsPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), nil, 0644); err != nil {
		t.Fatal(err)
	}

	cs := NewCshell()
	line := "cat " + dir + "/no"
	comp := cs.Complete(line, len(line))

	if len(comp.Candidates) != 1 || comp.Candidates[0] != dir+"/notes.md " {
		t.Errorf("expected single path candidate, got %v", comp.Candidates)
	}
}

func TestComplete_HidesDotfilesUnlessAsked(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{".hidden", "visible"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0644); err != nil {
			t.Fatal(err)
		}
	}

	cs := NewCshell()
	line := "cat " + dir + "/"
	comp := cs.Complete(line, len(line))
	for _, c := range comp.Candidates {
		if strings.Contains(c, ".hidden") {
			t.Errorf("dotfile offered without a dot prefix: %v", comp.Candidates)
		}
	}

	line = "cat " + dir + "/."
	comp = cs.Complete(line, len(line))
	found := false
	for _, c := range comp.Candidates {
		if strings.Contains(c, ".hidden") {
			found = true
		}
	}
	if !found {
		t.Errorf("dotfile not offered even with dot prefix: %v", comp.Candidates)
	}
}

func TestComplete_NoMatches(t *testing.T) {
	cs := NewCshell()
	line := "zzzznosuchcommandzzz"
	comp := cs.Complete(line, len(line))
	if len(comp.Candidates) != 0 {
		t.Errorf("expected no candidates, got %v", comp.Candidates)
	}
}

func TestLongestCommonPrefix(t *testing.T) {
	tests := []struct {
		cands    []string
		expected string
	}{
		{[]string{"alpha.txt ", "alphabet.txt ", "alphadir/"}, "alpha"},
		{[]string{"same ", "same "}, "same "},
		{[]string{"abc", "xyz"}, ""},
		{[]string{"one "}, "one "},
		{nil, ""},
	}
	for _, tt := range tests {
		if got := longestCommonPrefix(tt.cands); got != tt.expected {
			t.Errorf("lcp(%v): expected %q, got %q", tt.cands, tt.expected, got)
		}
	}
}
