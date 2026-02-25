//! Flow event reader and streaming.
//!
//! Reads `FlowEvent` structs from the eBPF ring buffer and distributes them
//! to connected gRPC stream subscribers.

use novanet_common::FlowEvent as RawFlowEvent;
use tokio::sync::broadcast;

/// Capacity of the broadcast channel for flow events.
const FLOW_CHANNEL_CAPACITY: usize = 4096;

/// Global flow event broadcaster. Subscribers (gRPC StreamFlows clients)
/// receive a clone of the broadcast receiver.
static FLOW_BROADCASTER: std::sync::OnceLock<broadcast::Sender<crate::proto::FlowEvent>> =
    std::sync::OnceLock::new();

/// Get (or initialize) the global flow event broadcaster.
pub fn flow_broadcaster() -> &'static broadcast::Sender<crate::proto::FlowEvent> {
    FLOW_BROADCASTER.get_or_init(|| {
        let (tx, _) = broadcast::channel(FLOW_CHANNEL_CAPACITY);
        tx
    })
}

/// Subscribe to flow events. Returns a broadcast receiver.
pub fn subscribe_flows() -> broadcast::Receiver<crate::proto::FlowEvent> {
    flow_broadcaster().subscribe()
}

/// Convert a raw eBPF FlowEvent to the protobuf FlowEvent.
#[allow(dead_code)]
fn raw_to_proto(raw: &RawFlowEvent) -> crate::proto::FlowEvent {
    use crate::proto::{DropReason, PolicyAction};

    let verdict = match raw.verdict {
        novanet_common::ACTION_ALLOW => PolicyAction::Allow as i32,
        _ => PolicyAction::Deny as i32,
    };

    let drop_reason = match raw.drop_reason {
        novanet_common::DROP_REASON_NONE => DropReason::None as i32,
        novanet_common::DROP_REASON_POLICY_DENIED => DropReason::PolicyDenied as i32,
        novanet_common::DROP_REASON_NO_IDENTITY => DropReason::NoIdentity as i32,
        novanet_common::DROP_REASON_NO_ROUTE => DropReason::NoRoute as i32,
        novanet_common::DROP_REASON_NO_TUNNEL => DropReason::NoTunnel as i32,
        novanet_common::DROP_REASON_TTL_EXCEEDED => DropReason::TtlExceeded as i32,
        _ => DropReason::None as i32,
    };

    crate::proto::FlowEvent {
        src_ip: raw.src_ip,
        dst_ip: raw.dst_ip,
        src_identity: raw.src_identity,
        dst_identity: raw.dst_identity,
        protocol: raw.protocol as u32,
        src_port: raw.src_port as u32,
        dst_port: raw.dst_port as u32,
        verdict,
        bytes: raw.bytes,
        packets: raw.packets,
        timestamp_ns: raw.timestamp_ns as i64,
        drop_reason,
    }
}

/// Background task that reads flow events from the eBPF ring buffer.
/// Only runs on Linux with real eBPF maps.
#[cfg(target_os = "linux")]
pub async fn flow_reader_task(mut ring_buf: aya::maps::RingBuf<aya::maps::MapData>) {
    use std::mem;

    let tx = flow_broadcaster();

    tracing::info!("Flow event reader started");

    loop {
        // Poll the ring buffer. In a production implementation, we would use
        // epoll/AsyncFd for efficient waiting. For now, we poll with a small sleep.
        while let Some(item) = ring_buf.next() {
            let data = item.as_ref();
            if data.len() < mem::size_of::<RawFlowEvent>() {
                tracing::warn!(
                    len = data.len(),
                    expected = mem::size_of::<RawFlowEvent>(),
                    "Short flow event from ring buffer"
                );
                continue;
            }

            let raw: &RawFlowEvent = unsafe { &*(data.as_ptr() as *const RawFlowEvent) };
            let proto_event = raw_to_proto(raw);

            // Broadcast to all subscribers. If nobody is listening, that's fine.
            let _ = tx.send(proto_event);
        }

        // Sleep briefly before polling again.
        tokio::time::sleep(tokio::time::Duration::from_millis(10)).await;
    }
}

/// Stub for non-Linux platforms — the flow reader does nothing.
#[cfg(not(target_os = "linux"))]
#[allow(dead_code)]
pub async fn flow_reader_task(_ring_buf: ()) {
    tracing::info!("Flow event reader is a no-op on this platform");
    // Just park forever.
    std::future::pending::<()>().await;
}
