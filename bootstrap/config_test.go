package bootstrap

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	ed2k "github.com/goed2k/core"
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

func TestBuildSettingsMapsPolicyFields(t *testing.T) {
	cfg := DefaultConfig()
	cfg.UseEmuleTempLayout = true
	cfg.PartialKadPublish = false
	cfg.PreallocateDiskSpace = true
	cfg.UseSparseFiles = true
	cfg.EnableWebDownload = false
	cfg.MaxHttpSources = 9
	cfg.MaxConcurrentHttpBlocks = 5
	cfg.WebCacheDir = "/tmp/webcache"
	cfg.HttpRequestTimeoutSec = 11
	cfg.MaxDownloadRateKB = 256
	cfg.MaxUploadRateKB = 32

	settings, err := BuildSettings(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !settings.UseEmuleTempLayout || settings.PartialKadPublish || !settings.PreallocateDiskSpace || !settings.UseSparseFiles {
		t.Fatalf("disk/kad flags: %#v", settings)
	}
	if settings.EnableWebDownload || settings.MaxHttpSources != 9 || settings.MaxConcurrentHttpBlocks != 5 {
		t.Fatalf("web: %#v", settings)
	}
	if settings.WebCacheDir != "/tmp/webcache" || settings.HttpRequestTimeoutSec != 11 {
		t.Fatalf("http: %q %d", settings.WebCacheDir, settings.HttpRequestTimeoutSec)
	}
	if settings.MaxDownloadRateKB != 256 || settings.MaxUploadRateKB != 32 {
		t.Fatalf("rates %d/%d", settings.MaxDownloadRateKB, settings.MaxUploadRateKB)
	}
}

func TestDefaultConfigAlignsPersistableSettings(t *testing.T) {
	cfg := DefaultConfig()
	settings, err := BuildSettings(cfg)
	if err != nil {
		t.Fatal(err)
	}
	lib := ed2k.NewSettings()
	if settings.PartialKadPublish != lib.PartialKadPublish ||
		settings.EnableWebDownload != lib.EnableWebDownload ||
		settings.MaxHttpSources != lib.MaxHttpSources ||
		settings.MaxConcurrentHttpBlocks != lib.MaxConcurrentHttpBlocks ||
		settings.HttpRequestTimeoutSec != lib.HttpRequestTimeoutSec ||
		settings.UseEmuleTempLayout != lib.UseEmuleTempLayout ||
		settings.PreallocateDiskSpace != lib.PreallocateDiskSpace ||
		settings.UseSparseFiles != lib.UseSparseFiles {
		t.Fatalf("default config drifted from NewSettings: cfg mapped=%+v lib=%+v", settings, lib)
	}
}

func TestApplyEnvOverridesPolicyFields(t *testing.T) {
	t.Setenv("GOED2K_EMULE_TEMP_LAYOUT", "true")
	t.Setenv("GOED2K_PREALLOCATE_DISK", "true")
	t.Setenv("GOED2K_SPARSE_FILES", "true")
	t.Setenv("GOED2K_WEB_DOWNLOAD", "false")
	t.Setenv("GOED2K_MAX_HTTP_SOURCES", "8")
	cfg := DefaultConfig()
	cfg.ApplyEnv("GOED2K_")
	if !cfg.UseEmuleTempLayout || !cfg.PreallocateDiskSpace || !cfg.UseSparseFiles {
		t.Fatalf("env disk flags: %+v", cfg)
	}
	if cfg.EnableWebDownload || cfg.MaxHttpSources != 8 {
		t.Fatalf("env web: %+v", cfg)
	}
}

func TestInitClientConfigWinsOverRestoredSettings(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")

	first := DefaultConfig()
	first.ListenPort = 0
	first.UDPPort = 0
	first.EnableKAD = false
	first.EnableKADV6 = false
	first.EnableUPnP = false
	first.ServerAddr = ""
	first.ServerMetPath = ""
	first.KADNodesDat = ""
	first.StatePath = statePath
	first.UseSparseFiles = true
	first.MaxDownloadRateKB = 10
	client, err := InitClient(first, nil)
	if err != nil {
		t.Fatalf("init first: %v", err)
	}
	if err := client.SaveState(""); err != nil {
		client.Close()
		t.Fatal(err)
	}
	client.Close()

	second := first
	second.UseSparseFiles = false
	second.MaxDownloadRateKB = 99
	client2, err := InitClient(second, nil)
	if err != nil {
		t.Fatalf("init second: %v", err)
	}
	defer client2.Close()
	if err := client2.SaveState(""); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"use_sparse_files": false`)) {
		t.Fatalf("config should win over restored sparse flag: %s", raw)
	}
	if !bytes.Contains(raw, []byte(`"max_download_rate_kb": 99`)) {
		t.Fatalf("config should win over restored rate: %s", raw)
	}
}

func TestSplitCommaList(t *testing.T) {
	got := SplitCommaList(" a , ,b, c ")
	if len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Fatalf("unexpected: %v", got)
	}
}
