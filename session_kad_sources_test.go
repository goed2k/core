package goed2k

import (
	"testing"

	"github.com/goed2k/core/protocol"
	kadproto "github.com/goed2k/core/protocol/kad"
)

func TestMergeKadSearchSourcesHighIDAddsDialablePeer(t *testing.T) {
	session, transfer := newTestTransfer(t)
	ep, err := protocol.EndpointFromString("192.0.2.10", 4662)
	if err != nil {
		t.Fatalf("endpoint: %v", err)
	}
	entry := kadproto.SearchEntry{
		Tags: []kadproto.Tag{
			{ID: kadproto.TagSourceType, UInt64: kadproto.SourceTypeHighID},
			{ID: kadproto.TagSourceIP, UInt64: uint64(uint32(ep.IP()))},
			{ID: kadproto.TagSourcePort, UInt64: uint64(ep.Port())},
		},
	}

	session.mergeKadSearchSources(transfer.hash, transfer, []kadproto.SearchEntry{entry})
	if transfer.policy.Size() != 1 {
		t.Fatalf("expected 1 peer, got %d", transfer.policy.Size())
	}
	peer := transfer.policy.peers[0]
	if peer.ServerClientID != 0 {
		t.Fatalf("HighID 不得进入回调路径, ServerClientID=%d", peer.ServerClientID)
	}
	if !peer.Endpoint.Equal(ep) {
		t.Fatalf("unexpected endpoint %v", peer.Endpoint)
	}
	if peer.SourceFlag&int(PeerDHT) == 0 {
		t.Fatal("expected PeerDHT flag")
	}
}

func TestMergeKadSearchSourcesLowIDUsesCallbackPeer(t *testing.T) {
	session, transfer := newTestTransfer(t)
	session.clientID = 0x0100007f
	entry := kadproto.SearchEntry{
		Tags: []kadproto.Tag{
			{ID: kadproto.TagSourceType, UInt64: kadproto.SourceTypeLowID},
			{ID: kadproto.TagSourceIP, UInt64: 4242},
			{ID: kadproto.TagSourcePort, UInt64: 4662},
		},
	}

	session.mergeKadSearchSources(transfer.hash, transfer, []kadproto.SearchEntry{entry})
	if transfer.policy.Size() != 1 {
		t.Fatalf("expected 1 peer, got %d", transfer.policy.Size())
	}
	peer := transfer.policy.peers[0]
	if peer.ServerClientID != 4242 {
		t.Fatalf("expected ServerClientID 4242, got %d", peer.ServerClientID)
	}
	if peer.Endpoint.Defined() {
		t.Fatalf("LowID 不得作为普通可拨号 endpoint, got %v", peer.Endpoint)
	}
	if peer.DialAddr != nil {
		t.Fatal("LowID 不得带 DialAddr")
	}
}

func TestMergeKadSearchSourcesType3AndClientIDOnly(t *testing.T) {
	session, transfer := newTestTransfer(t)
	entries := []kadproto.SearchEntry{
		{Tags: []kadproto.Tag{
			{ID: kadproto.TagSourceType, UInt64: kadproto.SourceTypeFirewalled},
			{ID: kadproto.TagSourceIP, UInt64: uint64(uint32(0x0100007f))},
			{ID: kadproto.TagSourcePort, UInt64: 4662},
			{ID: kadproto.TagClientLowID, UInt64: 777},
		}},
		{Tags: []kadproto.Tag{
			{ID: kadproto.TagClientLowID, UInt64: 888},
		}},
	}
	session.mergeKadSearchSources(transfer.hash, transfer, entries)
	if transfer.policy.Size() != 2 {
		t.Fatalf("expected 2 callback peers, got %d", transfer.policy.Size())
	}
	seen := map[int32]bool{}
	for _, peer := range transfer.policy.peers {
		if peer.Endpoint.Defined() {
			t.Fatalf("callback peer should not be dialable, got %v", peer.Endpoint)
		}
		seen[peer.ServerClientID] = true
	}
	if !seen[777] || !seen[888] {
		t.Fatalf("missing callback ids: %#v", seen)
	}
}

