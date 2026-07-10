package main

import "testing"

func TestEditor_InsertAndMove(t *testing.T) {
	e := &Editor{}
	e.InsertString("hello")
	if e.String() != "hello" || e.Cursor != 5 {
		t.Fatalf("got %q cursor %d", e.String(), e.Cursor)
	}

	e.MoveHome()
	e.Insert('>')
	if e.String() != ">hello" || e.Cursor != 1 {
		t.Errorf("insert at home: got %q cursor %d", e.String(), e.Cursor)
	}

	e.MoveEnd()
	if e.Cursor != 6 {
		t.Errorf("expected cursor 6, got %d", e.Cursor)
	}

	e.MoveLeft()
	e.MoveLeft()
	e.Insert('!')
	if e.String() != ">hel!lo" {
		t.Errorf("insert mid-line: got %q", e.String())
	}
}

func TestEditor_BackspaceDelete(t *testing.T) {
	e := &Editor{}
	e.InsertString("abc")

	e.Backspace()
	if e.String() != "ab" || e.Cursor != 2 {
		t.Errorf("backspace: got %q cursor %d", e.String(), e.Cursor)
	}

	e.MoveHome()
	e.Delete()
	if e.String() != "b" || e.Cursor != 0 {
		t.Errorf("delete: got %q cursor %d", e.String(), e.Cursor)
	}

	// no-ops at the edges
	e.Backspace()
	e.MoveEnd()
	e.Delete()
	if e.String() != "b" {
		t.Errorf("edge ops changed buffer: %q", e.String())
	}
}

func TestEditor_WordMovement(t *testing.T) {
	e := &Editor{}
	e.InsertString("echo hello world")

	e.WordLeft()
	if e.Cursor != 11 { // start of "world"
		t.Errorf("word left: cursor %d", e.Cursor)
	}
	e.WordLeft()
	if e.Cursor != 5 { // start of "hello"
		t.Errorf("word left again: cursor %d", e.Cursor)
	}
	e.WordRight()
	if e.Cursor != 10 { // end of "hello"
		t.Errorf("word right: cursor %d", e.Cursor)
	}
}

func TestEditor_DeleteWordBack(t *testing.T) {
	e := &Editor{}
	e.InsertString("echo hello world")

	e.DeleteWordBack()
	if e.String() != "echo hello " {
		t.Errorf("got %q", e.String())
	}
	e.DeleteWordBack()
	if e.String() != "echo " {
		t.Errorf("got %q", e.String())
	}
}

func TestEditor_KillAndYank(t *testing.T) {
	e := &Editor{}
	e.InsertString("echo hello")
	e.MoveHome()
	e.WordRight() // cursor after "echo"

	e.KillToEnd()
	if e.String() != "echo" {
		t.Errorf("kill to end: got %q", e.String())
	}

	e.Yank()
	if e.String() != "echo hello" {
		t.Errorf("yank: got %q", e.String())
	}

	e.MoveEnd()
	e.KillToStart()
	if e.String() != "" || e.Cursor != 0 {
		t.Errorf("kill to start: got %q cursor %d", e.String(), e.Cursor)
	}
}

func TestEditor_ReplaceRange(t *testing.T) {
	e := &Editor{}
	e.InsertString("cat fil")

	e.ReplaceRange(4, 7, "file.txt ")
	if e.String() != "cat file.txt " {
		t.Errorf("got %q", e.String())
	}
	if e.Cursor != 13 {
		t.Errorf("cursor should be after replacement, got %d", e.Cursor)
	}

	// replace in the middle keeps the tail
	e.Set("aXXd")
	e.ReplaceRange(1, 3, "bc")
	if e.String() != "abcd" {
		t.Errorf("got %q", e.String())
	}
	if e.Cursor != 3 {
		t.Errorf("cursor after inserted text, got %d", e.Cursor)
	}
}

func TestEditor_SetAndReset(t *testing.T) {
	e := &Editor{}
	e.InsertString("something")
	e.Set("recalled command")
	if e.String() != "recalled command" || e.Cursor != 16 {
		t.Errorf("set: got %q cursor %d", e.String(), e.Cursor)
	}
	e.Reset()
	if e.String() != "" || e.Cursor != 0 {
		t.Errorf("reset: got %q cursor %d", e.String(), e.Cursor)
	}
}
