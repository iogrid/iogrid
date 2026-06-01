module github.com/iogrid/iogrid/cmd/iogrid

go 1.23.0

toolchain go1.23.4

require github.com/iogrid/iogrid/sdks/go/vpn v0.0.0

require (
	github.com/vishvananda/netlink v1.3.0 // indirect
	github.com/vishvananda/netns v0.0.4 // indirect
	golang.org/x/crypto v0.37.0 // indirect
	golang.org/x/net v0.39.0 // indirect
	golang.org/x/sys v0.32.0 // indirect
	golang.zx2c4.com/wintun v0.0.0-20230126152724-0fa3db229ce2 // indirect
	golang.zx2c4.com/wireguard v0.0.0-20231211153847-12269c276173 // indirect
)

replace github.com/iogrid/iogrid/sdks/go/vpn => ../../sdks/go/vpn
