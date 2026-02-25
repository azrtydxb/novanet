package main

import (
	"github.com/spf13/cobra"
)

func newDropsCmd() *cobra.Command {
	var identityFilter uint32

	cmd := &cobra.Command{
		Use:   "drops",
		Short: "Stream dropped packet events",
		Long:  "Display real-time packet drop events from the dataplane. Equivalent to 'flows' with drops-only filter. Streams continuously until interrupted with Ctrl+C.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFlows(identityFilter, true)
		},
	}

	cmd.Flags().Uint32Var(&identityFilter, "identity", 0, "Filter drops by identity ID (0 = all)")

	return cmd
}
