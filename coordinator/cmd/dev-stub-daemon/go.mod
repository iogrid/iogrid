module github.com/iogrid/iogrid/coordinator/cmd/dev-stub-daemon

go 1.22

toolchain go1.23.4

require (
	connectrpc.com/connect v1.18.1
	github.com/iogrid/iogrid/coordinator/internal/pb v0.0.0-00010101000000-000000000000
	golang.org/x/net v0.30.0
	google.golang.org/protobuf v1.35.1
)

replace github.com/iogrid/iogrid/coordinator/internal/pb => ../../internal/pb
