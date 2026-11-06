package main

import (
	"fmt"
	"io"
	"os"

	"github.com/matejch/kripke/fv"
	"github.com/spf13/cobra"
)

func traceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "trace <file|->",
		Short: "Pretty-print a JSON result written by check --json",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var r io.Reader = os.Stdin
			if args[0] != "-" {
				f, err := os.Open(args[0])
				if err != nil {
					return err
				}
				defer f.Close()
				r = f
			}
			rs, err := fv.ReadJSON(r)
			if err != nil {
				return err
			}
			for _, res := range rs {
				fmt.Fprint(cmd.OutOrStdout(), fv.RenderResult(res))
			}
			return nil
		},
	}
}
