package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	pb "github.com/piwi3910/novanet/api/v1"

	"github.com/spf13/cobra"
)

func newIdentityCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "identity",
		Short: "Show identity mappings",
		Long:  "Display the identity allocator state showing identity IDs, their label sets, and reference counts.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIdentity()
		},
	}

	return cmd
}

func runIdentity() error {
	conn, err := connectAgent()
	if err != nil {
		return err
	}
	defer conn.Close()

	client := newAgentClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	// Get identity count from agent status.
	status, err := client.GetAgentStatus(ctx, &pb.GetAgentStatusRequest{})
	if err != nil {
		return fmt.Errorf("GetAgentStatus failed: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "IDENTITY MAPPINGS\n")
	fmt.Fprintf(w, "=================\n\n")
	fmt.Fprintf(w, "Total Identities:\t%d\n\n", status.IdentityCount)

	if status.IdentityCount == 0 {
		fmt.Fprintln(w, "No identities allocated.")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Identities are allocated when pods are scheduled to this node.")
	} else {
		fmt.Fprintf(w, "ID\tLABELS\tPOD_COUNT\n")
		// Detailed identity listing requires a ListIdentities RPC that
		// is not yet exposed by the agent. The identity allocator runs
		// in-memory on the agent; a dedicated list endpoint will be
		// added in a future release.
		fmt.Fprintf(w, "(detailed identity listing requires ListIdentities RPC — coming soon)\n")
	}

	return w.Flush()
}
