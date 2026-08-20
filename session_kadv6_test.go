package goed2k

import (
	"net"
	"testing"

	"github.com/goed2k/core/protocol"
	kadv6 "github.com/goed2k/core/protocol/kadv6"
)

func TestSendDHTv6SourcesRequestReturnsFalseWithoutTracker(t *testing.T) {
	session, transfer := newTestTransfer(t)
	if session.SendDHTv6SourcesRequest(transfer.hash, transfer.size, transfer) {
		t.Fatal("expected false without kadv6 tracker")
	}
}

func TestMergeKADV6SearchResultsAddsIPv6Peers(t *testing.T) {
	session, transfer := newTestTransfer(t)
	session.transfers[transfer.hash] = transfer

	ip := net.ParseIP("2001:db8::42")
	entry := kadv6.SearchEntry{
		ID: kadv6.NewID(protocol.MustHashFromString("31D6CFE0D16AE931B73C59D7E0C089C0")),
		Tags: []kadv6.Tag{
			{Type: kadv6.TagTypeUint8, ID: kadv6.TagAddrFamily, UInt64: uint64(kadv6.AddrFamilyIPv6)},
			{Type: kadv6.TagTypeBytes, ID: kadv6.TagSourceIP6, Bytes: append([]byte(nil), ip.To16()...)},
			{Type: kadv6.TagTypeUint16, ID: kadv6.TagSourcePort, UInt64: 4662},
		},
	}

	session.mergeKADV6SearchResults(transfer.hash, transfer, []kadv6.SearchEntry{entry})
	if transfer.policy.Size() != 1 {
		t.Fatalf("expected 1 peer in policy, got %d", transfer.policy.Size())
	}
	peer := transfer.policy.peers[0]
	if peer.DialAddr == nil || peer.DialAddr.Port != 4662 {
		t.Fatalf("unexpected dial addr: %+v", peer.DialAddr)
	}
	if peer.SourceFlag&int(PeerKADV6) == 0 {
		t.Fatal("expected PeerKADV6 source flag")
	}
}

func TestMergeKADV6SearchResultsRejectsLowIDSourceType(t *testing.T) {
	session, transfer := newTestTransfer(t)
	ip := net.ParseIP("2001:db8::42")
	entry := kadv6.SearchEntry{
		ID: kadv6.NewID(protocol.MustHashFromString("31D6CFE0D16AE931B73C59D7E0C089C0")),
		Tags: []kadv6.Tag{
			{Type: kadv6.TagTypeUint8, ID: kadv6.TagSourceType, UInt64: 2},
			{Type: kadv6.TagTypeUint8, ID: kadv6.TagAddrFamily, UInt64: uint64(kadv6.AddrFamilyIPv6)},
			{Type: kadv6.TagTypeBytes, ID: kadv6.TagSourceIP6, Bytes: append([]byte(nil), ip.To16()...)},
			{Type: kadv6.TagTypeUint16, ID: kadv6.TagSourcePort, UInt64: 4662},
		},
	}
	session.mergeKADV6SearchResults(transfer.hash, transfer, []kadv6.SearchEntry{entry})
	if transfer.policy.Size() != 0 {
		t.Fatalf("KADV6 LowID 条目不得当作可直连来源, got %d", transfer.policy.Size())
	}
}

func TestMergeKADV6SearchResultsIgnoresStaleTransfer(t *testing.T) {
	session, transfer := newTestTransfer(t)
	session.transfers[transfer.hash] = transfer

	ip := net.ParseIP("2001:db8::1")
	entry := kadv6.SearchEntry{
		ID: kadv6.NewID(protocol.MustHashFromString("31D6CFE0D16AE931B73C59D7E0C089C0")),
		Tags: []kadv6.Tag{
			{Type: kadv6.TagTypeUint8, ID: kadv6.TagAddrFamily, UInt64: uint64(kadv6.AddrFamilyIPv6)},
			{Type: kadv6.TagTypeBytes, ID: kadv6.TagSourceIP6, Bytes: append([]byte(nil), ip.To16()...)},
			{Type: kadv6.TagTypeUint16, ID: kadv6.TagSourcePort, UInt64: 4662},
		},
	}

	otherSession, otherTransfer := newTestTransfer(t)
	_ = otherSession
	session.mergeKADV6SearchResults(transfer.hash, otherTransfer, []kadv6.SearchEntry{entry})
	if transfer.policy.Size() != 0 {
		t.Fatalf("expected no peers for stale transfer pointer, got %d", transfer.policy.Size())
	}
}

func TestRequestSourcesNowTriggersKADV6WhenTrackerHasNodes(t *testing.T) {
	session, transfer := newTestTransfer(t)
	addr := &net.TCPAddr{IP: net.IPv4(45, 82, 80, 155), Port: 5687}
	server := NewServerConnection("a", addr, session)
	server.handshakeCompleted = true
	session.serverConnection = server
	session.serverConnections["a"] = server

	tracker := NewKADV6Tracker(0, 0)
	v6, err := net.ResolveUDPAddr("udp6", "[::1]:4672")
	if err != nil {
		t.Fatalf("resolve udp6: %v", err)
	}
	tracker.AddNode(v6)
	session.SetDHTv6Tracker(tracker)

	if !session.RequestSourcesNow(transfer) {
		t.Fatal("expected source discovery to succeed")
	}
}
