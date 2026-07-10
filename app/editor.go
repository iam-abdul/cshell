package main

import "unicode"

// Editor is the line editor's state machine: a rune buffer plus a cursor.
// It knows nothing about terminals, which keeps every operation unit-testable;
// TermSession renders it.
type Editor struct {
	Buf    []rune
	Cursor int
	kill   []rune // most recently killed text, for Ctrl+Y
}

func (e *Editor) Reset() {
	e.Buf = e.Buf[:0]
	e.Cursor = 0
}

func (e *Editor) String() string {
	return string(e.Buf)
}

// Set replaces the whole line, cursor at the end (history recall).
func (e *Editor) Set(s string) {
	e.Buf = append(e.Buf[:0], []rune(s)...)
	e.Cursor = len(e.Buf)
}

func (e *Editor) Insert(r rune) {
	e.Buf = append(e.Buf, 0)
	copy(e.Buf[e.Cursor+1:], e.Buf[e.Cursor:])
	e.Buf[e.Cursor] = r
	e.Cursor++
}

func (e *Editor) InsertString(s string) {
	for _, r := range s {
		e.Insert(r)
	}
}

func (e *Editor) Backspace() {
	if e.Cursor == 0 {
		return
	}
	e.Buf = append(e.Buf[:e.Cursor-1], e.Buf[e.Cursor:]...)
	e.Cursor--
}

func (e *Editor) Delete() {
	if e.Cursor >= len(e.Buf) {
		return
	}
	e.Buf = append(e.Buf[:e.Cursor], e.Buf[e.Cursor+1:]...)
}

func (e *Editor) MoveLeft() {
	if e.Cursor > 0 {
		e.Cursor--
	}
}

func (e *Editor) MoveRight() {
	if e.Cursor < len(e.Buf) {
		e.Cursor++
	}
}

func (e *Editor) MoveHome() { e.Cursor = 0 }
func (e *Editor) MoveEnd()  { e.Cursor = len(e.Buf) }

// wordLeftIndex finds the start of the word left of the cursor.
func (e *Editor) wordLeftIndex() int {
	i := e.Cursor
	for i > 0 && unicode.IsSpace(e.Buf[i-1]) {
		i--
	}
	for i > 0 && !unicode.IsSpace(e.Buf[i-1]) {
		i--
	}
	return i
}

// wordRightIndex finds the end of the word right of the cursor.
func (e *Editor) wordRightIndex() int {
	i := e.Cursor
	for i < len(e.Buf) && unicode.IsSpace(e.Buf[i]) {
		i++
	}
	for i < len(e.Buf) && !unicode.IsSpace(e.Buf[i]) {
		i++
	}
	return i
}

func (e *Editor) WordLeft()  { e.Cursor = e.wordLeftIndex() }
func (e *Editor) WordRight() { e.Cursor = e.wordRightIndex() }

// DeleteWordBack removes from the start of the previous word to the cursor
// (Ctrl+W).
func (e *Editor) DeleteWordBack() {
	start := e.wordLeftIndex()
	if start == e.Cursor {
		return
	}
	e.kill = append(e.kill[:0], e.Buf[start:e.Cursor]...)
	e.Buf = append(e.Buf[:start], e.Buf[e.Cursor:]...)
	e.Cursor = start
}

// DeleteWordForward removes from the cursor to the end of the next word
// (Alt+D).
func (e *Editor) DeleteWordForward() {
	end := e.wordRightIndex()
	if end == e.Cursor {
		return
	}
	e.kill = append(e.kill[:0], e.Buf[e.Cursor:end]...)
	e.Buf = append(e.Buf[:e.Cursor], e.Buf[end:]...)
}

// KillToEnd removes from the cursor to end of line (Ctrl+K).
func (e *Editor) KillToEnd() {
	if e.Cursor >= len(e.Buf) {
		return
	}
	e.kill = append(e.kill[:0], e.Buf[e.Cursor:]...)
	e.Buf = e.Buf[:e.Cursor]
}

// KillToStart removes from start of line to the cursor (Ctrl+U).
func (e *Editor) KillToStart() {
	if e.Cursor == 0 {
		return
	}
	e.kill = append(e.kill[:0], e.Buf[:e.Cursor]...)
	e.Buf = append(e.Buf[:0], e.Buf[e.Cursor:]...)
	e.Cursor = 0
}

// Yank re-inserts the last killed text at the cursor (Ctrl+Y).
func (e *Editor) Yank() {
	for _, r := range e.kill {
		e.Insert(r)
	}
}

// ReplaceRange swaps [start,end) with s and puts the cursor after it
// (completion, grab insertion).
func (e *Editor) ReplaceRange(start, end int, s string) {
	if start < 0 || end > len(e.Buf) || start > end {
		return
	}
	tail := append([]rune{}, e.Buf[end:]...)
	e.Buf = append(e.Buf[:start], []rune(s)...)
	e.Cursor = len(e.Buf)
	e.Buf = append(e.Buf, tail...)
}
