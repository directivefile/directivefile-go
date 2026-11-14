package directivefile

import (
	"reflect"
	"strings"
	"testing"
)

// TODO: badcases still missing: duplicate definition, definition without a
// block, malformed index ({args[foo]} / {args[01]} / {args[0]}{args[1]}), and a
// definition nested inside a block.

// extractTemplateTestdata drives one template fixture through the front half of
// the pipeline — read file → lex → parse commands → extract templates — so the
// fixture is both the input and the source of every Position.
//
// It deliberately stops BEFORE parseFile's expansion steps: once parseFile runs
// extractTemplates itself, the template table is no longer observable, and the
// Slots/Arity assertions below would have nothing to check.
func extractTemplateTestdata(t *testing.T, path string) (Templates, []Directive) {
	t.Helper()

	tokens, err := Tokenize(readTestdata(t, path), path)
	if err != nil {
		t.Fatalf("Tokenize() error = %v", err)
	}

	p := &parser{tokens: tokens}
	cmds, err := p.parseDirectives()
	if err != nil {
		t.Fatalf("parseDirectives() error = %v", err)
	}

	templates, commands, err := extractTemplates(cmds)
	if err != nil {
		t.Fatalf("extractTemplates() error = %v", err)
	}
	return templates, commands
}

func TestExtractTemplatesGoodCases(t *testing.T) {
	t.Run("1-extract-and-keep-commands", func(t *testing.T) {
		const path = "testsdata/syntax/template/goodcases/1-extract-and-keep-commands.conf"
		templates, commands := extractTemplateTestdata(t, path)

		// The definition left the tree; the ordinary command stayed.
		wantCommands := []Directive{
			{
				Name:       "dns",
				Parameters: []string{"example.com"},
				Position:   Position{Filename: path, Line: 1, Column: 1},
			},
		}
		if !reflect.DeepEqual(commands, wantCommands) {
			t.Fatalf("commands:\n got: %#v\nwant: %#v", commands, wantCommands)
		}

		if len(templates) != 1 {
			t.Fatalf("templates = %d entries, want 1", len(templates))
		}
		tmpl, ok := templates["proxy"]
		if !ok {
			t.Fatal(`template "proxy" not found`)
		}

		want := &Template{
			Name:  "proxy",
			Arity: 2,
			Body: []Directive{
				{
					Name:       "forward",
					Parameters: []string{"{args[0]}", "{args[1]}", "{args[0]}"},
					Position:   Position{Filename: path, Line: 4, Column: 5},
				},
				{
					Name:       "header",
					Parameters: []string{"{args[1]}"},
					Position:   Position{Filename: path, Line: 5, Column: 5},
				},
			},
			// {args[0]} appears twice in "forward", and the body keeps BOTH:
			// Param 0 is a parameter, not a slot identity.
			Slots: []Slot{
				{LocationPath: []int{0}, LocationParameter: 0, TemplateParam: 0},
				{LocationPath: []int{0}, LocationParameter: 1, TemplateParam: 1},
				{LocationPath: []int{0}, LocationParameter: 2, TemplateParam: 0},
				{LocationPath: []int{1}, LocationParameter: 0, TemplateParam: 1},
			},
		}
		if !reflect.DeepEqual(tmpl, want) {
			t.Fatalf("template:\n got: %#v\nwant: %#v", tmpl, want)
		}
	})

	t.Run("2-nested-body-and-no-args", func(t *testing.T) {
		const path = "testsdata/syntax/template/goodcases/2-nested-body-and-no-args.conf"
		templates, commands := extractTemplateTestdata(t, path)

		// The file holds definitions only, so nothing reaches the command tree.
		if len(commands) != 0 {
			t.Fatalf("commands: want none, got %#v", commands)
		}

		if len(templates) != 2 {
			t.Fatalf("templates = %d entries, want 2", len(templates))
		}

		// A placeholder one level down must carry a two-element LocationPath: the
		// distinction between a body index and an arg index is the whole point
		// of Slot.Path, and getting it wrong only shows up when nesting.
		wantSite := &Template{
			Name:  "site",
			Arity: 2,
			Body: []Directive{
				{
					Name:       "route",
					Parameters: []string{"/api"},
					Position:   Position{Filename: path, Line: 2, Column: 5},
					SubDirectives: []Directive{
						{
							Name:       "rewrite",
							Parameters: []string{"{args[0]}"},
							Position:   Position{Filename: path, Line: 3, Column: 9},
						},
						{
							Name:       "upstream",
							Parameters: []string{"{args[1]}", "{args[0]}"},
							Position:   Position{Filename: path, Line: 4, Column: 9},
						},
					},
				},
			},
			Slots: []Slot{
				{LocationPath: []int{0, 0}, LocationParameter: 0, TemplateParam: 0},
				{LocationPath: []int{0, 1}, LocationParameter: 0, TemplateParam: 1},
				{LocationPath: []int{0, 1}, LocationParameter: 1, TemplateParam: 0},
			},
		}
		if got := templates["site"]; !reflect.DeepEqual(got, wantSite) {
			t.Fatalf("template site:\n got: %#v\nwant: %#v", got, wantSite)
		}

		// No placeholders at all: arity 0 and no slots.
		wantNoParameters := &Template{
			Name:  "no-args",
			Arity: 0,
			Body: []Directive{
				{
					Name:     "logging",
					Position: Position{Filename: path, Line: 9, Column: 5},
				},
			},
		}
		if got := templates["no-args"]; !reflect.DeepEqual(got, wantNoParameters) {
			t.Fatalf("template no-args:\n got: %#v\nwant: %#v", got, wantNoParameters)
		}
	})
}

