package kadv6

import (
	"bytes"
	"net"
	"testing"

	"github.com/monkeyWie/goed2k/protocol"
)

func testEntryV6(t *testing.T) EntryV6 {
	t.Helper()
	endpoint, err := EndpointFromIP(net.ParseIP("2001:db8::50"), 4672, 4661)
	if err != nil {
		t.Fatalf("endpoint from ip: %v", err)
	}
	return EntryV6{
		ID:       NewID(protocol.MustHashFromString("31D6CFE0D16AE931B73C59D7E0C089C0")),
		Endpoint: endpoint,
		Version:  KademliaVersion,
		Verified: true,
	}
}

func testSourceEntryV6(t *testing.T) SearchEntry {
	t.Helper()
	return SearchEntry{
		ID: NewID(protocol.MustHashFromString("23A8CEFF57A7A32D562D649ED7893796")),
		Tags: []Tag{
			{Type: TagTypeUint8, ID: TagAddrFamily, UInt64: uint64(AddrFamilyIPv6)},
			{Type: TagTypeBytes, ID: TagSourceIP6, Bytes: bytes.Clone(net.ParseIP("2001:db8::60").To16())},
			{Type: TagTypeUint16, ID: TagSourcePort, UInt64: 4662},
			{Type: TagTypeUint8, ID: TagSourceType, UInt64: 1},
			{Type: TagTypeUint64, ID: TagFileSize, UInt64: 1024},
		},
	}
}

func TestPacketCombinerRoundTripSearchSourcesReq(t *testing.T) {
	combiner := PacketCombiner{}
	raw, err := combiner.Pack(SearchSourcesReq{
		Target:   NewID(protocol.MustHashFromString("23A8CEFF57A7A32D562D649ED7893796")),
		StartPos: 7,
		Size:     12345,
	})
	if err != nil {
		t.Fatalf("pack search sources req: %v", err)
	}
	opcode, msg, err := combiner.Unpack(raw)
	if err != nil {
		t.Fatalf("unpack search sources req: %v", err)
	}
	if opcode != SearchSrcReqOp {
		t.Fatalf("expected opcode %x, got %x", SearchSrcReqOp, opcode)
	}
	req, ok := msg.(*SearchSourcesReq)
	if !ok {
		t.Fatalf("expected SearchSourcesReq, got %T", msg)
	}
	if req.StartPos != 7 || req.Size != 12345 {
		t.Fatalf("unexpected unpacked request %+v", req)
	}
}

func TestPacketCombinerRoundTripBootstrapRes(t *testing.T) {
	combiner := PacketCombiner{}
	raw, err := combiner.Pack(BootstrapRes{
		ID:       NewID(protocol.MustHashFromString("23A8CEFF57A7A32D562D649ED7893796")),
		TCPPort:  4661,
		Version:  KademliaVersion,
		Contacts: []EntryV6{testEntryV6(t)},
	})
	if err != nil {
		t.Fatalf("pack bootstrap res: %v", err)
	}
	opcode, msg, err := combiner.Unpack(raw)
	if err != nil {
		t.Fatalf("unpack bootstrap res: %v", err)
	}
	if opcode != BootstrapResOp {
		t.Fatalf("expected opcode %x, got %x", BootstrapResOp, opcode)
	}
	res, ok := msg.(*BootstrapRes)
	if !ok {
		t.Fatalf("expected BootstrapRes, got %T", msg)
	}
	if len(res.Contacts) != 1 {
		t.Fatalf("expected 1 contact, got %d", len(res.Contacts))
	}
}

func TestPacketCombinerRoundTripHelloRes(t *testing.T) {
	combiner := PacketCombiner{}
	raw, err := combiner.Pack(Hello{
		ID:      NewID(protocol.MustHashFromString("23A8CEFF57A7A32D562D649ED7893796")),
		TCPPort: 4661,
		Version: KademliaVersion,
		Tags: []Tag{
			{Type: TagTypeString, ID: TagName, String: "goed2k"},
		},
	}, HelloResOp)
	if err != nil {
		t.Fatalf("pack hello res: %v", err)
	}
	opcode, msg, err := combiner.Unpack(raw)
	if err != nil {
		t.Fatalf("unpack hello res: %v", err)
	}
	if opcode != HelloResOp {
		t.Fatalf("expected opcode %x, got %x", HelloResOp, opcode)
	}
	hello, ok := msg.(*Hello)
	if !ok {
		t.Fatalf("expected Hello, got %T", msg)
	}
	if len(hello.Tags) != 1 || hello.Tags[0].ID != TagName || hello.Tags[0].String != "goed2k" {
		t.Fatalf("unexpected hello tags %+v", hello.Tags)
	}
}

