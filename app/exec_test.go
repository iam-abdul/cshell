package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runLine lexes, parses and executes one line, returning stdout, stderr and
// the exit status.
func runLine(t *testing.T, cs *Cshell, input string) (string, string, int) {
	t.Helper()
	if err := cs.processInput(input); err != nil {
		t.Fatalf("processInput(%q): %v", input, err)
	}
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	status := cs.execNode(cs.AST, IOStreams{In: strings.NewReader(""), Out: out, Err: errBuf})
	return out.String(), errBuf.String(), status
}

func TestExec_Builtin(t *testing.T) {
	cs := NewCshell()

	out, _, status := runLine(t, cs, "echo hello world")
	if out != "hello world\n" {
		t.Errorf("expected %q, got %q", "hello world\n", out)
	}
	if status != 0 {
		t.Errorf("expected status 0, got %d", status)
	}
}

func TestExec_ExternalCommand(t *testing.T) {
	cs := NewCshell()

	// PATH-resolved external command
	out, _, status := runLine(t, cs, "cat "+os.DevNull)
	if status != 0 {
		t.Errorf("expected status 0 from cat, got %d", status)
	}
	if out != "" {
		t.Errorf("expected no output, got %q", out)
	}
}

func TestExec_ExternalCommandByPath(t *testing.T) {
	cs := NewCshell()

	// a name with a slash bypasses PATH lookup: /bin/echo exists on every unix
	out, _, status := runLine(t, cs, "/bin/echo external")
	if status != 0 {
		t.Fatalf("expected status 0, got %d", status)
	}
	if out != "external\n" {
		t.Errorf("expected %q, got %q", "external\n", out)
	}

	_, errOut, status := runLine(t, cs, "/definitely/not/a/binary")
	if status != 127 {
		t.Errorf("expected status 127, got %d", status)
	}
	if !strings.Contains(errOut, "No such file or directory") {
		t.Errorf("expected missing-file error, got %q", errOut)
	}
}

func TestExec_ExternalExitStatus(t *testing.T) {
	cs := NewCshell()

	_, _, status := runLine(t, cs, "true")
	if status != 0 {
		t.Errorf("true: expected status 0, got %d", status)
	}

	_, _, status = runLine(t, cs, "false")
	if status == 0 {
		t.Errorf("false: expected non-zero status, got 0")
	}
}

func TestExec_CommandNotFound(t *testing.T) {
	cs := NewCshell()

	_, errOut, status := runLine(t, cs, "definitelynotarealcommand123")
	if status != 127 {
		t.Errorf("expected status 127, got %d", status)
	}
	if !strings.Contains(errOut, "command not found") {
		t.Errorf("expected 'command not found' on stderr, got %q", errOut)
	}
}

func TestExec_Pipeline(t *testing.T) {
	cs := NewCshell()

	// builtin feeding an external through a real pipe
	out, _, status := runLine(t, cs, "echo hello | cat")
	if out != "hello\n" {
		t.Errorf("expected %q, got %q", "hello\n", out)
	}
	if status != 0 {
		t.Errorf("expected status 0, got %d", status)
	}
}

func TestExec_PipelineThreeStages(t *testing.T) {
	cs := NewCshell()

	out, _, status := runLine(t, cs, "echo hello world | tr a-z A-Z | cat")
	if out != "HELLO WORLD\n" {
		t.Errorf("expected %q, got %q", "HELLO WORLD\n", out)
	}
	if status != 0 {
		t.Errorf("expected status 0, got %d", status)
	}
}

func TestExec_PipelineStatusIsLastCommand(t *testing.T) {
	cs := NewCshell()

	// last command succeeds even though the first fails
	_, _, status := runLine(t, cs, "false | true")
	if status != 0 {
		t.Errorf("expected status of last command (0), got %d", status)
	}

	_, _, status = runLine(t, cs, "true | false")
	if status == 0 {
		t.Errorf("expected non-zero status from last command, got 0")
	}
}

func TestExec_LogicalAnd(t *testing.T) {
	cs := NewCshell()

	out, _, _ := runLine(t, cs, "true && echo yes")
	if out != "yes\n" {
		t.Errorf("true && echo: expected %q, got %q", "yes\n", out)
	}

	out, _, status := runLine(t, cs, "false && echo yes")
	if out != "" {
		t.Errorf("false && echo: expected no output, got %q", out)
	}
	if status == 0 {
		t.Errorf("false && echo: expected non-zero status")
	}
}

