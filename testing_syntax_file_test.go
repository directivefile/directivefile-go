package directivefile

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestParseFileGoodCases drives the file goodcases through Parse() and asserts
// the whole tree, position included.
//
// 1-simple-block.conf and 2-nested-block.conf are driven by
// TestParseDirectiveGoodCases instead; 3-backtick.conf and 4-comment.conf are not
// driven yet.
func TestParseFileGoodCases(t *testing.T) {
	t.Run("5-brace-in-quote.conf", func(t *testing.T) {
		const path = "testsdata/syntax/file/goodcases/5-brace-in-quote.conf"

		got, err := Parse(path)
		if err != nil {
			t.Fatalf("Parse() error = %v", err)
		}

		// A brace is structural only when it is a token of its own, so a `{` or
		// `}` inside quotes (either quoting kind) is an ordinary parameter and
		// never opens or closes a block.
		want := File{Directives: []Directive{{
			Name:     "command",
			Position: Position{Filename: path, Line: 1, Column: 1},
			SubDirectives: []Directive{
				{
					Name:       "arg",
					Parameters: []string{"{"},
					Position:   Position{Filename: path, Line: 2, Column: 5},
				},
				{
					Name:       "arg",
					Parameters: []string{"}"},
					Position:   Position{Filename: path, Line: 3, Column: 5},
				},
				{
					Name:       "arg",
					Parameters: []string{"{", "}"},
					Position:   Position{Filename: path, Line: 4, Column: 5},
				},
			},
		}}}

		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Parse():\n got: %#v\nwant: %#v", got, want)
		}
	})

	// A dot is a name character, not punctuation: `dns.forwarder` is ONE token
	// and a valid directive — at the top level and inside a block alike, and with
	// as many segments as the module id has.
	t.Run("6-dotted-directive.conf", func(t *testing.T) {
		const path = "testsdata/syntax/file/goodcases/6-dotted-directive.conf"

		got, err := Parse(path)
		if err != nil {
			t.Fatalf("Parse() error = %v", err)
		}

		want := File{Directives: []Directive{
			{
				Name:     "dns.forwarder",
				Position: Position{Filename: path, Line: 12, Column: 1},
				SubDirectives: []Directive{{
					Name:       "upstream",
					Parameters: []string{"1.1.1.1"},
					Position:   Position{Filename: path, Line: 13, Column: 5},
				}},
			},
			{
				Name:     "endpoint",
				Position: Position{Filename: path, Line: 16, Column: 1},
				SubDirectives: []Directive{{
					Name:     "endpoint.http.proxy",
					Position: Position{Filename: path, Line: 17, Column: 5},
					SubDirectives: []Directive{{
						Name:       "to",
						Parameters: []string{"127.0.0.1:8080"},
						Position:   Position{Filename: path, Line: 18, Column: 9},
					}},
				}},
			},
		}}

		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Parse():\n got: %#v\nwant: %#v", got, want)
		}
	})

	// A UTF-8 BOM is metadata, not content: it is dropped before lexing, so the
	// first directive starts at 1:1 and its name does not carry U+FEFF.
	t.Run("7-utf-8-with-bom", func(t *testing.T) {
		const path = "testsdata/syntax/file/goodcases/7-utf-8-with-bom.df"

		got, err := Parse(path)
		if err != nil {
			t.Fatalf("Parse() error = %v", err)
		}

		want := File{Directives: []Directive{{
			Name:       "server",
			Parameters: []string{"127.0.0.1:8080"},
			Position:   Position{Filename: path, Line: 1, Column: 1},
		}}}

		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Parse():\n got: %#v\nwant: %#v", got, want)
		}
	})
}

