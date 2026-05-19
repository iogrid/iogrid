//! tonic-build generated protobuf bindings.
//!
//! The build script (`build.rs`) invokes `tonic-build` against the
//! top-level `proto/iogrid/workloads/v1/dispatch.proto`, producing the
//! prost-generated message types plus the `WorkloadDispatchServiceClient`
//! gRPC client stub. We re-export the workloads module as `workloads` and
//! the dependency common module as `common` so call-sites read naturally.
//!
//! These are the ONLY types that travel over the gRPC wire — the
//! hand-rolled [`crate::DispatchFrame`] enum in `lib.rs` remains the
//! public API consumed by the daemon supervisor; conversion lives in
//! [`crate::convert`].

#![allow(missing_docs)]
#![allow(clippy::all)]
#![allow(clippy::pedantic)]

pub mod common {
    pub mod v1 {
        tonic::include_proto!("iogrid.common.v1");
    }
}

pub mod workloads {
    pub mod v1 {
        tonic::include_proto!("iogrid.workloads.v1");
    }
}
