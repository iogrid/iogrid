//! Conversion between the crate-public hand-rolled [`crate::DispatchFrame`]
//! enum and the prost-generated `pb::workloads::v1::DispatchFrame` on the
//! wire.
//!
//! Keeping the public enum hand-rolled means the rest of the daemon
//! (`iogrid_core::WorkloadRouter` et al.) doesn't have to depend on a
//! generated module that lives in `OUT_DIR` and that bakes in prost'
//! type-aliasing. The conversion layer is the only place that knows
//! both representations.

use crate::pb::common::v1 as commonv1;
use crate::pb::providers::v1 as provv1;
use crate::pb::workloads::v1 as wlv1;
use crate::{DispatchFrame, Heartbeat};
use chrono::{DateTime, Utc};
use prost_types::Timestamp;

fn workload_type_from_slug(s: &str) -> i32 {
    use commonv1::WorkloadType as W;
    let t = match s {
        "BANDWIDTH" => W::Bandwidth,
        "DOCKER" => W::Docker,
        "GPU" => W::Gpu,
        "IOS_BUILD" => W::IosBuild,
        _ => W::Unspecified,
    };
    t as i32
}

fn workload_type_to_slug(v: i32) -> String {
    use commonv1::WorkloadType as W;
    let parsed = W::try_from(v).unwrap_or(W::Unspecified);
    match parsed {
        W::Bandwidth => "BANDWIDTH",
        W::Docker => "DOCKER",
        W::Gpu => "GPU",
        W::IosBuild => "IOS_BUILD",
        W::Unspecified => "UNSPECIFIED",
    }
    .to_string()
}

fn uuid(s: &str) -> commonv1::Uuid {
    commonv1::Uuid {
        value: s.to_string(),
    }
}

fn uuid_string(u: &Option<commonv1::Uuid>) -> String {
    u.as_ref().map(|x| x.value.clone()).unwrap_or_default()
}

fn ts_from_rfc3339(s: &str) -> Option<Timestamp> {
    DateTime::parse_from_rfc3339(s).ok().map(|dt| {
        let utc: DateTime<Utc> = dt.with_timezone(&Utc);
        Timestamp {
            seconds: utc.timestamp(),
            nanos: utc.timestamp_subsec_nanos() as i32,
        }
    })
}

fn ts_to_rfc3339(ts: &Option<Timestamp>) -> String {
    match ts {
        Some(t) => {
            let dt =
                DateTime::<Utc>::from_timestamp(t.seconds, t.nanos as u32).unwrap_or_else(Utc::now);
            dt.to_rfc3339()
        }
        None => String::new(),
    }
}

fn status_slug(v: i32) -> String {
    use wlv1::WorkloadStatus as W;
    let parsed = W::try_from(v).unwrap_or(W::Unspecified);
    match parsed {
        W::Queued => "queued",
        W::Dispatched => "dispatched",
        W::Running => "running",
        W::Succeeded => "succeeded",
        W::Failed => "failed",
        W::TimedOut => "timed_out",
        W::Cancelled => "cancelled",
        W::Rejected => "rejected",
        W::Unspecified => "unspecified",
    }
    .to_string()
}

fn status_from_slug(s: &str) -> i32 {
    use wlv1::WorkloadStatus as W;
    let v = match s {
        "queued" => W::Queued,
        "dispatched" => W::Dispatched,
        "running" => W::Running,
        "succeeded" => W::Succeeded,
        "failed" => W::Failed,
        "timed_out" => W::TimedOut,
        "cancelled" => W::Cancelled,
        "rejected" => W::Rejected,
        _ => W::Unspecified,
    };
    v as i32
}

/// Map the daemon's scheduler-state slug (e.g. `"active"`,
/// `"paused_bandwidth_cap"`) to the proto enum the coordinator expects.
fn scheduler_state_from_slug(s: &str) -> i32 {
    use provv1::SchedulerState as S;
    let v = match s {
        "active" => S::Active,
        "paused_bandwidth_cap" => S::PausedBandwidthCap,
        "paused_cpu_cap" => S::PausedCpuCap,
        "paused_memory_cap" => S::PausedMemoryCap,
        "paused_outside_calendar" => S::PausedOutsideCalendar,
        "paused_user_active" => S::PausedUserActive,
        "paused_operations" => S::PausedOperations,
        _ => S::Unspecified,
    };
    v as i32
}

