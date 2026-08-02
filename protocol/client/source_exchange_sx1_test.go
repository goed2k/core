package client

import (
	"bytes"
	"testing"

	"github.com/goed2k/core/protocol"
)

func TestAnswerSourcesSX1Roundtrip(t *testing.T) {
	hash := protocol.MustHashFromString("31D6CFE0D16AE931B73C59D7E0C089C0")
	ans := AnswerSources{
		Hash: hash,
		Entries: []SourceExchangeEntry{{
			UserID:     0x04030201,
			TCPPort:    4662,
			ServerIP:   0x0A000001,
			ServerPort: 4661,
		}},
	}
	var buf bytes.Buffer
	if err := ans.Put(&buf); err != nil {
		t.Fatal(err)
	}
	var got AnswerSources
	if err := got.Get(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 1 || got.Entries[0].TCPPort != 4662 {
		t.Fatalf("unexpected entries: %+v", got.Entries)
	}
}

func TestRequestSourcesSX1Roundtrip(t *testing.T) {
	hash := protocol.MustHashFromString("31D6CFE0D16AE931B73C59D7E0C089C0")
	req := RequestSources{Hash: hash}
	var buf bytes.Buffer
	if err := req.Put(&buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 16 {
		t.Fatalf("expected 16 bytes, got %d", buf.Len())
	}
	var got RequestSources
	if err := got.Get(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatal(err)
	}
	if !got.Hash.Equal(hash) {
		t.Fatal("hash mismatch")
	}
}
