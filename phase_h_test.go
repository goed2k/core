package goed2k

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/goed2k/core/data"
	"github.com/goed2k/core/protocol"
)

func TestTransferPrioritySortsConnectNewPeersOrder(t *testing.T) {
	UpdateCachedTime()
	session := NewSession(NewSettings())
	low, err := NewTransfer(session, AddTransferParams{
		Hash:       protocol.EMule,
		CreateTime: 100,
		Size:       PieceSize,
	})
	if err != nil {
		t.Fatalf("new transfer low: %v", err)
	}
	low.SetDownloadPriority(TransferPriorityLow)
	high, err := NewTransfer(session, AddTransferParams{
		Hash:       protocol.MustHashFromString("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"),
		CreateTime: 200,
		Size:       PieceSize,
	})
	if err != nil {
		t.Fatalf("new transfer high: %v", err)
	}
	high.SetDownloadPriority(TransferPriorityHigh)
	session.transfers[low.hash] = low
	session.transfers[high.hash] = high

	sorted := sortTransfersByDownloadPriority(session.snapshotTransfers())
	if len(sorted) != 2 {
		t.Fatalf("expected 2 transfers, got %d", len(sorted))
	}
	if sorted[0] != high {
		t.Fatal("expected high priority transfer first")
	}
	if sorted[1] != low {
		t.Fatal("expected low priority transfer second")
	}
}

func TestSetTransferPriorityPersistsInClientState(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	client := NewClient(NewSettings())
	client.SetStatePath(statePath)

	handle, err := client.AddTransfer(AddTransferParams{
		Hash:       protocol.EMule,
		CreateTime: CurrentTimeMillis(),
		Size:       PieceSize,
		FilePath:   filepath.Join(dir, "file.bin"),
	})
	if err != nil {
		t.Fatalf("add transfer: %v", err)
	}
	if err := client.SetTransferPriority(handle.GetHash(), TransferPriorityVeryHigh); err != nil {
		t.Fatalf("set priority: %v", err)
	}

	restored := NewClient(NewSettings())
	if err := restored.LoadState(statePath); err != nil {
		t.Fatalf("load state: %v", err)
	}
	restoredHandle := restored.FindTransfer(handle.GetHash())
	if !restoredHandle.IsValid() {
		t.Fatal("restored transfer not found")
	}
	if restoredHandle.transfer.DownloadPriority() != TransferPriorityVeryHigh {
		t.Fatalf("expected priority %d, got %d", TransferPriorityVeryHigh, restoredHandle.transfer.DownloadPriority())
	}
}

func TestParseIPFilterBlocksCIDRAndSingleIP(t *testing.T) {
	filter, err := ParseIPFilter("10.0.0.0/8\n203.0.113.55\n# comment\n")
	if err != nil {
		t.Fatalf("parse filter: %v", err)
	}
	if !filter.Contains(netParseIP(t, "10.1.2.3")) {
		t.Fatal("expected 10.1.2.3 blocked")
	}
	if !filter.Contains(netParseIP(t, "203.0.113.55")) {
		t.Fatal("expected single IP blocked")
	}
	if filter.Contains(netParseIP(t, "8.8.8.8")) {
		t.Fatal("expected public IP allowed")
	}
}

func TestPolicySkipsIPFilteredPeer(t *testing.T) {
	session, transfer := newTestTransfer(t)
	filter, err := ParseIPFilter("1.2.3.4\n")
	if err != nil {
		t.Fatalf("parse filter: %v", err)
	}
	session.SetIPFilter(filter)
	endpoint, err := protocol.EndpointFromString("1.2.3.4", 4662)
	if err != nil {
		t.Fatalf("endpoint: %v", err)
	}
	if err := transfer.AddPeer(endpoint, int(PeerServer)); err != nil {
		t.Fatalf("add peer: %v", err)
	}
	peer := transfer.policy.FindPeer(endpoint)
	if peer == nil {
		t.Fatal("peer not found")
	}
	if transfer.policy.IsConnectCandidate(*peer) {
		t.Fatal("expected filtered peer to be rejected as connect candidate")
	}
}