/// Convert a daemon-side [`Heartbeat`] into the wire form.
///
/// #311: the prior in-memory test sink ignored the payload entirely, so
/// the coordinator never saw any heartbeat and `providers.last_seen_at`
/// was frozen at `registered_at`. The fields we populate here mirror
/// `iogrid.providers.v1.Heartbeat`: provider_id, scheduler state enum,
/// the usage snapshot (cpu / memory / idle / bandwidth + observed_at),
/// and the monotonic sequence number. `active_duration` is left unset —
/// the scheduler doesn't currently track per-tick active duration; this
/// is a follow-up (it's used only for billing audit on the server, and
/// the proto field is optional).
pub fn heartbeat_to_pb(h: &Heartbeat) -> provv1::Heartbeat {
    let observed_at = ts_from_rfc3339(&h.emitted_at);
    provv1::Heartbeat {
        provider_id: Some(uuid(&h.provider_id)),
        state: scheduler_state_from_slug(&h.state),
        usage: Some(provv1::CurrentUsageSnapshot {
            bandwidth_used_bytes_this_month: h.bandwidth_bytes_this_month,
            cpu_percent: h.cpu_pct as u32,
            memory_percent: h.memory_pct as u32,
            gpu_percent: 0,
            idle_seconds: h.idle_secs.min(u32::MAX as u64) as u32,
            observed_at,
        }),
        sequence: h.sequence,
        active_duration: None,
    }
}

/// Convert a daemon-side [`DispatchFrame`] into the wire form for sending.
pub fn frame_to_pb(f: &DispatchFrame) -> wlv1::DispatchFrame {
    use wlv1::dispatch_frame::Frame;
    let frame = match f {
        DispatchFrame::DaemonHello {
            provider_id,
            eligible_types,
            max_concurrent,
        } => Frame::DaemonHello(wlv1::DaemonHello {
            provider_id: Some(uuid(provider_id)),
            eligible_types: eligible_types
                .iter()
                .map(|s| workload_type_from_slug(s))
                .collect(),
            max_concurrent: *max_concurrent,
        }),
        DispatchFrame::CoordinatorHello {
            provider_id,
            accepted_at,
        } => Frame::CoordinatorHello(wlv1::CoordinatorHello {
            provider_id: Some(uuid(provider_id)),
            accepted_at: ts_from_rfc3339(accepted_at),
        }),
        DispatchFrame::Assignment {
            workload_id: _,
            attempt_id,
            workload_type: _,
            deadline_rfc3339,
            dispatch_token,
            payload_json: _,
        } => Frame::Assignment(wlv1::WorkloadAssignment {
            workload: None,
            attempt_id: Some(uuid(attempt_id)),
            deadline: ts_from_rfc3339(deadline_rfc3339),
            dispatch_token: dispatch_token.clone(),
        }),
        DispatchFrame::Update {
            workload_id,
            attempt_id,
            status,
            observed_at_rfc3339,
            note,
            bytes_in,
            bytes_out,
            exit_code,
            logs_s3_key,
            rejection_reason,
        } => Frame::Update(wlv1::WorkloadStatusUpdate {
            workload_id: Some(uuid(workload_id)),
            attempt_id: Some(uuid(attempt_id)),
            status: status_from_slug(status),
            observed_at: ts_from_rfc3339(observed_at_rfc3339),
            note: note.clone().unwrap_or_default(),
            bytes_in: *bytes_in,
            bytes_out: *bytes_out,
            exit_code: *exit_code,
            logs_s3_key: logs_s3_key.clone().unwrap_or_default(),
            artifact_s3_keys: Vec::new(),
            rejection_reason: rejection_reason.clone().unwrap_or_default(),
        }),
        DispatchFrame::Cancel { workload_id } => Frame::CancelWorkloadId(uuid(workload_id)),
        DispatchFrame::Ping { at_rfc3339 } => {
            Frame::Ping(ts_from_rfc3339(at_rfc3339).unwrap_or(Timestamp {
                seconds: 0,
                nanos: 0,
            }))
        }
        DispatchFrame::Drain => Frame::Drain(true),
        DispatchFrame::TunnelOpen {
            attempt_id,
            target_host_port,
        } => Frame::TunnelOpen(wlv1::TunnelOpen {
            attempt_id: Some(uuid(attempt_id)),
            target_host_port: target_host_port.clone(),
        }),
        DispatchFrame::TunnelData {
            attempt_id,
            payload,
        } => Frame::TunnelData(wlv1::TunnelData {
            attempt_id: Some(uuid(attempt_id)),
            payload: payload.clone(),
        }),
        DispatchFrame::TunnelClose { attempt_id, error } => Frame::TunnelClose(wlv1::TunnelClose {
            attempt_id: Some(uuid(attempt_id)),
            error: error.clone(),
        }),
    };
    wlv1::DispatchFrame { frame: Some(frame) }
}

