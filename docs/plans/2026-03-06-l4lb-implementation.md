# L4 Load Balancer Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace kube-proxy with eBPF-based L4 load balancing for all Kubernetes Service types (ClusterIP, NodePort, ExternalIP, LoadBalancer) with conntrack and 3 backend selection algorithms.

**Architecture:** Go agent watches Services/EndpointSlices → pushes to Rust dataplane via gRPC → populates eBPF maps (SERVICES, BACKENDS, CONNTRACK, MAGLEV). eBPF `tc_egress` does DNAT on Service VIP traffic; `tc_ingress`/`tc_tunnel_ingress` does reverse SNAT using conntrack. New `tc_host_ingress` on physical interface handles NodePort/ExternalIP.

**Tech Stack:** Rust + aya-ebpf (eBPF maps and programs), Go (Service watcher, backend allocator, Maglev table gen), Protobuf/gRPC (agent↔dataplane), `bpf_l3_csum_replace`/`bpf_l4_csum_replace` (incremental checksums).

**Design doc:** `docs/plans/2026-03-06-l4lb-design.md`

---

## Task 1: Add L4 LB types to novanet-common

**Files:**
- Modify: `dataplane/novanet-common/src/lib.rs`

**Step 1: Add new map struct types**

Add after the `EgressValue` struct (around line 135) and before the `FlowEvent` struct:

```rust
// ---------------------------------------------------------------------------
// Service map: Service VIP → backend selection info
// ---------------------------------------------------------------------------

/// Key for the service map.
#[repr(C)]
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct ServiceKey {
    /// Service virtual IP in network byte order (0 for NodePort scope).
    pub ip: u32,
    /// Service port in host byte order.
    pub port: u16,
    /// IP protocol (6=TCP, 17=UDP, 132=SCTP).
    pub protocol: u8,
    /// Service scope: 0=ClusterIP, 1=NodePort, 2=ExternalIP, 3=LoadBalancer.
    pub scope: u8,
}

/// Value stored for each service.
#[repr(C)]
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct ServiceValue {
    /// Number of backends for this service.
    pub backend_count: u16,
    /// Starting index in the BACKENDS array.
    pub backend_offset: u16,
    /// Backend selection algorithm: 0=random, 1=round-robin, 2=maglev.
    pub algorithm: u8,
    /// Flags: bit 0 = session affinity, bit 1 = externalTrafficPolicy=Local.
    pub flags: u8,
    /// Session affinity timeout in seconds (0 = disabled).
    pub affinity_timeout: u16,
    /// Offset into MAGLEV lookup table (only when algorithm=2).
    pub maglev_offset: u32,
}

// ---------------------------------------------------------------------------
// Backend map: flat array of backend endpoints
// ---------------------------------------------------------------------------

/// Backend endpoint entry (stored in a flat array, indexed by offset + selection).
#[repr(C)]
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct BackendValue {
    /// Backend pod IPv4 address in network byte order.
    pub ip: u32,
    /// Backend target port in host byte order.
    pub port: u16,
    /// Padding for alignment.
    pub _pad: [u8; 2],
    /// Node IP hosting this backend (for externalTrafficPolicy: Local).
    pub node_ip: u32,
}

// ---------------------------------------------------------------------------
// Conntrack map: connection tracking for NAT state
// ---------------------------------------------------------------------------

/// Key for the conntrack LRU map.
#[repr(C)]
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct CtKey {
    /// Source IPv4 address in network byte order.
    pub src_ip: u32,
    /// Destination IPv4 address in network byte order (the original VIP).
    pub dst_ip: u32,
    /// Source port in host byte order.
    pub src_port: u16,
    /// Destination port in host byte order.
    pub dst_port: u16,
    /// IP protocol (6=TCP, 17=UDP).
    pub protocol: u8,
    /// Padding for alignment.
    pub _pad: [u8; 3],
}

/// Value stored in the conntrack map.
#[repr(C)]
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct CtValue {
    /// DNAT'd backend IP in network byte order.
    pub backend_ip: u32,
    /// DNAT'd backend port in host byte order.
    pub backend_port: u16,
    /// Original service VIP in network byte order (for reverse SNAT).
    pub origin_ip: u32,
    /// Original service port in host byte order (for reverse SNAT).
    pub origin_port: u16,
    /// TCP state flags for connection tracking.
    pub flags: u8,
    /// Padding.
    pub _pad: u8,
    /// Timestamp (bpf_ktime_get_ns, for session affinity).
    pub timestamp: u64,
}
```

**Step 2: Add new constants**

Add after `CONFIG_KEY_POD_CIDR_PREFIX_LEN` (line 199):

```rust
/// L4 LB enabled: 0 = off, 1 = on.
pub const CONFIG_KEY_L4LB_ENABLED: u32 = 10;
```

Add after map sizes section (after line 316):

```rust
/// Maximum entries in the service map.
pub const MAX_SERVICES: u32 = 16384;
/// Maximum entries in the backend array.
pub const MAX_BACKENDS: u32 = 65536;
/// Maximum entries in the conntrack LRU map.
pub const MAX_CONNTRACK: u32 = 524288;
/// Maximum entries in the Maglev lookup table.
pub const MAX_MAGLEV: u32 = 1048576;
/// Maglev lookup table size per service.
pub const MAGLEV_TABLE_SIZE: u32 = 65537;
```

Add after egress action constants (after line 219):

