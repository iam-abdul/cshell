package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestCshell_ProcessInput(t *testing.T) {
	type processInputTestCase struct {
		name     string
		input    string
		expected []Token
		hasError bool
	}

	basicTests := []processInputTestCase{
		{
			name:  "simple single word command",
			input: `exit`,
			expected: []Token{
				{Type: WORD, Value: "exit", Start: 0, End: 4},
			},
			hasError: false,
		},

		{
			name:  "simple command without single quotes",
			input: `echo hello    world`,
			expected: []Token{
				{Type: WORD, Value: "echo", Start: 0, End: 4},
				{Type: WORD, Value: "hello", Start: 5, End: 10},
				{Type: WORD, Value: "world", Start: 14, End: 19},
			},
			hasError: false,
		},
	}

	singleQuoteTests := []processInputTestCase{
		{
			name:  "simple command with single quotes",
			input: `echo 'hello    world'`,
			expected: []Token{
				{Type: WORD, Value: "echo", Start: 0, End: 4},
				{Type: WORD, Value: "hello    world", Start: 6, End: 20},
			},
			hasError: false,
		},
		{
			name:     "Did not close single quote",
			input:    `echo 'hello  `,
			expected: []Token{},
			hasError: true,
		},
		{
			name:  "Adjacent quoted strings 'hello' and 'world' are concatenated.",
			input: `echo 'hello''world'`,
			expected: []Token{
				{Type: WORD, Value: "echo", Start: 0, End: 4},
				{Type: WORD, Value: "helloworld", Start: 6, End: 18},
			},
			hasError: false,
		},
		{
			name:  "Empty quotes '' are ignored",
			input: `echo hello''world`,
			expected: []Token{
				{Type: WORD, Value: "echo", Start: 0, End: 4},
				{Type: WORD, Value: "helloworld", Start: 5, End: 17},
			},
			hasError: false,
		},
	}

	doubleQuoteTests := []processInputTestCase{
		{
			name:  "Multiple spaces preserved in double quotes",
			input: `echo "hello    world"`,
			expected: []Token{
				{Type: WORD, Value: "echo", Start: 0, End: 4},
				{Type: WORD, Value: "hello    world", Start: 6, End: 20},
			},
			hasError: false,
		},
		{
			name:  "Quoted strings next to each other are concatenated in double quotes",
			input: `echo "hello""world"`,
			expected: []Token{
				{Type: WORD, Value: "echo", Start: 0, End: 4},
				{Type: WORD, Value: "helloworld", Start: 6, End: 18},
			},
			hasError: false,
		},
		{
			name:  "Separate arguments in double quotes",
			input: `echo "hello" "world"`,
			expected: []Token{
				{Type: WORD, Value: "echo", Start: 0, End: 4},
				{Type: WORD, Value: "hello", Start: 6, End: 11},
				{Type: WORD, Value: "world", Start: 14, End: 19},
			},
			hasError: false,
		},
		{
			name:  "Single quotes inside double quotes are literal",
			input: `echo "shell's test"`,
			expected: []Token{
				{Type: WORD, Value: "echo", Start: 0, End: 4},
				{Type: WORD, Value: "shell's test", Start: 6, End: 18},
			},
			hasError: false,
		},
		{
			name:  "sqash adjacent spaces with double quoted words",
			input: `echo "quz  hello"  "bar"`,
			expected: []Token{
				{Type: WORD, Value: "echo", Start: 0, End: 4},
				{Type: WORD, Value: "quz  hello", Start: 6, End: 16},
				{Type: WORD, Value: "bar", Start: 20, End: 23},
			},
		},
		{
			name:  "sqash adjacent spaces with double quoted words and a single quote word",
			input: `echo "bar"  "shell's"  "foo"`,
			expected: []Token{
				{Type: WORD, Value: "echo", Start: 0, End: 4},
				{Type: WORD, Value: "bar", Start: 6, End: 9},
				{Type: WORD, Value: "shell's", Start: 13, End: 20},
				{Type: WORD, Value: "foo", Start: 24, End: 27},
			},
		},
	}

	escapeChar := []processInputTestCase{
		{
			name:  `Each \  creates a literal space as part of one argument.`,
			input: `echo world\ \ \ \ \ \ script`,
			expected: []Token{
				{Type: WORD, Value: "echo", Start: 0, End: 4},
				{Type: WORD, Value: "world      script", Start: 5, End: 28},
			},
			hasError: false,
		},
		{
			name:  "The backslash preserves the first space literally, but the shell collapses the subsequent unescaped spaces.",
			input: `echo before\ after`,
			expected: []Token{
				{Type: WORD, Value: "echo", Start: 0, End: 4},
				{Type: WORD, Value: "before after", Start: 5, End: 18},
			},
			hasError: false,
		},
		{
			name:  `\n becomes just n`,
			input: `echo test\nexample`,
			expected: []Token{
				{Type: WORD, Value: "echo", Start: 0, End: 4},
				{Type: WORD, Value: "testnexample", Start: 5, End: 18},
			},
			hasError: false,
		},
		{
			name:  "The first backslash escapes the second, and the result is a single literal backslash in the argument.",
			input: `echo hello\\world`,
			expected: []Token{
				{Type: WORD, Value: "echo", Start: 0, End: 4},
				{Type: WORD, Value: `hello\world`, Start: 5, End: 17},
			},
			hasError: false,
		},
		{
			name:  `\' makes the single quotes literal characters.`,
			input: `echo \'hello\'`,
			expected: []Token{
				{Type: WORD, Value: "echo", Start: 0, End: 4},
				{Type: WORD, Value: `'hello'`, Start: 6, End: 14},
			},
			hasError: false,
		},

		// // escape character in double quotes
		{
			name:  "escaping double quotes in double quotes",
			input: `echo "hello \"sir\" world"`,
			expected: []Token{
				{Type: WORD, Value: `echo`, Start: 0, End: 4},
				{Type: WORD, Value: `hello "sir" world`, Start: 6, End: 25},
			},
			hasError: false,
		},
		{
			name:  "backslash should escape backslash in double quotes",
			input: `echo "A \\ escapes itself"`,
			expected: []Token{
				{Type: WORD, Value: "echo", Start: 0, End: 4},
				{Type: WORD, Value: `A \ escapes itself`, Start: 6, End: 25},
			},
			hasError: false,
		},
		{
			name:  "backslash should escape double quote in double quotes",
			input: `echo "A \" inside double quotes"`,
			expected: []Token{
				{Type: WORD, Value: "echo", Start: 0, End: 4},
				{Type: WORD, Value: `A " inside double quotes`, Start: 6, End: 31},
			},
			hasError: false,
		},
	}

	pipes := []processInputTestCase{
		{
			name:  "single pipe",
			input: "ls | wc",
			expected: []Token{
				{Type: WORD, Value: "ls", Start: 0, End: 2},
				{Type: PIPE, Value: "|", Start: 3, End: 4},
				{Type: WORD, Value: "wc", Start: 5, End: 7},
			},
			hasError: false,
		},
		{
			name:  "multiple pipes",
			input: "a | b | c",
			expected: []Token{
				{Type: WORD, Value: "a", Start: 0, End: 1},
				{Type: PIPE, Value: "|", Start: 2, End: 3},
				{Type: WORD, Value: "b", Start: 4, End: 5},
				{Type: PIPE, Value: "|", Start: 6, End: 7},
				{Type: WORD, Value: "c", Start: 8, End: 9},
			},
			hasError: false,
		},
		{
			name:  "pipe ignored in single quotes",
			input: "echo 'a | b'",
			expected: []Token{
				{Type: WORD, Value: "echo", Start: 0, End: 4},
				{Type: WORD, Value: "a | b", Start: 6, End: 11},
			},
			hasError: false,
		},
		{
			name:  "pipe ignored in double quotes",
			input: `echo  "a | b"`,
			expected: []Token{
				{Type: WORD, Value: "echo", Start: 0, End: 4},
				{Type: WORD, Value: "a | b", Start: 7, End: 12},
			},
			hasError: false,
		},
		{
			name:  "pipe in single word without spaces",
			input: `ls|wc`,
			expected: []Token{
				{Type: WORD, Value: "ls", Start: 0, End: 2},
				{Type: PIPE, Value: "|", Start: 2, End: 3},
				{Type: WORD, Value: "wc", Start: 3, End: 5},
			},
			hasError: false,
		},
	}

	redirectInput := []processInputTestCase{
		{
			name:  "input redirection",
			input: "cat < in.txt",
			expected: []Token{
				{Type: WORD, Value: "cat", Start: 0, End: 3},
				{Type: REDIRECT_IN, Value: "<", Start: 4, End: 5},
				{Type: WORD, Value: "in.txt", Start: 6, End: 12},
			},
			hasError: false,
		},
		{
			name:  "heredoc operator",
			input: "cat << in.txt",
			expected: []Token{
				{Type: WORD, Value: "cat", Start: 0, End: 3},
				{Type: HEREDOC, Value: "<<", Start: 4, End: 6},
				{Type: WORD, Value: "in.txt", Start: 7, End: 13},
			},
			hasError: false,
		},
		{
			name:  "input redirection append ignored in single quotes",
			input: "cat '<< in.txt'",
			expected: []Token{
				{Type: WORD, Value: "cat", Start: 0, End: 3},
				{Type: WORD, Value: "<< in.txt", Start: 5, End: 14},
			},
			hasError: false,
		},
		{
			name:  "input redirection append ignored in double quotes",
			input: `cat "<< in.txt"`,
			expected: []Token{
				{Type: WORD, Value: "cat", Start: 0, End: 3},
				{Type: WORD, Value: "<< in.txt", Start: 5, End: 14},
			},
			hasError: false,
		},
		{
			name:  "input redirection without spaces",
			input: `cat<file.txt`,
			expected: []Token{
				{Type: WORD, Value: "cat", Start: 0, End: 3},
				{Type: REDIRECT_IN, Value: "<", Start: 3, End: 4},
				{Type: WORD, Value: "file.txt", Start: 4, End: 12},
			},
			hasError: false,
		},
		{
			name:  "multiple input redirections",
			input: `< f1 < f2 cat`,
			expected: []Token{
				{Type: REDIRECT_IN, Value: "<", Start: 0, End: 1},
				{Type: WORD, Value: "f1", Start: 2, End: 4},
				{Type: REDIRECT_IN, Value: "<", Start: 5, End: 6},
				{Type: WORD, Value: "f2", Start: 7, End: 9},
				{Type: WORD, Value: "cat", Start: 10, End: 13},
			},
			hasError: false,
		},
	}

	redirectOutput := []processInputTestCase{
		{
			name:  "output redirection",
			input: "cat > in.txt",
			expected: []Token{
				{Type: WORD, Value: "cat", Start: 0, End: 3},
				{Type: REDIRECT_OUT, Value: ">", Start: 4, End: 5},
				{Type: WORD, Value: "in.txt", Start: 6, End: 12},
			},
			hasError: false,
		},
		{
			name:  "output redirection append",
			input: "cat >> in.txt",
			expected: []Token{
				{Type: WORD, Value: "cat", Start: 0, End: 3},
				{Type: REDIRECT_OUT_APPEND, Value: ">>", Start: 4, End: 6},
				{Type: WORD, Value: "in.txt", Start: 7, End: 13},
			},
			hasError: false,
		},
		{
			name:  "output redirection append ignored in single quotes",
			input: "cat '>> in.txt'",
			expected: []Token{
				{Type: WORD, Value: "cat", Start: 0, End: 3},
				{Type: WORD, Value: ">> in.txt", Start: 5, End: 14},
			},
			hasError: false,
		},
		{
			name:  "output redirection append ignored in double quotes",
			input: `cat ">> in.txt"`,
			expected: []Token{
				{Type: WORD, Value: "cat", Start: 0, End: 3},
				{Type: WORD, Value: ">> in.txt", Start: 5, End: 14},
			},
			hasError: false,
		},
		{
			name:  "output redirection without spaces",
			input: `cat>file.txt`,
			expected: []Token{
				{Type: WORD, Value: "cat", Start: 0, End: 3},
				{Type: REDIRECT_OUT, Value: ">", Start: 3, End: 4},
				{Type: WORD, Value: "file.txt", Start: 4, End: 12},
			},
			hasError: false,
		},
		{
			name:  "multiple output redirections",
			input: `cat > f1 > f2`,
			expected: []Token{
				{Type: WORD, Value: "cat", Start: 0, End: 3},
				{Type: REDIRECT_OUT, Value: ">", Start: 4, End: 5},
				{Type: WORD, Value: "f1", Start: 6, End: 8},
				{Type: REDIRECT_OUT, Value: ">", Start: 9, End: 10},
				{Type: WORD, Value: "f2", Start: 11, End: 13},
			},
			hasError: false,
		},
		{
			name:  "output append redirection",
			input: `ls >> out.log`,
			expected: []Token{
				{Type: WORD, Value: "ls", Start: 0, End: 2},
				{Type: REDIRECT_OUT_APPEND, Value: ">>", Start: 3, End: 5},
				{Type: WORD, Value: "out.log", Start: 6, End: 13},
			},
			hasError: false,
		},
		{
			name:  "output redirection at start with file descriptor",
			input: `1>out cat`,
			expected: []Token{
				{Type: REDIRECT_OUT, Value: "1>", Start: 0, End: 2},
				{Type: WORD, Value: "out", Start: 2, End: 5},
				{Type: WORD, Value: "cat", Start: 6, End: 9},
			},
			hasError: false,
		},
	}

	inputOutputMixed :=
		[]processInputTestCase{
			{
				name:  "standard input and output redirection",
				input: `cat < in.txt > out.txt`,
				expected: []Token{
					{Type: WORD, Value: "cat", Start: 0, End: 3},
					{Type: REDIRECT_IN, Value: "<", Start: 4, End: 5},
					{Type: WORD, Value: "in.txt", Start: 6, End: 12},
					{Type: REDIRECT_OUT, Value: ">", Start: 13, End: 14},
					{Type: WORD, Value: "out.txt", Start: 15, End: 22},
				},
				hasError: false,
			},
			{
				name:  "input and append redirection mixed",
				input: `sort < list.txt >> sorted.txt`,
				expected: []Token{
					{Type: WORD, Value: "sort", Start: 0, End: 4},
					{Type: REDIRECT_IN, Value: "<", Start: 5, End: 6},
					{Type: WORD, Value: "list.txt", Start: 7, End: 15},
					{Type: REDIRECT_OUT_APPEND, Value: ">>", Start: 16, End: 18},
					{Type: WORD, Value: "sorted.txt", Start: 19, End: 29},
				},
				hasError: false,
			},
			{
				name:  "digits attached to a word stay part of the word (POSIX)",
				input: `echo abc2>file`,
				expected: []Token{
					{Type: WORD, Value: "echo", Start: 0, End: 4},
					{Type: WORD, Value: "abc2", Start: 5, End: 9},
					{Type: REDIRECT_OUT, Value: ">", Start: 9, End: 10},
					{Type: WORD, Value: "file", Start: 10, End: 14},
				},
				hasError: false,
			},
			{
				name:  "quoted digits are an argument, not a file descriptor",
				input: `echo "2">out`,
				expected: []Token{
					{Type: WORD, Value: "echo", Start: 0, End: 4},
					{Type: WORD, Value: "2", Start: 6, End: 7},
					{Type: REDIRECT_OUT, Value: ">", Start: 8, End: 9},
					{Type: WORD, Value: "out", Start: 9, End: 12},
				},
				hasError: false,
			},
			{
				name:  "multi-digit file descriptor",
				input: `10>f`,
				expected: []Token{
					{Type: REDIRECT_OUT, Value: "10>", Start: 0, End: 3},
					{Type: WORD, Value: "f", Start: 3, End: 4},
				},
				hasError: false,
			},
			{
				name:  "stderr dup onto stdout",
				input: `ls > out 2>&1`,
				expected: []Token{
					{Type: WORD, Value: "ls", Start: 0, End: 2},
					{Type: REDIRECT_OUT, Value: ">", Start: 3, End: 4},
					{Type: WORD, Value: "out", Start: 5, End: 8},
					{Type: REDIRECT_DUP_OUT, Value: "2>&", Start: 9, End: 12},
					{Type: WORD, Value: "1", Start: 12, End: 13},
				},
				hasError: false,
			},
			{
				name:  "input fd dup",
				input: `cmd <&3`,
				expected: []Token{
					{Type: WORD, Value: "cmd", Start: 0, End: 3},
					{Type: REDIRECT_DUP_IN, Value: "<&", Start: 4, End: 6},
					{Type: WORD, Value: "3", Start: 6, End: 7},
				},
				hasError: false,
			},
			{
				name:  "no spaces between input and output operators",
				input: `grep foo<in>out`,
				expected: []Token{
					{Type: WORD, Value: "grep", Start: 0, End: 4},
					{Type: WORD, Value: "foo", Start: 5, End: 8},
					{Type: REDIRECT_IN, Value: "<", Start: 8, End: 9},
					{Type: WORD, Value: "in", Start: 9, End: 11},
					{Type: REDIRECT_OUT, Value: ">", Start: 11, End: 12},
					{Type: WORD, Value: "out", Start: 12, End: 15},
				},
				hasError: false,
			},
			{
				name:  "leading input redirect with trailing output redirect",
				input: `< input.txt cat > output.txt`,
				expected: []Token{
					{Type: REDIRECT_IN, Value: "<", Start: 0, End: 1},
					{Type: WORD, Value: "input.txt", Start: 2, End: 11},
					{Type: WORD, Value: "cat", Start: 12, End: 15},
					{Type: REDIRECT_OUT, Value: ">", Start: 16, End: 17},
					{Type: WORD, Value: "output.txt", Start: 18, End: 28},
				},
				hasError: false,
			},
			{
				name:  "input redirect with error output redirect",
				input: `cat < file.txt 2> error.log`,
				expected: []Token{
					{Type: WORD, Value: "cat", Start: 0, End: 3},
					{Type: REDIRECT_IN, Value: "<", Start: 4, End: 5},
					{Type: WORD, Value: "file.txt", Start: 6, End: 14},
					{Type: REDIRECT_OUT, Value: "2>", Start: 15, End: 17},
					{Type: WORD, Value: "error.log", Start: 18, End: 27},
				},
				hasError: false,
			},

			{
				name:  "Sticky redirection with append",
				input: `echo "hi">>out.txt`,
				expected: []Token{
					{Type: WORD, Value: "echo", Start: 0, End: 4},
					{Type: WORD, Value: "hi", Start: 6, End: 8},
					{Type: REDIRECT_OUT_APPEND, Value: ">>", Start: 9, End: 11},
					{Type: WORD, Value: "out.txt", Start: 11, End: 18},
				},
				hasError: false,
			},
			{
				name:  "Redirect stdout and stderr to different files",
				input: `ls /tmp >out.log 2>err.log`,
				expected: []Token{
					{Type: WORD, Value: "ls", Start: 0, End: 2},
					{Type: WORD, Value: "/tmp", Start: 3, End: 7},
					{Type: REDIRECT_OUT, Value: ">", Start: 8, End: 9},
					{Type: WORD, Value: "out.log", Start: 9, End: 16},
					{Type: REDIRECT_OUT, Value: "2>", Start: 17, End: 19},
					{Type: WORD, Value: "err.log", Start: 19, End: 26},
				},
				hasError: false,
			},
			{
				name:  "Logical AND with pipe and append",
				input: `grep "error" log.txt | wc -l >> report.txt && echo "Done"`,
				expected: []Token{
					{Type: WORD, Value: "grep", Start: 0, End: 4},
					{Type: WORD, Value: "error", Start: 6, End: 11},
					{Type: WORD, Value: "log.txt", Start: 13, End: 20},
					{Type: PIPE, Value: "|", Start: 21, End: 22},
					{Type: WORD, Value: "wc", Start: 23, End: 25},
					{Type: WORD, Value: "-l", Start: 26, End: 28},
					{Type: REDIRECT_OUT_APPEND, Value: ">>", Start: 29, End: 31},
					{Type: WORD, Value: "report.txt", Start: 32, End: 42},
					{Type: LOGICAL_AND, Value: "&&", Start: 43, End: 45},
					{Type: WORD, Value: "echo", Start: 46, End: 50},
					{Type: WORD, Value: "Done", Start: 52, End: 56},
				},
				hasError: false,
			},
			{
				name:  "Complex logical OR and input redirection",
				input: `cat < input.txt || echo "failed"`,
				expected: []Token{
					{Type: WORD, Value: "cat", Start: 0, End: 3},
					{Type: REDIRECT_IN, Value: "<", Start: 4, End: 5},
					{Type: WORD, Value: "input.txt", Start: 6, End: 15},
					{Type: LOGICAL_OR, Value: "||", Start: 16, End: 18},
					{Type: WORD, Value: "echo", Start: 19, End: 23},
					{Type: WORD, Value: "failed", Start: 25, End: 31},
				},
				hasError: false,
			},
		}

	ampersandTests := []processInputTestCase{
		{
			name:  "background operator",
			input: "sleep 1 &",
			expected: []Token{
				{Type: WORD, Value: "sleep", Start: 0, End: 5},
				{Type: WORD, Value: "1", Start: 6, End: 7},
				{Type: AMPERSAND, Value: "&", Start: 8, End: 9},
			},
			hasError: false,
		},
		{
			name:  "ampersand ignored in single quotes",
			input: "echo 'a & b'",
			expected: []Token{
				{Type: WORD, Value: "echo", Start: 0, End: 4},
				{Type: WORD, Value: "a & b", Start: 6, End: 11},
			},
			hasError: false,
		},
		{
			name:  "ampersand ignored in double quotes",
			input: `echo "a & b"`,
			expected: []Token{
				{Type: WORD, Value: "echo", Start: 0, End: 4},
				{Type: WORD, Value: "a & b", Start: 6, End: 11},
			},
			hasError: false,
		},
	}

	logicalAndTests := []processInputTestCase{
		{
			name:  "logical AND basic",
			input: "a && b",
			expected: []Token{
				{Type: WORD, Value: "a", Start: 0, End: 1},
				{Type: LOGICAL_AND, Value: "&&", Start: 2, End: 4},
				{Type: WORD, Value: "b", Start: 5, End: 6},
			},
			hasError: false,
		},
		{
			name:  "logical AND without spaces",
			input: "a&&b",
			expected: []Token{
				{Type: WORD, Value: "a", Start: 0, End: 1},
				{Type: LOGICAL_AND, Value: "&&", Start: 1, End: 3},
				{Type: WORD, Value: "b", Start: 3, End: 4},
			},
			hasError: false,
		},
		{
			name:  "logical AND ignored in single quotes",
			input: "echo 'a && b'",
			expected: []Token{
				{Type: WORD, Value: "echo", Start: 0, End: 4},
				{Type: WORD, Value: "a && b", Start: 6, End: 12},
			},
			hasError: false,
		},
		{
			name:  "logical AND ignored in double quotes",
			input: `echo "a && b"`,
			expected: []Token{
				{Type: WORD, Value: "echo", Start: 0, End: 4},
				{Type: WORD, Value: "a && b", Start: 6, End: 12},
			},
			hasError: false,
		},
	}

	logicalOrTests := []processInputTestCase{
		{
			name:  "logical OR basic",
			input: "a || b",
			expected: []Token{
				{Type: WORD, Value: "a", Start: 0, End: 1},
				{Type: LOGICAL_OR, Value: "||", Start: 2, End: 4},
				{Type: WORD, Value: "b", Start: 5, End: 6},
			},
			hasError: false,
		},
		{
			name:  "logical OR without spaces",
			input: "a||b",
			expected: []Token{
				{Type: WORD, Value: "a", Start: 0, End: 1},
				{Type: LOGICAL_OR, Value: "||", Start: 1, End: 3},
				{Type: WORD, Value: "b", Start: 3, End: 4},
			},
			hasError: false,
		},
		{
			name:  "logical OR ignored in single quotes",
			input: "echo 'a || b'",
			expected: []Token{
				{Type: WORD, Value: "echo", Start: 0, End: 4},
				{Type: WORD, Value: "a || b", Start: 6, End: 12},
			},
			hasError: false,
		},
		{
			name:  "logical OR ignored in double quotes",
			input: `echo "a || b"`,
			expected: []Token{
				{Type: WORD, Value: "echo", Start: 0, End: 4},
				{Type: WORD, Value: "a || b", Start: 6, End: 12},
			},
			hasError: false,
		},
	}

	semicolonTests := []processInputTestCase{
		{
			name:  "semicolon separator",
			input: "a ; b",
			expected: []Token{
				{Type: WORD, Value: "a", Start: 0, End: 1},
				{Type: SEMICOLON, Value: ";", Start: 2, End: 3},
				{Type: WORD, Value: "b", Start: 4, End: 5},
			},
			hasError: false,
		},
		{
			name:  "semicolon without spaces",
			input: "a;b",
			expected: []Token{
				{Type: WORD, Value: "a", Start: 0, End: 1},
				{Type: SEMICOLON, Value: ";", Start: 1, End: 2},
				{Type: WORD, Value: "b", Start: 2, End: 3},
			},
			hasError: false,
		},
		{
			name:  "semicolon ignored in single quotes",
			input: "echo 'a ; b'",
			expected: []Token{
				{Type: WORD, Value: "echo", Start: 0, End: 4},
				{Type: WORD, Value: "a ; b", Start: 6, End: 11},
			},
			hasError: false,
		},
	}

	unclosedQuoteTests := []processInputTestCase{
		{
			name:     "bare opening single quote at EOF",
			input:    `echo '`,
			expected: []Token{},
			hasError: true,
		},
		{
			name:     "bare opening double quote at EOF",
			input:    `echo "`,
			expected: []Token{},
			hasError: true,
		},
		{
			name:     "unclosed double quote with content",
			input:    `echo "hello`,
			expected: []Token{},
			hasError: true,
		},
	}

	var tests []processInputTestCase
	tests = append(tests, basicTests...)
	tests = append(tests, singleQuoteTests...)
	tests = append(tests, doubleQuoteTests...)
	tests = append(tests, escapeChar...)
	tests = append(tests, pipes...)
	tests = append(tests, redirectInput...)
	tests = append(tests, redirectOutput...)
	tests = append(tests, inputOutputMixed...)
	tests = append(tests, logicalAndTests...)
	tests = append(tests, logicalOrTests...)
	tests = append(tests, ampersandTests...)
	tests = append(tests, semicolonTests...)
	tests = append(tests, unclosedQuoteTests...)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cs := NewCshell()
			// lexInput, not processInput: this test checks tokens only, and
			// some inputs (e.g. `sleep 1 &`, heredocs) do not parse yet
			err := cs.lexInput(tt.input)

			if tt.hasError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if len(cs.Tokens) != len(tt.expected) {
				t.Logf("Tokens: %v", cs.Tokens)
				t.Logf("Expected: %v", tt.expected)
				t.Errorf("expected %d tokens, got %d", len(tt.expected), len(cs.Tokens))
				return
			}

			for i, expected := range tt.expected {
				if cs.Tokens[i].Type != expected.Type {
					t.Errorf("token %d: expected type %v, got %v for %q",
						i, expected.Type, cs.Tokens[i].Type, cs.Tokens[i].Value)
				}
				if cs.Tokens[i].Value != expected.Value {
					t.Errorf("token %d: expected value %q, got %q",
						i, expected.Value, cs.Tokens[i].Value)
				}
				if cs.Tokens[i].Start != expected.Start {
					t.Errorf("token %d: expected start %d, got %d",
						i, expected.Start, cs.Tokens[i].Start)
				}
				if cs.Tokens[i].End != expected.End {
					t.Errorf("token %d: expected end %d, got %d",
						i, expected.End, cs.Tokens[i].End)
				}
			}

		})
	}
}

