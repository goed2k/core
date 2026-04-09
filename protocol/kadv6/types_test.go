package kadv6

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/monkeyWie/goed2k/protocol"
)

func TestIDRoundTrip(t *testing.T) {
	original := NewID(protocol.MustHashFromString("23A8CEFF57A7A32D562D649ED7893796"))
	var buffer bytes.Buffer
	if err := original.Put(&buffer); err != nil {
		t.Fatalf("put id: %v", err)
	}
	var restored ID
	if err := restored.Get(bytes.NewReader(buffer.Bytes())); err != nil {
		t.Fatalf("get id: %v", err)
	}
	if !restored.Hash.Equal(original.Hash) {
		t.Fatalf("expected %s, got %s", original.Hash.String(), restored.Hash.String())
	}
}

func TestEndpointFromIPRejectsIPv4(t *testing.T) {
	if _, err := EndpointFromIP(net.ParseIP("127.0.0.1"), 4665, 4662); err == nil {
		t.Fatal("expected ipv4 endpoint rejection")
	}
}

func TestEndpointV6RoundTrip(t *testing.T) {
	original, err := EndpointFromIP(net.ParseIP("2001:db8::10"), 4665, 4662)
	if err != nil {
		t.Fatalf("endpoint from ip: %v", err)
	}
	var buffer bytes.Buffer
	if err := original.Put(&buffer); err != nil {
		t.Fatalf("put endpoint: %v", err)
	}
	var restored EndpointV6
	if err := restored.Get(bytes.NewReader(buffer.Bytes())); err != nil {
		t.Fatalf("get endpoint: %v", err)
	}
	if restored.UDPPort != 4665 || restored.TCPPort != 4662 {
		t.Fatalf("unexpected ports udp=%d tcp=%d", restored.UDPPort, restored.TCPPort)
	}
	if restored.IPNet().String() != "2001:db8::10" {
		t.Fatalf("unexpected ip %s", restored.IPNet().String())
	}
}

func TestSearchEntryExtractsIPv6Source(t *testing.T) {
	ip := net.ParseIP("2001:db8::20").To16()
	entry := SearchEntry{
		Tags: []Tag{
			{Type: TagTypeUint8, ID: TagAddrFamily, UInt64: uint64(AddrFamilyIPv6)},
			{Type: TagTypeBytes, ID: TagSourceIP6, Bytes: bytes.Clone(ip)},
			{Type: TagTypeUint16, ID: TagSourcePort, UInt64: 4662},
			{Type: TagTypeUint8, ID: TagSourceType, UInt64: 1},
		},
	}
	addr, ok := entry.SourceAddr()
	if !ok {
		t.Fatal("expected source address to be extracted")
	}
	if addr.Port != 4662 {
		t.Fatalf("unexpected port %d", addr.Port)
	}
	if addr.IP.String() != "2001:db8::20" {
		t.Fatalf("unexpected ip %s", addr.IP.String())
	}
}

func TestSearchEntryRejectsWrongFamily(t *testing.T) {
	ip := net.ParseIP("2001:db8::20").To16()
	entry := SearchEntry{
		Tags: []Tag{
			{Type: TagTypeUint8, ID: TagAddrFamily, UInt64: 4},
			{Type: TagTypeBytes, ID: TagSourceIP6, Bytes: bytes.Clone(ip)},
			{Type: TagTypeUint16, ID: TagSourcePort, UInt64: 4662},
		},
	}
	if _, ok := entry.SourceAddr(); ok {
		t.Fatal("expected wrong family to be rejected")
	}
}

func TestNodesDatRoundTrip(t *testing.T) {
	endpoint, err := EndpointFromIP(net.ParseIP("2001:db8::30"), 4672, 4661)
	if err != nil {
		t.Fatalf("endpoint from ip: %v", err)
	}
	nodes := NodesDat{
		Version:          1,
		BootstrapEdition: 1,
		Contacts: []EntryV6{
			{
				ID:       NewID(protocol.MustHashFromString("31D6CFE0D16AE931B73C59D7E0C089C0")),
				Endpoint: endpoint,
				Version:  KademliaVersion,
				Verified: true,
			},
		},
	}
	raw, err := nodes.Pack()
	if err != nil {
		t.Fatalf("pack nodes.dat: %v", err)
	}
	restored, err := ParseNodesDat(raw)
	if err != nil {
		t.Fatalf("parse nodes.dat: %v", err)
	}
	if restored.Version != 1 || restored.BootstrapEdition != 1 {
		t.Fatalf("unexpected header version=%d edition=%d", restored.Version, restored.BootstrapEdition)
	}
	if len(restored.Contacts) != 1 {
		t.Fatalf("expected 1 contact, got %d", len(restored.Contacts))
	}
	if !restored.Contacts[0].Verified {
		t.Fatal("expected contact to remain verified")
	}
}

func TestLoadNodesDat(t *testing.T) {
	endpoint, err := EndpointFromIP(net.ParseIP("2001:db8::40"), 4672, 4661)
	if err != nil {
		t.Fatalf("endpoint from ip: %v", err)
	}
	nodes := NodesDat{
		Version: 1,
		Contacts: []EntryV6{
			{
				ID:       NewID(protocol.MustHashFromString("31D6CFE0D14CE931B73C59D7E0C04BC0")),
				Endpoint: endpoint,
				Version:  KademliaVersion,
			},
		},
	}
	raw, err := nodes.Pack()
	if err != nil {
		t.Fatalf("pack nodes.dat: %v", err)
	}
	path := filepath.Join(t.TempDir(), "nodes6.dat")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("write nodes.dat: %v", err)
	}
	restored, err := LoadNodesDat(path)
	if err != nil {
		t.Fatalf("load nodes.dat: %v", err)
	}
	if len(restored.Contacts) != 1 {
		t.Fatalf("expected 1 contact, got %d", len(restored.Contacts))
	}
}

func TestParseNodesDatRejectsBadMagic(t *testing.T) {
	raw := []byte("BAD!" + "\x01\x00\x00\x00")
	if _, err := ParseNodesDat(raw); err == nil {
		t.Fatal("expected bad magic to be rejected")
	}
}

func TestDecodePacketRejectsBadHeader(t *testing.T) {
	if _, _, err := DecodePacket([]byte{0xE4, PingOp}); err == nil {
		t.Fatal("expected header mismatch to be rejected")
	}
}
