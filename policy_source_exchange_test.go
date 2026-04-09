package goed2k

import (
	"testing"

	"github.com/monkeyWie/goed2k/protocol"
)

func TestMergeSourceExchangePeersDedupes(t *testing.T) {
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
	p := &tf.policy
	ep, err := protocol.EndpointFromString("192.168.1.2", 4662)
	if err != nil {
		t.Fatal(err)
	}
	p1 := Peer{Endpoint: ep, Connectable: true, SourceFlag: 1}
	p2 := Peer{Endpoint: ep, Connectable: true, SourceFlag: 2}
	n := p.MergeSourceExchangePeers([]Peer{p1, p2})
	if n != 1 {
		t.Fatalf("expected 1 new peer, got %d", n)
	}
}
