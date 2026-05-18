package wireguard

import (
	"context"
	"net"
	"testing"
)

func TestMockLifecycle(t *testing.T) {
	m := NewMock()
	ctx := context.Background()
	addr, _ := net.ResolveUDPAddr("udp", ":51820")
	var priv [32]byte
	for i := range priv {
		priv[i] = byte(i)
	}
	if err := m.Start(ctx, addr, priv); err != nil {
		t.Fatalf("Start: %v", err)
	}
	pub := m.PublicKey()
	if pub == ([32]byte{}) {
		t.Error("PublicKey should be derived")
	}

	var peerPK [32]byte
	for i := range peerPK {
		peerPK[i] = byte(0xa0 | i)
	}
	_, ipnet, _ := net.ParseCIDR("10.99.0.42/32")
	ip := net.ParseIP("10.99.0.42")
	if err := m.AddPeer(ctx, peerPK, ip, []*net.IPNet{ipnet}); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}
	if !m.HasPeer(peerPK) {
		t.Error("HasPeer should be true")
	}
	if m.PeerCount() != 1 {
		t.Errorf("PeerCount = %d, want 1", m.PeerCount())
	}

	m.SimulateTraffic(peerPK, 1000, 2000)
	st, err := m.PeerStats(ctx, peerPK)
	if err != nil {
		t.Fatalf("PeerStats: %v", err)
	}
	if st.BytesReceived != 1000 || st.BytesSent != 2000 {
		t.Errorf("stats = %+v", st)
	}

	if err := m.RemovePeer(ctx, peerPK); err != nil {
		t.Fatalf("RemovePeer: %v", err)
	}
	if m.HasPeer(peerPK) {
		t.Error("HasPeer should be false after removal")
	}
}

func TestRemoveUnknownPeer(t *testing.T) {
	m := NewMock()
	var pk [32]byte
	pk[0] = 0xff
	err := m.RemovePeer(context.Background(), pk)
	if _, ok := err.(ErrUnknownPeer); !ok {
		t.Errorf("RemovePeer unknown = %T %v, want ErrUnknownPeer", err, err)
	}
}

func TestPeerStatsUnknown(t *testing.T) {
	m := NewMock()
	var pk [32]byte
	pk[0] = 0xee
	if _, err := m.PeerStats(context.Background(), pk); err == nil {
		t.Error("PeerStats unknown should error")
	}
}

func TestStopClearsPeers(t *testing.T) {
	m := NewMock()
	ctx := context.Background()
	_ = m.Start(ctx, &net.UDPAddr{Port: 1}, [32]byte{1})
	var pk [32]byte
	pk[0] = 0xaa
	_ = m.AddPeer(ctx, pk, net.ParseIP("10.99.0.5"), nil)
	_ = m.Stop(ctx)
	if m.PeerCount() != 0 {
		t.Error("Stop should clear peers")
	}
}
