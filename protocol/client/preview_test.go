package client

import (
	"bytes"
	"testing"

	"github.com/goed2k/core/protocol"
)

func TestRequestPreviewRoundtrip(t *testing.T) {
	hash := protocol.MustHashFromString("31D6CFE0D16AE931B73C59D7E0C089C0")
	req := RequestPreview{Hash: hash, PieceIndex: 3}
	var buf bytes.Buffer
	if err := req.Put(&buf); err != nil {
		t.Fatal(err)
	}
	var got RequestPreview
	if err := got.Get(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatal(err)
	}
	if !got.Hash.Equal(hash) || got.PieceIndex != 3 {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
}

func TestPreviewAnswerRoundtrip(t *testing.T) {
	hash := protocol.MustHashFromString("31D6CFE0D16AE931B73C59D7E0C089C0")
	ans := PreviewAnswer{Hash: hash, PieceIndex: 1, Data: []byte("preview-bytes")}
	var buf bytes.Buffer
	if err := ans.Put(&buf); err != nil {
		t.Fatal(err)
	}
	var got PreviewAnswer
	if err := got.Get(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Data, ans.Data) {
		t.Fatalf("data mismatch")
	}
}
