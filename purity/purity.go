package purity

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Finding is one violation of the purity rules.
type Finding struct {
	Pkg, File string
	Line, Col int
	Rule, Msg string
}

// String formats the finding as "file:line:col: [rule] msg".
func (f Finding) String() string {
	return fmt.Sprintf("%s:%d:%d: [%s] %s", f.File, f.Line, f.Col, f.Rule, f.Msg)
}

// Check lints the packages matched by pattern, resolved with go list in dir, and their same-module dependencies.
func Check(dir, pattern string, p Policy) ([]Finding, error) {
	pkgs, targets, err := goList(dir, pattern)
	if err != nil {
		return nil, err
	}
	reached, order := walkModule(pkgs, targets)
	base, _ := filepath.Abs(dir)
	var out []Finding
	for _, path := range order {
		fs, err := checkPackage(pkgs[path], reached[path], p)
		if err != nil {
			return nil, fmt.Errorf("purity: %s: %w", path, err)
		}
		for i := range fs {
			fs[i].File = relPath(base, fs[i].File)
		}
		out = append(out, fs...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Col < b.Col
	})
	return out, nil
}

// relPath shortens file to a path relative to base when it lies under base.
func relPath(base, file string) string {
	if base == "" {
		return file
	}
	rel, err := filepath.Rel(base, file)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return file
	}
	return rel
}

// AssertPure loads moduleRoot/purity.yaml, checks pattern and fails t once per finding.
func AssertPure(t testing.TB, moduleRoot, pattern string) {
	t.Helper()
	p, err := LoadPolicy(filepath.Join(moduleRoot, "purity.yaml"))
	if err != nil {
		t.Fatalf("purity: %v", err)
	}
	fs, err := Check(moduleRoot, pattern, p)
	if err != nil {
		t.Fatalf("purity: %v", err)
	}
	for _, f := range fs {
		t.Errorf("%s", f)
	}
}
