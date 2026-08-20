package goed2k

import (
	"testing"

	"github.com/goed2k/core/protocol"
)

func TestPolicyMaxFailCountUsesSettings(t *testing.T) {
	session, transfer := newTestTransfer(t)
	session.settings.MaxFailCount = 2
	endpoint, err := protocol.EndpointFromString("192.0.2.10", 4662)
	if err != nil {
		t.Fatal(err)
	}
	peer := Peer{Endpoint: endpoint, Connectable: true, FailCount: 3}
	if transfer.policy.IsConnectCandidate(peer) {
		t.Fatal("FailCount above Settings.MaxFailCount should not be a connect candidate")
	}
	peer.FailCount = 2
	if !transfer.policy.IsConnectCandidate(peer) {
		t.Fatal("FailCount equal to Settings.MaxFailCount should still be connectable")
	}
}

func TestPolicyMaxFailCountDefaultTwentyWithoutSession(t *testing.T) {
	policy := NewPolicy(nil)
	endpoint, err := protocol.EndpointFromString("192.0.2.11", 4662)
	if err != nil {
		t.Fatal(err)
	}
	peer := Peer{Endpoint: endpoint, Connectable: true, FailCount: 20}
	if !policy.IsConnectCandidate(peer) {
		t.Fatal("default MaxFailCount 20 should still allow FailCount=20")
	}
	peer.FailCount = 21
	if policy.IsConnectCandidate(peer) {
		t.Fatal("default MaxFailCount 20 should reject FailCount=21")
	}
}
