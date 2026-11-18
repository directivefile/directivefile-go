package directivefile

import "fmt"

// templates holds the templates defined in a single file, keyed by name.
type templates map[string]*template

type template struct {
	Name          string      // template name, with the surrounding parentheses removed
	SubDirectives []Directive // template body; cloned per instantiation and filled via Slots

	// Arity is the number of arguments the template accepts: max(TemplateParam)+1.
	// It is not len(Slots), because one argument may appear in several placeholders.
	Arity int

	Slots []slot // placeholder bindings: where to write, and which argument supplies the value
}

const maxTemplateArity = 32

// slot marks one actual placeholder occurrence in a template body. The target
// command is addressed by an index path because SubDirectives is a slice of
// values;
type slot struct {
	Location      slotLocation // where to write the value
	TemplateParam int          // which tmpl argument supplies the value
}

// slotLocation is a placeholder's destination: walk SubDirectives by Path to a
// command, then select one of its parameters with ParamIdx.
type slotLocation struct {
	Path     []int // indexes from the SubDirectives root, one per nesting level
	ParamIdx int   // index into that command's Parameters, not the tmpl call's argument index
}

// expandTemplates replaces tmpl calls in place. It may overwrite the backing
// array, so callers MUST use the returned slice and must not rely on the input
// slice remaining unchanged.
func expandTemplates(directives []Directive, tmpls templates) ([]Directive, error) {
	for i := 0; i < len(directives); i++ {
		d := &directives[i]

		if d.Name == "tmpl" {
			if len(d.Parameters) == 0 {
				return nil, fmt.Errorf(
					"%s: syntax: tmpl requires a template name",
					d.Position,
				)
			}
			if d.HasSubDirectives() {
				return nil, fmt.Errorf(
					"%s: syntax: tmpl directive cannot have a block",
					d.Position,
				)
			}

			name := d.Parameters[0]
			tmpl, ok := tmpls[name]
			if !ok {
				return nil, fmt.Errorf(
					"%s: syntax: template %q not found in this file",
					d.Position,
					name,
				)
			}

			rendered, err := tmpl.render(d.Parameters[1:], Origin{
				TmplPosition: d.Position,
				TemplateName: name,
			})
			if err != nil {
				return nil, fmt.Errorf("%s: syntax: %w", d.Position, err)
			}

			directives = append(directives[:i], append(rendered, directives[i+1:]...)...)
			i += len(rendered) - 1
			continue
		}

		subDirectives, err := expandTemplates(d.SubDirectives, tmpls)
		if err != nil {
			return nil, err
		}
		d.SubDirectives = subDirectives
	}

	return directives, nil
}

func extractTemplates(directives []Directive) (templates, []Directive, error) {
	tmpls := make(templates)
	commands := make([]Directive, 0, len(directives))

	extractDirectiveName := func(s string) string {
		if s[0] != '(' || s[len(s)-1] != ')' {
			return ""
		}
		return s[1 : len(s)-1]
	}

	for _, d := range directives {
		name := extractDirectiveName(d.Name)
		if name == "" {
			commands = append(commands, d)
			continue
		}

		if len(d.Parameters) > 0 {
			return nil, nil, fmt.Errorf(
				"%s: syntax: template definition cannot have parameters",
				d.Position,
			)
		}

		if len(d.SubDirectives) == 0 {
			return nil, nil, fmt.Errorf(
				"%s: syntax: template %s requires a block",
				d.Position,
				name,
			)
		}

		if _, exists := tmpls[name]; exists {
			return nil, nil, fmt.Errorf(
				"%s: syntax: duplicate template: %s",
				d.Position,
				name,
			)
		}

		slots, arity, err := analyzeTemplateBody(d.SubDirectives, d.Position)
		if err != nil {
			return nil, nil, err
		}

		tmpls[name] = &template{
			Name:          name,
			SubDirectives: d.SubDirectives,
			Arity:         arity,
			Slots:         slots,
		}
	}

	return tmpls, commands, nil
}

func analyzeTemplateBody(directives []Directive, pos Position) ([]slot, int, error) {
	var slots []slot
	maxParam := -1

	var walk func([]Directive, []int) error

	walk = func(directives []Directive, path []int) error {
		for i, d := range directives {
			cmdPath := appendPath(path, i)

			// TmplBlockCommandDirective must be a normal directive.
			if isTemplateDirectiveName(d.Name) {
				return fmt.Errorf(
					"%s: template definition %s is not allowed in a template body",
					d.Position,
					d.Name,
				)
			}

			for paramIdx, value := range d.Parameters {
				param, ok, err := parseTemplateArg(value)
				if err != nil {
					return fmt.Errorf(
						"%s: syntax: %w",
						d.Position,
						err,
					)
				}
				if !ok {
					continue
				}

				slots = append(slots, slot{
					Location: slotLocation{
						Path:     cmdPath,
						ParamIdx: paramIdx,
					},
					TemplateParam: param,
				})

				if param > maxParam {
					maxParam = param
				}
			}

			if err := walk(d.SubDirectives, cmdPath); err != nil {
				return err
			}
		}

		return nil
	}

	if err := walk(directives, nil); err != nil {
		return nil, 0, err
	}

	if err := validateTemplateSlots(slots, maxParam+1); err != nil {
		return []slot{}, 0, fmt.Errorf("%s: syntax: %w", pos, err)
	}

	return slots, maxParam + 1, nil
}