func TestMergeKadSearchSourcesKeepsHighIDAndLowIDSeparate(t *testing.T) {
	session, transfer := newTestTransfer(t)
	high, err := protocol.EndpointFromString("192.0.2.20", 4662)
	if err != nil {
		t.Fatalf("endpoint: %v", err)
	}
	entries := []kadproto.SearchEntry{
		{Tags: []kadproto.Tag{
			{ID: kadproto.TagSourceType, UInt64: kadproto.SourceTypeHighIDCrypt},
			{ID: kadproto.TagSourceIP, UInt64: uint64(uint32(high.IP()))},
			{ID: kadproto.TagSourcePort, UInt64: uint64(high.Port())},
		}},
		{Tags: []kadproto.Tag{
			{ID: kadproto.TagSourceType, UInt64: kadproto.SourceTypeLowID},
			{ID: kadproto.TagSourceIP, UInt64: 4242},
		}},
	}
	session.mergeKadSearchSources(transfer.hash, transfer, entries)
	if transfer.policy.Size() != 2 {
		t.Fatalf("expected highid+lowid peers, got %d", transfer.policy.Size())
	}
	var sawDirect, sawCallback bool
	for _, peer := range transfer.policy.peers {
		if peer.ServerClientID == 4242 && !peer.Endpoint.Defined() {
			sawCallback = true
		}
		if peer.ServerClientID == 0 && peer.Endpoint.Equal(high) {
			sawDirect = true
		}
	}
	if !sawDirect || !sawCallback {
		t.Fatalf("expected both kinds, peers=%#v", transfer.policy.peers)
	}
}

func TestMergeKadSearchSourcesDedupsSameLowID(t *testing.T) {
	session, transfer := newTestTransfer(t)
	entry := kadproto.SearchEntry{
		Tags: []kadproto.Tag{
			{ID: kadproto.TagSourceType, UInt64: kadproto.SourceTypeLowID},
			{ID: kadproto.TagSourceIP, UInt64: 4242},
		},
	}
	session.mergeKadSearchSources(transfer.hash, transfer, []kadproto.SearchEntry{entry, entry})
	if transfer.policy.Size() != 1 {
		t.Fatalf("expected deduped lowid peer, got %d", transfer.policy.Size())
	}
}

func TestMergeKadSearchSourcesIgnoresUnusableLowID(t *testing.T) {
	session, transfer := newTestTransfer(t)
	entry := kadproto.SearchEntry{
		Tags: []kadproto.Tag{
			{ID: kadproto.TagSourceType, UInt64: kadproto.SourceTypeLowID},
			{ID: kadproto.TagSourceIP, UInt64: uint64(uint32(0x0100007f))},
			{ID: kadproto.TagSourcePort, UInt64: 4662},
		},
	}
	session.mergeKadSearchSources(transfer.hash, transfer, []kadproto.SearchEntry{entry})
	if transfer.policy.Size() != 0 {
		t.Fatalf("type2 without client id must not become a peer, got %d", transfer.policy.Size())
	}
}

func TestMergeKadSearchSourcesIgnoresStaleTransfer(t *testing.T) {
	session, transfer := newTestTransfer(t)
	_, other := newTestTransfer(t)
	entry := kadproto.SearchEntry{
		Tags: []kadproto.Tag{
			{ID: kadproto.TagSourceType, UInt64: kadproto.SourceTypeHighID},
			{ID: kadproto.TagSourceIP, UInt64: uint64(uint32(0x0100007f))},
			{ID: kadproto.TagSourcePort, UInt64: 4662},
		},
	}
	session.mergeKadSearchSources(transfer.hash, other, []kadproto.SearchEntry{entry})
	if transfer.policy.Size() != 0 {
		t.Fatalf("expected no peers for stale transfer, got %d", transfer.policy.Size())
	}
}

func TestPolicyConnectOnePeerCallbackForKadLowID(t *testing.T) {
	session, transfer := newTestTransfer(t)
	session.clientID = 0x0100007f
	sc := NewServerConnection("srv", nil, session)
	sc.handshakeCompleted = true
	session.serverConnection = sc

	entry := kadproto.SearchEntry{
		Tags: []kadproto.Tag{
			{ID: kadproto.TagSourceType, UInt64: kadproto.SourceTypeLowID},
			{ID: kadproto.TagClientLowID, UInt64: 5252},
		},
	}
	session.mergeKadSearchSources(transfer.hash, transfer, []kadproto.SearchEntry{entry})

	connected, err := transfer.policy.ConnectOnePeer(CurrentTime())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if !connected {
		t.Fatal("expected server callback for kad lowid source")
	}
	if _, ok := session.callbacks[5252]; !ok {
		t.Fatal("expected pending callback")
	}
	if transfer.policy.peers[0].Connection != nil {
		t.Fatal("callback source must not open a direct TCP connection")
	}
}

func TestPolicyConnectOnePeerDoesNotDialWhenLocalLowID(t *testing.T) {
	session, transfer := newTestTransfer(t)
	session.clientID = 12345
	peer := NewPeerWithSource(protocol.Endpoint{}, true, int(PeerDHT))
	peer.ServerClientID = 4242
	if _, err := transfer.policy.AddPeer(peer); err != nil {
		t.Fatalf("add peer: %v", err)
	}
	connected, err := transfer.policy.ConnectOnePeer(CurrentTime())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if connected {
		t.Fatal("本机 LowID 不能对回调源直连")
	}
	if _, ok := session.callbacks[4242]; ok {
		t.Fatal("本机 LowID 不得发出服务器回调")
	}
	if transfer.policy.peers[0].Connection != nil {
		t.Fatal("不得创建直连")
	}
}
