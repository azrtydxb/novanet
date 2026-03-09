package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	pb "github.com/azrtydxb/novanet/api/v1"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	// defaultEBPFServicesSocket is the default eBPF Services gRPC socket.
	defaultEBPFServicesSocket = "/run/novanet/ebpf-services.sock"
)

// Global flag for the ebpf-services socket path.
var ebpfServicesSocket string

// connectEBPFServices dials the eBPF Services gRPC socket and returns a client connection.
func connectEBPFServices() (*grpc.ClientConn, error) {
	conn, err := grpc.NewClient(
		"unix://"+ebpfServicesSocket,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to ebpf-services at %s: %w", ebpfServicesSocket, err)
	}
	return conn, nil
}

func newEBPFCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ebpf",
		Short: "Inspect eBPF Services state",
		Long:  "Commands for inspecting eBPF Services (SOCKMAP, mesh redirects, rate limiting, backend health).",
	}

	cmd.PersistentFlags().StringVar(&ebpfServicesSocket, "ebpf-socket", defaultEBPFServicesSocket,
		"Path to the eBPF Services gRPC socket")

	// Parent commands for grouping.
	sockmapCmd := &cobra.Command{
		Use:   "sockmap",
		Short: "SOCKMAP acceleration commands",
	}
	meshCmd := &cobra.Command{
		Use:   "mesh",
		Short: "Mesh redirect commands",
	}
	ratelimitCmd := &cobra.Command{
		Use:   "ratelimit",
		Short: "Rate limiting commands",
	}
	healthCmd := &cobra.Command{
		Use:   "health",
		Short: "Backend health commands",
	}

	sockmapCmd.AddCommand(newSockmapStatusCmd())
	meshCmd.AddCommand(newMeshListCmd())
	ratelimitCmd.AddCommand(newRateLimitStatsCmd())
	healthCmd.AddCommand(newHealthListCmd())

	cmd.AddCommand(sockmapCmd, meshCmd, ratelimitCmd, healthCmd)
	return cmd
}

func newSockmapStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show SOCKMAP acceleration stats",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSockmapStatus()
		},
	}
}

func runSockmapStatus() error {
	conn, err := connectEBPFServices()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	client := pb.NewEBPFServicesClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	resp, err := client.GetSockmapStats(ctx, &pb.GetSockmapStatsRequest{})
	if err != nil {
		return fmt.Errorf("GetSockmapStats failed: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "SOCKMAP Stats")
	_, _ = fmt.Fprintln(w, "=============")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintf(w, "Redirected:\t%d\n", resp.Redirected)
	_, _ = fmt.Fprintf(w, "Fallback:\t%d\n", resp.Fallback)
	_, _ = fmt.Fprintf(w, "Active Sockets:\t%d\n", resp.ActiveSockets)
	return w.Flush()
}

func newMeshListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List mesh redirect entries",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMeshList()
		},
	}
}

func runMeshList() error {
	conn, err := connectEBPFServices()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	client := pb.NewEBPFServicesClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	resp, err := client.ListMeshRedirects(ctx, &pb.ListMeshRedirectsRequest{})
	if err != nil {
		return fmt.Errorf("ListMeshRedirects failed: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "MESH REDIRECTS")
	_, _ = fmt.Fprintln(w, "==============")
	_, _ = fmt.Fprintln(w)

	if len(resp.Entries) == 0 {
		_, _ = fmt.Fprintln(w, "No mesh redirect entries.")
	} else {
		_, _ = fmt.Fprintf(w, "IP\tPORT\tREDIRECT PORT\n")
		for _, e := range resp.Entries {
			_, _ = fmt.Fprintf(w, "%s\t%d\t%d\n", e.Ip, e.Port, e.RedirectPort)
		}
	}
	return w.Flush()
}

func newRateLimitStatsCmd() *cobra.Command {
	var cidr string

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show rate limit stats for a CIDR",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRateLimitStats(cidr)
		},
	}

	cmd.Flags().StringVar(&cidr, "cidr", "", "CIDR to query rate limit stats for (required)")
	_ = cmd.MarkFlagRequired("cidr")

	return cmd
}

func runRateLimitStats(cidr string) error {
	conn, err := connectEBPFServices()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	client := pb.NewEBPFServicesClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	resp, err := client.GetRateLimitStats(ctx, &pb.GetRateLimitStatsRequest{Cidr: cidr})
	if err != nil {
		return fmt.Errorf("GetRateLimitStats failed: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "RATE LIMIT STATS")
	_, _ = fmt.Fprintln(w, "================")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintf(w, "CIDR:\t%s\n", cidr)
	_, _ = fmt.Fprintf(w, "Allowed:\t%d\n", resp.Allowed)
	_, _ = fmt.Fprintf(w, "Denied:\t%d\n", resp.Denied)
	return w.Flush()
}

func newHealthListCmd() *cobra.Command {
	var ip string
	var port uint32

	cmd := &cobra.Command{
		Use:   "list",
		Short: "Show backend health status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHealthList(ip, port)
		},
	}

	cmd.Flags().StringVar(&ip, "ip", "", "Backend IP to filter (optional)")
	cmd.Flags().Uint32Var(&port, "port", 0, "Backend port to filter (optional)")

	return cmd
}

func runHealthList(ip string, port uint32) error {
	conn, err := connectEBPFServices()
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	client := pb.NewEBPFServicesClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	resp, err := client.GetBackendHealth(ctx, &pb.GetBackendHealthRequest{Ip: ip, Port: port})
	if err != nil {
		return fmt.Errorf("GetBackendHealth failed: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "BACKEND HEALTH")
	_, _ = fmt.Fprintln(w, "==============")
	_, _ = fmt.Fprintln(w)

	if len(resp.Backends) == 0 {
		_, _ = fmt.Fprintln(w, "No backend health data.")
	} else {
		_, _ = fmt.Fprintf(w, "IP\tPORT\tTOTAL\tSUCCESS\tFAILED\tTIMEOUT\tAVG RTT\tFAILURE RATE\n")
		for _, b := range resp.Backends {
			rttMs := float64(b.AvgRttNs) / 1e6
			_, _ = fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\t%d\t%.2fms\t%.2f%%\n",
				b.Ip, b.Port, b.TotalConns, b.SuccessConns, b.FailedConns,
				b.TimeoutConns, rttMs, b.FailureRate*100)
		}
	}
	return w.Flush()
}
