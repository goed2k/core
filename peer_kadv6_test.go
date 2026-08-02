package goed2k

import (
	"net"
	"testing"

	"github.com/goed2k/core/protocol"
	kadv6 "github.com/goed2k/core/protocol/kadv6"
)

func TestPeerFromKADV6SearchEntry(t *testing.T) {
	ip := net.ParseIP("2001:db8::1")
	se := kadv6.SearchEntry{
		ID: kadv6.NewID(protocol.MustHashFromString("31D6CFE0D16AE931B73C59D7E0C089C0")),
		Tags: []kadv6.Tag{
			{Type: kadv6.TagTypeUint8, ID: kadv6.TagAddrFamily, UInt64: uint64(kadv6.AddrFamilyIPv6)},
			{Type: kadv6.TagTypeBytes, ID: kadv6.TagSourceIP6, Bytes: []byte(ip.To16())},
			{Type: kadv6.TagTypeUint16, ID: kadv6.TagSourcePort, UInt64: 4662},
		},
	}
	p, ok := PeerFromKADV6SearchEntry(se, int(PeerKADV6))
	if !ok {
		t.Fatal("expected ok")
	}
	if p.DialAddr == nil || p.DialAddr.Port != 4662 {
		t.Fatalf("dial addr: %+v", p.DialAddr)
	}
	if p.Endpoint.Defined() {
		t.Fatal("ipv6-only should not set ipv4 endpoint")
	}
	if p.SourceFlag&int(PeerKADV6) == 0 {
		t.Fatal("expected PeerKADV6 flag")
	}
	if p.CanEncodeAnswerSources2(4) {
		t.Fatal("pure ipv6 must not encode in SX v4")
	}
	if !p.CanEncodeAnswerSources2(5) {
		t.Fatal("pure ipv6 should encode in SX v5")
	}
}

func TestNewPeerFromTCPAddrIPv4MappedEncodesSX(t *testing.T) {
	addr := &net.TCPAddr{IP: net.ParseIP("::ffff:192.0.2.1"), Port: 4662}
	p := NewPeerFromTCPAddr(addr, true, 0)
	if !p.Endpoint.Defined() {
		t.Fatal("expected endpoint from mapped")
	}
	if !p.CanEncodeAnswerSources2(4) {
		t.Fatal("mapped ipv6 should encode as ipv4 in SX v4")
	}
}

func TestPeerSortKeyIPv6Dial(t *testing.T) {
	a := NewPeerFromTCPAddr(&net.TCPAddr{IP: net.ParseIP("2001:db8::1"), Port: 4662}, true, 0)
	b := NewPeerFromTCPAddr(&net.TCPAddr{IP: net.ParseIP("2001:db8::2"), Port: 4662}, true, 0)
	if a.Compare(b) == 0 {
		t.Fatal("expected distinct sort keys")
	}
}
