package directivefile

import (
	"fmt"
	"strings"
)

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
