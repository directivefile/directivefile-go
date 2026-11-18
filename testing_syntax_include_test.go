package directivefile

import (
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// fakeDirective is the semantic shape of a directive: no Position, no Origin.
// Include order is not part of the contract, so comparisons are done on a
// canonically sorted tree instead of on slice order.
type fakeDirective struct {
	Name          string
	Params        []string
	SubDirectives []fakeDirective
}

func toFakeDirectives(directives []Directive) []fakeDirective {
	if len(directives) == 0 {
		return nil
	}

	out := make([]fakeDirective, 0, len(directives))
	for _, d := range directives {
		out = append(out, fakeDirective{
			Name:          d.Name,
			Params:        append([]string(nil), d.Parameters...),
			SubDirectives: toFakeDirectives(d.SubDirectives),
		})
	}
	return out
}

// sortFakeDirectives sorts every sibling list by a canonical key. Children are
// normalized first so the key covers the whole subtree, not just this level.
func sortFakeDirectives(directives []fakeDirective) {
	for i := range directives {
		sortFakeDirectives(directives[i].SubDirectives)
	}
	sort.SliceStable(directives, func(i, j int) bool {
		return fakeDirectiveKey(directives[i]) < fakeDirectiveKey(directives[j])
	})
}

func fakeDirectiveKey(d fakeDirective) string {
	var b strings.Builder
	b.WriteString(d.Name)
	for _, p := range d.Params {
		b.WriteByte(0)
		b.WriteString(p)
	}
	for _, sub := range d.SubDirectives {
		b.WriteByte(1)
		b.WriteString(fakeDirectiveKey(sub))
	}
	return b.String()
}

func assertIncludeFile(t *testing.T, path string, want []fakeDirective) {
	t.Helper()

	file, err := Parse(filepath.FromSlash(path))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	got := toFakeDirectives(file.Directives)
	sortFakeDirectives(got)
	sortFakeDirectives(want)

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Parse():\n got: %#v\nwant: %#v", got, want)
	}
}

func assertIncludeError(t *testing.T, path string, wantSubstrings ...string) {
	t.Helper()

	_, err := Parse(path)
	if err == nil {
		t.Fatalf("Parse(%s): expected error, got nil", path)
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("expected error containing %q, got %q", want, err)
		}
	}
}

