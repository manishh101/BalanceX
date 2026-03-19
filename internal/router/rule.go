package router

import (
	"fmt"
	"strings"
	"unicode"
)

// TokenType represents a lexed token type.
type TokenType int

const (
	TokenEOF TokenType = iota
	TokenAnd
	TokenOr
	TokenLParen
	TokenRParen
	TokenMatcher
)

type Token struct {
	Type  TokenType
	Value string
}

type lexer struct {
	input string
	pos   int
}

func (l *lexer) skipWhitespace() {
	for l.pos < len(l.input) && unicode.IsSpace(rune(l.input[l.pos])) {
		l.pos++
	}
}

func (l *lexer) readMatcher() (Token, error) {
	start := l.pos
	inQuotes := false
	var quoteChar byte
	for l.pos < len(l.input) {
		c := l.input[l.pos]
		if inQuotes {
			if c == quoteChar {
				inQuotes = false
			}
		} else {
			if c == '\'' || c == '"' {
				inQuotes = true
				quoteChar = c
			} else if c == ')' {
				l.pos++ // consume ')'
				return Token{Type: TokenMatcher, Value: l.input[start:l.pos]}, nil
			} else if c == '(' || c == '&' || c == '|' {
				// We expect matchers to end with ')', so encountering another meta-character
				// without quotes before ')' is an error in matcher syntax.
				if c == '(' && l.pos == start {
					// wait, '(' at start means it's an expression paren, not a matcher
					break
				}
				// if we see '(' after an ident, it's the start of args, which is fine
			}
		}
		l.pos++
	}
	return Token{}, fmt.Errorf("unterminated matcher starting at position %d", start)
}

func (l *lexer) nextToken() (Token, error) {
	l.skipWhitespace()
	if l.pos >= len(l.input) {
		return Token{Type: TokenEOF}, nil
	}

	c := l.input[l.pos]
	if c == '(' {
		l.pos++
		return Token{Type: TokenLParen, Value: "("}, nil
	}
	if c == ')' {
		l.pos++
		return Token{Type: TokenRParen, Value: ")"}, nil
	}
	if c == '&' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '&' {
		l.pos += 2
		return Token{Type: TokenAnd, Value: "&&"}, nil
	}
	if c == '|' && l.pos+1 < len(l.input) && l.input[l.pos+1] == '|' {
		l.pos += 2
		return Token{Type: TokenOr, Value: "||"}, nil
	}

	if unicode.IsLetter(rune(c)) {
		return l.readMatcher()
	}

	return Token{}, fmt.Errorf("unexpected character '%c' at position %d", c, l.pos)
}

type parser struct {
	lex    *lexer
	curTok Token
}

func (p *parser) next() error {
	var err error
	p.curTok, err = p.lex.nextToken()
	return err
}

// ParseRule parses a routing expression and builds a MatcherFunc trees.
func ParseRule(rule string) (MatcherFunc, error) {
	p := &parser{lex: &lexer{input: rule}}
	if err := p.next(); err != nil {
		return nil, err
	}
	m, err := p.parseExpression()
	if err != nil {
		return nil, err
	}
	if p.curTok.Type != TokenEOF {
		return nil, fmt.Errorf("unexpected token '%s' at end of rule", p.curTok.Value)
	}
	return m, nil
}

// Expression -> Term ( "||" Term )*
func (p *parser) parseExpression() (MatcherFunc, error) {
	m, err := p.parseTerm()
	if err != nil {
		return nil, err
	}
	for p.curTok.Type == TokenOr {
		if err := p.next(); err != nil {
			return nil, err
		}
		right, err := p.parseTerm()
		if err != nil {
			return nil, err
		}
		m = MatchOr(m, right)
	}
	return m, nil
}

// Term -> Factor ( "&&" Factor )*
func (p *parser) parseTerm() (MatcherFunc, error) {
	m, err := p.parseFactor()
	if err != nil {
		return nil, err
	}
	for p.curTok.Type == TokenAnd {
		if err := p.next(); err != nil {
			return nil, err
		}
		right, err := p.parseFactor()
		if err != nil {
			return nil, err
		}
		m = MatchAnd(m, right)
	}
	return m, nil
}

// Factor -> Matcher | "(" Expression ")"
func (p *parser) parseFactor() (MatcherFunc, error) {
	if p.curTok.Type == TokenLParen {
		if err := p.next(); err != nil {
			return nil, err
		}
		m, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		if p.curTok.Type != TokenRParen {
			return nil, fmt.Errorf("expected ')', got '%s'", p.curTok.Value)
		}
		if err := p.next(); err != nil {
			return nil, err
		}
		return m, nil
	}

	if p.curTok.Type == TokenMatcher {
		m, err := createMatcher(p.curTok.Value)
		if err != nil {
			return nil, err
		}
		if err := p.next(); err != nil {
			return nil, err
		}
		return m, nil
	}

	return nil, fmt.Errorf("unexpected token '%s', expected a rule or '('", p.curTok.Value)
}

func parseArgs(s string) ([]string, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	
	var args []string
	var current []rune
	inQuotes := false
	var quoteChar rune

	for _, c := range s {
		if inQuotes {
			if c == quoteChar {
				inQuotes = false // end of quoted string
			} else {
				current = append(current, c) // inside string
			}
		} else {
			if c == '\'' || c == '"' {
				inQuotes = true
				quoteChar = c
			} else if c == ',' {
				args = append(args, strings.TrimSpace(string(current)))
				current = nil // reset for next arg
			} else {
				current = append(current, c)
			}
		}
	}
	if inQuotes {
		return nil, fmt.Errorf("unterminated string literal in arguments")
	}
	if len(current) > 0 {
		args = append(args, strings.TrimSpace(string(current)))
	}
	return args, nil
}

func createMatcher(s string) (MatcherFunc, error) {
	idx := strings.Index(s, "(")
	if idx == -1 || !strings.HasSuffix(s, ")") {
		return nil, fmt.Errorf("invalid matcher syntax: %s", s)
	}

	name := strings.TrimSpace(s[:idx])
	argsStr := strings.TrimSpace(s[idx+1 : len(s)-1])
	args, err := parseArgs(argsStr)
	if err != nil {
		return nil, err
	}

	switch name {
	case "PathPrefix":
		if len(args) != 1 {
			return nil, fmt.Errorf("PathPrefix expects 1 argument")
		}
		return MatchPathPrefix(args[0]), nil
	case "Path":
		if len(args) != 1 {
			return nil, fmt.Errorf("Path expects 1 argument")
		}
		return MatchPath(args[0]), nil
	case "Method":
		if len(args) != 1 {
			return nil, fmt.Errorf("Method expects 1 argument")
		}
		return MatchMethod(args[0]), nil
	case "Header":
		if len(args) != 2 {
			return nil, fmt.Errorf("Header expects 2 arguments")
		}
		return MatchHeader(args[0], args[1]), nil
	case "ClientIP":
		if len(args) != 1 {
			return nil, fmt.Errorf("ClientIP expects 1 argument")
		}
		return MatchClientIP(args[0]), nil
	default:
		return nil, fmt.Errorf("unknown matcher: %s", name)
	}
}
