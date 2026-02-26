# Remaining Fixes Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix IPAM persistence, add NAT masquerade, mount /lib/modules for PrepareOverlay.

**Architecture:** Three independent fixes: (1) file-backed IPAM under /var/lib/cni/networks/novanet, (2) iptables MASQUERADE in POSTROUTING for pod→external, (3) hostPath volume for /lib/modules.

**Tech Stack:** Go, netlink, iptables (via exec), Helm templates

---

### Task 1: Mount /lib/modules in DaemonSet

**Files:**
- Modify: `deploy/helm/novanet/templates/daemonset.yaml`

**Step 1:** Add volume mount to agent container and volume definition.

Add to agent volumeMounts (after bpf-maps mount at line 139):
```yaml
            - name: lib-modules
              mountPath: /lib/modules
              readOnly: true
```

Add to volumes section (after proc volume at line 209):
```yaml
        - name: lib-modules
          hostPath:
            path: /lib/modules
            type: Directory
```

**Step 2:** Verify with `helm template` that the YAML renders correctly.

**Step 3:** Commit.

---

### Task 2: IPAM File Persistence

**Files:**
- Modify: `internal/ipam/allocator.go`
- Modify: `internal/ipam/allocator_test.go`
- Modify: `deploy/helm/novanet/templates/daemonset.yaml` (mount /var/lib/cni)

**Step 1:** Add `stateDir` field to Allocator and a `NewAllocatorWithStateDir` constructor that:
- Takes an optional state directory path
- On init, scans existing IP files in `<stateDir>/` to rebuild the bitmap
- On `Allocate()`, writes `<stateDir>/<IP>` file (content: empty or container ID)
- On `Release()`, removes the file
- If stateDir is empty string, behaves like current in-memory only

**Step 2:** Add hostPath volume `/var/lib/cni` to DaemonSet for IPAM state persistence.

**Step 3:** Update agent main.go to pass state dir to allocator.

**Step 4:** Update tests.

**Step 5:** Commit.

---

### Task 3: NAT Masquerade for External Connectivity

**Files:**
- Create: `internal/masquerade/masquerade_linux.go`
- Create: `internal/masquerade/masquerade_other.go`
- Modify: `cmd/novanet-agent/main.go`

**Step 1:** Create masquerade package with `EnsureMasquerade(podCIDR, clusterCIDR string) error` that:
- Adds iptables rule: `-t nat -A POSTROUTING -s <podCIDR> ! -d <clusterCIDR> -j MASQUERADE`
- Uses `iptables` command via exec (same pattern as K3s/Flannel)
- Idempotent: checks if rule exists before adding
- Adds a comment for identification: `--comment "novanet masquerade"`

**Step 2:** Call `EnsureMasquerade` at agent startup after IPAM setup.

**Step 3:** Create non-Linux stub.

**Step 4:** Commit.

---
