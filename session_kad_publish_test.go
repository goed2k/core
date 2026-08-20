package goed2k

import (
	"net"
	"testing"

	"github.com/goed2k/core/protocol"
	kadproto "github.com/goed2k/core/protocol/kad"
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

func TestKadSourcePublishEndpointUsesClientID(t *testing.T) {
	session := NewSession(NewSettings())
	session.settings.ListenPort = 4661

	session.clientID = 12345
	low := session.kadSourcePublishEndpoint()
	if !low.Defined() || low.IP() != 12345 || low.Port() != 4661 {
		t.Fatalf("lowid publish endpoint = %v", low)
	}

	session.clientID = 0x0100007f
	high := session.kadSourcePublishEndpoint()
	if !high.Defined() || high.IP() != 0x0100007f || high.Port() != 4661 {
		t.Fatalf("highid publish endpoint = %v", high)
	}
}

func TestPublishSourceSetsSourceTypeByClientID(t *testing.T) {
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

	lowEp := protocol.NewEndpoint(12345, 4661)
	_ = tracker.PublishSource(transfer.hash, lowEp, transfer.Size())
	lowResults := tracker.searchEntriesLocked(transfer.hash)
	if len(lowResults) != 1 {
		t.Fatalf("expected 1 lowid source, got %d", len(lowResults))
	}
	if sourceType, ok := lowResults[0].UIntTag(kadproto.TagSourceType); !ok || sourceType != kadproto.SourceTypeLowID {
		t.Fatalf("expected SourceType=2, got %d ok=%v", sourceType, ok)
	}
	if clientID, ok := lowResults[0].UIntTag(kadproto.TagClientLowID); !ok || clientID != 12345 {
		t.Fatalf("expected TagClientLowID=12345, got %d ok=%v", clientID, ok)
	}
	info, ok := lowResults[0].SourceInfo()
	if !ok || info.Kind != kadproto.SourceKindCallback || info.ClientID != 12345 {
		t.Fatalf("unexpected lowid source info %#v ok=%v", info, ok)
	}
	if _, ok := lowResults[0].SourceEndpoint(); ok {
		t.Fatal("published lowid must not expose a dialable endpoint")
	}

	highEp, err := protocol.EndpointFromString("192.0.2.42", 4661)
	if err != nil {
		t.Fatalf("high endpoint: %v", err)
	}
	_ = tracker.PublishSource(transfer.hash, highEp, transfer.Size())
	highResults := tracker.searchEntriesLocked(transfer.hash)
	if len(highResults) != 2 {
		t.Fatalf("expected highid and lowid to coexist, got %d", len(highResults))
	}
	var sawHigh bool
	for _, entry := range highResults {
		sourceType, _ := entry.UIntTag(kadproto.TagSourceType)
		if sourceType != kadproto.SourceTypeHighID {
			continue
		}
		sawHigh = true
		got, ok := entry.SourceEndpoint()
		if !ok || got.String() != "192.0.2.42:4661" {
			t.Fatalf("unexpected highid endpoint %v ok=%v", got, ok)
		}
	}
	if !sawHigh {
		t.Fatal("expected a SourceType=1 highid entry")
	}
}

func TestPublishTransferToKADUsesLowIDWithoutPublicIP(t *testing.T) {
	session, transfer := newTestTransfer(t)
	session.settings.EnableDHT = true
	session.settings.ListenPort = 4661
	session.clientID = 12345
	transfer.state = Finished

	tracker := NewDHTTracker(0, 0)
	addr, err := net.ResolveUDPAddr("udp", "1.2.3.4:4672")
	if err != nil {
		t.Fatalf("resolve addr: %v", err)
	}
	tracker.AddNode(addr)
	session.dhtTracker = tracker

	session.PublishTransferToKAD(transfer)
	results := tracker.searchEntriesLocked(transfer.hash)
	if len(results) != 1 {
		t.Fatalf("expected lowid publish without public ip, got %d", len(results))
	}
	if sourceType, ok := results[0].UIntTag(kadproto.TagSourceType); !ok || sourceType != kadproto.SourceTypeLowID {
		t.Fatalf("expected SourceType=2, got %d ok=%v", sourceType, ok)
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
