package directivefile

import (
	"fmt"
	"strings"
)

// findRecursiveFlag returns the index of "-r" in params.
// It returns -1 when "-r" is absent, and an error when it appears more than once
// or sits in a structurally illegal position.
//
// A legal index is 0, 1, or len(params)-1:
//   - 0:        "-r" is first
//   - 1:        config folder path before "-r"
//   - len-1:    "-r" is the last parameter.
func findRecursiveFlag(params []string) (int, error) {
	index := -1
	count := 0
	for i, p := range params {
		if p != "-r" {
			continue
		}
		count++
		if count > 1 {
			return -1, fmt.Errorf(`"-r" may appear only once`)
		}
		index = i
	}

	if index > 0 && index != 1 && index != len(params)-1 {
		return -1, fmt.Errorf(
			`"-r" must be right before or after config folder path, or the last parameter`)
	}

	return index, nil
}

func extractFileNameExtensions(filters []string) ([]string, error) {
	seen := make(map[string]struct{}, len(filters))
	out := make([]string, 0, len(filters))
	for _, f := range filters {
		if err := validateFileNameExtension(f); err != nil {
			return nil, err
		}
		if _, ok := seen[f]; ok {
			continue
		}
		seen[f] = struct{}{}
		out = append(out, f)
	}
	return out, nil
}

// A file name extension is "." followed by a suffix drawn from a subset
// of the POSIX portable filename character set: [A-Za-z0-9_-]+.
func validateFileNameExtension(extension string) error {
	const charset = "[A-Za-z0-9_-]+"

	ext, found := strings.CutPrefix(extension, ".")
	if !found || ext == "" {
		return fmt.Errorf(
			"invalid file name extension %q: must start with '.' followed by %s",
			extension, charset,
		)
	}

	for i := 0; i < len(ext); i++ {
		c := ext[i]
		if !(c >= 'A' && c <= 'Z' ||
			c >= 'a' && c <= 'z' ||
			c >= '0' && c <= '9' ||
			c == '_' || c == '-') {
			return fmt.Errorf(
				"invalid file name extension %q: suffix must match %s",
				extension, charset,
			)
		}
	}

	return nil
}
