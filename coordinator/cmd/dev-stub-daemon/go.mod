module github.com/iogrid/iogrid/coordinator/cmd/dev-stub-daemon

go 1.23.0

toolchain go1.23.4

require (
	github.com/iogrid/iogrid/coordinator/internal/pb v0.0.0-00010101000000-000000000000
	golang.org/x/net v0.39.0
	google.golang.org/protobuf v1.35.1
)

require golang.org/x/text v0.24.0 // indirect

// connectrpc.com/connect is pulled in transitively via the pb package's
// generated workloadsv1connect — we don't reference it directly.
require connectrpc.com/connect v1.18.1 // indirect

replace github.com/iogrid/iogrid/coordinator/internal/pb => ../../internal/pb