// isTemplateDirectiveName reports whether an already-validated directive name
// has the template definition shape, `(name)`.
//
// This is a weaker check than validateDirectiveName: the name has already
// passed that validation, so only the parentheses shape matters here.
func isTemplateDirectiveName(s string) bool {
	if len(s) == 0 {
		return false
	}

	return s[0] == '(' && s[len(s)-1] == ')'
}

func appendPath(path []int, index int) []int {
	result := make([]int, len(path)+1)
	copy(result, path)
	result[len(path)] = index
	return result
}

func parseTemplateArg(s string) (param int, ok bool, err error) {
	if len(s) < 8 || s[:6] != "{args[" || s[len(s)-2] != ']' || s[len(s)-1] != '}' {
		return 0, false, nil
	}

	index := s[6 : len(s)-2]
	if index == "" || (len(index) > 1 && index[0] == '0') {
		return 0, false, fmt.Errorf("invalid template parameter: %s", s)
	}

	for i := 0; i < len(index); i++ {
		if index[i] < '0' || index[i] > '9' {
			return 0, false, fmt.Errorf("invalid template parameter: %s", s)
		}

		param = param*10 + int(index[i]-'0')
		// The caller derives Arity as param+1, so param must be less than
		// maxTemplateArity (equivalently, param+1 <= maxTemplateArity).
		if param >= maxTemplateArity {
			return 0, false, fmt.Errorf("%s exceeds the maximum of %d template parameters", s, maxTemplateArity)
		}
	}

	return param, true, nil
}

func validateTemplateSlots(slots []slot, arity int) error {
	if arity > maxTemplateArity {
		return fmt.Errorf("internal error: template arity %d exceeds the limit of %d (please report this as a bug)", arity, maxTemplateArity)
	}
	seen := make([]bool, arity)

	for _, sl := range slots {
		if sl.TemplateParam >= arity {
			return fmt.Errorf("internal error: slot uses template parameter %d, but arity is only %d (please report this as a bug)", sl.TemplateParam, arity)
		}
		seen[sl.TemplateParam] = true
	}

	for param, ok := range seen {
		if !ok {
			return fmt.Errorf("{args[%d]} is missing: placeholders must be numbered consecutively from 0", param)
		}
	}

	return nil
}

// TODO better error info
func (t *template) render(args []string, origin Origin) ([]Directive, error) {
	if len(args) != t.Arity {
		return nil, fmt.Errorf(
			"template %q expects %d parameters, got %d",
			t.Name,
			t.Arity,
			len(args),
		)
	}

	expanded := make([]Directive, len(t.SubDirectives))
	for i := range t.SubDirectives {
		expanded[i] = cloneDirective(t.SubDirectives[i], origin)
	}

	for _, sl := range t.Slots {
		if len(sl.Location.Path) == 0 || sl.Location.Path[0] < 0 || sl.Location.Path[0] >= len(expanded) {
			return nil, fmt.Errorf(
				"template %q: invalid slot path",
				t.Name,
			)
		}

		d := &expanded[sl.Location.Path[0]]
		for _, i := range sl.Location.Path[1:] {
			if i < 0 || i >= len(d.SubDirectives) {
				return nil, fmt.Errorf(
					"template %q: invalid slot path",
					t.Name,
				)
			}
			d = &d.SubDirectives[i] // lookup directive by path
		}

		if sl.Location.ParamIdx < 0 || sl.Location.ParamIdx >= len(d.Parameters) {
			return nil, fmt.Errorf(
				"template %q: slot parameter index out of range",
				t.Name,
			)
		}

		if sl.TemplateParam < 0 || sl.TemplateParam >= len(args) {
			return nil, fmt.Errorf(
				"template %q: slot parameter index out of range",
				t.Name,
			)
		}

		d.Parameters[sl.Location.ParamIdx] = args[sl.TemplateParam]
	}

	return expanded, nil
}

func cloneDirective(src Directive, origin Origin) Directive {
	dst := Directive{
		Name:       src.Name,
		Parameters: append([]string(nil), src.Parameters...),
		Origin:     origin,
		Position:   src.Position,
	}

	if len(src.SubDirectives) > 0 {
		dst.SubDirectives = make([]Directive, len(src.SubDirectives))
		for i := range src.SubDirectives {
			dst.SubDirectives[i] = cloneDirective(src.SubDirectives[i], origin)
		}
	}

	return dst
}