```rust
// ---------------------------------------------------------------------------
// Service scope constants
// ---------------------------------------------------------------------------

/// ClusterIP service scope.
pub const SVC_SCOPE_CLUSTER_IP: u8 = 0;
/// NodePort service scope.
pub const SVC_SCOPE_NODE_PORT: u8 = 1;
/// ExternalIP service scope.
pub const SVC_SCOPE_EXTERNAL_IP: u8 = 2;
/// LoadBalancer service scope.
pub const SVC_SCOPE_LOAD_BALANCER: u8 = 3;

// ---------------------------------------------------------------------------
// Backend selection algorithm constants
// ---------------------------------------------------------------------------

/// Random backend selection (hash of 5-tuple).
pub const LB_ALG_RANDOM: u8 = 0;
/// Round-robin backend selection.
pub const LB_ALG_ROUND_ROBIN: u8 = 1;
/// Maglev consistent hashing.
pub const LB_ALG_MAGLEV: u8 = 2;

// ---------------------------------------------------------------------------
// Service flags
// ---------------------------------------------------------------------------

/// Session affinity enabled.
pub const SVC_FLAG_AFFINITY: u8 = 0x01;
/// externalTrafficPolicy: Local.
pub const SVC_FLAG_EXT_LOCAL: u8 = 0x02;
```

**Step 3: Add Pod implementations and size tests**

Add new types to the `impl_pod!` macro (around line 323):

```rust
ServiceKey,
ServiceValue,
BackendValue,
CtKey,
CtValue,
```

Add size and alignment tests:

```rust
#[test]
fn service_key_size() {
    assert_eq!(mem::size_of::<ServiceKey>(), 8);
}
#[test]
fn service_value_size() {
    // backend_count(2) + backend_offset(2) + algorithm(1) + flags(1) + affinity_timeout(2) + maglev_offset(4) = 12
    assert_eq!(mem::size_of::<ServiceValue>(), 12);
}
#[test]
fn backend_value_size() {
    // ip(4) + port(2) + pad(2) + node_ip(4) = 12
    assert_eq!(mem::size_of::<BackendValue>(), 12);
}
#[test]
fn ct_key_size() {
    // src_ip(4) + dst_ip(4) + src_port(2) + dst_port(2) + protocol(1) + pad(3) = 16
    assert_eq!(mem::size_of::<CtKey>(), 16);
}
#[test]
fn ct_value_size() {
    // backend_ip(4) + backend_port(2) + origin_ip(4) + origin_port(2) + flags(1) + pad(1) + timestamp(8) = 22
    // But with alignment to 8 (u64 field), likely 24
    assert_eq!(mem::size_of::<CtValue>(), 24);
}
```

**Step 4: Run tests**

Run: `cd dataplane/novanet-common && cargo test`
Expected: All existing tests pass + new size/alignment tests pass.

**Step 5: Commit**

```bash
git add dataplane/novanet-common/src/lib.rs
git commit -m "feat(l4lb): add service, backend, and conntrack map types to novanet-common"
```

---

## Task 2: Add eBPF maps and service lookup to eBPF program

**Files:**
- Modify: `dataplane/novanet-ebpf/src/main.rs`

**Step 1: Add new eBPF maps**

After the existing map declarations (after line 55, `DROP_COUNTERS`):

```rust
#[map]
static SERVICES: HashMap<ServiceKey, ServiceValue> = HashMap::with_max_entries(MAX_SERVICES, 0);

#[map]
static BACKENDS: Array<BackendValue> = Array::with_max_entries(MAX_BACKENDS, 0);

#[map]
static CONNTRACK: LruHashMap<CtKey, CtValue> = LruHashMap::with_max_entries(MAX_CONNTRACK, 0);

#[map]
static MAGLEV: Array<u32> = Array::with_max_entries(MAX_MAGLEV, 0);

#[map]
static RR_COUNTERS: PerCpuArray<u32> = PerCpuArray::with_max_entries(MAX_SERVICES, 0);
```

Add necessary imports at the top of the file. The `LruHashMap` and `Array` types come from `aya_ebpf::maps`.

**Step 2: Add service lookup helper**

Add a helper function that does the full service lookup + backend selection + DNAT:

```rust
/// Look up a service by VIP and port. Returns the selected backend if found.
#[inline(always)]
fn service_lookup(
    dst_ip: u32,
    dst_port: u16,
    protocol: u8,
    src_ip: u32,
    src_port: u16,
) -> Option<(u32, u16, u32, u16)> {
    // (backend_ip, backend_port, origin_ip, origin_port)

    // Check if L4LB is enabled.
    if get_config(CONFIG_KEY_L4LB_ENABLED) == 0 {
        return None;
    }

    // First check conntrack for existing connection.
    let ct_key = CtKey {
        src_ip,
        dst_ip,
        src_port,
        dst_port,
        protocol,
        _pad: [0; 3],
    };
    if let Some(ct) = unsafe { CONNTRACK.get(&ct_key) } {
        let backend_ip = ct.backend_ip;
        let backend_port = ct.backend_port;
        let origin_ip = ct.origin_ip;
        let origin_port = ct.origin_port;
        return Some((backend_ip, backend_port, origin_ip, origin_port));
    }

    // Look up service — try ClusterIP scope first.
    let svc_key = ServiceKey {
        ip: dst_ip,
        port: dst_port,
        protocol,
        scope: SVC_SCOPE_CLUSTER_IP,
    };
    let svc = unsafe { SERVICES.get(&svc_key) }?;

    let count = svc.backend_count;
    if count == 0 {
        return None;
    }
    let offset = svc.backend_offset;
    let algorithm = svc.algorithm;

    // Select backend index.
    let idx = match algorithm {
        LB_ALG_ROUND_ROBIN => {
            // Use per-CPU counter for round-robin.
            let counter = unsafe { RR_COUNTERS.get_ptr_mut(offset as u32) };
            if let Some(c) = counter {
                let val = unsafe { *c };
                unsafe { *c = val.wrapping_add(1) };
                val % (count as u32)
            } else {
                0
            }
        }
        LB_ALG_MAGLEV => {
            // Hash 5-tuple and look up in Maglev table.
            let hash = hash_5tuple(src_ip, dst_ip, src_port, dst_port, protocol);
            let maglev_idx = svc.maglev_offset + (hash % MAGLEV_TABLE_SIZE);
            if let Some(backend_idx) = unsafe { MAGLEV.get(maglev_idx) } {
                *backend_idx
            } else {
                0
            }
        }
        _ => {
            // Random (default): hash 5-tuple mod count.
            let hash = hash_5tuple(src_ip, dst_ip, src_port, dst_port, protocol);
            hash % (count as u32)
        }
    };

    // Look up the selected backend.
    let backend_array_idx = (offset as u32) + idx;
    let backend = unsafe { BACKENDS.get(backend_array_idx) }?;
    let backend_ip = backend.ip;
    let backend_port = backend.port;

    // Create conntrack entry (forward direction).
    let ct_val = CtValue {
        backend_ip,
        backend_port,
        origin_ip: dst_ip,
        origin_port: dst_port,
        flags: 0,
        _pad: 0,
        timestamp: 0, // TODO: bpf_ktime_get_ns when available
    };
    let _ = unsafe { CONNTRACK.insert(&ct_key, &ct_val, 0) };

    // Create reverse conntrack entry (for return traffic SNAT).
    let rev_ct_key = CtKey {
        src_ip: backend_ip,
        dst_ip: src_ip,
        src_port: backend_port,
        dst_port: src_port,
        protocol,
        _pad: [0; 3],
    };
    let rev_ct_val = CtValue {
        backend_ip: 0,       // Not used for reverse.
        backend_port: 0,
        origin_ip: dst_ip,   // The VIP to restore as src.
        origin_port: dst_port,
        flags: 0,
        _pad: 0,
        timestamp: 0,
    };
    let _ = unsafe { CONNTRACK.insert(&rev_ct_key, &rev_ct_val, 0) };

    Some((backend_ip, backend_port, dst_ip, dst_port))
}

/// Simple hash of 5-tuple for backend selection.
#[inline(always)]
fn hash_5tuple(src_ip: u32, dst_ip: u32, src_port: u16, dst_port: u16, proto: u8) -> u32 {
    // FNV-1a inspired hash (simple, fast, good distribution).
    let mut h: u32 = 2166136261;
    h ^= src_ip;
    h = h.wrapping_mul(16777619);
    h ^= dst_ip;
    h = h.wrapping_mul(16777619);
    h ^= src_port as u32;
    h = h.wrapping_mul(16777619);
    h ^= dst_port as u32;
    h = h.wrapping_mul(16777619);
    h ^= proto as u32;
    h = h.wrapping_mul(16777619);
    h
}
```

**Step 3: Add DNAT helper**

```rust
/// Perform DNAT: rewrite destination IP and port in the packet, update checksums.
#[inline(always)]
fn perform_dnat(
    ctx: &mut TcContext,
    l4_offset: usize,
    protocol: u8,
    old_ip: u32,
    new_ip: u32,
    old_port: u16,
    new_port: u16,
) -> Result<(), ()> {
    // Rewrite destination IP in IPv4 header.
    // dst_addr is at offset ETH_HLEN + 16.
    let ip_dst_offset = (ETH_HLEN + 16) as u32;
    let old_ip_be = old_ip.to_be();
    let new_ip_be = new_ip.to_be();

    // Update IP checksum (at ETH_HLEN + 10).
    let ret = unsafe {
        aya_ebpf::helpers::bpf_l3_csum_replace(
            ctx.as_ptr() as *mut _,
            (ETH_HLEN + 10) as u32 as u64,
            old_ip_be as u64,
            new_ip_be as u64,
            4,
        )
    };
    if ret != 0 { return Err(()); }

    // Write new destination IP.
    ctx.store(ETH_HLEN + 16, &new_ip_be, 0).map_err(|_| ())?;

    // Update L4 checksum and port.
    if protocol == 6 || protocol == 17 {
        // L4 checksum offset: TCP=16, UDP=6 (relative to L4 start).
        let l4_csum_offset = if protocol == 6 { l4_offset + 16 } else { l4_offset + 6 };

        // Update L4 checksum for IP change.
        let ret = unsafe {
            aya_ebpf::helpers::bpf_l4_csum_replace(
                ctx.as_ptr() as *mut _,
                l4_csum_offset as u64,
                old_ip_be as u64,
                new_ip_be as u64,
                0x01 | 4, // BPF_F_PSEUDO_HDR | sizeof(u32)
            )
        };
        if ret != 0 { return Err(()); }

        // Rewrite destination port (at l4_offset + 2 for both TCP and UDP).
        let old_port_be = old_port.to_be();
        let new_port_be = new_port.to_be();

        let ret = unsafe {
            aya_ebpf::helpers::bpf_l4_csum_replace(
                ctx.as_ptr() as *mut _,
                l4_csum_offset as u64,
                old_port_be as u64,
                new_port_be as u64,
                2,
            )
        };
        if ret != 0 { return Err(()); }

        ctx.store(l4_offset + 2, &new_port_be, 0).map_err(|_| ())?;
    }

    Ok(())
}

/// Perform reverse SNAT: rewrite source IP and port back to VIP.
#[inline(always)]
fn perform_snat(
    ctx: &mut TcContext,
    l4_offset: usize,
    protocol: u8,
    old_ip: u32,
    new_ip: u32,
    old_port: u16,
    new_port: u16,
) -> Result<(), ()> {
    // Rewrite source IP in IPv4 header.
    // src_addr is at offset ETH_HLEN + 12.
    let old_ip_be = old_ip.to_be();
    let new_ip_be = new_ip.to_be();

    let ret = unsafe {
        aya_ebpf::helpers::bpf_l3_csum_replace(
            ctx.as_ptr() as *mut _,
            (ETH_HLEN + 10) as u32 as u64,
            old_ip_be as u64,
            new_ip_be as u64,
            4,
        )
    };
    if ret != 0 { return Err(()); }

    ctx.store(ETH_HLEN + 12, &new_ip_be, 0).map_err(|_| ())?;

    if protocol == 6 || protocol == 17 {
        let l4_csum_offset = if protocol == 6 { l4_offset + 16 } else { l4_offset + 6 };

        let ret = unsafe {
            aya_ebpf::helpers::bpf_l4_csum_replace(
                ctx.as_ptr() as *mut _,
                l4_csum_offset as u64,
                old_ip_be as u64,
                new_ip_be as u64,
                0x01 | 4,
            )
        };
        if ret != 0 { return Err(()); }

        // Rewrite source port (at l4_offset + 0 for both TCP and UDP).
        let old_port_be = old_port.to_be();
        let new_port_be = new_port.to_be();

        let ret = unsafe {
            aya_ebpf::helpers::bpf_l4_csum_replace(
                ctx.as_ptr() as *mut _,
                l4_csum_offset as u64,
                old_port_be as u64,
                new_port_be as u64,
                2,
            )
        };
        if ret != 0 { return Err(()); }

        ctx.store(l4_offset, &new_port_be, 0).map_err(|_| ())?;
    }

    Ok(())
}
```