// testStreams returns IOStreams backed by buffers so builtin output can be
// asserted directly.
func testStreams() (IOStreams, *bytes.Buffer, *bytes.Buffer) {
	out := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	return IOStreams{In: strings.NewReader(""), Out: out, Err: errBuf}, out, errBuf
}

func TestCshell_Echo(t *testing.T) {
	cs := NewCshell()

	s, out, _ := testStreams()
	status := cs.echo([]string{"hello", "world"}, s)

	if out.String() != "hello world\n" {
		t.Errorf("expected %q, got %q", "hello world\n", out.String())
	}
	if status != 0 {
		t.Errorf("expected status 0, got %d", status)
	}
}

func TestCshell_EchoNoNewline(t *testing.T) {
	cs := NewCshell()

	s, out, _ := testStreams()
	cs.echo([]string{"-n", "hello"}, s)

	if out.String() != "hello" {
		t.Errorf("expected %q, got %q", "hello", out.String())
	}
}

func TestCshell_Pwd(t *testing.T) {
	cs := NewCshell()

	s, out, _ := testStreams()
	status := cs.pwd([]string{}, s)

	expected, _ := os.Getwd()
	if strings.TrimSpace(out.String()) != expected {
		t.Errorf("expected %q, got %q", expected, out.String())
	}
	if status != 0 {
		t.Errorf("expected status 0, got %d", status)
	}
}