func TestExec_LogicalOr(t *testing.T) {
	cs := NewCshell()

	out, _, status := runLine(t, cs, "false || echo rescued")
	if out != "rescued\n" {
		t.Errorf("false || echo: expected %q, got %q", "rescued\n", out)
	}
	if status != 0 {
		t.Errorf("false || echo: expected status 0, got %d", status)
	}

	out, _, _ = runLine(t, cs, "true || echo skipped")
	if out != "" {
		t.Errorf("true || echo: expected no output, got %q", out)
	}
}

func TestExec_AndOrChain(t *testing.T) {
	cs := NewCshell()

	out, _, _ := runLine(t, cs, "false && echo a || echo b")
	if out != "b\n" {
		t.Errorf("expected %q, got %q", "b\n", out)
	}
}

func TestExec_List(t *testing.T) {
	cs := NewCshell()

	out, _, status := runLine(t, cs, "echo a ; echo b ; echo c")
	if out != "a\nb\nc\n" {
		t.Errorf("expected %q, got %q", "a\nb\nc\n", out)
	}
	if status != 0 {
		t.Errorf("expected status 0, got %d", status)
	}

	// a failure mid-list does not stop the list, and the list's status is the
	// last command's status
	out, _, status = runLine(t, cs, "false ; echo still-here")
	if out != "still-here\n" {
		t.Errorf("expected %q, got %q", "still-here\n", out)
	}
	if status != 0 {
		t.Errorf("expected status 0 from last command, got %d", status)
	}
}

func TestExec_OutputRedirect(t *testing.T) {
	cs := NewCshell()
	dir := t.TempDir()
	file := filepath.Join(dir, "out.txt")

	out, _, status := runLine(t, cs, "echo redirected > "+file)
	if out != "" {
		t.Errorf("expected no output on stdout, got %q", out)
	}
	if status != 0 {
		t.Errorf("expected status 0, got %d", status)
	}

	content, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("reading %s: %v", file, err)
	}
	if string(content) != "redirected\n" {
		t.Errorf("expected file to contain %q, got %q", "redirected\n", string(content))
	}

	// > truncates
	runLine(t, cs, "echo second > "+file)
	content, _ = os.ReadFile(file)
	if string(content) != "second\n" {
		t.Errorf("expected truncated file to contain %q, got %q", "second\n", string(content))
	}
}

func TestExec_AppendRedirect(t *testing.T) {
	cs := NewCshell()
	dir := t.TempDir()
	file := filepath.Join(dir, "out.txt")

	runLine(t, cs, "echo one >> "+file)
	runLine(t, cs, "echo two >> "+file)

	content, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("reading %s: %v", file, err)
	}
	if string(content) != "one\ntwo\n" {
		t.Errorf("expected %q, got %q", "one\ntwo\n", string(content))
	}
}

func TestExec_InputRedirect(t *testing.T) {
	cs := NewCshell()
	dir := t.TempDir()
	file := filepath.Join(dir, "in.txt")
	if err := os.WriteFile(file, []byte("from a file\n"), 0644); err != nil {
		t.Fatal(err)
	}

	out, _, status := runLine(t, cs, "cat < "+file)
	if out != "from a file\n" {
		t.Errorf("expected %q, got %q", "from a file\n", out)
	}
	if status != 0 {
		t.Errorf("expected status 0, got %d", status)
	}
}

func TestExec_InputRedirectMissingFile(t *testing.T) {
	cs := NewCshell()

	out, errOut, status := runLine(t, cs, "cat < /definitely/not/a/file")
	if status != 1 {
		t.Errorf("expected status 1, got %d", status)
	}
	if out != "" {
		t.Errorf("expected no stdout, got %q", out)
	}
	if !strings.Contains(errOut, "No such file or directory") {
		t.Errorf("expected missing-file error, got %q", errOut)
	}
}