func TestExpandIncludesGoodCases(t *testing.T) {
	t.Parallel()

	t.Run("1-default", func(t *testing.T) {
		assertIncludeFile(t,
			"testsdata/syntax/include/goodcases/1-default.df",
			[]fakeDirective{
				{Name: "server", Params: []string{"127.0.0.1:8080"}}, // app.df
				{Name: "auto_router", Params: []string{"true"}},      // configfile
			},
		)
	})

	t.Run("2-explicit-file-extension", func(t *testing.T) {
		assertIncludeFile(t,
			"testsdata/syntax/include/goodcases/2-explicit-file-extension.df",
			[]fakeDirective{
				{Name: "max-connections", Params: []string{"100"}}, // app.conf
			},
		)
	})

	t.Run("3-multi-file-extension", func(t *testing.T) {
		// The union of ".df" and ".conf" keeps both matched files. The
		// extensionless configfile and the dot-file still lose: an explicit
		// filter removes the hidden-file exception, not the suffix requirement.
		assertIncludeFile(t,
			"testsdata/syntax/include/goodcases/3-multi-file-extension.df",
			[]fakeDirective{
				{Name: "server", Params: []string{"127.0.0.1:8080"}}, // app.df
				{Name: "max-connections", Params: []string{"100"}},   // app.conf
			},
		)
	})

	t.Run("4-include-all", func(t *testing.T) {
		// "*" implies recursion and drops every file filter: the hidden file,
		// the hidden directory and log/ are all included. The fixture has no
		// .gitignore or app.conf, so five files is the whole tree.
		assertIncludeFile(t,
			"testsdata/syntax/include/goodcases/4-include-all.df",
			[]fakeDirective{
				{Name: "max-mem", Params: []string{"100M"}},          // .hidden_configfile
				{Name: "PermitRootLogin", Params: []string{"yes"}},   // .sshd/config
				{Name: "server", Params: []string{"127.0.0.1:8080"}}, // app.df
				{Name: "auto_router", Params: []string{"true"}},      // configfile
				{Name: "loglevel", Params: []string{"INFO"}},         // log/configfile.df
			},
		)
	})

	t.Run("5-recursive-include-1", func(t *testing.T) {
		// "-r" with the default set descends into log/, but still skips the
		// hidden file and the hidden .sshd/ directory.
		assertIncludeFile(t,
			"testsdata/syntax/include/goodcases/5-recursive-include-1.df",
			[]fakeDirective{
				{Name: "server", Params: []string{"127.0.0.1:8080"}}, // app.df
				{Name: "auto_router", Params: []string{"true"}},      // configfile
				{Name: "output", Params: []string{"./app.log"}},      // log/configfile
				{Name: "loglevel", Params: []string{"INFO"}},         // log/configfile.df
			},
		)
	})

	t.Run("5-recursive-include-2", func(t *testing.T) {
		// An explicit ".df" filter still recurses, but only .df files survive:
		// the extensionless configfile, log/configfile and .hidden_configfile
		// are out, and the hidden .sshd/ directory is never entered.
		assertIncludeFile(t,
			"testsdata/syntax/include/goodcases/5-recursive-include-2.df",
			[]fakeDirective{
				{Name: "server", Params: []string{"127.0.0.1:8080"}}, // app.df
				{Name: "loglevel", Params: []string{"INFO"}},         // log/configfile.df
			},
		)
	})

	t.Run("6-suffix", func(t *testing.T) {
		// Every suffix here is legal under [A-Za-z0-9_-]+; x.plain is the
		// control file and must not match any of them.
		assertIncludeFile(t,
			"testsdata/syntax/include/goodcases/6-suffix.df",
			[]fakeDirective{
				{Name: "myconf2"},    // x.my-conf_2
				{Name: "v2x"},        // x.2x
				{Name: "underscore"}, // x._x
				{Name: "hyphen"},     // x.-x
				{Name: "mixed"},      // x.A1_b-2
			},
		)
	})

	t.Run("7-zero-match", func(t *testing.T) {
		// The folder exists but its only entry is the dot-file .keep, which the
		// default set skips: the include expands to nothing and stays silent.
		assertIncludeFile(t,
			"testsdata/syntax/include/goodcases/7-zero-match.df",
			nil,
		)
	})

	t.Run("8-template-block", func(t *testing.T) {
		// The include lives in a template body and only materialises when
		// "tmpl t" is called; it then resolves against main.df's directory and
		// imports conf/app.conf.
		assertIncludeFile(t,
			"testsdata/syntax/include/goodcases/8-template-block.df",
			[]fakeDirective{
				{Name: "server", Params: []string{"127.0.0.1:8080"}}, // conf/app.conf
			},
		)
	})

	t.Run("9-include-self", func(t *testing.T) {
		// "include ." scans main.df's own folder and collects main.df itself,
		// but toImportParams drops the including file: only the sibling
		// other.df is imported.
		assertIncludeFile(t,
			"testsdata/syntax/include/goodcases/9-include-self.df",
			[]fakeDirective{
				{Name: "other"}, // fixtures/self-1/other.df
			},
		)
	})

	t.Run("10-dotted-dir", func(t *testing.T) {
		// Dots in a directory name are path characters, not suffixes. Both
		// conf.config/ and conf.d/ are scanned, and each contributes its .df.
		assertIncludeFile(t,
			"testsdata/syntax/include/goodcases/10-dotted-dir.df",
			[]fakeDirective{
				{Name: "cmd"},  // conf.config/i.df
				{Name: "cmd2"}, // conf.d/h.df
			},
		)
	})

	t.Run("11-nested", func(t *testing.T) {
		// app.df itself contains an include: it is expanded while app.df is
		// parsed, resolves against app.df's directory, and contributes log.df
		// alongside app.df's own server command.
		assertIncludeFile(t,
			"testsdata/syntax/include/goodcases/11-nested.df",
			[]fakeDirective{
				{Name: "level", Params: []string{"INFO"}},          // log/log.df
				{Name: "server", Params: []string{"127.0.0.1:80"}}, // app.df
			},
		)
	})
}