func TestCshell_Type(t *testing.T) {
	cs := NewCshell()

	// Test builtin command
	s, out, _ := testStreams()
	status := cs.typeCmd([]string{"echo"}, s)

	if out.String() != "echo is a shell builtin\n" {
		t.Errorf("expected %q, got %q", "echo is a shell builtin\n", out.String())
	}
	if status != 0 {
		t.Errorf("expected status 0, got %d", status)
	}

	// Test non-existent command
	s, _, errBuf := testStreams()
	status = cs.typeCmd([]string{"nonexistentcommand"}, s)

	if !strings.Contains(errBuf.String(), "not found") {
		t.Errorf("expected 'not found' in stderr, got %q", errBuf.String())
	}
	if status != 1 {
		t.Errorf("expected status 1, got %d", status)
	}
}

func TestCshell_FindExecutable(t *testing.T) {
	cs := NewCshell()

	// Test finding a common executable (should exist on most systems)
	path, err := cs.findExecutable("ls")
	if err != nil {
		t.Logf("ls executable not found (may not exist on this system): %v", err)
	} else {
		if !strings.Contains(path, "ls") {
			t.Errorf("expected path to contain 'ls', got %q", path)
		}
	}

	// Test non-existent executable
	_, err = cs.findExecutable("definitelynotexistentcommand12345")
	if err == nil {
		t.Error("expected error for non-existent executable")
	}
}

