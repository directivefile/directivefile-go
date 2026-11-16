package directivefile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type includeOptions struct {
	ConfigFolderPath string
	Recursive        bool
	Filter           filterMode
	Filters          []string
}

type filterMode uint8

const (
	filterDefault filterMode = iota
	filterListed
	filterAll
)

func parseIncludeParams(params []string, currentDir string) (includeOptions, error) {
	if len(params) == 0 {
		return includeOptions{}, fmt.Errorf("missing config folder path")
	}

	var opts includeOptions
	cleanParams := params

	if rIndex, err := findRecursiveFlag(params); err != nil {
		return includeOptions{}, err
	} else if rIndex >= 0 {
		opts.Recursive = true
		cleanParams = make([]string, 0, len(params)-1)
		cleanParams = append(cleanParams, params[:rIndex]...)
		cleanParams = append(cleanParams, params[rIndex+1:]...)
	}

	if len(cleanParams) == 0 {
		return includeOptions{}, fmt.Errorf("missing config folder path")
	}

	configFolderPath, err := extractConfigFolderPath(cleanParams[0], currentDir)
	if err != nil {
		return includeOptions{}, err
	}
	opts.ConfigFolderPath = configFolderPath

	includeAll := false
	var rawFilters []string
	for _, p := range cleanParams[1:] {
		if p == "*" {
			includeAll = true
		} else {
			rawFilters = append(rawFilters, p)
		}
	}

	filters, err := extractFileNameExtensions(rawFilters)
	if err != nil {
		return includeOptions{}, err
	}
	opts.Filters = filters

	switch {
	case includeAll:
		opts.Filter = filterAll
	case len(opts.Filters) > 0:
		opts.Filter = filterListed
	}

	return opts, nil
}

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

func extractConfigFolderPath(path, currentDir string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("missing config folder path")
	}
	if path == "*" {
		return "", fmt.Errorf(
			`"*" is reserved for "all files" and cannot be used as a config folder path`)
	}
	if filepath.IsAbs(path) {
		return "", fmt.Errorf(
			"config folder path %q must be relative to %q", path, currentDir)
	}

	cleaned := filepath.Clean(path)
	target := filepath.Join(currentDir, cleaned)

	info, err := os.Stat(target)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf(
				"config folder path %q does not exist (resolved to %q)", path, target)
		}
		return "", fmt.Errorf("config folder path %q: %w", target, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf(
			"config folder path %q is not a directory (resolved to %q)", path, target)
	}

	return target, nil
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

func collectIncludeFiles(opts includeOptions) ([]string, error) {
	dir := opts.ConfigFolderPath

	suffixes := make(map[string]struct{}, len(opts.Filters))
	for _, f := range opts.Filters {
		suffixes[f] = struct{}{}
	}

	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if d.IsDir() {
			// Folder paths have no extension: directories are never filtered.
			switch {
			case filepath.Clean(p) == filepath.Clean(dir):
				return nil // the config folder is always scanned
			case opts.Recursive:
				return nil // -r: keep descending
			default:
				return filepath.SkipDir // no -r: top level only
			}
		}

		switch opts.Filter {
		case filterAll:
			// every file, whatever its name or extension
		case filterListed:
			if _, ok := suffixes[filepath.Ext(d.Name())]; !ok {
				return nil
			}
		default: // filterDefault
			name := d.Name()
			ext := filepath.Ext(name)
			if strings.HasPrefix(name, ".") || (ext != "" && ext != ".df") {
				return nil
			}
		}

		out = append(out, p)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan %q: %w", opts.ConfigFolderPath, err)
	}
	return out, nil
}

// toImportParams turns the scanned absolute paths into import parameters for
// the file that holds the include directive. include and import share the same
// base — the directory of that file — and the file itself is dropped so that
// "include . -r" cannot re-import it.
func toImportParams(currentFilePath string, files []string) ([]string, error) {
	base, err := filepath.Abs(filepath.Dir(currentFilePath))
	if err != nil {
		return nil, fmt.Errorf("resolve current directory: %w", err)
	}

	self, err := filepath.Abs(currentFilePath)
	if err != nil {
		return nil, fmt.Errorf("resolve current file: %w", err)
	}
	self = filepath.Clean(self)

	out := make([]string, 0, len(files))
	for _, f := range files {
		abs, err := filepath.Abs(f)
		if err != nil {
			return nil, fmt.Errorf("resolve %q: %w", f, err)
		}

		abs = filepath.Clean(abs)
		if abs == self {
			continue
		}

		rel, err := filepath.Rel(base, abs)
		if err != nil {
			return nil, fmt.Errorf("make %q relative to %q: %w", abs, base, err)
		}
		out = append(out, rel)
	}
	return out, nil
}
