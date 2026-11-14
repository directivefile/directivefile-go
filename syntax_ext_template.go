package directivefile

import "fmt"

// Templates 是「单个文件」内定义的模板集合
type Templates map[string]*Template

type Template struct {
	Name string      // 模板名：`(name)` 去掉括号后的部分
	Body []Directive // 模板体；展开时逐条克隆，再按 Slots 填参
	// Arity = max(TemplateParam) + 1，不是 len(Slots)：同一个占位符编号允许在体内出现多次。
	Arity int
	Slots []Slot // 体内每个占位符的绑定：写到哪个位置、用第几个参数填
}

const maxTemplateArity = 32

// Slot 标记体内一个真正的占位符位置。命令用「索引路径」定位，
// 因为 Body 是值切片，保存 *Directive 会随切片复制而失效。
type Slot struct {
	LocationPath      []int // 从 Body 根逐层的 SubDirectives 下标
	LocationParameter int   // 模板内命令的第几个 Arg，不是 tmpl 调用的第几个参数
	TemplateParam     int   // 使用第几个 tmpl 参数填写
}

// 递归遍历每一个 cmds 的 SubDirectives
// 如果 cmd.Name = "tmpl" 则尝试替换
// tmpl command 的第一个参数，用作 tmpl 查询名字，如果没有查到，则直接报错
// 查到 template 则调用 render 获取用以替换 []Directive
// 如果没有错误，则就地删除 tmpl command 就地插入 []Directive
//
// expandTemplates 就地替换 cmds 中的 tmpl 调用。
// 注意：它可能改写 cmds 的底层数组，**调用方必须使用返回值**，不能依赖入参保持不变。
func expandTemplates(cmds []Directive, templates Templates) ([]Directive, error) {
	for i := 0; i < len(cmds); i++ {
		cmd := &cmds[i]

		if cmd.Name == "tmpl" {
			if len(cmd.Parameters) == 0 {
				return nil, fmt.Errorf(
					"%s: syntax: tmpl requires a template name",
					cmd.Position,
				)
			}
			if cmd.HasSubDirectives() {
				return nil, fmt.Errorf(
					"%s: syntax: tmpl directive cannot have a block",
					cmd.Position,
				)
			}

			name := cmd.Parameters[0]
			tmpl, ok := templates[name]
			if !ok {
				return nil, fmt.Errorf(
					"%s: syntax: template %q not found in this file",
					cmd.Position,
					name,
				)
			}

			rendered, err := tmpl.render(cmd.Parameters[1:], Origin{
				TmplPosition: cmd.Position,
				TemplateName: name,
			})
			if err != nil {
				return nil, fmt.Errorf("%s: syntax: %w", cmd.Position, err)
			}

			cmds = append(cmds[:i], append(rendered, cmds[i+1:]...)...)
			i += len(rendered) - 1
			continue
		}

		subCommands, err := expandTemplates(cmd.SubDirectives, templates)
		if err != nil {
			return nil, err
		}
		cmd.SubDirectives = subCommands
	}

	return cmds, nil
}

func extractTemplates(cmds []Directive) (Templates, []Directive, error) {
	templates := make(Templates)
	commands := make([]Directive, 0, len(cmds))

	extractDirectiveName := func(s string) string {
		if s[0] != '(' || s[len(s)-1] != ')' {
			return ""
		}
		return s[1 : len(s)-1]
	}

	for _, cmd := range cmds {
		name := extractDirectiveName(cmd.Name)
		if name == "" {
			commands = append(commands, cmd)
			continue
		}

		if len(cmd.Parameters) > 0 {
			return nil, nil, fmt.Errorf(
				"%s: syntax: template definition cannot have parameters",
				cmd.Position,
			)
		}

		if len(cmd.SubDirectives) == 0 {
			return nil, nil, fmt.Errorf(
				"%s: syntax: template %s requires a block",
				cmd.Position,
				name,
			)
		}

		if _, exists := templates[name]; exists {
			return nil, nil, fmt.Errorf(
				"%s: syntax: duplicate template: %s",
				cmd.Position,
				name,
			)
		}

		slots, arity, err := analyzeTemplateBody(cmd.SubDirectives, cmd.Position)
		if err != nil {
			return nil, nil, err
		}

		templates[name] = &Template{
			Name:  name,
			Body:  cmd.SubDirectives,
			Arity: arity,
			Slots: slots,
		}
	}

	return templates, commands, nil
}

