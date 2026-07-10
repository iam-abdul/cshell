package main

import (
	"reflect"
	"strings"
	"testing"
)

// parseLine lexes and parses input in one step for test convenience.
func parseLine(t *testing.T, input string) (Node, error) {
	t.Helper()
	cs := NewCshell()
	if err := cs.lexInput(input); err != nil {
		t.Fatalf("lex error for %q: %v", input, err)
	}
	return Parse(cs.Tokens)
}

func TestParse_SimpleCommands(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected Node
	}{
		{
			name:     "single word",
			input:    "ls",
			expected: &SimpleCommand{Args: []string{"ls"}},
		},
		{
			name:     "command with args",
			input:    "echo hello world",
			expected: &SimpleCommand{Args: []string{"echo", "hello", "world"}},
		},
		{
			name:  "output redirect",
			input: "echo hi > out.txt",
			expected: &SimpleCommand{
				Args:      []string{"echo", "hi"},
				Redirects: []Redirect{{Op: REDIRECT_OUT, FD: 1, Target: "out.txt"}},
			},
		},
		{
			name:  "append redirect",
			input: "echo hi >> out.txt",
			expected: &SimpleCommand{
				Args:      []string{"echo", "hi"},
				Redirects: []Redirect{{Op: REDIRECT_OUT_APPEND, FD: 1, Target: "out.txt"}},
			},
		},
		{
			name:  "input redirect",
			input: "cat < in.txt",
			expected: &SimpleCommand{
				Args:      []string{"cat"},
				Redirects: []Redirect{{Op: REDIRECT_IN, FD: 0, Target: "in.txt"}},
			},
		},
		{
			name:  "stderr redirect via fd",
			input: "ls 2> err.log",
			expected: &SimpleCommand{
				Args:      []string{"ls"},
				Redirects: []Redirect{{Op: REDIRECT_OUT, FD: 2, Target: "err.log"}},
			},
		},
		{
			name:  "input and output redirects on one command",
			input: "sort < in.txt > out.txt",
			expected: &SimpleCommand{
				Args: []string{"sort"},
				Redirects: []Redirect{
					{Op: REDIRECT_IN, FD: 0, Target: "in.txt"},
					{Op: REDIRECT_OUT, FD: 1, Target: "out.txt"},
				},
			},
		},
		{
			name:  "stderr duplicated onto stdout",
			input: "ls > out.txt 2>&1",
			expected: &SimpleCommand{
				Args: []string{"ls"},
				Redirects: []Redirect{
					{Op: REDIRECT_OUT, FD: 1, Target: "out.txt"},
					{Op: REDIRECT_DUP_OUT, FD: 2, Target: "1"},
				},
			},
		},
		{
			name:  "redirect before command words",
			input: "< in.txt cat",
			expected: &SimpleCommand{
				Args:      []string{"cat"},
				Redirects: []Redirect{{Op: REDIRECT_IN, FD: 0, Target: "in.txt"}},
			},
		},
		{
			name:  "redirect-only command",
			input: "> file.txt",
			expected: &SimpleCommand{
				Redirects: []Redirect{{Op: REDIRECT_OUT, FD: 1, Target: "file.txt"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, err := parseLine(t, tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(node, tt.expected) {
				t.Errorf("expected %#v, got %#v", tt.expected, node)
			}
		})
	}
}

func TestParse_Pipelines(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected Node
	}{
		{
			name:  "two command pipeline",
			input: "ls | wc",
			expected: &Pipeline{Cmds: []*SimpleCommand{
				{Args: []string{"ls"}},
				{Args: []string{"wc"}},
			}},
		},
		{
			name:  "pipeline is flattened not nested",
			input: "a | b | c",
			expected: &Pipeline{Cmds: []*SimpleCommand{
				{Args: []string{"a"}},
				{Args: []string{"b"}},
				{Args: []string{"c"}},
			}},
		},
		{
			name:  "pipeline with redirects on both ends",
			input: "cat < in.txt | wc -l > count.txt",
			expected: &Pipeline{Cmds: []*SimpleCommand{
				{
					Args:      []string{"cat"},
					Redirects: []Redirect{{Op: REDIRECT_IN, FD: 0, Target: "in.txt"}},
				},
				{
					Args:      []string{"wc", "-l"},
					Redirects: []Redirect{{Op: REDIRECT_OUT, FD: 1, Target: "count.txt"}},
				},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, err := parseLine(t, tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(node, tt.expected) {
				t.Errorf("expected %#v, got %#v", tt.expected, node)
			}
		})
	}
}

func TestParse_LogicalOperators(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected Node
	}{
		{
			name:  "logical and",
			input: "a && b",
			expected: &AndOr{
				Op:    LOGICAL_AND,
				Left:  &SimpleCommand{Args: []string{"a"}},
				Right: &SimpleCommand{Args: []string{"b"}},
			},
		},
		{
			name:  "logical or",
			input: "a || b",
			expected: &AndOr{
				Op:    LOGICAL_OR,
				Left:  &SimpleCommand{Args: []string{"a"}},
				Right: &SimpleCommand{Args: []string{"b"}},
			},
		},
		{
			name:  "and-or chains are left associative",
			input: "a && b || c",
			expected: &AndOr{
				Op: LOGICAL_OR,
				Left: &AndOr{
					Op:    LOGICAL_AND,
					Left:  &SimpleCommand{Args: []string{"a"}},
					Right: &SimpleCommand{Args: []string{"b"}},
				},
				Right: &SimpleCommand{Args: []string{"c"}},
			},
		},
		{
			name:  "pipe binds tighter than and",
			input: "a && b | c",
			expected: &AndOr{
				Op:   LOGICAL_AND,
				Left: &SimpleCommand{Args: []string{"a"}},
				Right: &Pipeline{Cmds: []*SimpleCommand{
					{Args: []string{"b"}},
					{Args: []string{"c"}},
				}},
			},
		},
		{
			name:  "pipe binds tighter on the left too",
			input: "a | b && c",
			expected: &AndOr{
				Op: LOGICAL_AND,
				Left: &Pipeline{Cmds: []*SimpleCommand{
					{Args: []string{"a"}},
					{Args: []string{"b"}},
				}},
				Right: &SimpleCommand{Args: []string{"c"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, err := parseLine(t, tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(node, tt.expected) {
				t.Errorf("expected %#v, got %#v", tt.expected, node)
			}
		})
	}
}

func TestParse_Lists(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected Node
	}{
		{
			name:  "semicolon list",
			input: "a ; b",
			expected: &List{Items: []Node{
				&SimpleCommand{Args: []string{"a"}},
				&SimpleCommand{Args: []string{"b"}},
			}},
		},
		{
			name:  "list is flattened not nested",
			input: "a ; b ; c",
			expected: &List{Items: []Node{
				&SimpleCommand{Args: []string{"a"}},
				&SimpleCommand{Args: []string{"b"}},
				&SimpleCommand{Args: []string{"c"}},
			}},
		},
		{
			name:     "trailing semicolon is allowed",
			input:    "a ;",
			expected: &SimpleCommand{Args: []string{"a"}},
		},
		{
			name:  "list of and-or chains",
			input: "a && b ; c",
			expected: &List{Items: []Node{
				&AndOr{
					Op:    LOGICAL_AND,
					Left:  &SimpleCommand{Args: []string{"a"}},
					Right: &SimpleCommand{Args: []string{"b"}},
				},
				&SimpleCommand{Args: []string{"c"}},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, err := parseLine(t, tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(node, tt.expected) {
				t.Errorf("expected %#v, got %#v", tt.expected, node)
			}
		})
	}
}

func TestParse_Errors(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		errorContains string
	}{
		{name: "leading pipe", input: "| ls", errorContains: "syntax error"},
		{name: "trailing pipe", input: "ls |", errorContains: "syntax error"},
		{name: "double pipe with nothing between", input: "a | | b", errorContains: "syntax error"},
		{name: "trailing and", input: "a &&", errorContains: "syntax error"},
		{name: "leading and", input: "&& a", errorContains: "syntax error"},
		{name: "leading semicolon", input: "; a", errorContains: "syntax error"},
		{name: "redirect without target", input: "ls >", errorContains: "expected target"},
		{name: "redirect into operator", input: "ls > | wc", errorContains: "expected target"},
		{name: "heredoc unsupported", input: "cat << EOF", errorContains: "not supported"},
		{name: "background unsupported", input: "sleep 1 &", errorContains: "not supported"},
		{name: "dup to non-numeric target", input: "ls 2>& file", errorContains: "bad file descriptor"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, err := parseLine(t, tt.input)
			if err == nil {
				t.Fatalf("expected error, got AST %#v", node)
			}
			if !strings.Contains(err.Error(), tt.errorContains) {
				t.Errorf("expected error to contain %q, got %q", tt.errorContains, err.Error())
			}
		})
	}
}

func TestParse_EmptyInput(t *testing.T) {
	node, err := Parse(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if node != nil {
		t.Errorf("expected nil node for empty input, got %#v", node)
	}
}
