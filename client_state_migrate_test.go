package goed2k

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateClientStateAcceptsHistoricalVersions(t *testing.T) {
	t.Parallel()
	for _, v := range []int{0, 1, 2, 3, 4, 5, 6, 7, 8} {
		st := &ClientState{Version: v, ServerAddress: "1.2.3.4:4661"}
		if err := migrateClientState(st); err != nil {
			t.Fatalf("version %d: %v", v, err)
		}
		if st.Version != clientStateVersion {
			t.Fatalf("version %d migrated to %d, want %d", v, st.Version, clientStateVersion)
		}
	}
}

func TestMigrateClientStateRejectsUnknownVersion(t *testing.T) {
	t.Parallel()
	st := &ClientState{Version: 99}
	if err := migrateClientState(st); err == nil {
		t.Fatal("expected unsupported version 99")
	}
}

func TestPersistableSettingsRoundTrip(t *testing.T) {
	settings := NewSettings()
	settings.ListenPort = 0
	settings.UseEmuleTempLayout = true
	settings.PartialKadPublish = false
	settings.PreallocateDiskSpace = true
	settings.UseSparseFiles = true
	settings.EnableWebDownload = false
	settings.MaxHttpSources = 7
	settings.MaxConcurrentHttpBlocks = 3
	settings.WebCacheDir = filepath.Join(t.TempDir(), "httpcache")
	settings.HttpRequestTimeoutSec = 12
	settings.MaxDownloadRateKB = 128
	settings.MaxUploadRateKB = 64

	client := NewClient(settings)
	registerClientTransferFileCleanup(t, client)
	path := filepath.Join(t.TempDir(), "state.json")
	client.SetStatePath(path)
	if err := client.SaveState(""); err != nil {
		t.Fatalf("save: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var dumped ClientState
	if err := json.Unmarshal(raw, &dumped); err != nil {
		t.Fatal(err)
	}
	if dumped.Version != clientStateVersion {
		t.Fatalf("saved version %d", dumped.Version)
	}
	if dumped.Settings == nil {
		t.Fatal("expected settings in snapshot")
	}

	restored := NewClient(NewSettings())
	registerClientTransferFileCleanup(t, restored)
	if err := restored.LoadState(path); err != nil {
		t.Fatalf("load: %v", err)
	}
	got := persistableSettingsFrom(restored.session.settings)
	want := persistableSettingsFrom(settings)
	if got != want {
		t.Fatalf("restored %#v, want %#v", got, want)
	}
}

func TestLoadStateMigratesVersion5And6(t *testing.T) {
	dir := t.TempDir()
	for _, v := range []int{5, 6} {
		path := filepath.Join(dir, "state.json")
		legacy := ClientState{Version: v, ServerAddress: "9.9.9.9:4661"}
		raw, err := json.Marshal(legacy)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0o644); err != nil {
			t.Fatal(err)
		}
		client := NewClient(NewSettings())
		registerClientTransferFileCleanup(t, client)
		if err := client.LoadState(path); err != nil {
			t.Fatalf("load v%d: %v", v, err)
		}
		if client.ServerAddress() != "9.9.9.9:4661" {
			t.Fatalf("v%d server %q", v, client.ServerAddress())
		}
		if err := client.SaveState(""); err != nil {
			t.Fatalf("resave v%d: %v", v, err)
		}
		rewritten, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var got ClientState
		if err := json.Unmarshal(rewritten, &got); err != nil {
			t.Fatal(err)
		}
		if got.Version != clientStateVersion {
			t.Fatalf("v%d resaved as %d", v, got.Version)
		}
	}
}

func TestEphemeralSettingsStayProcessLocal(t *testing.T) {
	settings := NewSettings()
	settings.ListenPort = 0
	settings.ClientName = "ephemeral-name"
	settings.MaxPeerListSize = 42
	client := NewClient(settings)
	registerClientTransferFileCleanup(t, client)
	path := filepath.Join(t.TempDir(), "state.json")
	if err := client.SaveState(path); err != nil {
		t.Fatal(err)
	}
	other := NewClient(NewSettings())
	registerClientTransferFileCleanup(t, other)
	if err := other.LoadState(path); err != nil {
		t.Fatal(err)
	}
	if other.session.settings.ClientName == "ephemeral-name" {
		t.Fatal("ClientName should not be restored from state")
	}
	if other.session.settings.MaxPeerListSize == 42 {
		t.Fatal("MaxPeerListSize should not be restored from state")
	}
}
