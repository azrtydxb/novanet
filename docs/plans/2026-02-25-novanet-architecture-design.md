# NovaNet — Architecture Design Document

**Date:** 2026-02-25
**Status:** Approved
**Authors:** Pascal Welsch, Claude

---

## Context

NovaNet is the third component in the Nova networking stack:

- **NovaEdge** — Kubernetes load balancer, reverse proxy, VIP controller, SD-WAN gateway
- **NovaRoute** — Node-local routing control plane managing BGP/OSPF/BFD via FRR
- **NovaNet** — Cloud-native eBPF CNI fabric (this project)

NovaRoute is complete and production-deployed. NovaEdge is operational. NovaNet fills the missing layer: the cluster networking fabric that provides pod connectivity, policy enforcement, and observability.

### One-Line Definition

NovaNet is a high-performance eBPF-based Kubernetes networking fabric that provides secure pod connectivity, L3/L4 policy enforcement, and flexible routing integration without duplicating edge or routing functionality.

---

## Mission

Provide a simple, high-performance, policy-safe networking fabric for Kubernetes that integrates cleanly with NovaEdge and NovaRoute without duplicating their responsibilities.

NovaNet does NOT:
- Do L7 proxying (NovaEdge's domain)
- Run SD-WAN logic (NovaEdge's domain)
- Run BGP/OSPF directly (NovaRoute's domain)

NovaNet focuses on:
- Pod networking (same-node and cross-node)
- L4 load balancing (ClusterIP/NodePort/ExternalIP DNAT — replaces kube-proxy)
- Identity-based L3/L4 policy enforcement
- Egress control

When NovaEdge is installed, it supersedes NovaNet's L4 LB with full L7 load balancing, VIP management, and advanced traffic routing.
- Observability
- Integration with NovaRoute for native routing

---

## Core Architecture

### Management Plane: Go

- Kubernetes watchers (Pods, Nodes, Namespaces, NetworkPolicies)
- Policy compiler (NetworkPolicy → identity-based rules)
- IPAM manager (node-local PodCIDR allocation)
- State reconciler (desired vs applied)
- NovaRoute gRPC client (for native routing mode)
- Communicates with Rust dataplane via gRPC over Unix socket

### Data Plane: Rust + eBPF

- TC ingress + egress hooks on pod veth interfaces
- TC hooks on tunnel interfaces (overlay mode)
- eBPF maps for endpoint, identity, policy, tunnel, flow state
- Ring buffer for flow event export
- gRPC server receiving map updates from Go agent

### Inter-component Communication

```
novanet-agent (Go) ←—gRPC/Unix socket—→ novanet-dataplane (Rust)
novanet-agent (Go) ←—gRPC/Unix socket—→ NovaRoute (/run/novaroute/novaroute.sock)
novanet-cni (Go)   ←—gRPC/Unix socket—→ novanet-agent (Go)
```

---

## Networking Modes

NovaNet supports two mutually exclusive networking modes, selected at install time.

### Overlay Mode (Default)

No fabric changes required. Works everywhere.

#### Geneve (Default Overlay Protocol)

- UDP port 6081, 24-bit VNI
- Variable-length TLV options carry identity metadata in-band
- Source node writes `identity_id` into Geneve TLV on encapsulation
- Destination node reads TLV on decapsulation — no endpoint map lookup needed for identity
- Good hardware offload support (mlx5, newer Intel NICs)
- IETF standard (RFC 8926)

#### VXLAN (Fallback Overlay Protocol)

- UDP port 4789, 24-bit VNI, fixed header
- No TLV support — identity resolved via endpoint map lookup on ingress
- Widest hardware offload and firewall compatibility
- Config flag: `tunnel_protocol: geneve | vxlan`

#### Overlay Data Path

```
Pod A → TC egress → lookup tunnel map → Geneve/VXLAN encap → Node IP → underlay → Node IP → TC ingress on tunnel → decap → identity (TLV or lookup) → policy check → Pod B
```

Performance: ~10-40 Gbps typical.

### Native Routing Mode

No encapsulation. Underlay routes pod traffic directly via BGP or OSPF.

#### How It Works

- NovaNet agent connects to NovaRoute as owner `"novanet"` via `/run/novaroute/novaroute.sock`
- On startup: `AdvertisePrefix` for node's PodCIDR
- On shutdown: `WithdrawPrefix` for graceful drain
- NovaRoute handles all BGP/OSPF/BFD — NovaNet never touches routing protocols
- Underlay fabric learns PodCIDR routes and forwards natively

#### BGP Mode

- NovaRoute advertises PodCIDRs via BGP to ToR/spine switches
- Requires BGP-capable fabric (ToR or route reflector)

#### OSPF Mode

- NovaRoute advertises PodCIDRs via OSPF into the underlay IGP
- Requires OSPF-capable underlay

#### Native Data Path

```
Pod A → TC egress → policy check → Pod IP → underlay (has route via BGP/OSPF) → Pod B
```

No encapsulation overhead. Near line-rate performance.

#### Identity in Native Mode

No tunnel header means no in-band metadata. Identity is resolved via endpoint map lookup:

- TC ingress: extract source IP → lookup endpoint map → get identity ID → evaluate policy
- Single eBPF hash map read, O(1), nanoseconds
- Endpoint map kept fresh by management plane reconciler (sub-second staleness window)

### Mode Comparison

| Aspect | Overlay (Geneve) | Overlay (VXLAN) | Native (BGP/OSPF) |
|--------|------------------|-----------------|-------------------|
| Fabric requirements | None | None | BGP or OSPF capable |
| Encapsulation | Geneve | VXLAN | None |
| Identity propagation | TLV in header | Endpoint map lookup | Endpoint map lookup |
| Performance | ~10-40 Gbps | ~10-40 Gbps | Near line-rate |
| Extra latency | Small (encap/decap) | Small (encap/decap) | None |
| Hardware offload | Good (mlx5+) | Excellent (universal) | N/A |
| NovaRoute required | No | No | Yes |

---

## Identity-Based L3/L4 Policy

### Identity Model

- Each unique set of security-relevant labels maps to a deterministic identity ID
- Pods with identical labels share an identity (scales better than per-pod rules)
- Identity assigned at CNI ADD time
- Identity ID is a 32-bit integer derived from label hash

### Policy Compilation

Kubernetes NetworkPolicy is compiled into identity-based rules:

```
NetworkPolicy:
  podSelector: {app: frontend}    → identity 42
  ingress:
    - from:
      - podSelector: {app: api}   → identity 17
      ports:
        - port: 80, protocol: TCP

Compiled rule:
  (src_id=17, dst_id=42, proto=TCP, port=80) → ALLOW
```

### Enforcement Points

- **TC ingress**: extract source identity → check policy map → allow or drop
- **TC egress**: check egress policy map → allow or drop
- Drop reason codes recorded for every denied packet

### Default Stance

- Per Kubernetes spec: default-allow unless a NetworkPolicy selects the pod
- Configurable: cluster-wide default-deny mode for security-sensitive environments

---

## Egress Control

### Baseline Egress

- SNAT/masquerade in TC egress for pod → external traffic
- Configurable masquerade IP per namespace (node IP default)
- Namespace-level egress deny/allow rules
- Egress flow logging

### Egress Policy

```
Egress policy map:
  (src_identity, dst_cidr, proto, port) → ALLOW | DENY | SNAT
```

### NovaEdge Integration (Future)

- NovaNet can mark packets for NovaEdge steering
- NovaNet does NOT make WAN/link decisions

---

## Observability

### Flow Visibility

- BPF ring buffer exports flow events to Rust dataplane
- Events: source/dest IP, identity, port, protocol, verdict, latency, byte count
- Rust daemon aggregates and streams via gRPC

### Metrics (Prometheus)

- Flow counts per identity pair
- Policy verdicts (allow/deny) per rule
- Drop reasons with counters
- Bytes/packets per identity pair
- Latency histograms per flow
- TCP metrics: SYN/FIN/RST counts, retransmits (optional)

### CLI Inspection

- `novanetctl status` — full node status
- `novanetctl flows` — real-time flow table
- `novanetctl drops` — recent drops with reason codes
- `novanetctl policy` — compiled policy rules
- `novanetctl identity` — pod → identity mappings
- `novanetctl tunnels` — tunnel state (overlay mode)
- `novanetctl egress` — egress rules and counters
- `novanetctl metrics` — summary statistics

---

## eBPF Dataplane Design

### Attach Points

- TC ingress on pod veth interfaces
- TC egress on pod veth interfaces
- TC ingress/egress on tunnel interfaces (overlay mode)

### Core eBPF Maps

| Map | Key | Value | Purpose |
|-----|-----|-------|---------|
| Endpoint | Pod IP (u32) | ifindex + MAC + identity ID | Pod lookup |
| Identity | Identity ID (u32) | Label hash (u64) | Debugging/audit |
| Policy | (src_id, dst_id, proto, port) | ALLOW / DENY | Policy enforcement |
| Tunnel | Node IP (u32) | Tunnel ifindex + remote endpoint | Overlay routing |
| Flow | 5-tuple hash | Packet count, bytes, verdict, timestamp | Observability |
| Config | Key (u32) | Value (u64) | Mode flags, settings |
| Egress | (src_identity, dst_cidr) | ALLOW / DENY / SNAT | Egress control |

### Program Flow (Overlay Mode — Geneve)

```
Pod egress:
  TC egress on pod veth
  → read config map (mode=overlay)
  → lookup egress policy (src_identity, dst)
  → if remote pod: lookup tunnel map → Geneve encap with identity TLV
  → if external: SNAT/masquerade
  → forward

Pod ingress (from tunnel):
  TC ingress on tunnel interface
  → Geneve decap
  → read identity from TLV
  → lookup ingress policy (src_identity, dst_identity)
  → forward to pod veth or drop
```

### Program Flow (Native Mode)

```
Pod egress:
  TC egress on pod veth
  → read config map (mode=native)
  → lookup egress policy (src_identity, dst)
  → if external: SNAT/masquerade
  → forward (kernel routes via BGP/OSPF-learned routes)

Pod ingress:
  TC ingress on pod veth
  → extract source IP
  → lookup endpoint map → get source identity
  → lookup ingress policy (src_identity, dst_identity)
  → forward or drop
```

---

## NovaRoute Integration Details

### Registration

```go
// On agent startup (native routing mode)
client.Register(RegisterRequest{
    Owner: "novanet",
    Token: config.NovaRouteToken,
})
```

### PodCIDR Advertisement

```go
// Advertise node's PodCIDR
client.AdvertisePrefix(AdvertisePrefixRequest{
    Owner:  "novanet",
    Prefix: node.PodCIDR,  // e.g., "10.244.3.0/24"
})
```

### Graceful Shutdown

```go
// On SIGTERM
client.WithdrawPrefix(WithdrawPrefixRequest{
    Owner:  "novanet",
    Prefix: node.PodCIDR,
})
client.Deregister(DeregisterRequest{Owner: "novanet"})
```

### NovaRoute Owner Policy

NovaRoute's policy engine already supports `novanet` as an owner type:
- Allowed prefix type: `subnet` (not host routes — those are NovaEdge's domain)
- Configurable CIDR allowlist (e.g., `10.244.0.0/16`)

---

## Deployment Model

### DaemonSet

```yaml
containers:
  - name: novanet-agent        # Go management plane
  - name: novanet-dataplane    # Rust eBPF dataplane

initContainers:
  - name: install-cni          # Copies CNI binary to /opt/cni/bin/
```

Both containers run with `hostNetwork: true` (required for eBPF attachment and pod networking).

### Shared Volumes

| Volume | Type | Purpose |
|--------|------|---------|
| `/run/novanet/` | hostPath | Agent Unix socket for CNI binary and CLI |
| `/run/novaroute/` | hostPath | NovaRoute Unix socket (native mode) |
| `/opt/cni/bin/` | hostPath | CNI binary installation |
| `/etc/cni/net.d/` | hostPath | CNI config file |
| `/sys/fs/bpf/` | hostPath | Pinned eBPF maps (survive restarts) |
| `/proc/` | hostPath | Access to pod network namespaces |

### Optional Co-deployments

- **NovaRoute DaemonSet** — required for native routing mode
- **NovaEdge DaemonSet** — optional, handles VIP/LB when present

---

## Security Model

- Default deny configurable per cluster
- Namespace isolation via identity-scoped policy
- Host-network isolation optional (separate policy for host ↔ pod)
- eBPF programs pinned with minimal capabilities
- No secrets in eBPF maps — tokens stay in agent config
- Strict separation between NovaNet and NovaEdge traffic handling

---

## Differentiation

1. **Clean separation from routing protocols** — NovaRoute owns BGP/OSPF, not NovaNet
2. **Clean separation from ingress/L7** — NovaEdge owns VIPs and proxying
3. **Identity-based policy first** — scales better than IP-pair explosion
4. **Observability-first design** — Cilium-like visibility without L7 complexity
5. **Rust dataplane** — memory safety without GC pauses
6. **Dual overlay support** — Geneve default with identity TLV, VXLAN fallback
7. **Seamless NovaRoute integration** — native routing via gRPC, no embedded BGP
