package protocol

import (
	"bytes"
	"testing"
)

func TestEmulePartMetRoundTrip(t *testing.T) {
	hash := MustHashFromString("31D6CFE0D16AE931B73C59D7E0C089C0")
	piece := MustHashFromString("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	opts := EmulePartMetOptions{
		Hash:        hash,
		FileSize:    PieceSize * 2,
		Filename:    "demo.bin",
		Transferred: uint64(PieceSize),
		PieceHashes: []Hash{piece, piece},
		Gaps: []PartMetGap{
			{Start: uint64(PieceSize), End: uint64(PieceSize * 2)},
		},
	}
	raw, err := BuildEmulePartMet(opts)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !IsEmulePartMetBytes(raw) {
		t.Fatal("expected emule header")
	}
	met, err := ParseEmulePartMet(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !met.Hash.Equal(hash) || len(met.PieceHashes) != 2 {
		t.Fatalf("unexpected met %+v", met)
	}
	size, err := FileSizeFromEmulePartMet(met)
	if err != nil || size != PieceSize*2 {
		t.Fatalf("size %d err %v", size, err)
	}
	gaps, err := GapsFromEmulePartMet(met)
	if err != nil || len(gaps) != 1 || gaps[0].Start != uint64(PieceSize) {
		t.Fatalf("gaps %+v err %v", gaps, err)
	}
	if FilenameFromEmulePartMet(met) != "demo.bin" {
		t.Fatalf("filename %q", FilenameFromEmulePartMet(met))
	}
}

func TestParseEmulePartMetRejectsInvalidVersion(t *testing.T) {
	_, err := ParseEmulePartMet([]byte{0x53})
	if err == nil {
		t.Fatal("expected error for invalid version")
	}
}

func TestParseEmulePartMetTrailingBytes(t *testing.T) {
	hash := EMule
	var buf bytes.Buffer
	_ = buf.WriteByte(PartFileVersion)
	_ = WriteUInt32(&buf, 1)
	_ = WriteHash(&buf, hash)
	_ = WriteUInt16(&buf, 0)
	_ = WriteUInt32(&buf, 0)
	buf.WriteByte(0xFF)
	_, err := ParseEmulePartMet(buf.Bytes())
	if err == nil {
		t.Fatal("expected trailing bytes error")
	}
}

const PieceSize = 9728000