/// Serialize the proto [`wlv1::Workload`]'s typed `payload` oneof into the
/// JSON the daemon's runner crates deserialize (see
/// `iogrid_core::WorkloadRouter::dispatch_assignment` →
/// `serde_json::from_str::<{Docker,Gpu,IosBuild}Workload>`).
///
/// The transport crate deliberately does NOT depend on the `iogrid-workload-*`
/// crates (that would create a dependency cycle, since `iogrid-core` depends on
/// both transport and the workload crates). So we hand-roll the JSON shape here
/// to mirror each runner's `#[derive(Serialize, Deserialize)]` struct field
/// names exactly. Returns an empty string when the oneof is unset (the router
/// rejects an empty payload with `payload_decode_failed`).
fn serialize_workload_payload(w: &wlv1::Workload) -> String {
    use serde_json::json;
    use wlv1::workload::Payload;

    // Map the proto `map<string,string> env` into the runner structs' env shape.
    // Docker/Gpu serde `env` is `Vec<(String, String)>` → JSON array of pairs.
    fn env_pairs(m: &std::collections::HashMap<String, String>) -> Vec<(String, String)> {
        let mut v: Vec<(String, String)> = m.iter().map(|(k, v)| (k.clone(), v.clone())).collect();
        // Deterministic order so the serialized payload is stable across runs.
        v.sort_by(|a, b| a.0.cmp(&b.0));
        v
    }
    fn duration_secs(d: &Option<prost_types::Duration>) -> u32 {
        d.as_ref()
            .map(|d| d.seconds.max(0) as u32)
            .unwrap_or_default()
    }

    let id = uuid_string(&w.id);
    let value = match w.payload.as_ref() {
        Some(Payload::Docker(d)) => json!({
            "id": id,
            "image": d.image,
            "cmd": d.command,
            "env": env_pairs(&d.env),
            "cpu_millis": d.min_cpu_cores.saturating_mul(1000),
            "memory_mib": d.min_memory_mib.min(u32::MAX as u64) as u32,
            "timeout_secs": duration_secs(&d.timeout),
            "network_name": serde_json::Value::Null,
        }),
        Some(Payload::Gpu(g)) => json!({
            "id": id,
            "image": g.image,
            "cmd": g.command,
            "env": env_pairs(&g.env),
            "vram_mib": g.min_vram_mib,
            "timeout_secs": duration_secs(&g.timeout),
            "mlx": serde_json::Value::Null,
        }),
        Some(Payload::IosBuild(i)) => json!({
            "id": id,
            "tart_image": i.tart_image,
            "repo_url": i.repo_url,
            "git_ref": i.git_ref,
            "build_command": i.build_command,
            "artifact_guest_path": i.artifact_guest_path,
            "upload_url": i.upload_url,
            "cpu": i.cpu,
            "memory_mib": i.memory_mib,
            // The runner has no per-workload wall-clock field on the proto; reuse
            // the boot timeout as a sane floor when the coordinator omits it.
            "timeout_secs": i.boot_timeout_secs,
            "boot_timeout_secs": i.boot_timeout_secs,
        }),
        // Bandwidth is routed by the bandwidth crate (not the JSON-payload
        // WorkloadRouter), and an unset oneof is a protocol violation — either
        // way there is no daemon serde target, so emit nothing.
        Some(Payload::Bandwidth(_)) | None => return String::new(),
    };
    serde_json::to_string(&value).unwrap_or_default()
}