**Step 4: Integrate into tc_egress**

In `try_tc_egress`, after parsing the packet (after `parse_l4_ports`) and before the endpoint lookup, insert:

```rust
    // --- L4 LB: Service DNAT ---
    if let Some((backend_ip, backend_port, _origin_ip, _origin_port)) =
        service_lookup(dst_ip, dst_port, protocol, src_ip, src_port)
    {
        // Perform DNAT: rewrite dst to backend.
        if perform_dnat(ctx, l4_offset, protocol, dst_ip, backend_ip, dst_port, backend_port).is_err() {
            inc_drop_counter(DROP_REASON_NO_ROUTE);
            return Ok(BPF_TC_ACT_SHOT as i32);
        }
        // Update local variables so subsequent endpoint lookup uses the real backend IP.
        dst_ip = backend_ip;
        dst_port = backend_port;
    }
```

Note: `dst_ip` and `dst_port` must be declared as `let mut` earlier in the function.

**Step 5: Integrate reverse SNAT into tc_ingress**

In `try_tc_ingress`, after parsing the packet, add reverse conntrack lookup:

```rust
    // --- L4 LB: Reverse SNAT ---
    if get_config(CONFIG_KEY_L4LB_ENABLED) != 0 {
        let rev_key = CtKey {
            src_ip,
            dst_ip,
            src_port,
            dst_port,
            protocol,
            _pad: [0; 3],
        };
        if let Some(ct) = unsafe { CONNTRACK.get(&rev_key) } {
            let origin_ip = ct.origin_ip;
            let origin_port = ct.origin_port;
            if origin_ip != 0 {
                let _ = perform_snat(ctx, l4_offset, protocol, src_ip, origin_ip, src_port, origin_port);
            }
        }
    }
```

**Step 6: Add same reverse SNAT to tc_tunnel_ingress**

Same pattern in `try_tc_tunnel_ingress` for cross-node return traffic.

**Step 7: Add tc_host_ingress program**

New TC program attached to the node's physical interface for NodePort/ExternalIP:

