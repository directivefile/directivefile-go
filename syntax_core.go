package directivefile

import (
	"fmt"
	"strconv"
	"strings"
)

type File struct {
	Directives []Directive
}

type Directive struct {
	Name          string
	Parameters    []string
	SubDirectives []Directive
	Origin        Origin
	Position      Position
}

// Origin describes how a directive was expanded (a tmpl call or an import).
// The zero value means the directive came directly from the parsed file.
type Origin struct {
	TmplPosition   Position // tmpl call site, if instantiated from a template
	TemplateName   string   // template name, if instantiated from a template
	ImportPosition Position // import directive that brought the file in, if imported
	ImportChain    []string // files traversed to reach the imported file
}

// HasSubDirectives reports whether the directive opens a block ({ ... }).
func (d Directive) HasSubDirectives() bool {
	return len(d.SubDirectives) > 0
}

func (d Directive) Source() string {
	var b strings.Builder

	if d.Origin.ImportPosition.IsValid() {
		b.WriteString("imported at ")
		b.WriteString(d.Origin.ImportPosition.String())

		if len(d.Origin.ImportChain) > 0 {
			b.WriteString(" (")
			b.WriteString(strings.Join(d.Origin.ImportChain, " -> "))
			b.WriteString(")")
		}

		if d.Origin.TmplPosition.IsValid() {
			b.WriteString("; instantiated from template ")
			b.WriteString(strconv.Quote(d.Origin.TemplateName))
			b.WriteString(" at ")
			b.WriteString(d.Origin.TmplPosition.String())
		}

		return b.String()
	}

	if d.Origin.TmplPosition.IsValid() {
		return fmt.Sprintf(
			"instantiated from template %s at %s",
			strconv.Quote(d.Origin.TemplateName),
			d.Origin.TmplPosition,
		)
	}

	return d.Position.String()
}

// maxDirectiveDepth is the maximum number of nested command levels.
// Depth starts at 0, so valid depths are 0 through 63.
const maxDirectiveDepth = 64

// parseDirectives parses the whole token stream into top-level commands.
// Every Position is already stamped by the lexer, so no path is needed here.
func (p *parser) parseDirectives() ([]Directive, error) {
	var directives []Directive

	for {
		tok, ok := p.peekToken()
		if !ok {
			break
		}

		if tok.Type == NewLine {
			p.nextToken()
			continue
		}

		d, err := p.parseDirective(0)
		if err != nil {
			return nil, err
		}

		directives = append(directives, d)
	}

	return directives, nil
}

// TODO: introduce a dedicated syntax-error type so every branch reports a
// Position. A few defensive EOF branches still lack a useful location.
func (p *parser) parseDirective(nesting int) (Directive, error) {
	var d Directive

	tok, ok := p.nextToken()
	if !ok {
		return d, fmt.Errorf("syntax: expected directive, got EOF")
	}
	if tok.Type != UnquotedToken {
		return d, fmt.Errorf("%s: syntax: expected directive (UnquotedToken), got %s", tok.Position, tok.Type)
	}
	if !validateDirectiveName(tok.Text) {
		return d, fmt.Errorf("%s: syntax: invalid directive name: %s", tok.Position, tok.Text)
	}
	d.Name = tok.Text
	d.Position = tok.Position

	for {
		tok, ok = p.peekToken()
		if !ok {
			return d, fmt.Errorf("%s: syntax: expected newline after directive", d.Position)
		}

		switch tok.Type {
		case UnquotedToken:
			switch tok.Text {
			case "{":
				leftBracePos := tok.Position

				p.nextToken()

				// Block must start on a new line.
				tok, ok = p.nextToken()
				if !ok {
					return d, fmt.Errorf("%s: syntax: unexpected EOF, expected newline after '{'", leftBracePos)
				}
				if tok.Type != NewLine {
					return d, fmt.Errorf("%s: syntax: expected newline after '{' (block must start on a new line)", tok.Position)
				}

				// Additional blank lines are allowed.
				p.skipNewLines()

				subDirectives, err := p.parseSubDirectives(nesting+1, leftBracePos)
				if err != nil {
					return d, err
				}
				d.SubDirectives = subDirectives

				// Next tok must be UnquotedToken and tok.Text = "}"
				tok, ok = p.nextToken()
				if !ok {
					return d, fmt.Errorf("syntax: unexpected EOF, expected '}'")
				}
				if tok.Type != UnquotedToken || tok.Text != "}" {
					return d, fmt.Errorf("%s: syntax: expected '}', got %q", tok.Position, tok.Text)
				}
				rightBracePos := tok.Position

				// Directive must end with at least one newline.
				tok, ok = p.nextToken()
				if !ok {
					return d, fmt.Errorf("%s: syntax: unexpected EOF, expected newline after '}'", rightBracePos)
				}
				if tok.Type != NewLine {
					return d, fmt.Errorf("%s: syntax: expected newline after '}'", tok.Position)
				}

				p.skipNewLines()

				return d, nil

			case "}":
				return d, fmt.Errorf("%s: syntax: unexpected '}'", tok.Position)

			default:
				p.nextToken()
				param, err := resolveParameter(tok)
				if err != nil {
					return d, err
				}
				d.Parameters = append(d.Parameters, param)
			}

		case DoubleQuotedToken, BacktickQuotedToken:
			p.nextToken()
			param, err := resolveParameter(tok)
			if err != nil {
				return d, err
			}
			d.Parameters = append(d.Parameters, param)

		case NewLine:
			p.nextToken()
			return d, nil

		default:
			return d, fmt.Errorf("%s: syntax: unexpected token %v", tok.Position, tok.Type)
		}
	}
}

func (p *parser) parseSubDirectives(nesting int, leftBracePos Position) ([]Directive, error) {
	if nesting >= maxDirectiveDepth {
		return nil, fmt.Errorf("%s: syntax: directive nesting depth exceeds %d",
			leftBracePos,
			maxDirectiveDepth)
	}

	var directives []Directive

	for {
		tok, ok := p.peekToken()
		if !ok {
			return nil, fmt.Errorf("%s: syntax: unexpected EOF, expected '}'", leftBracePos)
		}

		switch tok.Type {
		case NewLine:
			p.nextToken()

		case UnquotedToken:
			if tok.Text == "}" {
				if len(directives) == 0 {
					return nil, fmt.Errorf("%s: syntax: empty block is not allowed", leftBracePos)
				}
				return directives, nil
			}

			d, err := p.parseDirective(nesting)
			if err != nil {
				return nil, err
			}
			directives = append(directives, d)

		default:
			return nil, fmt.Errorf("%s: syntax: unexpected token %q", tok.Position, tok.Text)
		}
	}
}

func resolveParameter(tok Token) (string, error) {
	envName, err := parseEnvReference(tok.Text)
	if err != nil {
		return tok.Text, nil
	}

	value, err := lookupEnv(envName)
	if err != nil {
		return "", fmt.Errorf("%s: %w", tok.Position, err)
	}

	return value, nil
}

func validateDirectiveName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if !isDirectiveNameRune(r) {
			return false
		}
	}
	return true
}

// isDirectiveNameRune reports whether r may appear in a directive name.
//
// r must match:
//
//	[^\x00-\x20\x7F-\x{10FFFF}{}"`#]
//
// A printable ASCII character excluding {, }, ", `, and #.
func isDirectiveNameRune(r rune) bool {
	if r <= 0x20 || r >= 0x7F {
		return false
	}

	switch r {
	case LBRACE, RBRACE, DoubleQuote, BacktickQuote, HashTag:
		return false
	}

	return true
}
