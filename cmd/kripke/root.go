package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

type exitError struct{ code int }

func (e exitError) Error() string { return fmt.Sprintf("exit %d", e.code) }

func run(args []string) int {
	root := &cobra.Command{
		Use:           "kripke",
		Short:         "Explicit-state model checker for Go models written with package fv",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(checkCmd(), traceCmd(), purityCmd())
	root.SetArgs(args)
	err := root.Execute()
	var ee exitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &ee):
		return ee.code
	default:
		fmt.Fprintln(os.Stderr, "kripke:", err)
		return 2
	}
}