```rust
#[classifier]
pub fn tc_host_ingress(mut ctx: TcContext) -> i32 {
    match try_tc_host_ingress(&mut ctx) {
        Ok(action) => action,
        Err(_) => BPF_TC_ACT_OK as i32,
    }
}

#[inline(always)]
fn try_tc_host_ingress(ctx: &mut TcContext) -> Result<i32, ()> {
    if get_config(CONFIG_KEY_L4LB_ENABLED) == 0 {
        return Ok(BPF_TC_ACT_OK as i32);
    }

    let eth: EthHdr = ctx.load(0).map_err(|_| ())?;
    let ether_type = eth.ether_type;
    if ether_type != EtherType::Ipv4 {
        return Ok(BPF_TC_ACT_OK as i32);
    }

    let ipv4: Ipv4Hdr = ctx.load(ETH_HLEN).map_err(|_| ())?;
    let src_ip = u32::to_be(ipv4.src_addr);
    let dst_ip = u32::to_be(ipv4.dst_addr);
    let protocol = ipv4.proto as u8;
    let ihl = (ipv4.ihl() as usize) * 4;
    let l4_offset = ETH_HLEN + ihl;

    let (src_port, dst_port, _tcp_flags) = parse_l4_ports(ctx, l4_offset, protocol);

    // Check conntrack first (return traffic from backends).
    let ct_key = CtKey {
        src_ip,
        dst_ip,
        src_port,
        dst_port,
        protocol,
        _pad: [0; 3],
    };
    if let Some(ct) = unsafe { CONNTRACK.get(&ct_key) } {
        let origin_ip = ct.origin_ip;
        let origin_port = ct.origin_port;
        if origin_ip != 0 {
            let _ = perform_snat(ctx, l4_offset, protocol, src_ip, origin_ip, src_port, origin_port);
        }
        return Ok(BPF_TC_ACT_OK as i32);
    }

    // Check NodePort scope (ip=0 means match any node IP on this port).
    let svc_key = ServiceKey {
        ip: 0,
        port: dst_port,
        protocol,
        scope: SVC_SCOPE_NODE_PORT,
    };
    if let Some(_svc) = unsafe { SERVICES.get(&svc_key) } {
        if let Some((backend_ip, backend_port, _origin_ip, _origin_port)) =
            service_lookup_with_scope(0, dst_port, protocol, src_ip, src_port, SVC_SCOPE_NODE_PORT)
        {
            let _ = perform_dnat(ctx, l4_offset, protocol, dst_ip, backend_ip, dst_port, backend_port);
        }
    }

    // Check ExternalIP scope.
    let ext_key = ServiceKey {
        ip: dst_ip,
        port: dst_port,
        protocol,
        scope: SVC_SCOPE_EXTERNAL_IP,
    };
    if let Some(_svc) = unsafe { SERVICES.get(&ext_key) } {
        if let Some((backend_ip, backend_port, _origin_ip, _origin_port)) =
            service_lookup_with_scope(dst_ip, dst_port, protocol, src_ip, src_port, SVC_SCOPE_EXTERNAL_IP)
        {
            let _ = perform_dnat(ctx, l4_offset, protocol, dst_ip, backend_ip, dst_port, backend_port);
        }
    }

    Ok(BPF_TC_ACT_OK as i32)
}
```

Note: `service_lookup_with_scope` is a variant of `service_lookup` that accepts a `scope` parameter instead of hardcoding `SVC_SCOPE_CLUSTER_IP`. Refactor `service_lookup` to call this internally.

**Step 8: Build**

Run: `cd dataplane && cargo build --release`
Expected: Compiles without errors.

**Step 9: Commit**

```bash
git add dataplane/novanet-ebpf/src/main.rs
git commit -m "feat(l4lb): add eBPF maps, service lookup, DNAT/SNAT, and host ingress program"
```

---

## Task 3: Add gRPC service RPCs to proto and Rust dataplane

**Files:**
- Modify: `api/v1/novanet.proto`
- Modify: `dataplane/novanet-dataplane/src/server.rs`
- Modify: `dataplane/novanet-dataplane/src/maps.rs`

**Step 1: Add proto messages and RPCs**

Add to `DataplaneControl` service (after line 33, before `// Observability`):

```protobuf
  // L4 Load Balancer
  rpc UpsertService(UpsertServiceRequest) returns (UpsertServiceResponse);
  rpc DeleteService(DeleteServiceRequest) returns (DeleteServiceResponse);
  rpc UpsertBackends(UpsertBackendsRequest) returns (UpsertBackendsResponse);
  rpc SyncServices(SyncServicesRequest) returns (SyncServicesResponse);
```

Add message definitions (before `// --- List RPCs ---`):

```protobuf
// --- L4 Load Balancer ---

message UpsertServiceRequest {
  uint32 ip = 1;           // VIP in network byte order (0 for NodePort)
  uint32 port = 2;         // Service port
  uint32 protocol = 3;     // 6=TCP, 17=UDP
  uint32 scope = 4;        // 0=ClusterIP, 1=NodePort, 2=ExternalIP, 3=LoadBalancer
  uint32 backend_count = 5;
  uint32 backend_offset = 6;
  uint32 algorithm = 7;    // 0=random, 1=round-robin, 2=maglev
  uint32 flags = 8;
  uint32 affinity_timeout = 9;
  uint32 maglev_offset = 10;
}
message UpsertServiceResponse {}

message DeleteServiceRequest {
  uint32 ip = 1;
  uint32 port = 2;
  uint32 protocol = 3;
  uint32 scope = 4;
}
message DeleteServiceResponse {}

message UpsertBackendsRequest {
  repeated BackendEntry backends = 1;
}
message BackendEntry {
  uint32 index = 1;     // Array index in BACKENDS map
  uint32 ip = 2;        // Backend pod IP
  uint32 port = 3;      // Backend target port
  uint32 node_ip = 4;   // Node hosting this backend
}
message UpsertBackendsResponse {}

message SyncServicesRequest {
  repeated ServiceEntry services = 1;
  repeated BackendEntry backends = 2;
}
message ServiceEntry {
  uint32 ip = 1;
  uint32 port = 2;
  uint32 protocol = 3;
  uint32 scope = 4;
  uint32 backend_count = 5;
  uint32 backend_offset = 6;
  uint32 algorithm = 7;
  uint32 flags = 8;
  uint32 affinity_timeout = 9;
  uint32 maglev_offset = 10;
}
message SyncServicesResponse {
  uint32 services_synced = 1;
  uint32 backends_synced = 2;
}

// Maglev table upload
message UpsertMaglevTableRequest {
  uint32 offset = 1;              // Starting offset in MAGLEV array
  repeated uint32 entries = 2;    // Backend indices (65537 entries per service)
}
message UpsertMaglevTableResponse {}
```

Add `UpsertMaglevTable` RPC to the service:
```protobuf
  rpc UpsertMaglevTable(UpsertMaglevTableRequest) returns (UpsertMaglevTableResponse);
```

Add to `AgentControl` service:
```protobuf
  rpc ListServices(ListServicesRequest) returns (ListServicesResponse);
```

