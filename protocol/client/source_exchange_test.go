package client

import (
	"bytes"
	"encoding/hex"
	"net"
	"testing"

	"github.com/goed2k/core/protocol"
)

func TestRequestSources2Golden(t *testing.T) {
	// 0x04 + reserved LE 0 + 16-byte hash（与 aMule DownloadClient 独立包格式一致）
	hash := protocol.MustHashFromString("31D6CFE0D16AE931B73C59D7E0C089C0")
	wantHex := "040000" + hex.EncodeToString(hash.Bytes())
	var buf bytes.Buffer
	if err := (RequestSources2{Version: SourceExchange2Version, Reserved: 0, Hash: hash}).Put(&buf); err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(buf.Bytes()); got != wantHex {
		t.Fatalf("put hex mismatch\ngot  %s\nwant %s", got, wantHex)
	}
	r := bytes.NewReader(buf.Bytes())
	var req RequestSources2
	if err := req.Get(r); err != nil {
		t.Fatal(err)
	}
	if req.Version != SourceExchange2Version || req.Reserved != 0 || !req.Hash.Equal(hash) {
		t.Fatalf("roundtrip mismatch %+v", req)
	}
}

func TestAnswerSources2RoundtripV4(t *testing.T) {
	hash := protocol.MustHashFromString("31D6CFE0D16AE931B73C59D7E0C089C0")
	uh := protocol.MustHashFromString("FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF")
	a := AnswerSources2{
		Version: 4,
		Hash:    hash,
		Entries: []SourceExchangeEntry{
			{
				UserID:       0x01020304,
				TCPPort:      4662,
				ServerIP:     0,
				ServerPort:   0,
				UserHash:     uh,
				CryptOptions: 0,
			},
		},
	}
	var buf bytes.Buffer
	if err := a.Put(&buf); err != nil {
		t.Fatal(err)
	}
	var got AnswerSources2
	if err := got.Get(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatal(err)
	}
	if got.Version != a.Version || !got.Hash.Equal(a.Hash) || len(got.Entries) != 1 {
		t.Fatalf("unexpected %+v", got)
	}
	e := got.Entries[0]
	if e.UserID != 0x01020304 || e.TCPPort != 4662 || e.CryptOptions != 0 || !e.UserHash.Equal(uh) {
		t.Fatalf("entry mismatch %+v", e)
	}
}

func TestAnswerSources2EmptyCount(t *testing.T) {
	hash := protocol.MustHashFromString("31D6CFE0D16AE931B73C59D7E0C089C0")
	a := AnswerSources2{Version: 4, Hash: hash, Entries: nil}
	var buf bytes.Buffer
	if err := a.Put(&buf); err != nil {
		t.Fatal(err)
	}
	var got AnswerSources2
	if err := got.Get(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 0 {
		t.Fatalf("want 0 entries got %d", len(got.Entries))
	}
}

func TestAnswerSources2RoundtripV5IPv6(t *testing.T) {
	hash := protocol.MustHashFromString("31D6CFE0D16AE931B73C59D7E0C089C0")
	ip := net.ParseIP("2001:db8::1").To16()
	var ip6 [16]byte
	copy(ip6[:], ip)
	a := AnswerSources2{
		Version: SourceExchangeIPv6Version,
		Hash:    hash,
		Entries: []SourceExchangeEntry{{
			TCPPort: 4662,
			IPv6:    ip6,
		}},
	}
	var buf bytes.Buffer
	if err := a.Put(&buf); err != nil {
		t.Fatal(err)
	}
	var got AnswerSources2
	if err := got.Get(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 1 || !got.Entries[0].EntryHasIPv6() {
		t.Fatalf("unexpected %+v", got.Entries)
	}
	if !net.IP(got.Entries[0].IPv6[:]).Equal(ip) {
		t.Fatalf("ipv6 mismatch %v", net.IP(got.Entries[0].IPv6[:]))
	}
}

func TestSwapUint32(t *testing.T) {
	if SwapUint32(0x01020304) != 0x04030201 {
		t.Fatal(SwapUint32(0x01020304))
	}
}
