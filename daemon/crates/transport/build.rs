//! Compile the workloads dispatch + providers scheduling protos into the
//! transport crate.
//!
//! Produces `OUT_DIR/iogrid.workloads.v1.rs`, `iogrid.providers.v1.rs`
//! (and `iogrid.common.v1.rs`) consumed via `tonic::include_proto!` in
//! `src/pb.rs`. We only need the client side — the daemon is a client of
//! `WorkloadDispatchService` and `SchedulingService`.
//!
//! Requires `protoc` on PATH (provided by the daemon-ci workflow via
//! `arduino/setup-protoc`).

fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Paths are relative to this crate's Cargo.toml.
    // proto root lives at the repo top-level `proto/` directory:
    //   daemon/crates/transport/build.rs -> ../../../proto/
    let proto_root = std::path::PathBuf::from("../../../proto");
    let dispatch_proto = proto_root.join("iogrid/workloads/v1/dispatch.proto");
    // #311: pulled in so the heartbeat pump can stream against the real
    // `iogrid.providers.v1.SchedulingService/StreamHeartbeats` bidi RPC
    // instead of the in-memory test sink.
    let scheduling_proto = proto_root.join("iogrid/providers/v1/scheduling.proto");

    // Re-run the build script only when the protos (or this build script)
    // change — avoids spurious rebuilds on every cargo invocation.
    println!("cargo:rerun-if-changed=build.rs");
    println!("cargo:rerun-if-changed={}", dispatch_proto.display());
    println!("cargo:rerun-if-changed={}", scheduling_proto.display());
    println!(
        "cargo:rerun-if-changed={}",
        proto_root
            .join("iogrid/workloads/v1/submit.proto")
            .display()
    );
    println!(
        "cargo:rerun-if-changed={}",
        proto_root.join("iogrid/common/v1/types.proto").display()
    );

    // Build both client (production) and server (tests use an in-process
    // tonic Server stub to assert the daemon-side handshake + pump).
    tonic_build::configure()
        .build_server(true)
        .build_client(true)
        .compile_protos(
            &[
                dispatch_proto.to_string_lossy().as_ref(),
                scheduling_proto.to_string_lossy().as_ref(),
            ],
            &[proto_root.to_string_lossy().as_ref()],
        )?;
    Ok(())
}