Add list messages:
```protobuf
message ListServicesRequest {}
message ListServicesResponse {
  repeated ServiceInfo services = 1;
}
message ServiceInfo {
  string cluster_ip = 1;
  uint32 port = 2;
  string protocol = 3;
  string scope = 4;
  uint32 backend_count = 5;
  string algorithm = 6;
  repeated string backends = 7;  // "ip:port" strings
}
```

Update `GetDataplaneStatusResponse` to include service count:
```protobuf
  uint32 service_count = 8;
  uint32 conntrack_count = 9;
```

**Step 2: Regenerate proto**

Run: the project's proto generation command (check Makefile or build script).

**Step 3: Implement Rust gRPC handlers**

In `server.rs`, implement the new RPCs. Pattern follows existing `upsert_endpoint`/`delete_endpoint`:

- `upsert_service` → insert into SERVICES map
- `delete_service` → remove from SERVICES map
- `upsert_backends` → insert entries into BACKENDS array
- `sync_services` → full sync (clear + repopulate)
- `upsert_maglev_table` → write entries into MAGLEV array

In `maps.rs`, add map management methods to `MapManager`:

- `upsert_service(key: ServiceKey, value: ServiceValue)`
- `delete_service(key: ServiceKey)`
- `upsert_backend(index: u32, value: BackendValue)`
- `service_count() -> u32`

**Step 4: Build and test**

Run: `cd dataplane && cargo build --release`
Expected: Compiles.

Run: `cd dataplane/novanet-dataplane && cargo test`
Expected: All tests pass.

**Step 5: Commit**

```bash
git add api/v1/novanet.proto dataplane/novanet-dataplane/
git commit -m "feat(l4lb): add service/backend gRPC RPCs and Rust dataplane handlers"
```

---

## Task 4: Go agent — config, Service/EndpointSlice watcher

**Files:**
- Modify: `internal/config/config.go`
- Create: `internal/service/watcher.go`
- Create: `internal/service/allocator.go`
- Create: `internal/service/maglev.go`
- Create: `internal/service/watcher_test.go`
- Create: `internal/service/allocator_test.go`
- Create: `internal/service/maglev_test.go`

**Step 1: Add L4LB config field**

In `internal/config/config.go`, add to `Config` struct (after `Policy`):

```go
// L4LB holds L4 load balancer settings.
L4LB L4LBConfig `json:"l4lb"`
```

Add new config struct:

```go
// L4LBConfig holds L4 load balancer settings.
type L4LBConfig struct {
    // Enabled controls whether eBPF-based L4 load balancing is active.
    // When enabled, NovaNet replaces kube-proxy for Service DNAT.
    Enabled bool `json:"enabled"`

    // DefaultAlgorithm is the default backend selection algorithm.
    // Valid values: "random", "round-robin", "maglev". Default: "random".
    DefaultAlgorithm string `json:"default_algorithm"`
}
```

Update `DefaultConfig()`:

```go
L4LB: L4LBConfig{
    Enabled:          false,
    DefaultAlgorithm: "random",
},
```

Add env var expansion in `ExpandEnvVars`:

```go
if v := os.Getenv("NOVANET_L4LB_ENABLED"); v == "true" || v == "1" {
    cfg.L4LB.Enabled = true
}
```

**Step 2: Write backend slot allocator tests**

Create `internal/service/allocator_test.go`:

```go
package service

import "testing"

func TestAllocatorAllocFree(t *testing.T) {
    a := NewSlotAllocator(100)

    // Allocate 3 slots.
    offset, err := a.Alloc(3)
    if err != nil { t.Fatal(err) }
    if offset != 0 { t.Fatalf("expected offset 0, got %d", offset) }

    // Allocate 5 more.
    offset2, err := a.Alloc(5)
    if err != nil { t.Fatal(err) }
    if offset2 != 3 { t.Fatalf("expected offset 3, got %d", offset2) }

    // Free the first block.
    a.Free(0, 3)

    // Re-allocate 2 — should reuse freed space.
    offset3, err := a.Alloc(2)
    if err != nil { t.Fatal(err) }
    if offset3 != 0 { t.Fatalf("expected reuse at offset 0, got %d", offset3) }
}

func TestAllocatorExhaustion(t *testing.T) {
    a := NewSlotAllocator(4)
    _, err := a.Alloc(4)
    if err != nil { t.Fatal(err) }
    _, err = a.Alloc(1)
    if err == nil { t.Fatal("expected error on exhausted allocator") }
}
```

**Step 3: Implement allocator**

Create `internal/service/allocator.go`:

```go
package service

import (
    "errors"
    "sort"
)

var ErrNoFreeSlots = errors.New("no free backend slots available")

// SlotAllocator manages contiguous slot ranges in the flat backend array.
type SlotAllocator struct {
    maxSlots uint32
    free     []slotRange // sorted by offset
}

type slotRange struct {
    offset uint32
    count  uint32
}

func NewSlotAllocator(maxSlots uint32) *SlotAllocator {
    return &SlotAllocator{
        maxSlots: maxSlots,
        free:     []slotRange{{offset: 0, count: maxSlots}},
    }
}

func (a *SlotAllocator) Alloc(count uint32) (uint32, error) {
    for i, r := range a.free {
        if r.count >= count {
            offset := r.offset
            if r.count == count {
                a.free = append(a.free[:i], a.free[i+1:]...)
            } else {
                a.free[i] = slotRange{offset: r.offset + count, count: r.count - count}
            }
            return offset, nil
        }
    }
    return 0, ErrNoFreeSlots
}

func (a *SlotAllocator) Free(offset, count uint32) {
    a.free = append(a.free, slotRange{offset: offset, count: count})
    sort.Slice(a.free, func(i, j int) bool { return a.free[i].offset < a.free[j].offset })
    a.merge()
}

func (a *SlotAllocator) merge() {
    merged := make([]slotRange, 0, len(a.free))
    for _, r := range a.free {
        if len(merged) > 0 && merged[len(merged)-1].offset+merged[len(merged)-1].count == r.offset {
            merged[len(merged)-1].count += r.count
        } else {
            merged = append(merged, r)
        }
    }
    a.free = merged
}
```

