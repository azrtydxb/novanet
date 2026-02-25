package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	pb "github.com/piwi3910/novanet/api/v1"

	"github.com/spf13/cobra"
)

func newEgressCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "egress",
		Short: "Show egress policy rules",
		Long:  "Display the egress policy rules currently installed for pod-to-external traffic control.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEgress()
		},
	}

	return cmd
}

func runEgress() error {
	conn, err := connectAgent()
	if err != nil {
		return err
	}
	defer conn.Close()

	client := newAgentClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	// Get status from agent.
	status, err := client.GetAgentStatus(ctx, &pb.GetAgentStatusRequest{})
	if err != nil {
		return fmt.Errorf("GetAgentStatus failed: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "EGRESS POLICIES\n")
	fmt.Fprintf(w, "===============\n\n")
	fmt.Fprintf(w, "Routing Mode:\t%s\n\n", status.RoutingMode)

	fmt.Fprintf(w, "SRC_IDENTITY\tDST_CIDR\tPROTOCOL\tPORT\tACTION\n")
	// Detailed egress policy listing requires a ListEgressPolicies RPC
	// that is not yet exposed by the agent. The current proto defines
	// upsert/delete operations for egress policies but no list endpoint.
	// A dedicated list endpoint will be added in a future release.
	fmt.Fprintf(w, "(detailed egress listing requires ListEgressPolicies RPC — coming soon)\n")

	return w.Flush()
}
