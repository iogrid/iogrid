# go-sdk changelog

All notable changes to `github.com/iogrid/go-sdk` are documented here. The
format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and this module adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] — TBD

### Added
- Initial public release of the Go SDK.
- Ten methods covering workload submission, lifecycle, streaming, API
  keys, usage, and invoices — see `sdks/README.md` for the matrix.
- Zero runtime dependencies — pure `net/http` + `encoding/json`.
- Streaming workload events via `(<-chan WorkloadEvent, <-chan error)`.
