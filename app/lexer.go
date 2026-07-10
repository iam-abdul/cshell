package main

import (
	"errors"
	"unicode"
)

type TokenType int

const (
	WORD                TokenType = iota
	PIPE                          // |
	LOGICAL_OR                    // ||
	AMPERSAND                     // &
	LOGICAL_AND                   // &&
	REDIRECT_OUT                  // >
	REDIRECT_OUT_APPEND           // >>
	REDIRECT_IN                   // <
	HEREDOC                       // <<
	REDIRECT_DUP_OUT              // >& as in 2>&1
	REDIRECT_DUP_IN               // <& as in 0<&3
	SEMICOLON                     // ;
)

type Token struct {
	Type  TokenType
	Value string
	Start int
	End   int
	// Quoted marks words with any quoted or escaped part; expansions like ~
	// must leave those alone (POSIX)
	Quoted bool
}

// Sentinel errors so the interactive loop can tell "keep reading, the quote
// isn't closed yet" (PS2 continuation) apart from real syntax errors.
var (
	ErrUnclosedSingleQuote = errors.New("single quote not closed")
	ErrUnclosedDoubleQuote = errors.New("double quote not closed")
)

func (cs *Cshell) peek() rune {
	if cs.Position+1 < len(cs.Input) {
		return rune(cs.Input[cs.Position+1])
	}
	return 0
}

func (cs *Cshell) currentChar() rune {
	return rune(cs.Input[cs.Position])
}

func (cs *Cshell) isValidEscapeInDoubleQuotes() bool {
	// If current char isn't a backslash, it's definitely not an escape
	if cs.currentChar() != '\\' {
		return false
	}

	next := cs.peek()

	// Inside double quotes, only these characters can be escaped.
	// Note: '0' is the null/EOF return from peek()
	switch next {
	case '$', '"', '\\', '`', '\n':
		return true
	default:
		return false
	}
}

