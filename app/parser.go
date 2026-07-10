package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// AST node types. Parse returns one of these:
//
//	SimpleCommand   echo hi > out.txt
//	Pipeline        a | b | c
//	AndOr           a && b, a || b
//	List            a ; b ; c
type Node interface{}

// Redirect is one redirection attached to a simple command.
// FD is the file descriptor being redirected (the 2 in 2>err.log).
// For dup redirects (2>&1) Target holds the fd being duplicated ("1").
type Redirect struct {
	Op     TokenType
	FD     int
	Target string
}

type SimpleCommand struct {
	Args      []string
	Redirects []Redirect
}

type Pipeline struct {
	Cmds []*SimpleCommand
}

type AndOr struct {
	Op    TokenType // LOGICAL_AND or LOGICAL_OR
	Left  Node
	Right Node
}

type List struct {
	Items []Node
}

type Parser struct {
	tokens []Token
	pos    int
}

// Parse turns a token stream into an AST. Empty input yields (nil, nil).
func Parse(tokens []Token) (Node, error) {
	if len(tokens) == 0 {
		return nil, nil
	}
	p := &Parser{tokens: tokens}
	node, err := p.parseExpr(1)
	if err != nil {
		return nil, err
	}
	if !p.atEnd() {
		return nil, fmt.Errorf("syntax error near unexpected token %q", p.current().Value)
	}
	return node, nil
}

func (p *Parser) atEnd() bool {
	return p.pos >= len(p.tokens)
}

func (p *Parser) current() Token {
	return p.tokens[p.pos]
}

// bindingPower orders the shell's binary operators, loosest first.
// 0 means "not a binary operator".
func bindingPower(t TokenType) int {
	switch t {
	case SEMICOLON:
		return 1
	case LOGICAL_AND, LOGICAL_OR:
		return 2
	case PIPE:
		return 3
	default:
		return 0
	}
}

// parseExpr is a Pratt-style precedence climber. Atoms are simple commands;
// everything that combines them (| && || ;) is a left-associative binary
// operator ranked by bindingPower.
func (p *Parser) parseExpr(minBP int) (Node, error) {
	left, err := p.parseCommand()
	if err != nil {
		return nil, err
	}

	for !p.atEnd() {
		tok := p.current()

		if tok.Type == AMPERSAND {
			return nil, errors.New("background execution (&) is not supported yet")
		}

		bp := bindingPower(tok.Type)
		if bp == 0 {
			return nil, fmt.Errorf("syntax error near unexpected token %q", tok.Value)
		}
		if bp < minBP {
			// an outer parseExpr call owns this operator
			break
		}
		p.pos++ // consume the operator

		// a trailing semicolon terminates the list: `a ; b ;`
		if tok.Type == SEMICOLON && p.atEnd() {
			break
		}

		// bp+1 keeps same-precedence operators left-associative
		right, err := p.parseExpr(bp + 1)
		if err != nil {
			return nil, err
		}

		left, err = combine(tok.Type, left, right)
		if err != nil {
			return nil, err
		}
	}

	return left, nil
}

// combine folds `left op right` into a node, flattening pipelines and lists
// so `a | b | c` becomes one Pipeline instead of nested pairs.
func combine(op TokenType, left, right Node) (Node, error) {
	switch op {
	case PIPE:
		rcmd, rok := right.(*SimpleCommand)
		if !rok {
			return nil, errors.New("syntax error in pipeline")
		}
		if lp, ok := left.(*Pipeline); ok {
			lp.Cmds = append(lp.Cmds, rcmd)
			return lp, nil
		}
		lcmd, lok := left.(*SimpleCommand)
		if !lok {
			return nil, errors.New("syntax error in pipeline")
		}
		return &Pipeline{Cmds: []*SimpleCommand{lcmd, rcmd}}, nil

	case LOGICAL_AND, LOGICAL_OR:
		return &AndOr{Op: op, Left: left, Right: right}, nil

	case SEMICOLON:
		if ll, ok := left.(*List); ok {
			ll.Items = append(ll.Items, right)
			return ll, nil
		}
		return &List{Items: []Node{left, right}}, nil
	}
	return nil, fmt.Errorf("unexpected operator %d", op)
}

// parseCommand consumes one simple command: words and redirects in any order,
// stopping at the first binary operator. POSIX allows a command that is only
// redirects (`> file` truncates/creates file).
func (p *Parser) parseCommand() (Node, error) {
	cmd := &SimpleCommand{}

loop:
	for !p.atEnd() {
		tok := p.current()
		switch tok.Type {
		case WORD:
			value := tok.Value
			if !tok.Quoted {
				value = expandTilde(value)
			}
			cmd.Args = append(cmd.Args, value)
			p.pos++

		case REDIRECT_IN, REDIRECT_OUT, REDIRECT_OUT_APPEND, REDIRECT_DUP_OUT, REDIRECT_DUP_IN:
			r, err := p.parseRedirect(tok)
			if err != nil {
				return nil, err
			}
			cmd.Redirects = append(cmd.Redirects, *r)

		case HEREDOC:
			return nil, errors.New("heredocs (<<) are not supported yet")

		default:
			break loop
		}
	}

	if len(cmd.Args) == 0 && len(cmd.Redirects) == 0 {
		if p.atEnd() {
			return nil, errors.New("syntax error: unexpected end of input")
		}
		return nil, fmt.Errorf("syntax error near unexpected token %q", p.current().Value)
	}
	return cmd, nil
}

func (p *Parser) parseRedirect(tok Token) (*Redirect, error) {
	p.pos++ // consume the operator token

	fd := defaultFD(tok.Type)
	if digits := leadingDigits(tok.Value); digits != "" {
		n, err := strconv.Atoi(digits)
		if err != nil {
			return nil, fmt.Errorf("%s: bad file descriptor", digits)
		}
		fd = n
	}

	if p.atEnd() || p.current().Type != WORD {
		return nil, fmt.Errorf("syntax error: expected target after %q", tok.Value)
	}
	target := p.current().Value
	if !p.current().Quoted {
		target = expandTilde(target)
	}
	p.pos++

	if tok.Type == REDIRECT_DUP_OUT || tok.Type == REDIRECT_DUP_IN {
		if _, err := strconv.Atoi(target); err != nil {
			return nil, fmt.Errorf("%s: bad file descriptor", target)
		}
	}

	return &Redirect{Op: tok.Type, FD: fd, Target: target}, nil
}

func defaultFD(t TokenType) int {
	switch t {
	case REDIRECT_IN, REDIRECT_DUP_IN:
		return 0
	default:
		return 1
	}
}

// expandTilde rewrites a leading unquoted ~ or ~/ to the home directory.
// ~user is left alone (not supported yet), as is ~ anywhere but the front.
func expandTilde(word string) string {
	if word != "~" && !strings.HasPrefix(word, "~/") {
		return word
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return word
	}
	if word == "~" {
		return home
	}
	return home + word[1:]
}

func leadingDigits(s string) string {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return s[:i]
}
