package client

import (
	"bytes"
	"testing"

	"github.com/goed2k/core/protocol"
)

func TestFileCommentRoundTrip(t *testing.T) {
	packet := FileComment{
		Rating:  4,
		Comment: []byte("solid release"),
	}
	var buf bytes.Buffer
	if err := packet.Put(&buf); err != nil {
		t.Fatalf("put file comment: %v", err)
	}
	header := protocol.PacketHeader{
		Protocol: protocol.EMuleProt,
		Size:     int32(packet.BytesCount()),
		Packet:   opFileComment,
	}
	combiner := NewPacketCombiner()
	unpacked, err := combiner.Unpack(header, buf.Bytes())
	if err != nil {
		t.Fatalf("unpack file comment: %v", err)
	}
	got, ok := unpacked.(*FileComment)
	if !ok {
		t.Fatalf("expected *FileComment, got %T", unpacked)
	}
	if got.Rating != 4 || string(got.Comment) != "solid release" {
		t.Fatalf("unexpected file comment %+v", got)
	}
}
