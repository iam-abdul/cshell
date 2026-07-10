package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestShellIntegration tests the shell's complete command processing pipeline
func TestShellIntegration(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		expectedOutput string
		expectedError  bool
		errorContains  string
		setup          func() *Cshell
	}{
		{
			name:           "echo builtin command",
			input:          "echo hello world\n",
			expectedOutput: "hello world\n",
			expectedError:  false,
			setup:          NewCshell,
		},
		{
			name:           "echo with quotes",
			input:          "echo 'hello world'\n",
			expectedOutput: "hello world\n",
			expectedError:  false,
			setup:          NewCshell,
		},
		{
			name:           "pwd builtin command",
			input:          "pwd\n",
			expectedOutput: "", // Will be filled during test
			expectedError:  false,
			setup:          NewCshell,
		},
		{
			name:           "type builtin command",
			input:          "type echo\n",
			expectedOutput: "echo is a shell builtin\n",
			expectedError:  false,
			setup:          NewCshell,
		},
		{
			name:           "type external command",
			input:          "type ls\n",
			expectedOutput: "", // Will be filled during test
			expectedError:  false,
			setup:          NewCshell,
		},
		{
			name:           "type non-existent command",
			input:          "type nonexistent\n",
			expectedOutput: "not found",
			expectedError:  false,
			setup:          NewCshell,
		},
		{
			name:           "unknown command",
			input:          "unknowncommand\n",
			expectedOutput: "command not found",
			expectedError:  false,
			setup:          NewCshell,
		},
		{
			name:           "unclosed quote error",
			input:          "echo 'hello\n",
			expectedOutput: "",
			expectedError:  true,
			errorContains:  "single quote not closed",
			setup:          NewCshell,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs := tt.setup()

			// Set up output capture
			oldStdout := os.Stdout
			oldStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stdout = w
			os.Stderr = w

			// Process input
			err := cs.processInput(tt.input)
			if tt.expectedError && err == nil {
				t.Errorf("expected error but got none")
				return
			}
			if !tt.expectedError && err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if tt.errorContains != "" && err != nil && !strings.Contains(err.Error(), tt.errorContains) {
				t.Errorf("expected error to contain %q, got %q", tt.errorContains, err.Error())
			}

			// Execute command if no processing error
			if err == nil {
				cs.execute()
			}

			// Capture output
			w.Close()
			os.Stdout = oldStdout
			os.Stderr = oldStderr

			var buf bytes.Buffer
			buf.ReadFrom(r)
			output := buf.String()

			// Handle dynamic expected outputs
			expectedOutput := tt.expectedOutput
			if tt.name == "pwd builtin command" && expectedOutput == "" {
				pwd, _ := os.Getwd()
				expectedOutput = pwd + "\n"
			}
			if tt.name == "type external command" && expectedOutput == "" {
				// Try to find ls, if not found, expect "not found"
				if _, err := cs.findExecutable("ls"); err != nil {
					expectedOutput = "ls:not found\n"
				} else {
					// If ls exists, we can't predict the exact path, so just check it contains "ls is "
					if !strings.Contains(output, "ls is ") {
						t.Errorf("expected output to contain 'ls is ', got %q", output)
					}
					return
				}
			}

			if expectedOutput != "" && !strings.Contains(output, expectedOutput) {
				t.Errorf("expected output to contain %q, got %q", expectedOutput, output)
			}
		})
	}
}

// TestShellWithMockInput tests shell behavior with controlled input/output
func TestShellWithMockInput(t *testing.T) {
	cs := NewCshell()

	// Test multiple commands in sequence
	commands := []string{
		"echo test1\n",
		"echo 'test 2'\n",
		"pwd\n",
	}

	for i, cmd := range commands {
		t.Run(fmt.Sprintf("command_%d", i), func(t *testing.T) {
			// Reset shell state
			cs.Tokens = nil
			cs.Input = ""

			// Capture output
			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			err := cs.processInput(cmd)
			if err != nil {
				t.Errorf("unexpected error processing command %q: %v", cmd, err)
				return
			}

			cs.execute()

			w.Close()
			os.Stdout = oldStdout

			var buf bytes.Buffer
			buf.ReadFrom(r)
			output := buf.String()

			if len(output) == 0 {
				t.Errorf("expected output for command %q, got empty string", cmd)
			}
		})
	}
}

// TestShellLexerEdgeCases tests edge cases in the lexer
func TestShellLexerEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		shouldError bool
		expected    []string
	}{
		{
			name:        "empty input",
			input:       "",
			shouldError: false,
			expected:    []string{},
		},
		{
			name:        "whitespace only",
			input:       "   \t  \n  ",
			shouldError: false,
			expected:    []string{},
		},
		{
			name:        "multiple consecutive spaces",
			input:       "echo    hello",
			shouldError: false,
			expected:    []string{"echo", "hello"},
		},
		{
			name:        "tabs and spaces mixed",
			input:       "echo\thello   world",
			shouldError: false,
			expected:    []string{"echo", "hello", "world"},
		},
		{
			name:        "escaped characters",
			input:       `echo "hello \"world\""`,
			shouldError: false,
			expected:    []string{"echo", `hello "world"`},
		},
		{
			name:        "nested quotes not allowed",
			input:       `echo "hello 'world'"`,
			shouldError: false,
			expected:    []string{"echo", "hello 'world'"},
		},
		{
			name:        "escaped quote in double quotes",
			input:       `echo "hello \"world\""`,
			shouldError: false,
			expected:    []string{"echo", `hello "world"`},
		},
		{
			// backslash is literal inside single quotes (POSIX), so the quote
			// after it closes the string and the final quote is left unclosed
			name:        "backslash does not escape inside single quotes",
			input:       `echo 'hello \'world\''`,
			shouldError: true,
			expected:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs := NewCshell()
			err := cs.processInput(tt.input)

			if tt.shouldError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			// Extract token values
			var actual []string
			for _, token := range cs.Tokens {
				actual = append(actual, token.Value)
			}

			if len(actual) != len(tt.expected) {
				t.Errorf("expected %d tokens, got %d", len(tt.expected), len(actual))
				return
			}

			for i, expected := range tt.expected {
				if actual[i] != expected {
					t.Errorf("token %d: expected %q, got %q", i, expected, actual[i])
				}
			}
		})
	}
}
