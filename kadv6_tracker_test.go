package goed2k

import (
	"net"
	"testing"
	"time"

	"github.com/goed2k/core/protocol"
	kadv6proto "github.com/goed2k/core/protocol/kadv6"
)

func mustUDPAddrV6(t *testing.T, value string) *net.UDPAddr {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp6", value)
	if err != nil {
		t.Fatalf("resolve udp6 addr %s: %v", value, err)
	}
	return addr
}

func TestKADV6TrackerBootstrapResponseAddsContacts(t *testing.T) {
	tracker := NewKADV6Tracker(0, 0)
	addr := mustUDPAddrV6(t, "[2001:db8::1]:4672")
	contactIP := [16]byte{}
	copy(contactIP[:], net.ParseIP("::1").To16())
	tracker.handleBootstrapResponse(addr, kadv6proto.BootstrapRes{
		ID:      kadv6proto.NewID(protocol.MustHashFromString("23A8CEFF57A7A32D562D649ED7893796")),
		TCPPort: 4661,
		Version: kadv6proto.KademliaVersion,
		Contacts: []kadv6proto.EntryV6{
			{
				ID: kadv6proto.NewID(protocol.MustHashFromString("31D6CFE0D16AE931B73C59D7E0C089C0")),
				Endpoint: kadv6proto.EndpointV6{
					IP:      contactIP,
					UDPPort: 4672,
					TCPPort: 4661,
				},
				Version: kadv6proto.KademliaVersion,
			},
		},
	})
	if got := len(tracker.nodes); got != 2 {
		t.Fatalf("expected 2 nodes after bootstrap response, got %d", got)
	}
}

func TestKADV6TrackerFindResponseAddsDiscoveredNodes(t *testing.T) {
	tracker := NewKADV6Tracker(0, 0)
	target := protocol.MustHashFromString("23A8CEFF57A7A32D562D649ED7893796")
	addr := mustUDPAddrV6(t, "[::1]:4672")
	contactIP := [16]byte{}
	copy(contactIP[:], net.ParseIP("::1").To16())
	tracker.handleFindResponse(addr, kadv6proto.FindNodeRes{
		Target: kadv6proto.NewID(target),
		Results: []kadv6proto.EntryV6{
			{
				ID: kadv6proto.NewID(protocol.MustHashFromString("31D6CFE0D14CE931B73C59D7E0C04BC0")),
				Endpoint: kadv6proto.EndpointV6{
					IP:      contactIP,
					UDPPort: 4672,
					TCPPort: 4661,
				},
				Version: kadv6proto.KademliaVersion,
			},
		},
	})
	if got := len(tracker.nodes); got != 1 {
		t.Fatalf("expected 1 discovered node, got %d", got)
	}
}

func TestKADV6TrackerHandlePublishAndSearchSources(t *testing.T) {
	tracker := NewKADV6Tracker(0, 0)
	addr := mustUDPAddrV6(t, "[::1]:4672")
	fileHash := protocol.MustHashFromString("23A8CEFF57A7A32D562D649ED7893796")
	ip := net.ParseIP("::1").To16()
	req := kadv6proto.PublishSourcesReq{
		FileID: kadv6proto.NewID(fileHash),
		Source: kadv6proto.SearchEntry{
			ID: kadv6proto.NewID(protocol.MustHashFromString("31D6CFE0D16AE931B73C59D7E0C089C0")),
			Tags: []kadv6proto.Tag{
				{Type: kadv6proto.TagTypeUint8, ID: kadv6proto.TagSourceType, UInt64: 1},
				{Type: kadv6proto.TagTypeUint8, ID: kadv6proto.TagAddrFamily, UInt64: uint64(kadv6proto.AddrFamilyIPv6)},
				{Type: kadv6proto.TagTypeBytes, ID: kadv6proto.TagSourceIP6, Bytes: append([]byte(nil), ip...)},
				{Type: kadv6proto.TagTypeUint16, ID: kadv6proto.TagSourcePort, UInt64: 4662},
			},
		},
	}
	tracker.handlePublishSourcesRequest(addr, req)
	results := tracker.searchEntriesLocked(fileHash)
	if len(results) != 1 {
		t.Fatalf("expected 1 indexed result, got %d", len(results))
	}
	if tcp, ok := results[0].SourceAddr(); !ok || tcp.String() != "[::1]:4662" {
		t.Fatalf("unexpected indexed endpoint %v %v", tcp, ok)
	}
}