func TestCshell_Register(t *testing.T) {
	cs := NewCshell()
	initialCount := len(cs.Commands)

	cs.Register("testcmd", func(args []string, s IOStreams) int { return 0 })

	if len(cs.Commands) != initialCount+1 {
		t.Errorf("expected %d commands, got %d", initialCount+1, len(cs.Commands))
	}

	if _, exists := cs.Commands["testcmd"]; !exists {
		t.Error("testcmd was not registered")
	}
}

func TestNewCshell(t *testing.T) {
	cs := NewCshell()

	expectedCommands := []string{"exit", "echo", "type", "pwd", "cd"}
	for _, cmd := range expectedCommands {
		if _, exists := cs.Commands[cmd]; !exists {
			t.Errorf("expected builtin command %q to be registered", cmd)
		}
	}
}

func TestCshell_Cd(t *testing.T) {
	cs := NewCshell()

	// restore the working directory so later tests are unaffected
	originalDir, _ := os.Getwd()
	defer os.Chdir(originalDir)

	// Test cd to home directory
	s, _, _ := testStreams()
	status := cs.cd([]string{}, s)
	currentDir, _ := os.Getwd()
	homeDir, _ := os.UserHomeDir()
	if currentDir != homeDir {
		t.Errorf("expected to be in home directory %q, got %q", homeDir, currentDir)
	}
	if status != 0 {
		t.Errorf("expected status 0, got %d", status)
	}

	// Test cd to non-existent directory
	s, _, errBuf := testStreams()
	status = cs.cd([]string{"/nonexistent/directory"}, s)

	if !strings.Contains(errBuf.String(), "No such file or directory") {
		t.Errorf("expected error message for non-existent directory, got %q", errBuf.String())
	}
	if status != 1 {
		t.Errorf("expected status 1, got %d", status)
	}
}
