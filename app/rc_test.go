package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- lexer: comments ------------------------------------------------------

func TestLex_Comments(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string // token values
	}{
		{"trailing comment", "echo hi # a comment", []string{"echo", "hi"}},
		{"whole-line comment", "# nothing here", nil},
		{"hash mid-word is literal", "echo file#name", []string{"echo", "file#name"}},
		{"hash in quotes is literal", "echo '#not a comment'", []string{"echo", "#not a comment"}},
		{"comment after operator", "echo a; # rest", []string{"echo", "a", ";"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs := NewCshell()
			if err := cs.lexInput(tt.input); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			var values []string
			for _, tok := range cs.Tokens {
				values = append(values, tok.Value)
			}
			if len(values) != len(tt.expected) {
				t.Fatalf("expected %v, got %v", tt.expected, values)
			}
			for i, want := range tt.expected {
				if values[i] != want {
					t.Errorf("token %d: expected %q, got %q", i, want, values[i])
				}
			}
		})
	}
}

// --- variable assignment --------------------------------------------------

func TestSplitAssignment(t *testing.T) {
	tests := []struct {
		word        string
		name, value string
		ok          bool
	}{
		{"PS1=hello", "PS1", "hello", true},
		{"FOO=", "FOO", "", true},
		{"A=b=c", "A", "b=c", true},
		{"_x1=y", "_x1", "y", true},
		{"1BAD=x", "", "", false},
		{"not-a-var=x", "", "", false},
		{"=value", "", "", false},
		{"plainword", "", "", false},
	}
	for _, tt := range tests {
		name, value, ok := splitAssignment(tt.word)
		if ok != tt.ok || name != tt.name || value != tt.value {
			t.Errorf("splitAssignment(%q) = (%q,%q,%v), want (%q,%q,%v)",
				tt.word, name, value, ok, tt.name, tt.value, tt.ok)
		}
	}
}

func TestExec_Assignment(t *testing.T) {
	cs := NewCshell()

	// the > needs quoting or it would lex as a redirect — just like bash
	_, _, status := runLine(t, cs, "PS1='custom>'")
	if status != 0 {
		t.Fatalf("assignment status %d", status)
	}
	if v, ok := cs.getVar("PS1"); !ok || v != "custom>" {
		t.Errorf("PS1 not set: %q %v", v, ok)
	}

	// multiple assignments in one command
	runLine(t, cs, "A=1 B=2")
	if v, _ := cs.getVar("A"); v != "1" {
		t.Errorf("A = %q", v)
	}
	if v, _ := cs.getVar("B"); v != "2" {
		t.Errorf("B = %q", v)
	}
}

func TestExec_AssignmentEnvPrefix(t *testing.T) {
	cs := NewCshell()

	// FOO=... cmd exports only into that command's environment
	out, _, status := runLine(t, cs, "PREFIXVAR=only-for-child printenv PREFIXVAR")
	if status != 0 {
		t.Fatalf("status %d", status)
	}
	if strings.TrimSpace(out) != "only-for-child" {
		t.Errorf("child env: got %q", out)
	}
	if _, ok := cs.getVar("PREFIXVAR"); ok {
		t.Error("prefix assignment leaked into the shell")
	}
}

func TestExec_AssignmentInPipelineIsSubshell(t *testing.T) {
	cs := NewCshell()

	runLine(t, cs, "SUBVAR=nope | cat")
	if _, ok := cs.getVar("SUBVAR"); ok {
		t.Error("assignment inside a pipeline must not affect the shell")
	}
}

func TestExec_ExportBuiltin(t *testing.T) {
	cs := NewCshell()
	t.Cleanup(func() { os.Unsetenv("EXPORTED_TEST_VAR") })

	out, _, status := runLine(t, cs, "export EXPORTED_TEST_VAR=visible ; printenv EXPORTED_TEST_VAR")
	if status != 0 {
		t.Fatalf("status %d", status)
	}
	if strings.TrimSpace(out) != "visible" {
		t.Errorf("expected exported var in child, got %q", out)
	}

	// export of an existing shell var
	runLine(t, cs, "PROMOTE_ME=promoted")
	runLine(t, cs, "export PROMOTE_ME")
	t.Cleanup(func() { os.Unsetenv("PROMOTE_ME") })
	if os.Getenv("PROMOTE_ME") != "promoted" {
		t.Errorf("export NAME did not promote the shell var")
	}

	// invalid identifier
	_, errOut, status := runLine(t, cs, "export not-valid")
	if status != 1 || !strings.Contains(errOut, "not a valid identifier") {
		t.Errorf("expected identifier error, got status %d, stderr %q", status, errOut)
	}
}

// --- prompt expansion -----------------------------------------------------

