package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	pb "github.com/piwi3910/novanet/api/v1"

	"github.com/spf13/cobra"
)

func newPolicyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy",
		Short: "Show compiled policy rules",
		Long:  "Display the compiled policy rules currently installed in the dataplane.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPolicy()
		},
	}

	return cmd
}

func runPolicy() error {
	conn, err := connectAgent()
	if err != nil {
		return err
	}
	defer conn.Close()

	client := newAgentClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	// Get policy count from agent status.
	status, err := client.GetAgentStatus(ctx, &pb.GetAgentStatusRequest{})
	if err != nil {
		return fmt.Errorf("GetAgentStatus failed: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "POLICY RULES\n")
	fmt.Fprintf(w, "============\n\n")
	fmt.Fprintf(w, "Total Policies:\t%d\n\n", status.PolicyCount)

	if status.PolicyCount == 0 {
		fmt.Fprintln(w, "No policies installed.")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Policies are compiled from Kubernetes NetworkPolicy resources.")
	} else {
		fmt.Fprintf(w, "SRC_IDENTITY\tDST_IDENTITY\tPROTOCOL\tPORT\tACTION\n")
		// Detailed policy listing requires a ListPolicies RPC that is
		// not yet exposed by the agent. The current proto only provides
		// upsert/delete/sync operations. A dedicated list endpoint will
		// be added in a future release.
		fmt.Fprintf(w, "(detailed policy listing requires ListPolicies RPC — coming soon)\n")
	}

	return w.Flush()
}