func TestPacketCombinerRoundTripFindNode(t *testing.T) {
	combiner := PacketCombiner{}
	raw, err := combiner.Pack(FindNodeReq{
		SearchType: FindNode,
		Target:     NewID(protocol.MustHashFromString("23A8CEFF57A7A32D562D649ED7893796")),
		Receiver:   NewID(protocol.MustHashFromString("31D6CFE0D16AE931B73C59D7E0C089C0")),
	})
	if err != nil {
		t.Fatalf("pack find-node req: %v", err)
	}
	opcode, msg, err := combiner.Unpack(raw)
	if err != nil {
		t.Fatalf("unpack find-node req: %v", err)
	}
	if opcode != FindNodeReqOp {
		t.Fatalf("expected opcode %x, got %x", FindNodeReqOp, opcode)
	}
	req, ok := msg.(*FindNodeReq)
	if !ok {
		t.Fatalf("expected FindNodeReq, got %T", msg)
	}
	if req.SearchType != FindNode {
		t.Fatalf("unexpected search type %d", req.SearchType)
	}
}

func TestPacketCombinerRoundTripPublishSourceReq(t *testing.T) {
	combiner := PacketCombiner{}
	raw, err := combiner.Pack(PublishSourcesReq{
		FileID: NewID(protocol.MustHashFromString("31D6CFE0D16AE931B73C59D7E0C089C0")),
		Source: testSourceEntryV6(t),
	})
	if err != nil {
		t.Fatalf("pack publish source req: %v", err)
	}
	opcode, msg, err := combiner.Unpack(raw)
	if err != nil {
		t.Fatalf("unpack publish source req: %v", err)
	}
	if opcode != PublishSourceReqOp {
		t.Fatalf("expected opcode %x, got %x", PublishSourceReqOp, opcode)
	}
	req, ok := msg.(*PublishSourcesReq)
	if !ok {
		t.Fatalf("expected PublishSourcesReq, got %T", msg)
	}
	if _, ok := req.Source.SourceAddr(); !ok {
		t.Fatal("expected source address to survive round-trip")
	}
}

func TestPacketCombinerRoundTripPublishKeysReq(t *testing.T) {
	combiner := PacketCombiner{}
	raw, err := combiner.Pack(PublishKeysReq{
		KeywordID: NewID(protocol.MustHashFromString("31D6CFE0D14CE931B73C59D7E0C04BC0")),
		Sources:   []SearchEntry{testSourceEntryV6(t)},
	})
	if err != nil {
		t.Fatalf("pack publish keys req: %v", err)
	}
	opcode, msg, err := combiner.Unpack(raw)
	if err != nil {
		t.Fatalf("unpack publish keys req: %v", err)
	}
	if opcode != PublishKeysReqOp {
		t.Fatalf("expected opcode %x, got %x", PublishKeysReqOp, opcode)
	}
	req, ok := msg.(*PublishKeysReq)
	if !ok {
		t.Fatalf("expected PublishKeysReq, got %T", msg)
	}
	if len(req.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(req.Sources))
	}
}

func TestPacketCombinerRoundTripSearchResAndPong(t *testing.T) {
	combiner := PacketCombiner{}
	raw, err := combiner.Pack(SearchRes{
		Source:  NewID(protocol.MustHashFromString("23A8CEFF57A7A32D562D649ED7893796")),
		Target:  NewID(protocol.MustHashFromString("31D6CFE0D16AE931B73C59D7E0C089C0")),
		Results: []SearchEntry{testSourceEntryV6(t)},
	})
	if err != nil {
		t.Fatalf("pack search res: %v", err)
	}
	opcode, msg, err := combiner.Unpack(raw)
	if err != nil {
		t.Fatalf("unpack search res: %v", err)
	}
	if opcode != SearchResOp {
		t.Fatalf("expected opcode %x, got %x", SearchResOp, opcode)
	}
	res, ok := msg.(*SearchRes)
	if !ok {
		t.Fatalf("expected SearchRes, got %T", msg)
	}
	if len(res.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(res.Results))
	}

	raw, err = combiner.Pack(Pong{UDPPort: 4665})
	if err != nil {
		t.Fatalf("pack pong: %v", err)
	}
	opcode, msg, err = combiner.Unpack(raw)
	if err != nil {
		t.Fatalf("unpack pong: %v", err)
	}
	if opcode != PongOp {
		t.Fatalf("expected opcode %x, got %x", PongOp, opcode)
	}
	pong, ok := msg.(*Pong)
	if !ok {
		t.Fatalf("expected Pong, got %T", msg)
	}
	if pong.UDPPort != 4665 {
		t.Fatalf("unexpected pong port %d", pong.UDPPort)
	}
}
