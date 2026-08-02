package server

import (
	"bytes"
	"testing"

	"github.com/goed2k/core/protocol"
)

func TestFoundFileSourcesObfuRoundtrip(t *testing.T) {
	hash := protocol.MustHashFromString("31D6CFE0D16AE931B73C59D7E0C089C0")
	uh := protocol.MustHashFromString("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	ep, err := protocol.EndpointFromString("192.0.2.1", 4662)
	if err != nil {
		t.Fatal(err)
	}
	pkt := FoundFileSourcesObfu{
		Hash: hash,
		Sources: []ObfuFileSource{{
			Endpoint:     ep,
			CryptOptions: CryptOptionObfuUserHash | 0x03,
			UserHash:     uh,
		}},
	}
	var buf bytes.Buffer
	if err := pkt.Put(&buf); err != nil {
		t.Fatal(err)
	}
	var got FoundFileSourcesObfu
	if err := got.Get(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatal(err)
	}
	if len(got.Sources) != 1 || !got.Sources[0].UserHash.Equal(uh) {
		t.Fatalf("unexpected sources: %+v", got.Sources)
	}
}

func TestGetFileSourcesObfuSamePayloadAsGetFileSources(t *testing.T) {
	hash := protocol.MustHashFromString("31D6CFE0D16AE931B73C59D7E0C089C0")
	plain := GetFileSources{Hash: hash, LowPart: 1024, HiPart: 0}
	obfu := GetFileSourcesObfu(plain)
	var a, b bytes.Buffer
	if err := plain.Put(&a); err != nil {
		t.Fatal(err)
	}
	if err := obfu.Put(&b); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Fatal("obfu request payload should match plain get sources")
	}
}