func TestBanPeerPersistsAndRemovesFromPolicy(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	client := NewClient(NewSettings())
	client.SetStatePath(statePath)

	handle, err := client.AddTransfer(AddTransferParams{
		Hash:       protocol.EMule,
		CreateTime: CurrentTimeMillis(),
		Size:       PieceSize,
		FilePath:   filepath.Join(dir, "file.bin"),
	})
	if err != nil {
		t.Fatalf("add transfer: %v", err)
	}
	endpoint, err := protocol.EndpointFromString("203.0.113.9", 4662)
	if err != nil {
		t.Fatalf("endpoint: %v", err)
	}
	if err := handle.transfer.AddPeer(endpoint, int(PeerServer)); err != nil {
		t.Fatalf("add peer: %v", err)
	}
	if err := client.BanPeer(endpoint); err != nil {
		t.Fatalf("ban peer: %v", err)
	}
	if peer := handle.transfer.policy.FindPeer(endpoint); peer != nil {
		t.Fatal("expected banned peer removed from policy")
	}

	restored := NewClient(NewSettings())
	if err := restored.LoadState(statePath); err != nil {
		t.Fatalf("load state: %v", err)
	}
	banned := restored.session.BannedPeers()
	if len(banned) != 1 || !banned[0].Equal(endpoint) {
		t.Fatalf("expected banned peer persisted, got %+v", banned)
	}
}

func TestExportPartMetWritesJSONSidecar(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "movie.avi")
	resume := &protocol.TransferResumeData{
		Hashes: []protocol.Hash{protocol.EMule},
		Pieces: protocol.NewBitField(2),
		DownloadedBlocks: []data.PieceBlock{
			data.NewPieceBlock(1, 0),
		},
		Peers: []protocol.Endpoint{
			mustEndpoint(t, "203.0.113.10", 4662),
		},
	}
	resume.Pieces.SetBit(0)

	if err := ExportPartMet(path, resume); err != nil {
		t.Fatalf("export part met: %v", err)
	}
	target := path + ".part.met"
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read part met: %v", err)
	}
	var doc PartMetDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if doc.Format != partMetFormat || doc.Version != partMetVersion {
		t.Fatalf("unexpected header: %+v", doc)
	}
	if len(doc.PieceHashes) != 1 || len(doc.CompletedPieces) != 2 || !doc.CompletedPieces[0] {
		t.Fatalf("unexpected piece data: %+v", doc)
	}
	if len(doc.DownloadedBlocks) != 1 || doc.DownloadedBlocks[0].Piece != 1 {
		t.Fatalf("unexpected blocks: %+v", doc.DownloadedBlocks)
	}
	if len(doc.KnownPeers) != 1 || doc.KnownPeers[0] != "203.0.113.10:4662" {
		t.Fatalf("unexpected peers: %+v", doc.KnownPeers)
	}
}

func TestServerConnectionPolicyPingSortAndTTL(t *testing.T) {
	now := int64(1_000_000)
	fast := NewServerConnectionPolicy(5, 5)
	fast.identifier = "fast"
	fast.SetPingResult(20, 1000, now)
	slow := NewServerConnectionPolicy(5, 5)
	slow.identifier = "slow"
	slow.SetPingResult(80, 1000, now)
	if !compareServerPing(fast, slow, now) {
		t.Fatal("expected faster server to sort first")
	}
	if fast.EffectivePingRTT(now+500) != 20 {
		t.Fatalf("expected ping within TTL, got %d", fast.EffectivePingRTT(now+500))
	}
	if fast.EffectivePingRTT(now+2000) != -1 {
		t.Fatal("expected expired ping to return -1")
	}
}

func TestSortServerIdentifiersByPing(t *testing.T) {
	session := NewSession(NewSettings())
	now := CurrentTime()
	fast := session.ensureServerConnectionPolicy("fast.example:4661")
	fast.SetPingResult(10, defaultServerPingTTLMs, now)
	slow := session.ensureServerConnectionPolicy("slow.example:4661")
	slow.SetPingResult(90, defaultServerPingTTLMs, now)
	ids := []string{"slow.example:4661", "fast.example:4661", "unknown.example:4661"}
	sortServerIdentifiersByPing(session, ids, now)
	if ids[0] != "fast.example:4661" {
		t.Fatalf("expected fast server first, got %v", ids)
	}
	if ids[1] != "slow.example:4661" {
		t.Fatalf("expected slow server second, got %v", ids)
	}
}

func netParseIP(t *testing.T, s string) net.IP {
	t.Helper()
	ip := net.ParseIP(s)
	if ip == nil {
		t.Fatalf("invalid ip %q", s)
	}
	return ip
}

func mustEndpoint(t *testing.T, host string, port int) protocol.Endpoint {
	t.Helper()
	ep, err := protocol.EndpointFromString(host, port)
	if err != nil {
		t.Fatalf("endpoint: %v", err)
	}
	return ep
}
