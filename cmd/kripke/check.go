package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

type listed struct {
	Dir        string
	ImportPath string
	Name       string
	Module     *struct{ Path, Dir string }
}

func checkCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "check <pkg> [flags]",
		Short: "Build the package's registered models into a temporary binary and check them",
		Long: `check resolves <pkg> with go list, generates a main package that imports it and calls fv.Main,
builds it with go build -overlay into a temporary directory, so nothing is written into the source tree,
and runs it. Every fv flag is forwarded:
--model --max-depth --max-states --deadline --dfs --workers --no-symmetry --por --por-validate --sha256
--simulate --seed --gc-percent --mem-limit --no-determinism-check --continue --json -v --list.
Exit code: 0 clean, 1 violation, 2 error.`,
		Args:               cobra.MinimumNArgs(1),
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if args[0] == "-h" || args[0] == "--help" {
				return cmd.Help()
			}
			code, err := check(args[0], args[1:])
			if err != nil {
				return err
			}
			if code != 0 {
				return exitError{code}
			}
			return nil
		},
	}
	return c
}

func check(pattern string, forward []string) (int, error) {
	out, err := exec.Command("go", "list", "-json", pattern).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return 0, fmt.Errorf("go list %s: %s", pattern, strings.TrimSpace(string(ee.Stderr)))
		}
		return 0, err
	}
	var p listed
	if err := json.Unmarshal(out, &p); err != nil {
		return 0, fmt.Errorf("go list %s: %v (one package expected)", pattern, err)
	}
	if p.Module == nil {
		return 0, fmt.Errorf("%s is not in a module", pattern)
	}
	if p.Name == "main" {
		return 0, fmt.Errorf("%s is a main package; models must live in an importable package", p.ImportPath)
	}
	tmp, err := os.MkdirTemp("", "kripke-run-")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(tmp)
	src := filepath.Join(tmp, "main.go")
	prog := fmt.Sprintf(`package main

import (
	"os"

	"github.com/matejch/kripke/fv"
	_ %q
)

func main() { os.Exit(fv.Main(os.Args[1:], os.Stdout, os.Stderr)) }
`, p.ImportPath)
	if err := os.WriteFile(src, []byte(prog), 0o644); err != nil {
		return 0, err
	}
	rel := fmt.Sprintf("kripke_run_%d", os.Getpid())
	virtual := filepath.Join(p.Module.Dir, rel, "main.go")
	overlay, _ := json.Marshal(map[string]map[string]string{"Replace": {virtual: src}})
	ov := filepath.Join(tmp, "overlay.json")
	if err := os.WriteFile(ov, overlay, 0o644); err != nil {
		return 0, err
	}
	bin := filepath.Join(tmp, "kripke-model")
	build := exec.Command("go", "build", "-overlay", ov, "-o", bin, "./"+rel)
	build.Dir = p.Module.Dir
	build.Stdout, build.Stderr = os.Stderr, os.Stderr
	if err := build.Run(); err != nil {
		return 0, fmt.Errorf("building the model binary for %s failed (does its module require github.com/matejch/kripke?)", p.ImportPath)
	}
	cmd := exec.Command(bin, forward...)
	cmd.Dir = p.Dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err = cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if code := ee.ExitCode(); code >= 0 {
			return code, nil
		}
		return 0, fmt.Errorf("model binary for %s was killed: %v", p.ImportPath, ee)
	}
	return 0, err
}