func TestExec_StderrRedirect(t *testing.T) {
	cs := NewCshell()
	dir := t.TempDir()
	file := filepath.Join(dir, "err.log")

	// ls of a missing path writes to stderr and fails
	out, _, status := runLine(t, cs, "ls /definitely/not/a/path 2> "+file)
	if status == 0 {
		t.Errorf("expected non-zero status")
	}
	if out != "" {
		t.Errorf("expected empty stdout, got %q", out)
	}

	content, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("reading %s: %v", file, err)
	}
	if len(content) == 0 {
		t.Error("expected stderr output in the file, got nothing")
	}
}

func TestExec_DupStderrToStdout(t *testing.T) {
	cs := NewCshell()

	// 2>&1 makes the error message land on stdout
	out, errOut, _ := runLine(t, cs, "ls /definitely/not/a/path 2>&1")
	if !strings.Contains(out, "not/a/path") {
		t.Errorf("expected error message on stdout via 2>&1, got stdout=%q stderr=%q", out, errOut)
	}
	if errOut != "" {
		t.Errorf("expected empty stderr, got %q", errOut)
	}
}

func TestExec_RedirectOrderMatters(t *testing.T) {
	cs := NewCshell()
	dir := t.TempDir()
	file := filepath.Join(dir, "both.log")

	// > file 2>&1: stderr follows stdout into the file
	_, errOut, _ := runLine(t, cs, "ls /definitely/not/a/path > "+file+" 2>&1")
	if errOut != "" {
		t.Errorf("expected empty stderr, got %q", errOut)
	}
	content, _ := os.ReadFile(file)
	if len(content) == 0 {
		t.Error("expected stderr to be captured in the file via `> file 2>&1`")
	}
}

func TestExec_RedirectOnlyCommandCreatesFile(t *testing.T) {
	cs := NewCshell()
	dir := t.TempDir()
	file := filepath.Join(dir, "created.txt")

	_, _, status := runLine(t, cs, "> "+file)
	if status != 0 {
		t.Errorf("expected status 0, got %d", status)
	}
	if _, err := os.Stat(file); err != nil {
		t.Errorf("expected %s to exist: %v", file, err)
	}
}

func TestExec_BuiltinOutputRedirect(t *testing.T) {
	cs := NewCshell()
	dir := t.TempDir()
	file := filepath.Join(dir, "builtin.txt")

	// builtins must honor redirects too
	out, _, _ := runLine(t, cs, "pwd > "+file)
	if out != "" {
		t.Errorf("expected no stdout, got %q", out)
	}
	content, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	wd, _ := os.Getwd()
	if strings.TrimSpace(string(content)) != wd {
		t.Errorf("expected %q in file, got %q", wd, string(content))
	}
}

func TestExec_PipelineWithRedirectedMiddle(t *testing.T) {
	cs := NewCshell()
	dir := t.TempDir()
	file := filepath.Join(dir, "mid.txt")

	// the middle command writes to a file instead of the pipe, so the last
	// command sees EOF and outputs nothing
	out, _, _ := runLine(t, cs, "echo hi > "+file+" | cat")
	if out != "" {
		t.Errorf("expected empty output, got %q", out)
	}
	content, _ := os.ReadFile(file)
	if string(content) != "hi\n" {
		t.Errorf("expected file to contain %q, got %q", "hi\n", string(content))
	}
}

func TestExec_MixedPrecedenceEndToEnd(t *testing.T) {
	cs := NewCshell()

	out, _, status := runLine(t, cs, "echo start ; false || echo fallback && echo chained")
	expected := "start\nfallback\nchained\n"
	if out != expected {
		t.Errorf("expected %q, got %q", expected, out)
	}
	if status != 0 {
		t.Errorf("expected status 0, got %d", status)
	}
}

func TestExec_LastStatusTracked(t *testing.T) {
	cs := NewCshell()

	if err := cs.processInput("false"); err != nil {
		t.Fatal(err)
	}
	cs.execute()
	if cs.LastStatus == 0 {
		t.Errorf("expected LastStatus to be non-zero after false")
	}

	if err := cs.processInput("true"); err != nil {
		t.Fatal(err)
	}
	cs.execute()
	if cs.LastStatus != 0 {
		t.Errorf("expected LastStatus 0 after true, got %d", cs.LastStatus)
	}
}
