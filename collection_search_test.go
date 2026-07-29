package goed2k

import (
	"encoding/base64"
	"net"
	"testing"

	"github.com/goed2k/core/protocol"
	kadproto "github.com/goed2k/core/protocol/kad"
)

func TestParseEMuleCollectionLink(t *testing.T) {
	raw := "ed2k://|file|one.bin|1024|31D6CFE0D16AE931B73C59D7E0C089C0|/\ned2k://|file|two.bin|2048|31D6CFE0D14CE931B73C59D7E0C04BC0|/"
	payload := base64.StdEncoding.EncodeToString([]byte(raw))
	link, err := ParseEMuleLink("ed2k://|ed2kcollection|demo|" + payload + "|/")
	if err != nil {
		t.Fatalf("parse collection link: %v", err)
	}
	if link.Type != LinkCollection {
		t.Fatalf("expected collection link, got %s", link.Type)
	}
	if link.StringValue != "demo" {
		t.Fatalf("unexpected collection name %q", link.StringValue)
	}
	if len(link.FileLinks) != 2 {
		t.Fatalf("expected 2 file links, got %d", len(link.FileLinks))
	}
	if link.FileLinks[0].StringValue != "one.bin" {
		t.Fatalf("unexpected first file %q", link.FileLinks[0].StringValue)
	}
}

func TestParseEMuleCollectionContentBase64(t *testing.T) {
	raw := "ed2k://|file|song.mp3|2048|31D6CFE0D10EE931B73C59D7E0C06FC0|/"
	encoded := base64.StdEncoding.EncodeToString([]byte(raw))
	links, err := ParseEMuleCollectionContent(encoded)
	if err != nil {
		t.Fatalf("parse base64 collection: %v", err)
	}
	if len(links) != 1 || links[0].StringValue != "song.mp3" {
		t.Fatalf("unexpected links %#v", links)
	}
}

func TestClientAddCollectionLinkCreatesTransfers(t *testing.T) {
	settings := NewSettings()
	settings.ListenPort = 0
	settings.UDPPort = 0

	client := NewClient(settings)
	raw := "ed2k://|file|one.bin|1024|31D6CFE0D16AE931B73C59D7E0C089C0|/\ned2k://|file|two.bin|2048|31D6CFE0D14CE931B73C59D7E0C04BC0|/"
	payload := base64.StdEncoding.EncodeToString([]byte(raw))
	result, err := client.AddCollectionLink("ed2k://|ed2kcollection|demo|"+payload+"|/", t.TempDir())
	if err != nil {
		t.Fatalf("add collection link: %v", err)
	}
	if len(result.Handles) != 2 {
		t.Fatalf("expected 2 handles, got %d", len(result.Handles))
	}
}

func TestClientSearchDHTNotesUsesTracker(t *testing.T) {
	settings := NewSettings()
	settings.ListenPort = 0
	settings.UDPPort = 0

	client := NewClient(settings)
	tracker := client.EnableDHT()
	addr, err := net.ResolveUDPAddr("udp", "1.2.3.4:4672")
	if err != nil {
		t.Fatalf("resolve dht addr: %v", err)
	}
	fileHash := protocol.MustHashFromString("23A8CEFF57A7A32D562D649ED7893796")
	tracker.addOrUpdateNode(kadproto.NewID(fileHash), addr, 4661, 8, true)
	tracker.table.NodeSeen(tracker.nodes[addr.String()])

	called := false
	if !client.SearchDHTNotes(fileHash, func(entries []kadproto.SearchEntry) {
		called = true
	}) {
		t.Fatal("expected SearchDHTNotes to start traversal")
	}
	if called {
		t.Fatal("search callback should not be invoked in pure unit test setup")
	}
}

func TestMakeSearchResultFromKADIncludesNote(t *testing.T) {
	entry := kadproto.SearchEntry{
		ID: kadproto.NewID(protocol.MustHashFromString("31D6CFE0D16AE931B73C59D7E0C089C0")),
		Tags: []kadproto.Tag{
			{Type: kadproto.TagTypeString, ID: protocol.FTFilename, String: "demo.mp3"},
			{Type: kadproto.TagTypeString, ID: kadTagDescription, String: "great track"},
		},
	}
	result := makeSearchResultFromKAD(entry)
	if result.Note != "great track" {
		t.Fatalf("expected note %q, got %q", "great track", result.Note)
	}
}
