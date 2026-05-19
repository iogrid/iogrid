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
use crate::pb::workloads::v1 as wlv1;
use crate::DispatchFrame;
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
            let dt = DateTime::<Utc>::from_timestamp(t.seconds, t.nanos as u32)
                .unwrap_or_else(Utc::now);
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
        DispatchFrame::Ping { at_rfc3339 } => Frame::Ping(
            ts_from_rfc3339(at_rfc3339).unwrap_or(Timestamp {
                seconds: 0,
                nanos: 0,
            }),
        ),
        DispatchFrame::Drain => Frame::Drain(true),
    };
    wlv1::DispatchFrame { frame: Some(frame) }
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
                    String::new(),
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
            note: if u.note.is_empty() { None } else { Some(u.note) },
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
    })
}