func analyzeTemplateBody(cmds []Directive, pos Position) ([]Slot, int, error) {
	var slots []Slot
	maxParam := -1

	var walk func([]Directive, []int) error

	walk = func(cmds []Directive, path []int) error {
		for i, cmd := range cmds {
			cmdPath := appendPath(path, i)

			// TmplBlockCommandDirective must be a normal directive.
			if isTemplateDirectiveName(cmd.Name) {
				return fmt.Errorf(
					"%s: template definition %s is not allowed in a template body",
					cmd.Position,
					cmd.Name,
				)
			}

			for argIndex, arg := range cmd.Parameters {
				param, ok, err := parseTemplateArg(arg)
				if err != nil {
					return fmt.Errorf(
						"%s: syntax: %w",
						cmd.Position,
						err,
					)
				}
				if !ok {
					continue
				}

				slots = append(slots, Slot{
					LocationPath:      cmdPath,
					LocationParameter: argIndex,
					TemplateParam:     param,
				})

				if param > maxParam {
					maxParam = param
				}
			}

			if err := walk(cmd.SubDirectives, cmdPath); err != nil {
				return err
			}
		}

		return nil
	}

	if err := walk(cmds, nil); err != nil {
		return nil, 0, err
	}

	if err := validateTemplateSlots(slots, maxParam+1); err != nil {
		return []Slot{}, 0, fmt.Errorf("%s: syntax: %w", pos, err)
	}

	return slots, maxParam + 1, nil
}

// isTemplateDirectiveName reports whether the already-validated directive name
// uses the template definition shape, `(name)`.
//
// 这是 validateDirectiveName 的弱化版：能走到这里的名字已经过了那道校验，
// 所以只需判形状，不用再管长度、码表与点号分段。
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
		// 调用方会 +1 得到 Arity，所以 param 必须 < maxTemplateArity，
		// 等价于 param+1 <= maxTemplateArity。
		if param >= maxTemplateArity {
			return 0, false, fmt.Errorf("%s exceeds the maximum of %d template parameters", s, maxTemplateArity)
		}
	}

	return param, true, nil
}

func validateTemplateSlots(slots []Slot, arity int) error {
	if arity > maxTemplateArity {
		return fmt.Errorf("internal error: template arity %d exceeds the limit of %d (please report this as a bug)", arity, maxTemplateArity)
	}
	seen := make([]bool, arity)

	for _, slot := range slots {
		if slot.TemplateParam >= arity {
			return fmt.Errorf("internal error: slot uses template parameter %d, but arity is only %d (please report this as a bug)", slot.TemplateParam, arity)
		}
		seen[slot.TemplateParam] = true
	}

	for param, ok := range seen {
		if !ok {
			return fmt.Errorf("{args[%d]} is missing: placeholders must be numbered consecutively from 0", param)
		}
	}

	return nil
}

// TODO better error info
func (t *Template) render(args []string, origin Origin) ([]Directive, error) {
	if len(args) != t.Arity {
		return nil, fmt.Errorf(
			"template %q expects %d parameters, got %d",
			t.Name,
			t.Arity,
			len(args),
		)
	}

	body := make([]Directive, len(t.Body))
	for i := range t.Body {
		body[i] = cloneDirective(t.Body[i], origin)
	}

	for _, slot := range t.Slots {
		if len(slot.LocationPath) == 0 || slot.LocationPath[0] < 0 || slot.LocationPath[0] >= len(body) {
			return nil, fmt.Errorf(
				"template %q: invalid slot path",
				t.Name,
			)
		}

		cmd := &body[slot.LocationPath[0]]
		for _, i := range slot.LocationPath[1:] {
			if i < 0 || i >= len(cmd.SubDirectives) {
				return nil, fmt.Errorf(
					"template %q: invalid slot path",
					t.Name,
				)
			}
			cmd = &cmd.SubDirectives[i]
		}

		if slot.LocationParameter < 0 || slot.LocationParameter >= len(cmd.Parameters) {
			return nil, fmt.Errorf(
				"template %q: slot parameter index out of range",
				t.Name,
			)
		}

		if slot.TemplateParam < 0 || slot.TemplateParam >= len(args) {
			return nil, fmt.Errorf(
				"template %q: slot parameter index out of range",
				t.Name,
			)
		}

		cmd.Parameters[slot.LocationParameter] = args[slot.TemplateParam]
	}

	return body, nil
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