// TestExpandTemplatesBadCases drives every template badcase through parse().
// The expected error is spelled out in full — message AND position — because
// WHERE a template error is reported is part of the contract: problems in the
// definition are reported at the definition, problems in a call at the call.
func TestExpandTemplatesBadCases(t *testing.T) {
	const dir = "testsdata/syntax/template/badcases/"

	cases := []struct {
		file    string
		wantErr string
	}{
		{
			"1-definition-with-args.conf",
			":7:1: syntax: template definition cannot have parameters",
		},
		{
			"2-call-with-block.conf",
			":12:1: syntax: tmpl command cannot have a block",
		},
		{
			"3-slot-gap.conf",
			":7:1: syntax: template parameter 1 is missing",
		},
		{
			"4-call-too-few-args.conf",
			":12:1: syntax: template \"proxy\" expects 2 parameters, got 1",
		},
		{
			"5-call-too-many-args.conf",
			":12:1: syntax: template \"proxy\" expects 2 parameters, got 3",
		},
		{
			"6-call-undefined-template.conf",
			":11:1: syntax: template \"missing\" not found in this file",
		},
		{
			"7-template-argument-index-too-large.conf",
			":17:5: syntax: template parameter index is too large: {args[32]}",
		},
	}

	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			path := dir + tc.file

			_, err := parse(path)
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			// The path prefix proves the error names the right file.
			wantErr := path + tc.wantErr
			if !strings.Contains(err.Error(), wantErr) {
				t.Fatalf("expected error containing %q, got %q", wantErr, err)
			}
		})
	}
}

func TestTemplateRenderGoodCases(t *testing.T) {
	// The origin is the CALL SITE, not the definition: render stamps it onto
	// every command it produces, including nested SubDirectives.
	callSite := Origin{
		TmplPosition: Position{Filename: "main.conf", Line: 7, Column: 3},
		TemplateName: "tmpl_1",
	}

	t.Run("test 1", func(t *testing.T) {
		template := Template{
			Name:  "tmpl_1",
			Arity: 1,
			Body: []Directive{
				{
					Name:       "cmd",
					Parameters: []string{"{args[0]}"},
				},
			},
			Slots: []Slot{
				{LocationPath: []int{0}, LocationParameter: 0, TemplateParam: 0},
			},
		}

		got, err := template.render([]string{"hello"}, callSite)
		if err != nil {
			t.Fatal(err)
		}

		want := []Directive{
			{
				Name:       "cmd",
				Parameters: []string{"hello"},
				Origin:     callSite,
			},
		}

		if !reflect.DeepEqual(got, want) {
			t.Fatalf("unexpected result:\n got: %#v\nwant: %#v", got, want)
		}
	})

	t.Run("nested commands and repeated parameters", func(t *testing.T) {
		origin := Origin{
			TmplPosition: Position{Filename: "main.conf", Line: 12, Column: 1},
			TemplateName: "proxy",
		}

		template := Template{
			Name:  "proxy",
			Arity: 2,
			Body: []Directive{
				{
					Name:       "proxy",
					Parameters: []string{"http"},
					SubDirectives: []Directive{
						{
							Name:       "listen",
							Parameters: []string{"{args[0]}"},
						},
						{
							Name:       "upstream",
							Parameters: []string{"{args[1]}", "{args[0]}"},
						},
					},
				},
			},
			Slots: []Slot{
				{LocationPath: []int{0, 0}, LocationParameter: 0, TemplateParam: 0},
				{LocationPath: []int{0, 1}, LocationParameter: 0, TemplateParam: 1},
				{LocationPath: []int{0, 1}, LocationParameter: 1, TemplateParam: 0},
			},
		}

		got, err := template.render([]string{
			"127.0.0.1:8080",
			"example.com:443",
		}, origin)
		if err != nil {
			t.Fatal(err)
		}

		want := []Directive{
			{
				Name:       "proxy",
				Parameters: []string{"http"},
				SubDirectives: []Directive{
					{
						Name:       "listen",
						Parameters: []string{"127.0.0.1:8080"},
						Origin:     origin,
					},
					{
						Name:       "upstream",
						Parameters: []string{"example.com:443", "127.0.0.1:8080"},
						Origin:     origin,
					},
				},
				Origin: origin,
			},
		}

		if !reflect.DeepEqual(got, want) {
			t.Fatalf("unexpected result:\n got: %#v\nwant: %#v", got, want)
		}
	})
}