func TestExpandPrompt(t *testing.T) {
	cs := NewCshell()
	t.Setenv("USER", "testuser")

	if got := cs.expandPrompt(`\u@x \$ `); got != "testuser@x $ " && got != "testuser@x # " {
		t.Errorf("\\u/\\$: got %q", got)
	}

	if got := cs.expandPrompt(`a\nb`); got != "a\nb" {
		t.Errorf("\\n: got %q", got)
	}

	if got := cs.expandPrompt(`\e[32mG\e[0m`); got != "\x1b[32mG\x1b[0m" {
		t.Errorf("\\e: got %q", got)
	}

	if got := cs.expandPrompt(`\\`); got != `\` {
		t.Errorf("\\\\: got %q", got)
	}

	// unknown escapes stay literal
	if got := cs.expandPrompt(`\q`); got != `\q` {
		t.Errorf("unknown escape: got %q", got)
	}

	// \[ \] markers are dropped
	if got := cs.expandPrompt(`\[\e[1m\]X\[\e[0m\]`); got != "\x1b[1mX\x1b[0m" {
		t.Errorf("markers: got %q", got)
	}
}

func TestExpandPrompt_Cwd(t *testing.T) {
	cs := NewCshell()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	if err := os.Chdir(home); err != nil {
		t.Skip("cannot chdir home")
	}
	if got := cs.expandPrompt(`\w`); got != "~" {
		t.Errorf("\\w in home: got %q", got)
	}
	if got := cs.expandPrompt(`\W`); got != "~" {
		t.Errorf("\\W in home: got %q", got)
	}

	sub := t.TempDir()
	os.Chdir(sub)
	if got := cs.expandPrompt(`\W`); got != filepath.Base(sub) {
		t.Errorf("\\W: expected %q, got %q", filepath.Base(sub), got)
	}
}

func TestPromptWidth(t *testing.T) {
	tests := []struct {
		prompt string
		width  int
	}{
		{"$ ", 2},
		{"", 0},
		{"\x1b[32m$\x1b[0m ", 2},              // colors take no columns
		{"\x1b]0;title\x07> ", 2},             // OSC sequence invisible
		{"\x1b[1;31muser@host\x1b[0m $ ", 12}, // user@host + " $ "
	}
	for _, tt := range tests {
		if got := promptWidth(tt.prompt); got != tt.width {
			t.Errorf("promptWidth(%q) = %d, want %d", tt.prompt, got, tt.width)
		}
	}
}

// --- rc file --------------------------------------------------------------

func runScriptFile(t *testing.T, cs *Cshell, content string) (string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".cshrc")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cs.RunScript(path, IOStreams{In: strings.NewReader(""), Out: out, Err: errBuf})
	return out.String(), errBuf.String()
}

func TestRunScript_SetsVarsAndRunsCommands(t *testing.T) {
	cs := NewCshell()
	out, errOut := runScriptFile(t, cs, `
# my cshell config
PS1='rc-prompt>'
echo rc-ran
`)
	if v, _ := cs.getVar("PS1"); v != "rc-prompt>" {
		t.Errorf("PS1 = %q", v)
	}
	if !strings.Contains(out, "rc-ran") {
		t.Errorf("rc command did not run: %q", out)
	}
	if errOut != "" {
		t.Errorf("unexpected stderr: %q", errOut)
	}
}

func TestRunScript_ContinuesPastErrors(t *testing.T) {
	cs := NewCshell()
	out, errOut := runScriptFile(t, cs, `
ls |
nosuchcommandxyz
echo survived
`)
	if !strings.Contains(out, "survived") {
		t.Errorf("script stopped at an error: out=%q err=%q", out, errOut)
	}
	if !strings.Contains(errOut, "syntax error") {
		t.Errorf("syntax error not reported: %q", errOut)
	}
	if !strings.Contains(errOut, "command not found") {
		t.Errorf("missing command not reported: %q", errOut)
	}
}

func TestRunScript_MultiLineQuote(t *testing.T) {
	cs := NewCshell()
	out, _ := runScriptFile(t, cs, "echo 'one\ntwo'\n")
	if !strings.Contains(out, "one\ntwo") {
		t.Errorf("multi-line quote: %q", out)
	}
}

func TestRunScript_UnclosedQuoteAtEOFReported(t *testing.T) {
	cs := NewCshell()
	_, errOut := runScriptFile(t, cs, "echo 'never closed\n")
	if !strings.Contains(errOut, "unclosed quote") {
		t.Errorf("expected unclosed-quote report, got %q", errOut)
	}
}

func TestRunScript_MissingFileIsFine(t *testing.T) {
	cs := NewCshell()
	cs.RunScript(filepath.Join(t.TempDir(), "nope"), IOStreams{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}})
	// nothing to assert: it just must not panic or error
}
