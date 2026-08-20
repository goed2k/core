package goed2k

import (
	"errors"
	"fmt"
	"testing"

	"github.com/goed2k/core/protocol"
)

func testPolicyPeer(t *testing.T, host string) Peer {
	t.Helper()
	endpoint, err := protocol.EndpointFromString(host, 4662)
	if err != nil {
		t.Fatal(err)
	}
	return Peer{Endpoint: endpoint, Connectable: true}
}

func addPolicyPeer(t *testing.T, policy *Policy, host string) {
	t.Helper()
	if _, err := policy.AddPeer(testPolicyPeer(t, host)); err != nil {
		t.Fatalf("add %s: %v", host, err)
	}
}

func TestPolicyMaxPeerListSizeUsesSettings(t *testing.T) {
	session, transfer := newTestTransfer(t)
	session.settings.MaxPeerListSize = 2

	addPolicyPeer(t, &transfer.policy, "192.0.2.10")
	addPolicyPeer(t, &transfer.policy, "192.0.2.11")
	if got := transfer.policy.Size(); got != 2 {
		t.Fatalf("peer count %d, want 2", got)
	}

	_, err := transfer.policy.AddPeer(testPolicyPeer(t, "192.0.2.12"))
	var jed2k *JED2KError
	if !errors.As(err, &jed2k) || jed2k.EC != PeerLimitExceeded {
		t.Fatalf("third peer err %v, want PeerLimitExceeded", err)
	}
	if got := transfer.policy.Size(); got != 2 {
		t.Fatalf("peer count after reject %d, want 2", got)
	}
}

func TestPolicyMaxPeerListSizeDefaultWithoutSession(t *testing.T) {
	policy := NewPolicy(nil)
	for i := 0; i < MaxPeerListSize; i++ {
		addPolicyPeer(t, &policy, fmt.Sprintf("192.0.2.%d", i+1))
	}
	_, err := policy.AddPeer(testPolicyPeer(t, "198.51.100.1"))
	var jed2k *JED2KError
	if !errors.As(err, &jed2k) || jed2k.EC != PeerLimitExceeded {
		t.Fatalf("101st peer err %v, want PeerLimitExceeded", err)
	}
	if got := policy.Size(); got != MaxPeerListSize {
		t.Fatalf("default list size %d, want %d", got, MaxPeerListSize)
	}
}

func TestPolicyMaxPeerListSizeNonPositiveFallsBack(t *testing.T) {
	session, transfer := newTestTransfer(t)
	session.settings.MaxPeerListSize = 0
	if got := transfer.policy.maxPeerListSize(); got != MaxPeerListSize {
		t.Fatalf("MaxPeerListSize<=0 fallback %d, want %d", got, MaxPeerListSize)
	}
}

func TestPolicyMaxPeerListSizeEraseThenAdd(t *testing.T) {
	session, transfer := newTestTransfer(t)
	session.settings.MaxPeerListSize = 2

	stale := testPolicyPeer(t, "192.0.2.20")
	stale.Connectable = false
	stale.FailCount = 1
	if _, err := transfer.policy.AddPeer(stale); err != nil {
		t.Fatal(err)
	}
	addPolicyPeer(t, &transfer.policy, "192.0.2.21")
	addPolicyPeer(t, &transfer.policy, "192.0.2.22")
	if transfer.policy.Get(stale.Endpoint) != nil {
		t.Fatal("stale peer should be erased when list is full")
	}
	if got := transfer.policy.Size(); got != 2 {
		t.Fatalf("peer count after erase-add %d, want 2", got)
	}
}
