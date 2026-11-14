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
	nextTokenIndex int // 当前的 token 下标
}

// nextToken returns the next Token and its index.
// It returns -1 as the index when all tokens have been consumed.
func (p *parser) nextToken() (Token, int) {
	if p.nextTokenIndex >= len(p.tokens) {
		return Token{}, -1
	}
	index := p.nextTokenIndex
	p.nextTokenIndex++
	return p.tokens[index], index
}

// peekToken returns the next Token without consuming it.
func (p *parser) peekToken() (Token, int) {
	if p.nextTokenIndex >= len(p.tokens) {
		return Token{}, -1
	}
	return p.tokens[p.nextTokenIndex], p.nextTokenIndex
}

func (p *parser) skipNewLines() {
	for {
		tok, index := p.peekToken()
		if index == -1 || tok.Type != NewLine {
			return
		}
		p.nextToken()
	}
}
