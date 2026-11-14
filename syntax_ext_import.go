package directivefile

import (
	"fmt"
	"path/filepath"
	"strings"
)

const maxImportDepth = 32

func expandImports(cmds []Directive, path string, origin Origin) ([]Directive, error) {
	var result []Directive

	for _, cmd := range cmds {
		if cmd.Name != "import" {
			subCommands, err := expandImports(cmd.SubDirectives, path, origin)
			if err != nil {
				return nil, err
			}
			cmd.SubDirectives = subCommands

			result = append(result, cmd)
			continue
		}

		if len(cmd.Parameters) == 0 {
			return nil, fmt.Errorf(
				"%s: syntax: import requires at least one parameter",
				cmd.Position,
			)
		}

		if cmd.HasSubDirectives() {
			return nil, fmt.Errorf(
				"%s: syntax: import directive cannot have a block",
				cmd.Position,
			)
		}

		var imported []Directive

		for _, arg := range cmd.Parameters {
			candidate := filepath.Join(filepath.Dir(path), arg)

			if onImportChain(origin.ImportChain, candidate) {
				chain := append(
					append([]string(nil), origin.ImportChain...),
					candidate,
				)

				return nil, fmt.Errorf(
					"%s: syntax: import cycle: %s",
					cmd.Position,
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
					cmd.Position,
					candidate,
				)

			}

			chain := append(
				append([]string(nil), origin.ImportChain...),
				candidate,
			)

			origin := Origin{
				ImportPosition: cmd.Position,
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

func setImportOrigin(cmds []Directive, origin Origin) {
	for i := range cmds {
		if !cmds[i].Origin.ImportPosition.IsValid() {
			cmds[i].Origin.ImportPosition = origin.ImportPosition
			cmds[i].Origin.ImportChain = origin.ImportChain
		}
		setImportOrigin(cmds[i].SubDirectives, origin)
	}
}
