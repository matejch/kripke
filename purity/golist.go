package purity

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

type listModule struct {
	Path string
}

type listError struct {
	Err string
}

type listPackage struct {
	ImportPath string
	Name       string
	Dir        string
	Standard   bool
	DepOnly    bool
	GoFiles    []string
	CgoFiles   []string
	Imports    []string
	Module     *listModule
	Error      *listError
	DepsErrors []*listError
}

// modulePath returns the module path of the package, or "" for packages outside any module.
func (p *listPackage) modulePath() string {
	if p == nil || p.Module == nil {
		return ""
	}
	return p.Module.Path
}

// goList runs `go list -deps -json` in dir and returns all packages keyed by import path plus the explicitly listed ones.
func goList(dir, pattern string) (map[string]*listPackage, []string, error) {
	pats := strings.Fields(pattern)
	if len(pats) == 0 {
		return nil, nil, errors.New("purity: empty package pattern")
	}
	args := append([]string{"list", "-deps", "-json"}, pats...)
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, nil, fmt.Errorf("purity: go list %s: %v: %s", pattern, err, strings.TrimSpace(stderr.String()))
	}
	pkgs := map[string]*listPackage{}
	var targets []string
	dec := json.NewDecoder(&stdout)
	for {
		var p listPackage
		err := dec.Decode(&p)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("purity: decode go list output: %w", err)
		}
		if p.Error != nil {
			return nil, nil, fmt.Errorf("purity: package %s: %s", p.ImportPath, p.Error.Err)
		}
		if len(p.DepsErrors) > 0 {
			return nil, nil, fmt.Errorf("purity: package %s: %s", p.ImportPath, p.DepsErrors[0].Err)
		}
		pkgs[p.ImportPath] = &p
		if !p.DepOnly {
			targets = append(targets, p.ImportPath)
		}
	}
	if len(targets) == 0 {
		return nil, nil, fmt.Errorf("purity: no packages match %s", pattern)
	}
	return pkgs, targets, nil
}