/// Convert a wire-form `DispatchFrame` into the daemon-side enum. Returns
/// `None` if the oneof is unset (which would be a protocol violation).
pub fn frame_from_pb(pb: wlv1::DispatchFrame) -> Option<DispatchFrame> {
    use wlv1::dispatch_frame::Frame;
    Some(match pb.frame? {
        Frame::DaemonHello(dh) => DispatchFrame::DaemonHello {
            provider_id: uuid_string(&dh.provider_id),
            eligible_types: dh
                .eligible_types
                .into_iter()
                .map(workload_type_to_slug)
                .collect(),
            max_concurrent: dh.max_concurrent,
        },
        Frame::CoordinatorHello(ch) => DispatchFrame::CoordinatorHello {
            provider_id: uuid_string(&ch.provider_id),
            accepted_at: ts_to_rfc3339(&ch.accepted_at),
        },
        Frame::Assignment(a) => {
            let (workload_id, workload_type, payload_json) = match &a.workload {
                Some(w) => (
                    uuid_string(&w.id),
                    workload_type_to_slug(w.r#type),
                    serialize_workload_payload(w),
                ),
                None => (String::new(), "UNSPECIFIED".to_string(), String::new()),
            };
            DispatchFrame::Assignment {
                workload_id,
                attempt_id: uuid_string(&a.attempt_id),
                workload_type,
                deadline_rfc3339: ts_to_rfc3339(&a.deadline),
                dispatch_token: a.dispatch_token,
                payload_json,
            }
        }
        Frame::Update(u) => DispatchFrame::Update {
            workload_id: uuid_string(&u.workload_id),
            attempt_id: uuid_string(&u.attempt_id),
            status: status_slug(u.status),
            observed_at_rfc3339: ts_to_rfc3339(&u.observed_at),
            note: if u.note.is_empty() {
                None
            } else {
                Some(u.note)
            },
            bytes_in: u.bytes_in,
            bytes_out: u.bytes_out,
            exit_code: u.exit_code,
            logs_s3_key: if u.logs_s3_key.is_empty() {
                None
            } else {
                Some(u.logs_s3_key)
            },
            rejection_reason: if u.rejection_reason.is_empty() {
                None
            } else {
                Some(u.rejection_reason)
            },
        },
        Frame::CancelWorkloadId(c) => DispatchFrame::Cancel {
            workload_id: c.value,
        },
        Frame::Ping(p) => DispatchFrame::Ping {
            at_rfc3339: ts_to_rfc3339(&Some(p)),
        },
        Frame::Drain(_) => DispatchFrame::Drain,
        // PR #228 added these 3 tunnel-byte-pipe variants for the NAT'd-
        // daemon byte-forwarding path; the daemon-side TunnelManager
        // (see core/src/tunnel.rs) consumes them — see iogrid/iogrid#482.
        Frame::TunnelOpen(o) => DispatchFrame::TunnelOpen {
            attempt_id: uuid_string(&o.attempt_id),
            target_host_port: o.target_host_port,
        },
        Frame::TunnelData(d) => DispatchFrame::TunnelData {
            attempt_id: uuid_string(&d.attempt_id),
            payload: d.payload,
        },
        Frame::TunnelClose(c) => DispatchFrame::TunnelClose {
            attempt_id: uuid_string(&c.attempt_id),
            error: c.error,
        },
        #[allow(unreachable_patterns)]
        _ => {
            return None;
        }
    })
}

#[cfg(test)]
mod payload_tests {
    use super::*;
    use crate::pb::workloads::v1 as wlv1;
    use serde_json::Value;
    use std::collections::HashMap;

    fn assignment(workload: wlv1::Workload) -> wlv1::DispatchFrame {
        use wlv1::dispatch_frame::Frame;
        wlv1::DispatchFrame {
            frame: Some(Frame::Assignment(wlv1::WorkloadAssignment {
                workload: Some(workload),
                attempt_id: Some(uuid("attempt-1")),
                deadline: None,
                dispatch_token: "tok".into(),
            })),
        }
    }

    fn decoded_payload(workload: wlv1::Workload) -> Value {
        let frame = frame_from_pb(assignment(workload)).expect("frame decodes");
        match frame {
            DispatchFrame::Assignment { payload_json, .. } => {
                assert!(!payload_json.is_empty(), "payload must not be empty");
                serde_json::from_str(&payload_json).expect("payload is valid JSON")
            }
            other => panic!("expected Assignment, got {other:?}"),
        }
    }

    // The daemon's `iogrid_workload_ios::IosBuildWorkload` serde struct requires
    // exactly these field names — assert convert produces them so the router's
    // `serde_json::from_str::<IosBuildWorkload>` decode (which we can't call here
    // without a dependency cycle) cannot silently regress.
    #[test]
    fn ios_build_payload_has_all_runner_fields() {
        let w = wlv1::Workload {
            id: Some(uuid("11111111-1111-1111-1111-111111111111")),
            r#type: workload_type_from_slug("IOS_BUILD"),
            payload: Some(wlv1::workload::Payload::IosBuild(wlv1::IosBuildRequest {
                tart_image: "ghcr.io/cirruslabs/macos-sequoia-xcode:latest".into(),
                repo_url: "https://github.com/iogrid/iogrid.git".into(),
                git_ref: "main".into(),
                build_command: "xcodebuild archive".into(),
                upload_url: "https://s3/put".into(),
                artifact_guest_path: "/Users/admin/build/app.ipa".into(),
                cpu: 4,
                memory_mib: 8192,
                boot_timeout_secs: 300,
                ..Default::default()
            })),
            ..Default::default()
        };
        let v = decoded_payload(w);
        for k in [
            "id",
            "tart_image",
            "repo_url",
            "git_ref",
            "build_command",
            "artifact_guest_path",
            "upload_url",
            "cpu",
            "memory_mib",
            "timeout_secs",
            "boot_timeout_secs",
        ] {
            assert!(v.get(k).is_some(), "ios payload missing field {k}");
        }
        assert_eq!(v["repo_url"], "https://github.com/iogrid/iogrid.git");
        assert_eq!(v["cpu"], 4);
    }

    #[test]
    fn docker_payload_maps_cores_to_millis_and_env_to_pairs() {
        let mut env = HashMap::new();
        env.insert("A".to_string(), "1".to_string());
        let w = wlv1::Workload {
            id: Some(uuid("22222222-2222-2222-2222-222222222222")),
            r#type: workload_type_from_slug("DOCKER"),
            payload: Some(wlv1::workload::Payload::Docker(wlv1::DockerRequest {
                image: "ghcr.io/foo/bar:latest".into(),
                command: vec!["echo".into(), "hi".into()],
                env,
                min_cpu_cores: 2,
                min_memory_mib: 512,
                ..Default::default()
            })),
            ..Default::default()
        };
        let v = decoded_payload(w);
        assert_eq!(v["cpu_millis"], 2000);
        assert_eq!(v["memory_mib"], 512);
        assert_eq!(v["cmd"], serde_json::json!(["echo", "hi"]));
        // Rust serde `env: Vec<(String, String)>` => array of [k, v] pairs.
        assert_eq!(v["env"], serde_json::json!([["A", "1"]]));
        assert!(v.get("network_name").is_some());
    }

    #[test]
    fn gpu_payload_has_vram_and_optional_mlx() {
        let w = wlv1::Workload {
            id: Some(uuid("33333333-3333-3333-3333-333333333333")),
            r#type: workload_type_from_slug("GPU"),
            payload: Some(wlv1::workload::Payload::Gpu(wlv1::GpuRequest {
                image: "ghcr.io/iogrid/cuda:latest".into(),
                min_vram_mib: 16384,
                ..Default::default()
            })),
            ..Default::default()
        };
        let v = decoded_payload(w);
        assert_eq!(v["vram_mib"], 16384u64);
        assert!(v.get("mlx").is_some());
    }

    #[test]
    fn unset_payload_yields_empty_string() {
        let w = wlv1::Workload {
            id: Some(uuid("44444444-4444-4444-4444-444444444444")),
            payload: None,
            ..Default::default()
        };
        let frame = frame_from_pb(assignment(w)).expect("frame decodes");
        match frame {
            DispatchFrame::Assignment { payload_json, .. } => {
                assert!(payload_json.is_empty())
            }
            other => panic!("expected Assignment, got {other:?}"),
        }
    }
}
