package goed2k

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/goed2k/core/disk"
	"github.com/goed2k/core/protocol"
	clientproto "github.com/goed2k/core/protocol/client"
)

func packetUsesCompressedPart32(packet []byte) bool {
	if len(packet) < protocol.PacketHeaderSize {
		return false
	}
	reader := bytes.NewReader(packet)
	var header protocol.PacketHeader
	if err := header.Get(reader); err != nil {
		return false
	}
	return header.Protocol == protocol.EMuleProt && header.Packet == 0x40
}

func unpackOutgoingPacket(t *testing.T, packet []byte) any {
	t.Helper()
	if len(packet) < protocol.PacketHeaderSize {
		t.Fatalf("packet too short: %d", len(packet))
	}
	reader := bytes.NewReader(packet)
	var header protocol.PacketHeader
	if err := header.Get(reader); err != nil {
		t.Fatalf("read header: %v", err)
	}
	body := make([]byte, header.SizePacket())
	if _, err := reader.Read(body); err != nil {
		t.Fatalf("read body: %v", err)
	}
	combiner := clientproto.NewPacketCombiner()
	decoded, err := combiner.Unpack(header, body)
	if err != nil {
		t.Fatalf("unpack: %v", err)
	}
	return decoded
}

func TestCompressPayloadReducesRepeatedData(t *testing.T) {
	payload := bytes.Repeat([]byte{0}, 50000)
	compressed, ok := tryCompressPayload(payload)
	if !ok {
		t.Fatal("expected repeated zeros to compress")
	}
	if len(compressed) >= len(payload) {
		t.Fatalf("expected smaller compressed payload, got %d >= %d", len(compressed), len(payload))
	}
}

func TestCompressedPartSendRoundTrip(t *testing.T) {
	original := bytes.Repeat([]byte{0x5A}, int(BlockSize))
	compressed, ok := tryCompressPayload(original)
	if !ok {
		t.Fatal("expected compressible block payload")
	}

	seedPath := filepath.Join(t.TempDir(), "seed.bin")
	if err := os.WriteFile(seedPath, original, 0o644); err != nil {
		t.Fatalf("write seed file: %v", err)
	}

	seedSettings := NewSettings()
	seedSettings.CompressionVersion = 1
	seedSession := NewSession(seedSettings)
	seedTransfer, err := NewTransfer(seedSession, AddTransferParams{
		Hash:       protocol.EMule,
		CreateTime: CurrentTimeMillis(),
		Size:       BlockSize,
		Handler:    disk.NewDesktopFileHandler(seedPath),
	})
	if err != nil {
		t.Fatalf("seed transfer: %v", err)
	}
	seedTransfer.WeHave(0)

	endpoint, err := protocol.EndpointFromString("127.0.0.1", 4662)
	if err != nil {
		t.Fatalf("endpoint: %v", err)
	}
	peer := NewPeerWithSource(endpoint, true, int(PeerResume))

	sender := NewPeerConnection(seedSession, endpoint, seedTransfer, &peer)
	sender.remotePeerInfo.Misc1.DataCompVer = 1
	sender.SetUploadResource(seedTransfer)
	if err := sender.sendCompressedPayload(0, compressed); err != nil {
		t.Fatalf("send compressed payload: %v", err)
	}
	packets := sender.PendingPackets()
	if len(packets) == 0 {
		t.Fatal("expected compressed upload packets")
	}
	if !packetUsesCompressedPart32(packets[0]) {
		t.Fatal("expected CompressedPart32 opcode in outgoing packets")
	}
	decoded, ok := unpackOutgoingPacket(t, packets[0]).(*clientproto.CompressedPart32)
	if !ok {
		t.Fatalf("expected CompressedPart32 packet, got %T", decoded)
	}
	if decoded.BeginOffset != 0 || decoded.CompressedLength != uint32(len(compressed)) {
		t.Fatalf("unexpected compressed header: %+v", decoded)
	}

	receiver := NewPeerConnection(NewSession(NewSettings()), endpoint, seedTransfer, &peer)
	receiver.recvReqCompressed = true
	pb := &PendingBlock{
		Buffer:   append([]byte(nil), compressed...),
		Received: int64(len(compressed)),
		DataSize: int64(len(compressed)),
	}
	if !receiver.CompleteBlock(pb) {
		t.Fatal("expected compressed block to complete")
	}
	if !bytes.Equal(pb.Buffer[:len(original)], original) {
		t.Fatal("decompressed payload mismatch")
	}
}

func TestSendBlockDataUsesCompressedPartWhenEnabled(t *testing.T) {
	original := bytes.Repeat([]byte{0}, int(BlockSize))
	seedPath := filepath.Join(t.TempDir(), "seed.bin")
	if err := os.WriteFile(seedPath, original, 0o644); err != nil {
		t.Fatalf("write seed file: %v", err)
	}

	settings := NewSettings()
	settings.CompressionVersion = 1
	session := NewSession(settings)
	transfer, err := NewTransfer(session, AddTransferParams{
		Hash:       protocol.EMule,
		CreateTime: CurrentTimeMillis(),
		Size:       BlockSize,
		Handler:    disk.NewDesktopFileHandler(seedPath),
	})
	if err != nil {
		t.Fatalf("transfer: %v", err)
	}
	transfer.WeHave(0)

	endpoint, err := protocol.EndpointFromString("127.0.0.1", 4662)
	if err != nil {
		t.Fatalf("endpoint: %v", err)
	}
	peer := NewPeerWithSource(endpoint, true, int(PeerResume))
	sender := NewPeerConnection(session, endpoint, transfer, &peer)
	sender.remotePeerInfo.Misc1.DataCompVer = 1
	sender.SetUploadResource(transfer)
	sender.uploadState = UploadStateUploading
	sender.uploadBlocks = []RequestedUploadBlock{{Begin: 0, End: BlockSize}}

	sender.SendBlockData()

	packets := sender.PendingPackets()
	if len(packets) == 0 {
		t.Fatal("expected upload packets")
	}
	for _, packet := range packets {
		switch unpackOutgoingPacket(t, packet).(type) {
		case *clientproto.SendingPart32, *clientproto.SendingPart64:
			t.Fatal("expected compressed upload, got SendingPart")
		}
	}
	if !packetUsesCompressedPart32(packets[0]) {
		t.Fatal("expected CompressedPart32 packet")
	}
}
