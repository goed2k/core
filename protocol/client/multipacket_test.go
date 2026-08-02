package client

import (
	"bytes"
	"testing"

	"github.com/goed2k/core/protocol"
)

func TestPackUnpackMultiPacketExt2(t *testing.T) {
	combiner := NewPacketCombiner()
	hello, err := combiner.Pack("client.HelloAnswer", &HelloAnswer{
		Hash:  protocol.EMule,
		Point: protocol.NewEndpoint(0x01020304, 4662),
	})
	if err != nil {
		t.Fatal(err)
	}
	ext, err := combiner.Pack("client.ExtHello", &ExtHello{
		ExtendedHandshake: ExtendedHandshake{
			Version:         0x10,
			ProtocolVersion: 0x01,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	combined, err := PackMultiPacketExt2([][]byte{hello, ext})
	if err != nil {
		t.Fatal(err)
	}
	if len(combined) <= len(hello)+len(ext) {
		t.Fatalf("expected compressed multipacket, got %d bytes", len(combined))
	}
	reader := bytes.NewReader(combined)
	var header protocol.PacketHeader
	if err := header.Get(reader); err != nil {
		t.Fatal(err)
	}
	if header.Packet != opMultiPacketExt2 {
		t.Fatalf("unexpected opcode %#x", header.Packet)
	}
	body, err := protocol.ReadBytes(reader, int(header.SizePacket()))
	if err != nil {
		t.Fatal(err)
	}
	frames, err := UnpackMultiPacketExt2(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 {
		t.Fatalf("expected 2 inner frames, got %d", len(frames))
	}
}

func TestPackMultiPacketExt2SingleFramePassthrough(t *testing.T) {
	frame := []byte{0xe3, 0x05, 0x00, 0x00, 0x00, 0x01}
	out, err := PackMultiPacketExt2([][]byte{frame})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, frame) {
		t.Fatalf("single frame should pass through unchanged")
	}
}