**Step 4: Run allocator tests**

Run: `cd internal/service && go test -run TestAllocator -v`
Expected: PASS

**Step 5: Write Maglev tests**

Create `internal/service/maglev_test.go`:

```go
package service

import "testing"

func TestMaglevTableSize(t *testing.T) {
    backends := []string{"10.42.0.1:8080", "10.42.0.2:8080", "10.42.0.3:8080"}
    table := GenerateMaglevTable(backends, 65537)
    if len(table) != 65537 {
        t.Fatalf("expected 65537 entries, got %d", len(table))
    }
}

func TestMaglevDistribution(t *testing.T) {
    backends := []string{"10.42.0.1:8080", "10.42.0.2:8080", "10.42.0.3:8080"}
    table := GenerateMaglevTable(backends, 65537)

    counts := make(map[uint32]int)
    for _, idx := range table {
        counts[idx]++
    }

    // Each backend should get roughly 1/3 of entries (±5%).
    expected := 65537 / 3
    for idx, count := range counts {
        ratio := float64(count) / float64(expected)
        if ratio < 0.90 || ratio > 1.10 {
            t.Errorf("backend %d: got %d entries (expected ~%d, ratio %.2f)", idx, count, expected, ratio)
        }
    }
}

func TestMaglevConsistency(t *testing.T) {
    backends3 := []string{"a:80", "b:80", "c:80"}
    backends4 := []string{"a:80", "b:80", "c:80", "d:80"}

    table3 := GenerateMaglevTable(backends3, 65537)
    table4 := GenerateMaglevTable(backends4, 65537)

    // Most entries for backends a,b,c should remain stable.
    stable := 0
    for i := range table3 {
        if table3[i] < 3 && table3[i] == table4[i] {
            stable++
        }
    }
    // At least 60% should be stable (Maglev guarantee is much higher).
    ratio := float64(stable) / float64(65537)
    if ratio < 0.60 {
        t.Errorf("only %.1f%% entries stable after adding backend", ratio*100)
    }
}
```

**Step 6: Implement Maglev**

Create `internal/service/maglev.go`:

```go
package service

import "hash/fnv"

// GenerateMaglevTable generates a Maglev consistent hash lookup table.
// Each entry maps to a backend index (0-based).
func GenerateMaglevTable(backends []string, tableSize int) []uint32 {
    n := len(backends)
    if n == 0 {
        return make([]uint32, tableSize)
    }

    // Compute offset and skip for each backend.
    offsets := make([]uint32, n)
    skips := make([]uint32, n)
    for i, b := range backends {
        h := fnv.New64a()
        h.Write([]byte(b))
        hash := h.Sum64()
        offsets[i] = uint32(hash % uint64(tableSize))
        skips[i] = uint32(hash>>32%uint64(tableSize-1)) + 1
    }

    // Populate table using Maglev algorithm.
    table := make([]uint32, tableSize)
    for i := range table {
        table[i] = ^uint32(0) // sentinel "empty"
    }

    next := make([]uint32, n)
    for i := range next {
        next[i] = offsets[i]
    }

    filled := 0
    for filled < tableSize {
        for i := 0; i < n; i++ {
            pos := next[i]
            for table[pos] != ^uint32(0) {
                pos = (pos + skips[i]) % uint32(tableSize)
            }
            table[pos] = uint32(i)
            next[i] = (pos + skips[i]) % uint32(tableSize)
            filled++
            if filled >= tableSize {
                break
            }
        }
    }

    return table
}
```

**Step 7: Run Maglev tests**

Run: `cd internal/service && go test -run TestMaglev -v`
Expected: PASS

**Step 8: Write service watcher**

Create `internal/service/watcher.go` — watches `v1.Service` and `discovery.k8s.io/v1.EndpointSlice`, translates to gRPC calls. This is the largest file and follows the pattern of existing watchers in `internal/k8s/watchers.go` and `internal/policy/watcher.go`.

Key functions:
- `NewServiceWatcher(client kubernetes.Interface, dpClient DataplaneClient, allocator *SlotAllocator, defaultAlg string) *ServiceWatcher`
- `Start(ctx context.Context) error` — starts informers
- `reconcileService(svc *v1.Service)` — compute map entries from Service + EndpointSlices
- `deleteService(key string)` — remove entries and free backend slots

**Step 9: Run all service tests**

Run: `cd internal/service && go test -v`
Expected: PASS

**Step 10: Commit**

```bash
git add internal/config/config.go internal/service/
git commit -m "feat(l4lb): add Go service watcher, slot allocator, and maglev table generator"
```

---

## Task 5: Integrate watcher into agent main and add host interface attachment

**Files:**
- Modify: `cmd/novanet-agent/main.go`

**Step 1: Start service watcher when l4lb enabled**

In `main.go`, after the existing watcher initialization, add:

