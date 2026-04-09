package goed2k

import (
	"bytes"
	"testing"

	"github.com/monkeyWie/goed2k/protocol"
	clientproto "github.com/monkeyWie/goed2k/protocol/client"
)

func TestPeersForSourceExchangeExcludesAndLimits(t *testing.T) {
	st := NewSettings()
	s := NewSession(st)
	tf, err := NewTransfer(s, AddTransferParams{
		Hash:       protocol.MustHashFromString("31D6CFE0D16AE931B73C59D7E0C089C0"),
		CreateTime: 1,
		Size:       1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 使用 TEST-NET（非 RFC1918），避免被 IsLocalAddress 过滤
	a, err := protocol.EndpointFromString("192.0.2.1", 4662)
	if err != nil {
		t.Fatal(err)
	}
	b, err := protocol.EndpointFromString("192.0.2.2", 4662)
	if err != nil {
		t.Fatal(err)
	}
	c, err := protocol.EndpointFromString("192.0.2.3", 4662)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = tf.policy.AddPeer(NewPeerWithSource(a, true, 0))
	_, _ = tf.policy.AddPeer(NewPeerWithSource(b, true, 0))
	_, _ = tf.policy.AddPeer(NewPeerWithSource(c, false, 0))
	list := tf.policy.PeersForSourceExchange(a, 10)
	if len(list) != 1 || !list[0].Endpoint.Equal(b) {
		t.Fatalf("expected only remote b, got %+v", list)
	}
	list2 := tf.policy.PeersForSourceExchange(protocol.Endpoint{}, 1)
	if len(list2) != 1 {
		t.Fatalf("expected limit 1, got %d", len(list2))
	}
}

func TestAnswerSources2UnpackMergeEndpointRoundtrip(t *testing.T) {
	st := NewSettings()
	s := NewSession(st)
	tf, err := NewTransfer(s, AddTransferParams{
		Hash:       protocol.MustHashFromString("31D6CFE0D16AE931B73C59D7E0C089C0"),
		CreateTime: 1,
		Size:       1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	ep, err := protocol.EndpointFromString("192.0.2.10", 4662)
	if err != nil {
		t.Fatal(err)
	}
	// v4 线上 UserID 为 hybrid：与 aMule 一致，v3+ 读入时会再 Swap；此处写入 wire 值 = Swap(IP)
	uid := clientproto.SwapUint32(uint32(ep.IP()))
	uh := protocol.MustHashFromString("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	ans := clientproto.AnswerSources2{
		Version: 4,
		Hash:    tf.GetHash(),
		Entries: []clientproto.SourceExchangeEntry{{
			UserID:       uid,
			TCPPort:      uint16(ep.Port()),
			ServerIP:     0,
			ServerPort:   0,
			UserHash:     uh,
			CryptOptions: 0,
		}},
	}
	var buf bytes.Buffer
	if err := ans.Put(&buf); err != nil {
		t.Fatal(err)
	}
	var got clientproto.AnswerSources2
	if err := got.Get(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 1 {
		t.Fatal("unpack entries")
	}
	e := got.Entries[0]
	round := endpointFromSourceExchangeEntry(e.UserID, e.TCPPort, got.Version)
	if !round.Equal(ep) {
		t.Fatalf("endpoint roundtrip got %s want %s", round.String(), ep.String())
	}
	n := tf.policy.MergeSourceExchangePeers([]Peer{NewPeerWithSource(round, true, int(PeerSourceExchange))})
	if n != 1 {
		t.Fatalf("merge want 1 got %d", n)
	}
}