func allDigits(rs []rune) bool {
	for _, r := range rs {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return len(rs) > 0
}

func (cs *Cshell) consumeWordLike() error {
	start := cs.Position
	end := 0

	// quoted becomes true once any part of this word was quoted or escaped.
	// POSIX only treats digits as a redirect's file descriptor when they are
	// unquoted: `2>f` redirects stderr, but `"2">f` passes "2" as an argument.
	quoted := false

	var value []rune

	emitWord := func() {
		cs.Tokens = append(cs.Tokens, Token{
			Type:   WORD,
			Start:  start,
			End:    end,
			Value:  string(value),
			Quoted: quoted,
		})
	}

	// flushBeforeOperator handles the word accumulated so far when an operator
	// begins. For redirects, an unquoted all-digits word directly before the
	// operator is its file descriptor (the 2 in 2>err.log), not an argument.
	flushBeforeOperator := func(isRedirect bool) (fd string, opStart int) {
		opStart = cs.Position
		if len(value) == 0 {
			return "", opStart
		}
		if isRedirect && !quoted && allDigits(value) {
			return string(value), start
		}
		emitWord()
		return "", opStart
	}

	for cs.Position < len(cs.Input) {
		switch cs.currentChar() {

		case '\\':
			quoted = true
			// consuming the global escape character
			next := cs.peek()
			cs.Position++
			// if the next character exists we add it to value literally
			if next != 0 {
				// if the word begins with escape character then we must adjust the start to point the start at the next (as the esc is not part of value)
				if len(value) == 0 {
					start = cs.Position
				}
				value = append(value, rune(next))
				cs.Position++
				end = cs.Position
			}

		case ' ', '\t', '\n':
			emitWord()
			return nil

		case ';':
			if len(value) > 0 {
				emitWord()
			}
			cs.Tokens = append(cs.Tokens, Token{
				Type:  SEMICOLON,
				Start: cs.Position,
				End:   cs.Position + 1,
				Value: ";",
			})
			cs.Position++
			return nil

		case '\'':
			quoted = true
			// consume opening quote
			if cs.Position == start {
				cs.Position++
				start = cs.Position
			} else {
				cs.Position++
			}

			closed := false
			for cs.Position < len(cs.Input) {
				if cs.currentChar() == '\'' {
					// do not consider the closing quote in end
					end = cs.Position

					cs.Position++ // consume closing quote
					closed = true
					break
				}
				value = append(value, cs.currentChar())
				cs.Position++
			}

			if !closed {
				return ErrUnclosedSingleQuote
			}

		case '"':
			quoted = true
			if cs.Position == start {
				cs.Position++
				start = cs.Position
			} else {
				cs.Position++
			}

			closed := false
			for cs.Position < len(cs.Input) {
				// handle the esc char inside quotes
				if cs.isValidEscapeInDoubleQuotes() {

					next := cs.peek()
					// consume the esc
					cs.Position++
					// add the next char as it is
					if next != 0 {
						value = append(value, next)
						// consume the added value
						cs.Position++
					}
					continue
				}
				if cs.currentChar() == '"' {
					// do not consider the ending quote
					end = cs.Position

					cs.Position++
					closed = true
					break
				}
				value = append(value, cs.currentChar())
				cs.Position++
			}

			if !closed {
				return ErrUnclosedDoubleQuote
			}

		case '|':
			_, opStart := flushBeforeOperator(false)

			// see if this is a logical OR
			nextChar := cs.peek()
			// consume the first pipe
			cs.Position++
			if nextChar == '|' {
				// consume the second | char
				cs.Position++
				cs.Tokens = append(cs.Tokens, Token{
					Type:  LOGICAL_OR,
					Start: opStart,
					End:   cs.Position,
					Value: "||",
				})
			} else {
				cs.Tokens = append(cs.Tokens, Token{
					Type:  PIPE,
					Start: opStart,
					End:   cs.Position,
					Value: "|",
				})
			}

			return nil

		case '&':
			_, opStart := flushBeforeOperator(false)

			// see if this is a logical AND
			nextChar := cs.peek()
			// consume the first ampersand
			cs.Position++
			if nextChar == '&' {
				// consume the second & char
				cs.Position++
				cs.Tokens = append(cs.Tokens, Token{
					Type:  LOGICAL_AND,
					Start: opStart,
					End:   cs.Position,
					Value: "&&",
				})
			} else {
				cs.Tokens = append(cs.Tokens, Token{
					Type:  AMPERSAND,
					Start: opStart,
					End:   cs.Position,
					Value: "&",
				})
			}

			return nil

		case '<':
			fd, opStart := flushBeforeOperator(true)

			next := cs.peek()
			// consume the '<'
			cs.Position++

			switch next {
			case '<':
				cs.Position++
				cs.Tokens = append(cs.Tokens, Token{
					Type:  HEREDOC,
					Start: opStart,
					End:   cs.Position,
					Value: fd + "<<",
				})
			case '&':
				cs.Position++
				cs.Tokens = append(cs.Tokens, Token{
					Type:  REDIRECT_DUP_IN,
					Start: opStart,
					End:   cs.Position,
					Value: fd + "<&",
				})
			default:
				cs.Tokens = append(cs.Tokens, Token{
					Type:  REDIRECT_IN,
					Start: opStart,
					End:   cs.Position,
					Value: fd + "<",
				})
			}

			return nil

		case '>':
			fd, opStart := flushBeforeOperator(true)

			next := cs.peek()
			// consume the '>'
			cs.Position++

			switch next {
			case '>':
				cs.Position++
				cs.Tokens = append(cs.Tokens, Token{
					Type:  REDIRECT_OUT_APPEND,
					Start: opStart,
					End:   cs.Position,
					Value: fd + ">>",
				})
			case '&':
				cs.Position++
				cs.Tokens = append(cs.Tokens, Token{
					Type:  REDIRECT_DUP_OUT,
					Start: opStart,
					End:   cs.Position,
					Value: fd + ">&",
				})
			default:
				cs.Tokens = append(cs.Tokens, Token{
					Type:  REDIRECT_OUT,
					Start: opStart,
					End:   cs.Position,
					Value: fd + ">",
				})
			}

			return nil

		default:
			value = append(value, cs.currentChar())
			cs.Position++
			end = cs.Position
		}
	}

	// append token at EOF
	emitWord()

	return nil
}

func (cs *Cshell) consumeSpaceTabLineChange() {
	for cs.Position < len(cs.Input) {
		switch cs.currentChar() {
		case ' ', '\t', '\n', '\r':
			cs.Position++
		default:
			return
		}
	}
}

func (cs *Cshell) lex() error {
	for cs.Position < len(cs.Input) {
		cs.consumeSpaceTabLineChange()

		if cs.Position >= len(cs.Input) {
			return nil
		}

		// # at the start of a word begins a comment (POSIX); mid-word it is
		// an ordinary character, so file#name survives
		if cs.currentChar() == '#' {
			for cs.Position < len(cs.Input) && cs.currentChar() != '\n' {
				cs.Position++
			}
			continue
		}

		if err := cs.consumeWordLike(); err != nil {
			return err
		}
	}
	return nil
}

// lexInput resets lexer state and tokenizes input. Useful on its own for
// tests; processInput builds on it.
func (cs *Cshell) lexInput(input string) error {
	cs.Input = input
	cs.Tokens = nil
	cs.Position = 0
	return cs.lex()
}