func TestExpandIncludesCornerCases(t *testing.T) {
	t.Parallel()

	t.Run("1-include-all", func(t *testing.T) {
		// "*" dominates the legal ".df" suffix: the suffix is silently
		// discarded, so hidden files/directories and extensionless files are
		// included exactly as with a bare "*".
		assertIncludeFile(t,
			"testsdata/syntax/include/cornercases/1-include-all.df",
			[]fakeDirective{
				{Name: "max-mem", Params: []string{"100M"}},          // .hidden_configfile
				{Name: "PermitRootLogin", Params: []string{"yes"}},   // .sshd/config
				{Name: "server", Params: []string{"127.0.0.1:8080"}}, // app.df
				{Name: "auto_router", Params: []string{"true"}},      // configfile
				{Name: "loglevel", Params: []string{"INFO"}},         // log/configfile.df
			},
		)
	})

	t.Run("1-include-all-deplicated-stars", func(t *testing.T) {
		// A second "*" is idempotent: it only sets the same "all files" mode
		// again, so this is accepted and yields the same five directives.
		assertIncludeFile(t,
			"testsdata/syntax/include/cornercases/1-include-all-deplicated-stars.df",
			[]fakeDirective{
				{Name: "max-mem", Params: []string{"100M"}},          // .hidden_configfile
				{Name: "PermitRootLogin", Params: []string{"yes"}},   // .sshd/config
				{Name: "server", Params: []string{"127.0.0.1:8080"}}, // app.df
				{Name: "auto_router", Params: []string{"true"}},      // configfile
				{Name: "loglevel", Params: []string{"INFO"}},         // log/configfile.df
			},
		)
	})

	t.Run("1-include-all-recursive", func(t *testing.T) {
		// "-r" together with "*" is redundant: "*" already implies recursion,
		// and -r does not change filtering. Same five directives as above.
		assertIncludeFile(t,
			"testsdata/syntax/include/cornercases/1-include-all-recursive.df",
			[]fakeDirective{
				{Name: "max-mem", Params: []string{"100M"}},          // .hidden_configfile
				{Name: "PermitRootLogin", Params: []string{"yes"}},   // .sshd/config
				{Name: "server", Params: []string{"127.0.0.1:8080"}}, // app.df
				{Name: "auto_router", Params: []string{"true"}},      // configfile
				{Name: "loglevel", Params: []string{"INFO"}},         // log/configfile.df
			},
		)
	})

	t.Run("1-include-all-self", func(t *testing.T) {
		// main.df is the file containing the include, so it is dropped; every
		// other file in the tree, including .sshd/config, is imported.
		assertIncludeFile(t,
			"testsdata/syntax/include/cornercases/1-include-all-self.df",
			[]fakeDirective{
				{Name: "PermitRootLogin", Params: []string{"yes"}}, // self-2/.sshd/config
				{Name: "other"}, // self-2/other.df
				{Name: "app"},   // self-2/sub/sub.df
			},
		)
	})

	t.Run("2-case-sensitive-suffix", func(t *testing.T) {
		// ".df" is compared case-sensitively: UPPER.DF has extension ".DF"
		// and is skipped, even on a case-insensitive filesystem.
		assertIncludeFile(t,
			"testsdata/syntax/include/cornercases/2-case-sensitive-suffix.df",
			[]fakeDirective{
				{Name: "lower"}, // lower.df
			},
		)
	})
}