```go
if cfg.L4LB.Enabled {
    logger.Info("L4 LB enabled — starting service watcher")

    // Set L4LB config key in dataplane.
    // (via UpdateConfig RPC, set CONFIG_KEY_L4LB_ENABLED=1)

    allocator := service.NewSlotAllocator(65536)
    svcWatcher := service.NewServiceWatcher(k8sClient, dpClient, allocator, cfg.L4LB.DefaultAlgorithm)
    if err := svcWatcher.Start(ctx); err != nil {
        logger.Fatal("failed to start service watcher", zap.Error(err))
    }

    // Attach tc_host_ingress to physical interface for NodePort/ExternalIP.
    hostIface := detectHostInterface() // detect bond0 or eth0
    if _, err := dpClient.AttachProgram(ctx, &pb.AttachProgramRequest{
        InterfaceName: hostIface,
        AttachType:    pb.AttachType_ATTACH_TC_INGRESS,
    }); err != nil {
        logger.Error("failed to attach host ingress program", zap.Error(err))
    }
}
```

**Step 2: Add host interface detection**

```go
func detectHostInterface() string {
    // Check common physical interfaces.
    for _, name := range []string{"bond0", "eth0", "ens192", "enp0s3"} {
        if _, err := net.InterfaceByName(name); err == nil {
            return name
        }
    }
    // Fallback: find the interface with the default route.
    // (use ip route get 8.8.8.8 dev output)
    return "eth0"
}
```

**Step 3: Build**

Run: `go build ./cmd/novanet-agent/`
Expected: Compiles.

**Step 4: Commit**

```bash
git add cmd/novanet-agent/main.go
git commit -m "feat(l4lb): integrate service watcher and host interface attachment into agent"
```

---

## Task 6: Add novanetctl services command

**Files:**
- Modify: `cmd/novanetctl/` (CLI tool, add `services` subcommand)

**Step 1: Add `services` command**

List all services tracked by NovaNet L4 LB via the `ListServices` RPC.

Output format:
```
SERVICE              TYPE        CLUSTER-IP     PORT    BACKENDS  ALGORITHM
kubernetes           ClusterIP   10.43.0.1      443     3         random
kube-dns             ClusterIP   10.43.0.10     53      2         random
argocd-server        NodePort    10.43.35.212   80      1         random
```

**Step 2: Commit**

```bash
git add cmd/novanetctl/
git commit -m "feat(l4lb): add novanetctl services command"
```

---

## Task 7: Helm chart and operator updates

**Files:**
- Modify: `charts/novanet-operator/values.yaml`
- Modify: `charts/novanet-operator/templates/` (configmap template)

**Step 1: Add l4lb values**

```yaml
l4lb:
  enabled: false
  defaultAlgorithm: random  # random, round-robin, maglev
```

**Step 2: Wire into configmap template**

Ensure the config JSON includes `l4lb.enabled` and `l4lb.default_algorithm` fields.

**Step 3: Commit**

```bash
git add charts/
git commit -m "feat(l4lb): add Helm values for L4 LB configuration"
```

---

## Task 8: L4 LB benchmark script

**Files:**
- Create: `tests/benchmark/bench-l4lb.sh`

**Step 1: Write benchmark script**

Follow the pattern of `bench-throughput.sh`. Tests:

1. **Direct pod-to-pod baseline** (no DNAT) — c=1,4,16,64
2. **ClusterIP same-node** — client and backends on same node, via ClusterIP — c=1,4,16,64
3. **ClusterIP cross-node** — client on node A, backends on node B, via ClusterIP — c=1,4,16,64
4. **NodePort** — host-network client, via NodePort — c=1,4,16,64

Creates a Service with 3 backend replicas for each test. Measures the DNAT overhead vs direct.

**Step 2: Test locally**

Run: `KUBECONFIG=$HOME/.kube/config bash tests/benchmark/bench-l4lb.sh`
Expected: All tests produce valid QPS/latency results.

**Step 3: Commit**

```bash
git add tests/benchmark/bench-l4lb.sh
git commit -m "feat(l4lb): add L4 LB benchmark script"
```

---

## Task 9: Integration testing and validation

**Step 1: Deploy with l4lb enabled**

Update ArgoCD values:
```yaml
l4lb:
  enabled: true
```

Sync and verify all pods restart.

**Step 2: Verify service map population**

Run: `novanetctl services` — should list all cluster services.

**Step 3: Test ClusterIP connectivity**

```bash
kubectl run test-client --image=busybox --restart=Never -- wget -qO- http://kube-dns.kube-system:53
kubectl exec test-client -- wget -qO- http://echo-svc:8080
```

**Step 4: Test NodePort connectivity**

```bash
curl http://<node-ip>:30080
```

**Step 5: Disable kube-proxy**

For k3s: restart with `--disable-kube-proxy` flag, verify services still work.

**Step 6: Run benchmark**

```bash
KUBECONFIG=$HOME/.kube/config bash tests/benchmark/bench-l4lb.sh
```

**Step 7: Commit any fixes**

```bash
git commit -m "fix(l4lb): <description of any fixes found during integration>"
```

---

## Summary

| Task | Description | Key files |
|------|-------------|-----------|
| 1 | Common types (Rust) | `novanet-common/src/lib.rs` |
| 2 | eBPF maps + DNAT/SNAT programs | `novanet-ebpf/src/main.rs` |
| 3 | gRPC proto + Rust handlers | `novanet.proto`, `server.rs`, `maps.rs` |
| 4 | Go watcher + allocator + Maglev | `internal/service/` |
| 5 | Agent integration | `cmd/novanet-agent/main.go` |
| 6 | CLI command | `cmd/novanetctl/` |
| 7 | Helm chart | `charts/` |
| 8 | Benchmark script | `tests/benchmark/bench-l4lb.sh` |
| 9 | Integration testing | Live cluster validation |
