package bootstrap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultStatePath(t *testing.T) {
	path := DefaultStatePath()
	if path == "" {
		t.Fatal("expected non-empty default state path")
	}
	if filepath.Base(path) != "state.json" {
		t.Fatalf("expected state.json basename, got %q", path)
	}
}

func TestApplyEnvOverrides(t *testing.T) {
	t.Setenv("GOED2K_OUTDIR", "/tmp/goed2k-out")
	t.Setenv("GOED2K_SERVERS", "1.2.3.4:4661")
	t.Setenv("GOED2K_KAD", "false")
	t.Setenv("GOED2K_LINKS", "ed2k://|file|a.bin|1|31D6CFE0D16AE931B73C59D7E0C089C0|/")

	cfg := DefaultConfig()
	cfg.ApplyEnv("GOED2K_")
	if cfg.OutDir != "/tmp/goed2k-out" {
		t.Fatalf("out dir: %q", cfg.OutDir)
	}
	if cfg.ServerAddr != "1.2.3.4:4661" {
		t.Fatalf("servers: %q", cfg.ServerAddr)
	}
	if cfg.EnableKAD {
		t.Fatal("expected KAD disabled")
	}
	if len(cfg.Links) != 1 {
		t.Fatalf("links: %v", cfg.Links)
	}
}

func TestInitClientPersistsStatePath(t *testing.T) {
	settings := DefaultConfig()
	settings.ListenPort = 0
	settings.UDPPort = 0
	settings.EnableKAD = false
	settings.EnableKADV6 = false
	settings.EnableUPnP = false
	settings.ServerAddr = ""
	settings.ServerMetPath = ""
	settings.KADNodesDat = ""
	settings.StatePath = filepath.Join(t.TempDir(), "state.json")
	settings.DisableState = false

	client, err := InitClient(settings, nil)
	if err != nil {
		t.Fatalf("init client: %v", err)
	}
	defer client.Close()

	if client.StatePath() != settings.StatePath {
		t.Fatalf("state path %q != %q", client.StatePath(), settings.StatePath)
	}
	if _, err := os.Stat(filepath.Dir(settings.StatePath)); err != nil {
		t.Fatalf("state dir: %v", err)
	}
}

func TestSplitCommaList(t *testing.T) {
	got := SplitCommaList(" a , ,b, c ")
	if len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Fatalf("unexpected: %v", got)
	}
}