// TestParseFileBadCases drives every file badcase through Parse().
//
// The expected error is spelled out in full — message AND position — because
// where an error is reported is part of the contract. The path prefix is part
// of it too: it proves the error names the right file.
//
// The files are numbered by order, not by rule: 1-3 reject an ill-formed block
// (the three structural criteria in wiki/spec.md), 4-5 reject a brace that never
// became a structural token, 6 rejects a depth overflow, 7-other-encode-* rejects
// non-UTF-8 input (UTF-16LE, Shift-JIS, GBK).
func TestParseFileBadCases(t *testing.T) {
	// The `{` sits on a line of its own, so `dns` ends at the newline and the
	// `{` starts a directive — which fails the directive shape rule. This is
	// criterion 3: a block is the LAST ARGUMENT of its directive.
	t.Run("1-block-open-next-line", func(t *testing.T) {
		const path = "testsdata/syntax/file/badcases/1-block-open-next-line.conf"

		_, err := Parse(path)
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		wantErr := path + ":3:1: syntax: invalid directive name: {"
		if !strings.Contains(err.Error(), wantErr) {
			t.Fatalf("expected error containing %q, got %q", wantErr, err)
		}
	})

	// Criterion 1: `{` must be followed by a newline. The error points at the
	// token that should have been that newline — here the `}` of `{ }`, which is
	// why the column is 15 rather than the position of the `{` itself.
	t.Run("2-block-open-same-line", func(t *testing.T) {
		const path = "testsdata/syntax/file/badcases/2-block-open-same-line.conf"

		_, err := Parse(path)
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		wantErr := path + ":11:15: syntax: expected newline after '{' (block must start on a new line)"
		if !strings.Contains(err.Error(), wantErr) {
			t.Fatalf("expected error containing %q, got %q", wantErr, err)
		}
	})

	// Criterion 2: a block must hold at least one directive. Reported at the `{`
	// of the inner block, which is the empty one.
	t.Run("3-empty-block", func(t *testing.T) {
		const path = "testsdata/syntax/file/badcases/3-empty-block.conf"

		_, err := Parse(path)
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		wantErr := path + ":9:13: syntax: empty block is not allowed"
		if !strings.Contains(err.Error(), wantErr) {
			t.Fatalf("expected error containing %q, got %q", wantErr, err)
		}
	})

	// `{ "key1:val1" }` is all on one line inside a directive, so the `{` is
	// followed by a value rather than a newline.
	t.Run("4-brace-in-one-line-1", func(t *testing.T) {
		const path = "testsdata/syntax/file/badcases/4-brace-in-one-line-1.conf"

		_, err := Parse(path)
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		wantErr := path + ":2:22: syntax: expected newline after '{' (block must start on a new line)"
		if !strings.Contains(err.Error(), wantErr) {
			t.Fatalf("expected error containing %q, got %q", wantErr, err)
		}
	})

	// The same fault with no closing brace at all, so this one cannot be
	// mistaken for a block that merely starts on the wrong line.
	t.Run("5-brace-in-one-line-2", func(t *testing.T) {
		const path = "testsdata/syntax/file/badcases/5-brace-in-one-line-2.conf"

		_, err := Parse(path)
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		wantErr := path + ":1:7: syntax: expected newline after '{' (block must start on a new line)"
		if !strings.Contains(err.Error(), wantErr) {
			t.Fatalf("expected error containing %q, got %q", wantErr, err)
		}
	})

	// The position is the interesting half here. maxDirectiveDepth = 64 is counted
	// in parseSubDirectives, which is entered once per `{`, so the FIRST refused
	// brace is the 64th one — line 80, the `{` of `a63 {`. Pinning that line
	// pins the boundary from both sides: a limit of 63 would report line 79, a
	// limit of 65 would report nothing at all.
	t.Run("6-command-nesting-too-deep", func(t *testing.T) {
		const path = "testsdata/syntax/file/badcases/6-command-nesting-too-deep.conf"

		_, err := Parse(path)
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		wantErr := path + ":80:5: syntax: directive nesting depth exceeds 64"
		if !strings.Contains(err.Error(), wantErr) {
			t.Fatalf("expected error containing %q, got %q", wantErr, err)
		}
	})

	for _, name := range []string{
		"7-other-encode-1-utf16le.df",
		"7-other-encode-2-shift-jis.df",
		"7-other-encode-3-gbk.df",
	} {
		t.Run(strings.TrimSuffix(name, ".df"), func(t *testing.T) {
			path := filepath.Join("testsdata", "syntax", "file", "badcases", name)

			_, err := Parse(path)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if wantErr := path + ": invalid UTF-8"; err.Error() != wantErr {
				t.Fatalf("expected %q, got %q", wantErr, err)
			}
		})
	}
}
