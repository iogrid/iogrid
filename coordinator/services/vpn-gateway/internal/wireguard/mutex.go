package wireguard

import "sync"

// peerMutex is a thin RWMutex alias so the Mock's mu field reads
// naturally in code. We separate it from wireguard.go because we may
// later swap in a custom striped mutex for the real implementation
// without churning the Mock surface.
type peerMutex struct{ sync.RWMutex }
