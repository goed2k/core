package goed2k

import (
	"testing"

	"github.com/goed2k/core/protocol"
)

func TestParseHelloTagList(t *testing.T) {
	var dst RemotePeerInfo
	props := protocol.TagList{
		protocol.NewStringTag(0x01, "nick"),
		protocol.NewStringTag(0x55, "eMule"),
		protocol.NewUInt32Tag(0x11, 0x3c),
		protocol.NewUInt32Tag(0xFB, uint32((3<<24)|((5)<<17)|((2)<<10)|((1)<<7))),
	}
	parseHelloTagList(&dst, &props)
	if dst.NickName != "nick" {
		t.Fatalf("nick: got %q", dst.NickName)
	}
	if dst.ModName != "eMule" {
		t.Fatalf("mod: got %q", dst.ModName)
	}
	if dst.Version != 0x3c {
		t.Fatalf("version: got %d", dst.Version)
	}
	if dst.ModVersion != "5.2.1" {
		t.Fatalf("mod composite str: got %q", dst.ModVersion)
	}
}

func TestDecodeHelloModCompositeZero(t *testing.T) {
	if s := decodeHelloModComposite(0); s != "" {
		t.Fatalf("expected empty, got %q", s)
	}
}
