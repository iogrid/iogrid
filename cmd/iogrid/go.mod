module github.com/iogrid/iogrid/cmd/iogrid

go 1.22

require github.com/iogrid/iogrid/sdks/go/vpn v0.0.0

require (
	golang.org/x/crypto v0.28.0 // indirect
	golang.org/x/net v0.30.0 // indirect
	golang.org/x/sys v0.26.0 // indirect
	golang.zx2c4.com/wintun v0.0.0-20230126152724-0fa3db229ce2 // indirect
	golang.zx2c4.com/wireguard v0.0.0-20231211153847-12269c276173 // indirect
)

replace github.com/iogrid/iogrid/sdks/go/vpn => ../../sdks/go/vpn
