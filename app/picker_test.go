package main

import (
	"reflect"
	"testing"
)

func TestPicker_EmptyQueryShowsAll(t *testing.T) {
	pk := NewPicker([]string{"alpha", "beta", "gamma"})
	if got := pk.Filtered(); !reflect.DeepEqual(got, []string{"alpha", "beta", "gamma"}) {
		t.Errorf("got %v", got)
	}
}

func TestPicker_FilterNarrowsAndRanksPrefixFirst(t *testing.T) {
	pk := NewPicker([]string{"notes.txt", "anote", "other"})
	pk.Input('n')
	pk.Input('o')

	// "notes.txt" starts with "no", "anote" merely contains it
	want := []string{"notes.txt", "anote"}
	if got := pk.Filtered(); !reflect.DeepEqual(got, want) {
		t.Errorf("expected %v, got %v", want, got)
	}
}

func TestPicker_CaseInsensitive(t *testing.T) {
	pk := NewPicker([]string{"README.md", "readme.txt"})
	for _, r := range "readme" {
		pk.Input(r)
	}
	if got := pk.Filtered(); len(got) != 2 {
		t.Errorf("expected both files, got %v", got)
	}
}

func TestPicker_BackspaceWidensAgain(t *testing.T) {
	pk := NewPicker([]string{"aa", "ab", "bb"})
	pk.Input('a')
	pk.Input('a')
	if got := pk.Filtered(); len(got) != 1 {
		t.Fatalf("expected 1 match, got %v", got)
	}
	pk.Backspace()
	if got := pk.Filtered(); len(got) != 2 {
		t.Errorf("expected 2 matches after backspace, got %v", got)
	}
}

func TestPicker_SelectionMovement(t *testing.T) {
	pk := NewPicker([]string{"one", "two", "three"})

	if sel, ok := pk.Selection(); !ok || sel != "one" {
		t.Errorf("initial selection: %q %v", sel, ok)
	}

	pk.Down()
	if sel, _ := pk.Selection(); sel != "two" {
		t.Errorf("after down: %q", sel)
	}

	pk.Down()
	pk.Down() // clamped at the end
	if sel, _ := pk.Selection(); sel != "three" {
		t.Errorf("after down past end: %q", sel)
	}

	pk.Up()
	if sel, _ := pk.Selection(); sel != "two" {
		t.Errorf("after up: %q", sel)
	}
}

func TestPicker_SelectionResetsOnInput(t *testing.T) {
	pk := NewPicker([]string{"aaa", "aab", "bbb"})
	pk.Down()
	pk.Input('a')
	if pk.Sel != 0 {
		t.Errorf("selection should reset to 0 on filter change, got %d", pk.Sel)
	}
}

func TestPicker_NoMatches(t *testing.T) {
	pk := NewPicker([]string{"alpha"})
	pk.Input('z')
	if _, ok := pk.Selection(); ok {
		t.Error("expected no selection with no matches")
	}
}
