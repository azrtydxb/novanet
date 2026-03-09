package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	pb "github.com/azrtydxb/novanet/api/v1"
	"github.com/spf13/cobra"
)

func newRoutingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "routing",
		Short: "Inspect routing state",
		Long:  "Commands for inspecting BGP/OSPF/BFD routing state in native routing mode.",
	}

	cmd.AddCommand(
		newRoutingStatusCmd(),
		newRoutingPeersCmd(),
		newRoutingPrefixesCmd(),
		newRoutingBFDCmd(),
		newRoutingOSPFCmd(),
		newRoutingEventsCmd(),
	)

	return cmd
}

// --- status ---

func newRoutingStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show routing status",
		Long:  "Display the current routing mode, protocol state, and connectivity status.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoutingStatus()
		},
	}
}

func runRoutingStatus() error {
	conn, err := connectAgent()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	client := newAgentClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	resp, err := client.GetAgentStatus(ctx, &pb.GetAgentStatusRequest{})
	if err != nil {
		return fmt.Errorf("GetAgentStatus failed: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)

	_, _ = fmt.Fprintln(w, "Routing Status")
	_, _ = fmt.Fprintln(w, "==============")
	_, _ = fmt.Fprintln(w)

	_, _ = fmt.Fprintf(w, "Routing Mode:\t%s\n", resp.RoutingMode)
	_, _ = fmt.Fprintf(w, "Routing Connected:\t%v\n", resp.NovarouteConnected)
	_, _ = fmt.Fprintf(w, "Tunnel Protocol:\t%s\n", resp.TunnelProtocol)

	return w.Flush()
}

// --- peers ---

func newRoutingPeersCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "peers",
		Short: "Show BGP peer state",
		Long:  "Display BGP peer sessions with state, prefix counts, and BFD status.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoutingPeers()
		},
	}
}

func runRoutingPeers() error {
	conn, err := connectAgent()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	client := newAgentClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	resp, err := client.GetRoutingPeers(ctx, &pb.GetRoutingPeersRequest{})
	if err != nil {
		return fmt.Errorf("GetRoutingPeers failed: %w", err)
	}

	if len(resp.Peers) == 0 {
		fmt.Println("No BGP peers configured.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NEIGHBOR\tREMOTE AS\tSTATE\tPFX RECV\tPFX SENT\tBFD\tUPTIME\tOWNER")

	for _, p := range resp.Peers {
		bfd := "-"
		if p.BfdStatus != "" {
			bfd = p.BfdStatus
		}
		uptime := p.Uptime
		if uptime == "" {
			uptime = "-"
		}
		owner := p.Owner
		if owner == "" {
			owner = "-"
		}
		_, _ = fmt.Fprintf(w, "%s\t%d\t%s\t%d\t%d\t%s\t%s\t%s\n",
			p.NeighborAddress, p.RemoteAs, p.State,
			p.PrefixesReceived, p.PrefixesSent,
			bfd, uptime, owner)
	}

	return w.Flush()
}

// --- prefixes ---

func newRoutingPrefixesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prefixes",
		Short: "Show advertised prefixes",
		Long:  "Display BGP/OSPF prefix advertisements from the intent store.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoutingPrefixes()
		},
	}
}

func runRoutingPrefixes() error {
	conn, err := connectAgent()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	client := newAgentClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	resp, err := client.GetRoutingPrefixes(ctx, &pb.GetRoutingPrefixesRequest{})
	if err != nil {
		return fmt.Errorf("GetRoutingPrefixes failed: %w", err)
	}

	if len(resp.Prefixes) == 0 {
		fmt.Println("No prefixes advertised.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "PREFIX\tPROTOCOL\tSTATE\tOWNER")

	for _, p := range resp.Prefixes {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			p.Prefix, p.Protocol, p.State, p.Owner)
	}

	return w.Flush()
}

// --- bfd ---

func newRoutingBFDCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "bfd",
		Short: "Show BFD session state",
		Long:  "Display BFD sessions with status, timers, and uptime.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoutingBFD()
		},
	}
}

func runRoutingBFD() error {
	conn, err := connectAgent()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	client := newAgentClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	resp, err := client.GetRoutingBFDSessions(ctx, &pb.GetRoutingBFDSessionsRequest{})
	if err != nil {
		return fmt.Errorf("GetRoutingBFDSessions failed: %w", err)
	}

	if len(resp.Sessions) == 0 {
		fmt.Println("No BFD sessions.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "PEER ADDRESS\tSTATUS\tMIN RX\tMIN TX\tDETECT MULT\tUPTIME\tOWNER")

	for _, s := range resp.Sessions {
		uptime := s.Uptime
		if uptime == "" {
			uptime = "-"
		}
		owner := s.Owner
		if owner == "" {
			owner = "-"
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%dms\t%dms\t%d\t%s\t%s\n",
			s.PeerAddress, s.Status,
			s.MinRxMs, s.MinTxMs, s.DetectMultiplier,
			uptime, owner)
	}

	return w.Flush()
}

// --- ospf ---

func newRoutingOSPFCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ospf",
		Short: "Show OSPF neighbor state",
		Long:  "Display OSPF neighbor adjacencies with state and interface info.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoutingOSPF()
		},
	}
}

func runRoutingOSPF() error {
	conn, err := connectAgent()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	client := newAgentClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	resp, err := client.GetRoutingOSPFNeighbors(ctx, &pb.GetRoutingOSPFNeighborsRequest{})
	if err != nil {
		return fmt.Errorf("GetRoutingOSPFNeighbors failed: %w", err)
	}

	if len(resp.Neighbors) == 0 {
		fmt.Println("No OSPF neighbors.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NEIGHBOR ID\tADDRESS\tINTERFACE\tSTATE\tOWNER")

	for _, n := range resp.Neighbors {
		owner := n.Owner
		if owner == "" {
			owner = "-"
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			n.NeighborId, n.Address, n.InterfaceName, n.State, owner)
	}

	return w.Flush()
}

// --- events ---

func newRoutingEventsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "events",
		Short: "Stream routing events",
		Long:  "Stream real-time BGP/BFD/OSPF routing events.",
		RunE: func(cmd *cobra.Command, args []string) error {
			owner, _ := cmd.Flags().GetString("owner")
			return runRoutingEvents(owner)
		},
	}

	cmd.Flags().String("owner", "", "Filter events by owner name")
	return cmd
}

func runRoutingEvents(ownerFilter string) error {
	conn, err := connectAgent()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	client := newAgentClient(conn)

	ctx := context.Background()

	stream, err := client.StreamRoutingEvents(ctx, &pb.StreamRoutingEventsRequest{
		OwnerFilter: ownerFilter,
	})
	if err != nil {
		return fmt.Errorf("StreamRoutingEvents failed: %w", err)
	}

	fmt.Println("Streaming routing events (Ctrl+C to stop)...")
	fmt.Println()

	for {
		evt, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("event stream error: %w", err)
		}

		ts := time.Unix(0, evt.TimestampNs).Format("15:04:05.000")
		owner := evt.Owner
		if owner == "" {
			owner = "-"
		}

		fmt.Printf("[%s] %-20s  owner=%-10s  %s\n", ts, evt.EventType, owner, evt.Detail)
	}
}