func TestKADV6TrackerHandlePublishAndSearchKeys(t *testing.T) {
	tracker := NewKADV6Tracker(0, 0)
	addr := mustUDPAddrV6(t, "[::1]:4672")
	keyword := protocol.MustHashFromString("23A8CEFF57A7A32D562D649ED7893796")
	req := kadv6proto.PublishKeysReq{
		KeywordID: kadv6proto.NewID(keyword),
		Sources: []kadv6proto.SearchEntry{
			{
				ID: kadv6proto.NewID(protocol.MustHashFromString("31D6CFE0D16AE931B73C59D7E0C089C0")),
				Tags: []kadv6proto.Tag{
					{Type: kadv6proto.TagTypeString, ID: kadv6proto.TagName, String: "demo.epub"},
				},
			},
		},
	}
	tracker.handlePublishKeysRequest(addr, req)
	results := tracker.keywordEntriesLocked(keyword)
	if len(results) != 1 {
		t.Fatalf("expected 1 keyword result, got %d", len(results))
	}
	if name, ok := results[0].StringTag(kadv6proto.TagName); !ok || name != "demo.epub" {
		t.Fatalf("unexpected keyword entry name %q %v", name, ok)
	}
}

func TestKADV6TrackerHandlePublishAndSearchNotes(t *testing.T) {
	tracker := NewKADV6Tracker(0, 0)
	addr := mustUDPAddrV6(t, "[::1]:4672")
	fileHash := protocol.MustHashFromString("23A8CEFF57A7A32D562D649ED7893796")
	req := kadv6proto.PublishNotesReq{
		FileID: kadv6proto.NewID(fileHash),
		Notes: []kadv6proto.SearchEntry{
			{
				ID: kadv6proto.NewID(protocol.MustHashFromString("31D6CFE0D16AE931B73C59D7E0C089C0")),
				Tags: []kadv6proto.Tag{
					{Type: kadv6proto.TagTypeString, ID: kadv6proto.TagName, String: "note text"},
				},
			},
		},
	}
	tracker.handlePublishNotesRequest(addr, req)
	results := tracker.notesEntriesLocked(fileHash)
	if len(results) != 1 {
		t.Fatalf("expected 1 note result, got %d", len(results))
	}
	if note, ok := results[0].StringTag(kadv6proto.TagName); !ok || note != "note text" {
		t.Fatalf("unexpected note %q %v", note, ok)
	}
}

func TestKADV6TrackerSnapshotAndApplyState(t *testing.T) {
	tracker := NewKADV6Tracker(0, 0)
	addr := mustUDPAddrV6(t, "[::1]:4672")
	router := mustUDPAddrV6(t, "[2001:db8::1]:4672")
	tracker.table.AddRouterNode(router)
	tracker.SetStoragePoint(router)
	tracker.addOrUpdateNodeLocked(kadv6proto.NewID(protocol.MustHashFromString("23A8CEFF57A7A32D562D649ED7893796")), addr, 4661, kadv6proto.KademliaVersion, true)
	tracker.nodes[addr.String()].Pinged = true
	tracker.table.NodeSeen(tracker.nodes[addr.String()])
	tracker.lastBootstrap = time.Now().Add(-time.Minute)
	tracker.lastRefresh = time.Now().Add(-time.Second)

	state := tracker.SnapshotState()
	if state == nil || len(state.Nodes) != 1 {
		t.Fatalf("expected one persisted DHTv6 node, got %+v", state)
	}

	restored := NewKADV6Tracker(0, 0)
	if err := restored.ApplyState(state); err != nil {
		t.Fatalf("apply kadv6 state: %v", err)
	}
	status := restored.Status()
	if !status.Bootstrapped {
		t.Fatal("expected restored tracker to be bootstrapped")
	}
	if status.RouterNodes != 1 {
		t.Fatalf("expected 1 router node, got %d", status.RouterNodes)
	}
	if status.StoragePoint != router.String() {
		t.Fatalf("expected storage point %s, got %s", router.String(), status.StoragePoint)
	}
}

func TestNormalizeUDPAddrV6RejectsIPv4(t *testing.T) {
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:4672")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if normalizeUDPAddrV6(addr) != nil {
		t.Fatal("expected ipv4 address to be rejected")
	}
	mapped := &net.UDPAddr{IP: net.ParseIP("::ffff:127.0.0.1"), Port: 4672}
	if normalizeUDPAddrV6(mapped) != nil {
		t.Fatal("expected ipv4-mapped address to be rejected")
	}
}