func TestExpandIncludesBadCases(t *testing.T) {
	t.Parallel()

	t.Run("1-include-file", func(t *testing.T) {
		path := filepath.FromSlash("testsdata/syntax/include/badcases/1-include-file.df")
		target := filepath.FromSlash("testsdata/syntax/include/fixtures/notdir.df")

		// include only accepts a folder. A path that resolves to a regular
		// file must be rejected while resolving the folder path, and the
		// error must point at the include line rather than at the file.
		assertIncludeError(t, path,
			path+":1:1",
			`syntax: config folder path "../fixtures/notdir.df" is not a directory`,
			fmt.Sprintf("resolved to %q", target),
		)
	})

	t.Run("2-invalid-suffix-1", func(t *testing.T) {
		path := filepath.FromSlash("testsdata/syntax/include/badcases/2-invalid-suffix-1.df")

		// "@" is outside [A-Za-z0-9_-], so ".@" is rejected before any file is
		// scanned; the error points at the include line.
		assertIncludeError(t, path,
			path+":1:1",
			`syntax: invalid file name extension ".@": suffix must match [A-Za-z0-9_-]+`,
		)
	})

	t.Run("2-invalid-suffix-2", func(t *testing.T) {
		path := filepath.FromSlash("testsdata/syntax/include/badcases/2-invalid-suffix-2.df")

		// "*" only voids LEGAL suffixes; it does not pardon an invalid token,
		// so ".@" still fails with the same validation error.
		assertIncludeError(t, path,
			path+":1:1",
			`syntax: invalid file name extension ".@": suffix must match [A-Za-z0-9_-]+`,
		)
	})

	t.Run("3-include-cycle", func(t *testing.T) {
		path := filepath.FromSlash("testsdata/syntax/include/badcases/3-include-cycle.df")
		self := filepath.FromSlash("testsdata/syntax/include/fixtures/cycle/self.df")

		// The include turns into an import of self.df, which imports itself.
		// The cycle closes on that inner import, so the error points at
		// self.df:3:1 and the chain lists the entry file, then self twice.
		assertIncludeError(t, path,
			self+":3:1",
			"syntax: import cycle:",
			path+" -> "+self+" -> "+self,
		)
	})

	t.Run("4-no-params", func(t *testing.T) {
		path := filepath.FromSlash("testsdata/syntax/include/badcases/4-no-params.df")

		// A bare include has no folder path at all; it is rejected before any
		// path resolution or filter validation happens.
		assertIncludeError(t, path,
			path+":1:1",
			"syntax: missing config folder path",
		)
	})

	t.Run("5-with-block", func(t *testing.T) {
		path := filepath.FromSlash("testsdata/syntax/include/badcases/5-with-block.df")

		// include expands to a list of directives, so there is no single
		// directive that could own a block; the block is rejected outright.
		assertIncludeError(t, path,
			path+":1:1",
			"syntax: include directive cannot have a block",
		)
	})

	t.Run("6-include-with-multi-recursive-flag-1", func(t *testing.T) {
		path := filepath.FromSlash("testsdata/syntax/include/badcases/6-include-with-multi-recursive-flag-1.df")

		// A second -r is rejected while scanning the parameters, before the
		// folder path is even resolved.
		assertIncludeError(t, path,
			path+":1:1",
			`syntax: "-r" may appear only once`,
		)
	})

	t.Run("6-invalid-position-recursive-flag-2", func(t *testing.T) {
		path := filepath.FromSlash("testsdata/syntax/include/badcases/6-invalid-position-recursive-flag-2.df")

		// -r between two suffixes is neither next to the path nor last, so it
		// is rejected instead of being silently treated as a filter.
		assertIncludeError(t, path,
			path+":1:1",
			`syntax: "-r" must be right before or after config folder path, or the last parameter`,
		)
	})

	t.Run("7-not-found-path", func(t *testing.T) {
		path := filepath.FromSlash("testsdata/syntax/include/badcases/7-not-found-path.df")
		target := filepath.FromSlash("testsdata/syntax/include/fixtures/notfound")

		// The folder does not exist: the error is raised while resolving the
		// path, and the resolved target shows where the lookup actually went.
		assertIncludeError(t, path,
			path+":1:1",
			`syntax: config folder path "../fixtures/notfound" does not exist`,
			fmt.Sprintf("resolved to %q", target),
		)
	})

	t.Run("8-include-star-path", func(t *testing.T) {
		path := filepath.FromSlash("testsdata/syntax/include/badcases/8-include-star-path.df")

		// "*" in the folder_path slot is a reserved word, not a wildcard for
		// the current directory; the valid spelling is "include . *".
		assertIncludeError(t, path,
			path+":1:1",
			`"*" is reserved for "all files" and cannot be used as a config folder path`,
		)
	})
}
