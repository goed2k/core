package main

import (
	"testing"

	ed2k "github.com/goed2k/core"
)

func TestBootstrapConfigKeepsLibraryPolicyDefaults(t *testing.T) {
	t.Setenv("GOED2K_EMULE_TEMP_LAYOUT", "")
	t.Setenv("GOED2K_PARTIAL_KAD_PUBLISH", "")
	t.Setenv("GOED2K_PREALLOCATE_DISK", "")
	t.Setenv("GOED2K_SPARSE_FILES", "")
	t.Setenv("GOED2K_WEB_DOWNLOAD", "")
	t.Setenv("GOED2K_MAX_HTTP_SOURCES", "")
	t.Setenv("GOED2K_MAX_CONCURRENT_HTTP_BLOCKS", "")
	t.Setenv("GOED2K_HTTP_REQUEST_TIMEOUT_SEC", "")
	cfg := defaultRunConfig()
	got := cfg.bootstrapConfig()
	lib := ed2k.NewSettings()
	if got.PartialKadPublish != lib.PartialKadPublish {
		t.Fatalf("PartialKadPublish=%v, want %v", got.PartialKadPublish, lib.PartialKadPublish)
	}
	if got.EnableWebDownload != lib.EnableWebDownload {
		t.Fatalf("EnableWebDownload=%v, want %v", got.EnableWebDownload, lib.EnableWebDownload)
	}
	if got.MaxHttpSources != lib.MaxHttpSources || got.MaxConcurrentHttpBlocks != lib.MaxConcurrentHttpBlocks {
		t.Fatalf("http sources %d/%d, want %d/%d", got.MaxHttpSources, got.MaxConcurrentHttpBlocks, lib.MaxHttpSources, lib.MaxConcurrentHttpBlocks)
	}
	if got.UseEmuleTempLayout || got.PreallocateDiskSpace || got.UseSparseFiles {
		t.Fatalf("expected disk flags off by default: %+v", got)
	}
}

func TestBootstrapConfigAppliesPolicyEnv(t *testing.T) {
	t.Setenv("GOED2K_SPARSE_FILES", "true")
	t.Setenv("GOED2K_WEB_DOWNLOAD", "false")
	t.Setenv("GOED2K_MAX_HTTP_SOURCES", "6")
	got := defaultRunConfig().bootstrapConfig()
	if !got.UseSparseFiles {
		t.Fatal("expected GOED2K_SPARSE_FILES to enable sparse files")
	}
	if got.EnableWebDownload {
		t.Fatal("expected GOED2K_WEB_DOWNLOAD=false to disable web download")
	}
	if got.MaxHttpSources != 6 {
		t.Fatalf("MaxHttpSources=%d", got.MaxHttpSources)
	}
}
