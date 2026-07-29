package goed2k

import (
	"net"
	"testing"

	"github.com/goed2k/core/protocol"
)

func TestPublishSingleTransferKADIndexesSource(t *testing.T) {
	session, transfer := newTestTransfer(t)
	session.settings.EnableDHT = true
	transfer.state = Finished

	tracker := NewDHTTracker(0, 0)
	addr, err := net.ResolveUDPAddr("udp", "1.2.3.4:4672")
	if err != nil {
		t.Fatalf("resolve addr: %v", err)
	}
	tracker.AddNode(addr)
	session.dhtTracker = tracker

	ep, err := protocol.EndpointFromString("192.0.2.42", 4661)
	if err != nil {
		t.Fatalf("endpoint: %v", err)
	}
	session.publishSingleTransferKAD(tracker, ep, transfer)

	results := tracker.searchEntriesLocked(transfer.hash)
	if len(results) != 1 {
		t.Fatalf("expected 1 indexed source, got %d", len(results))
	}
	got, ok := results[0].SourceEndpoint()
	if !ok || got.String() != "192.0.2.42:4661" {
		t.Fatalf("unexpected source endpoint %v ok=%v", got, ok)
	}
}

func TestPublishSingleTransferKADIndexesKeyword(t *testing.T) {
	session, transfer := newTestTransfer(t)
	session.settings.EnableDHT = true
	transfer.state = Finished
	transfer.filePath = "/tmp/demo-movie.mkv"

	tracker := NewDHTTracker(0, 0)
	addr, err := net.ResolveUDPAddr("udp", "1.2.3.4:4672")
	if err != nil {
		t.Fatalf("resolve addr: %v", err)
	}
	tracker.AddNode(addr)
	session.dhtTracker = tracker

	ep, err := protocol.EndpointFromString("192.0.2.42", 4661)
	if err != nil {
		t.Fatalf("endpoint: %v", err)
	}
	session.publishSingleTransferKAD(tracker, ep, transfer)

	keyword := pickKadKeyword(transfer.FileName())
	keywordHash, err := protocol.HashFromData([]byte(keyword))
	if err != nil {
		t.Fatalf("keyword hash: %v", err)
	}
	results := tracker.keywordEntriesLocked(keywordHash)
	if len(results) != 1 {
		t.Fatalf("expected 1 keyword entry, got %d", len(results))
	}
	name, ok := results[0].StringTag(protocol.FTFilename)
	if !ok || name != transfer.FileName() {
		t.Fatalf("unexpected keyword entry name %q ok=%v", name, ok)
	}
}

func TestPublishTransferToKADSkipsWithoutEndpoint(t *testing.T) {
	session, transfer := newTestTransfer(t)
	session.settings.EnableDHT = true
	transfer.state = Finished
	session.dhtTracker = NewDHTTracker(0, 0)
	session.settings.ListenPort = 0

	session.PublishTransferToKAD(transfer)

	if session.lastKadPublishEndpoint.Defined() {
		t.Fatal("expected no publish when endpoint unavailable")
	}
}

func TestNoteKadPublishStoresEndpoint(t *testing.T) {
	session := NewSession(NewSettings())
	ep, err := protocol.EndpointFromString("192.0.2.10", 4661)
	if err != nil {
		t.Fatalf("endpoint: %v", err)
	}
	session.noteKadPublish(ep, 456)

	if !session.lastKadPublishEndpoint.Equal(ep) {
		t.Fatalf("expected stored endpoint %v, got %v", ep, session.lastKadPublishEndpoint)
	}
	if session.lastKadPeriodicPublishAt != 456 {
		t.Fatalf("expected timestamp 456, got %d", session.lastKadPeriodicPublishAt)
	}
}

func TestKadTagsForSharedFileLargeSize(t *testing.T) {
	const large = int64(0x100000001)
	tags := kadTagsForSharedFile("big.bin", large)
	if len(tags) != 3 {
		t.Fatalf("expected 3 tags for large file, got %d", len(tags))
	}
	if tags[2].ID != protocol.FTFileSizeHi {
		t.Fatalf("expected FTFileSizeHi tag, got %#v", tags[2])
	}
}
