package directivefile

import (
	"fmt"
	"path/filepath"
	"strings"
)

const maxImportDepth = 32

func expandImports(directives []Directive, path string, origin Origin) ([]Directive, error) {
	var result []Directive

	for _, d := range directives {
		if d.Name != "import" {
			subDirectives, err := expandImports(d.SubDirectives, path, origin)
			if err != nil {
				return nil, err
			}
			d.SubDirectives = subDirectives

			result = append(result, d)
			continue
		}

		if len(d.Parameters) == 0 {
			return nil, fmt.Errorf(
				"%s: syntax: import requires at least one parameter",
				d.Position,
			)
		}

		if d.HasSubDirectives() {
			return nil, fmt.Errorf(
				"%s: syntax: import directive cannot have a block",
				d.Position,
			)
		}

		var imported []Directive

		for _, param := range d.Parameters {
			candidate := filepath.Join(filepath.Dir(path), param)

			if onImportChain(origin.ImportChain, candidate) {
				chain := append(
					append([]string(nil), origin.ImportChain...),
					candidate,
				)

				return nil, fmt.Errorf(
					"%s: syntax: import cycle: %s",
					d.Position,
					formatImportChain(chain),
				)
			}

			childOrigin := Origin{
				ImportChain: append(
					[]string(nil),
					origin.ImportChain...,
				),
			}

			file, err := parseFile(candidate, childOrigin)
			if err != nil {
				return nil, err
			}

			if len(file.Directives) == 0 {
				return nil, fmt.Errorf(
					"%s: syntax: imported file %q must contain at least one directive",
					d.Position,
					candidate,
				)

			}

			chain := append(
				append([]string(nil), origin.ImportChain...),
				candidate,
			)

			origin := Origin{
				ImportPosition: d.Position,
				ImportChain:    chain,
			}
			setImportOrigin(file.Directives, origin)

			imported = append(imported, file.Directives...)
		}

		result = append(result, imported...)
	}

	return result, nil
}

func onImportChain(chain []string, path string) bool {
	for _, item := range chain {
		if item == path {
			return true
		}
	}

	return false
}

func formatImportChain(chain []string) string {
	return strings.Join(chain, " -> ")
}

func setImportOrigin(directives []Directive, origin Origin) {
	for i := range directives {
		if !directives[i].Origin.ImportPosition.IsValid() {
			directives[i].Origin.ImportPosition = origin.ImportPosition
			directives[i].Origin.ImportChain = origin.ImportChain
		}
		setImportOrigin(directives[i].SubDirectives, origin)
	}
}
