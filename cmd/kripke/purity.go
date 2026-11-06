package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/matej/kripke/purity"
	"github.com/spf13/cobra"
)

func purityCmd() *cobra.Command {
	var policyPath string
	c := &cobra.Command{
		Use:   "purity <pkgs...>",
		Short: "Lint pure core packages: no I/O, clock, randomness, goroutines or mutable globals",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if policyPath == "" {
				root, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}").Output()
				if err != nil {
					return fmt.Errorf("finding the module root: %v", err)
				}
				policyPath = filepath.Join(strings.TrimSpace(string(root)), "purity.yaml")
			}
			p, err := purity.LoadPolicy(policyPath)
			if err != nil {
				return err
			}
			fs, err := purity.Check(".", strings.Join(args, " "), p)
			if err != nil {
				return err
			}
			for _, f := range fs {
				fmt.Fprintln(cmd.OutOrStdout(), f)
			}
			if len(fs) > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(), "%d purity findings\n", len(fs))
				return exitError{1}
			}
			return nil
		},
	}
	c.Flags().StringVar(&policyPath, "policy", "", "policy file (default: purity.yaml at the module root)")
	return c
}
