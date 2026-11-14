package directivefile

import (
	"fmt"
	"os"
	"strings"
)

func parse(path string) (File, error) {
	return parseFile(path, Origin{})
}

func parseFile(path string, origin Origin) (File, error) {
	chain := append(append([]string(nil), origin.ImportChain...), path)
	if len(chain) > maxImportDepth {
		return File{}, fmt.Errorf("%s: syntax: import nesting exceeds %d levels: %s",
			path, maxImportDepth, strings.Join(chain, " -> "))
	}
	origin.ImportChain = chain

	input, err := os.ReadFile(path)
	if err != nil {
		return File{}, err
	}
	tokens, err := Tokenize(input, path)
	if err != nil {
		return File{}, err
	}

	p := &parser{tokens: tokens}

	directives, err := p.parseDirectives()
	if err != nil {
		return File{}, err
	}

	// expand template
	templates, directives, err := extractTemplates(directives)
	if err != nil {
		return File{}, err
	}

	if len(directives) == 0 && len(templates) == 0 {
		return File{}, fmt.Errorf("syntax: file %s must contain at least one directive", path)
	}

	directives, err = expandTemplates(directives, templates)
	if err != nil {
		return File{}, err
	}

	// expand import
	directives, err = expandImports(directives, path, origin)
	if err != nil {
		return File{}, err
	}

	// validate file
	err = validateFile(directives)
	if err != nil {
		return File{}, err
	}

	return File{Directives: directives}, nil
}

func validateFile(directives []Directive) error {
	return validateDirectives(directives, 1, Position{})
}

// pos 是这一层命令所属的父指令位置，只用于深度超限时给出定位。
func validateDirectives(directives []Directive, depth int, pos Position) error {
	if depth > maxDirectiveDepth {
		return fmt.Errorf(
			"%s: syntax: directive nesting depth exceeds %d",
			pos,
			maxDirectiveDepth,
		)
	}
	for _, d := range directives {
		switch {
		case d.Name == "import":
			return fmt.Errorf(
				"%s: internal error: expandImports left an import directive in the "+
					"tree (please report this as a bug)",
				d.Source(),
			)
		case d.Name == "tmpl":
			return fmt.Errorf(
				"%s: syntax: tmpl calls are not allowed inside a template body",
				d.Source(),
			)
		case isTemplateDirectiveName(d.Name):
			return fmt.Errorf(
				"%s: syntax: template definition %s is only allowed at the top level",
				d.Source(),
				d.Name,
			)
		}
		if len(d.SubDirectives) > 0 {
			if err := validateDirectives(d.SubDirectives, depth+1, d.Position); err != nil {
				return err
			}
		}
	}
	return nil
}

type parser struct {
	tokens         []Token
	nextTokenIndex int // 下一条待读 token 的下标；nextToken 会把它推进一位
}

// nextToken returns the next Token and whether one was available.
// ok is false once all tokens have been consumed.
//
// 第二个返回值不能省。Token 的零值是 {Type: UnquotedToken, Text: ""}，
// 而 UnquotedToken 就是 iota 的 0，零值与合法 token 无法区分——"读完了"
// 这件事必须靠另一个值表达。不要试图把它简化成单个返回值或换成 error。
func (p *parser) nextToken() (Token, bool) {
	if p.nextTokenIndex >= len(p.tokens) {
		return Token{}, false
	}
	tok := p.tokens[p.nextTokenIndex]
	p.nextTokenIndex++
	return tok, true
}

// peekToken returns the next Token without consuming it.
// ok is false once all tokens have been consumed; see nextToken for why the
// boolean is part of the signature.
func (p *parser) peekToken() (Token, bool) {
	if p.nextTokenIndex >= len(p.tokens) {
		return Token{}, false
	}
	return p.tokens[p.nextTokenIndex], true
}

func (p *parser) skipNewLines() {
	for {
		tok, ok := p.peekToken()
		if !ok || tok.Type != NewLine {
			return
		}
		p.nextToken()
	}
}
