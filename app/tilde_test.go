package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLex_QuotedFlag(t *testing.T) {
	cs := NewCshell()
	if err := cs.lexInput(`plain 'single' "double" esc\aped`); err != nil {
		t.Fatal(err)
	}
	expected := []bool{false, true, true, true}
	for i, want := range expected {
		if cs.Tokens[i].Quoted != want {
			t.Errorf("token %d (%q): Quoted = %v, want %v", i, cs.Tokens[i].Value, cs.Tokens[i].Quoted, want)
		}
	}
}

func TestParse_TildeExpansion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	tests := []struct {
		name     string
		input    string
		expected []string // expected Args
	}{
		{"tilde slash prefix", "cat ~/notes.txt", []string{"cat", home + "/notes.txt"}},
		{"bare tilde", "echo ~", []string{"echo", home}},
		{"quoted tilde stays literal", `echo '~/literal'`, []string{"echo", "~/literal"}},
		{"escaped tilde stays literal", `echo \~/x`, []string{"echo", "~/x"}},
		{"mid-word tilde untouched", "echo a~b", []string{"echo", "a~b"}},
		{"tilde-user untouched", "echo ~root/x", []string{"echo", "~root/x"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, err := parseLine(t, tt.input)
			if err != nil {
				t.Fatal(err)
			}
			cmd, ok := node.(*SimpleCommand)
			if !ok {
				t.Fatalf("expected SimpleCommand, got %#v", node)
			}
			if len(cmd.Args) != len(tt.expected) {
				t.Fatalf("expected args %v, got %v", tt.expected, cmd.Args)
			}
			for i, want := range tt.expected {
				if cmd.Args[i] != want {
					t.Errorf("arg %d: expected %q, got %q", i, want, cmd.Args[i])
				}
			}
		})
	}
}

func TestParse_TildeInRedirectTarget(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	node, err := parseLine(t, "echo hi > ~/out.txt")
	if err != nil {
		t.Fatal(err)
	}
	cmd := node.(*SimpleCommand)
	if cmd.Redirects[0].Target != home+"/out.txt" {
		t.Errorf("redirect target: got %q", cmd.Redirects[0].Target)
	}
}

func TestExec_TildeEndToEnd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, "in.txt"), []byte("from home\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cs := NewCshell()
	out, _, status := runLine(t, cs, "cat ~/in.txt")
	if status != 0 {
		t.Fatalf("status %d", status)
	}
	if out != "from home\n" {
		t.Errorf("got %q", out)
	}

	runLine(t, cs, "echo written > ~/out.txt")
	content, err := os.ReadFile(filepath.Join(home, "out.txt"))
	if err != nil {
		t.Fatalf("redirect into ~ failed: %v", err)
	}
	if string(content) != "written\n" {
		t.Errorf("got %q", string(content))
	}
}

func TestComplete_TildePaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, name := range []string{"alpha.txt", "alphabet.md"} {
		if err := os.WriteFile(filepath.Join(home, name), nil, 0644); err != nil {
			t.Fatal(err)
		}
	}

	cs := NewCshell()
	line := "cat ~/al"
	comp := cs.Complete(line, len(line))

	if len(comp.Candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %v", comp.Candidates)
	}
	for _, c := range comp.Candidates {
		if !strings.HasPrefix(c, "~/") {
			t.Errorf("candidate should keep the ~ form: %q", c)
		}
	}

	// bare ~ completes to ~/
	line = "cat ~"
	comp = cs.Complete(line, len(line))
	if len(comp.Candidates) != 1 || comp.Candidates[0] != "~/" {
		t.Errorf("bare ~: expected [~/], got %v", comp.Candidates)
	}
}
