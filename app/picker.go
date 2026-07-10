package main

import "strings"

// Picker is the grab overlay's state machine: a candidate list narrowed by a
// typed query with a movable selection. Pure state — TermSession renders it.
type Picker struct {
	all      []string
	Query    []rune
	Sel      int
	filtered []string
	dirty    bool
}

func NewPicker(candidates []string) *Picker {
	return &Picker{all: candidates, dirty: true}
}

// Filtered returns candidates matching the query, best (most recent) first.
// Matching is case-insensitive substring; tokens that start with the query
// rank before tokens that merely contain it.
func (p *Picker) Filtered() []string {
	if !p.dirty {
		return p.filtered
	}
	q := strings.ToLower(string(p.Query))
	var prefixed, contained []string
	for _, c := range p.all {
		lc := strings.ToLower(c)
		switch {
		case q == "" || strings.HasPrefix(lc, q):
			prefixed = append(prefixed, c)
		case strings.Contains(lc, q):
			contained = append(contained, c)
		}
	}
	p.filtered = append(prefixed, contained...)
	p.dirty = false
	if p.Sel >= len(p.filtered) {
		p.Sel = 0
	}
	return p.filtered
}

func (p *Picker) Input(r rune) {
	p.Query = append(p.Query, r)
	p.Sel = 0
	p.dirty = true
}

func (p *Picker) Backspace() {
	if len(p.Query) == 0 {
		return
	}
	p.Query = p.Query[:len(p.Query)-1]
	p.Sel = 0
	p.dirty = true
}

func (p *Picker) Up() {
	if p.Sel > 0 {
		p.Sel--
	}
}

func (p *Picker) Down() {
	if p.Sel < len(p.Filtered())-1 {
		p.Sel++
	}
}

// Selection returns the currently highlighted candidate.
func (p *Picker) Selection() (string, bool) {
	f := p.Filtered()
	if len(f) == 0 || p.Sel >= len(f) {
		return "", false
	}
	return f[p.Sel], true
}
