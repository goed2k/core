package goed2k

import (
	"net"
	"path/filepath"
	"testing"
)

// TestKADV6PublishSearchMergePipeline 验证发布 → 本地索引 → 并入 Policy 的闭环（不依赖外网）。
func TestKADV6PublishSearchMergePipeline(t *testing.T) {
	session, transfer := newTestTransfer(t)
	session.settings.EnableDHTv6 = true
	transfer.state = Finished

	tracker := NewKADV6Tracker(0, 0)
	seed := mustUDPAddrV6(t, "[2001:db8::1]:4672")
	tracker.AddNode(seed)
	session.dhtv6Tracker = tracker

	tcpAddr := &net.TCPAddr{IP: net.ParseIP("2001:db8::42"), Port: 4661}
	session.publishSingleTransferKADV6(tracker, tcpAddr, transfer)

	entries := tracker.searchEntriesLocked(transfer.hash)
	if len(entries) != 1 {
		t.Fatalf("expected 1 published source, got %d", len(entries))
	}
	got, ok := entries[0].SourceAddr()
	if !ok || got.String() != "[2001:db8::42]:4661" {
		t.Fatalf("unexpected published source %v ok=%v", got, ok)
	}

	session.mergeKADV6SearchResults(transfer.hash, transfer, entries)
	if transfer.policy.Size() == 0 {
		t.Fatal("expected peer merged into transfer policy")
	}
	peer, ok := PeerFromKADV6SearchEntry(entries[0], int(PeerKADV6))
	if !ok || peer.DialAddr == nil {
		t.Fatal("expected IPv6 dial peer from search entry")
	}
}

// TestKADV6PublishSearchPipelineUsesInjectedEndpoint 用注入的文档地址走 PublishTransferToKADV6，不探测本机/公网 IPv6。
func TestKADV6PublishSearchPipelineUsesInjectedEndpoint(t *testing.T) {
	session, transfer := newTestTransfer(t)
	session.settings.EnableDHTv6 = true
	session.settings.ListenPort = 4661
	session.detectOutboundIPv6 = func() net.IP { return net.ParseIP("2001:db8::42") }
	transfer.state = Finished

	tracker := NewKADV6Tracker(0, 0)
	seed := mustUDPAddrV6(t, "[2001:db8::1]:4672")
	tracker.AddNode(seed)
	session.dhtv6Tracker = tracker

	tcpAddr := session.kadv6PublishEndpoint()
	if tcpAddr == nil || tcpAddr.String() != "[2001:db8::42]:4661" {
		t.Fatalf("expected injected publish endpoint, got %v", tcpAddr)
	}

	session.PublishTransferToKADV6(transfer)

	entries := tracker.searchEntriesLocked(transfer.hash)
	if len(entries) == 0 {
		t.Fatal("expected published source in local index after PublishTransferToKADV6")
	}
	got, ok := entries[0].SourceAddr()
	if !ok || got.String() != "[2001:db8::42]:4661" {
		t.Fatalf("unexpected published source %v ok=%v", got, ok)
	}
}

func TestClientLoadStateRestoresIdentityKeyFromStateFile(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "identity.pem")
	if _, err := GenerateIdentityKeyPair(keyPath); err != nil {
		t.Fatalf("generate identity: %v", err)
	}

	statePath := filepath.Join(dir, "state.json")
	client := NewClient(NewSettings())
	client.SetStatePath(statePath)
	if err := client.Session().LoadIdentity(keyPath); err != nil {
		t.Fatalf("load identity: %v", err)
	}
	if err := client.SaveState(statePath); err != nil {
		t.Fatalf("save state: %v", err)
	}

	restored := NewClient(NewSettings())
	if err := restored.LoadState(statePath); err != nil {
		t.Fatalf("load state: %v", err)
	}
	id := restored.Session().Identity()
	if id == nil || !id.Available() {
		t.Fatal("expected restored identity to be available")
	}
	if id.KeyPath() != keyPath {
		t.Fatalf("expected key path %q, got %q", keyPath, id.KeyPath())
	}
}
