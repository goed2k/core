package goed2k

import (
	"testing"
	"time"

	"github.com/goed2k/core/protocol"
	kadv6proto "github.com/goed2k/core/protocol/kadv6"
)

func TestKADV6RoutingTablePromotesReplacementOnFailure(t *testing.T) {
	self := kadv6proto.NewID(protocol.MustHashFromString("23A8CEFF57A7A32D562D649ED7893796"))
	table := newKADV6RoutingTable(self, 1)

	live := &KADV6RoutingNode{
		ID:       kadv6proto.NewID(protocol.MustHashFromString("31D6CFE0D16AE931B73C59D7E0C089C0")),
		Addr:     mustUDPAddrV6(t, "[::1]:4672"),
		Pinged:   true,
		LastSeen: time.Now(),
	}
	replacement := &KADV6RoutingNode{
		ID:       kadv6proto.NewID(protocol.MustHashFromString("31D6CFE0D14CE931B73C59D7E0C04BC0")),
		Addr:     mustUDPAddrV6(t, "[2001:db8::2]:4672"),
		Pinged:   true,
		LastSeen: time.Now(),
	}

	table.NodeSeen(live)
	table.HeardAbout(replacement)
	table.NodeFailed(live)

	nodes := table.FindClosest(self, 10, true)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 live node after promotion, got %d", len(nodes))
	}
	if !nodes[0].ID.Hash.Equal(replacement.ID.Hash) {
		t.Fatalf("expected replacement node to be promoted, got %s", nodes[0].ID.Hash.String())
	}
}

func TestKADV6RPCManagerReusesSharedManager(t *testing.T) {
	manager := newKadRPCManager()
	tx := &kadRPCTransaction{
		endpointKey: "[::1]:4672",
		opcode:      kadv6proto.BootstrapResOp,
		sentTime:    time.Now().Add(-13 * time.Second),
	}
	manager.transactions = append(manager.transactions, tx)

	_, expired := manager.Tick(time.Now())
	if len(expired) != 1 {
		t.Fatalf("expected 1 expired transaction, got %d", len(expired))
	}
	if len(manager.transactions) != 0 {
		t.Fatalf("expected transaction list to be empty, got %d", len(manager.transactions))
	}
}
