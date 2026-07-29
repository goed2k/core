package goed2k

import (
	"net"
	"testing"

	"github.com/goed2k/core/protocol"
	kadv6proto "github.com/goed2k/core/protocol/kadv6"
)

func TestPublishSingleTransferKADV6IndexesSource(t *testing.T) {
	session, transfer := newTestTransfer(t)
	session.settings.EnableDHTv6 = true
	transfer.state = Finished

	tracker := NewKADV6Tracker(0, 0)
	seed := mustUDPAddrV6(t, "[2001:db8::1]:4672")
	tracker.AddNode(seed)
	session.dhtv6Tracker = tracker

	tcpAddr := &net.TCPAddr{IP: net.ParseIP("2001:db8::42"), Port: 4661}
	session.publishSingleTransferKADV6(tracker, tcpAddr, transfer)

	results := tracker.searchEntriesLocked(transfer.hash)
	if len(results) != 1 {
		t.Fatalf("expected 1 indexed source, got %d", len(results))
	}
	got, ok := results[0].SourceAddr()
	if !ok || got.String() != "[2001:db8::42]:4661" {
		t.Fatalf("unexpected source endpoint %v ok=%v", got, ok)
	}
}

func TestPublishSingleTransferKADV6IndexesKeyword(t *testing.T) {
	session, transfer := newTestTransfer(t)
	session.settings.EnableDHTv6 = true
	transfer.state = Finished
	transfer.filePath = "/tmp/demo-movie.mkv"

	tracker := NewKADV6Tracker(0, 0)
	seed := mustUDPAddrV6(t, "[2001:db8::1]:4672")
	tracker.AddNode(seed)
	session.dhtv6Tracker = tracker

	tcpAddr := &net.TCPAddr{IP: net.ParseIP("2001:db8::42"), Port: 4661}
	session.publishSingleTransferKADV6(tracker, tcpAddr, transfer)

	keyword := pickKadKeyword(transfer.FileName())
	keywordHash, err := protocol.HashFromData([]byte(keyword))
	if err != nil {
		t.Fatalf("keyword hash: %v", err)
	}
	results := tracker.keywordEntriesLocked(keywordHash)
	if len(results) != 1 {
		t.Fatalf("expected 1 keyword entry, got %d", len(results))
	}
	name, ok := results[0].StringTag(kadv6proto.TagName)
	if !ok || name != transfer.FileName() {
		t.Fatalf("unexpected keyword entry name %q ok=%v", name, ok)
	}
}

func TestPublishTransferToKADV6SkipsWithoutIPv6Endpoint(t *testing.T) {
	session, transfer := newTestTransfer(t)
	session.settings.EnableDHTv6 = true
	transfer.state = Finished
	session.dhtv6Tracker = NewKADV6Tracker(0, 0)

	session.PublishTransferToKADV6(transfer)

	if session.lastKadv6PublishTCPAddr != nil {
		t.Fatal("expected no publish when IPv6 endpoint unavailable")
	}
}

func TestNoteKadv6PublishClonesEndpoint(t *testing.T) {
	session := NewSession(NewSettings())
	addr := &net.TCPAddr{IP: net.ParseIP("2001:db8::42"), Port: 4661}
	session.noteKadv6Publish(addr, 123)

	if session.lastKadv6PublishTCPAddr == addr {
		t.Fatal("expected cloned publish endpoint")
	}
	if !kadv6PublishAddrsEqual(session.lastKadv6PublishTCPAddr, addr) {
		t.Fatalf("expected stored addr %v, got %v", addr, session.lastKadv6PublishTCPAddr)
	}
	if session.lastKadv6PeriodicPublishAt != 123 {
		t.Fatalf("expected timestamp 123, got %d", session.lastKadv6PeriodicPublishAt)
	}
}

func TestKadv6PublishAddrsEqual(t *testing.T) {
	a := &net.TCPAddr{IP: net.ParseIP("2001:db8::10"), Port: 4661}
	b := &net.TCPAddr{IP: net.ParseIP("2001:db8::20"), Port: 4661}
	if kadv6PublishAddrsEqual(a, b) {
		t.Fatal("expected different endpoints")
	}
	if !kadv6PublishAddrsEqual(a, a) {
		t.Fatal("expected equal endpoints")
	}
	if !kadv6PublishAddrsEqual(nil, nil) {
		t.Fatal("expected nil endpoints to be equal")
	}
}
