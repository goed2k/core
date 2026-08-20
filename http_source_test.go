package goed2k

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/goed2k/core/data"
	"github.com/goed2k/core/disk"
	"github.com/goed2k/core/protocol"
)

func TestTransferHttpSourceDownloadsBlock(t *testing.T) {
	UpdateCachedTime()
	payload := make([]byte, BlockSize)
	for i := range payload {
		payload[i] = byte(i % 251)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Range"); got != fmt.Sprintf("bytes=0-%d", BlockSize-1) {
			t.Fatalf("unexpected range %q", got)
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", BlockSize-1, BlockSize))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "download.bin")
	settings := NewSettings()
	settings.EnableWebDownload = true
	settings.MaxConcurrentHttpBlocks = 1
	session := NewSession(settings)

	handle, err := session.AddTransferParams(AddTransferParams{
		Hash:       protocol.MustHashFromString("BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"),
		CreateTime: CurrentTimeMillis(),
		Size:       BlockSize,
		Handler:    disk.NewDesktopFileHandler(path),
		HttpSources: []string{
			server.URL + "/file.bin",
		},
	})
	if err != nil {
		t.Fatalf("add transfer: %v", err)
	}
	registerTransferFileCleanup(t, handle)
	transfer := handle.transfer

	deadline := CurrentTime() + Seconds(5)
	for CurrentTime() < deadline {
		session.SecondTick(CurrentTime(), 100)
		session.processDiskTasks()
		if transfer.picker.HavePiece(0) {
			break
		}
	}
	if !transfer.picker.HavePiece(0) {
		t.Fatal("expected piece 0 to be downloaded via http")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if len(raw) != len(payload) {
		t.Fatalf("expected %d bytes, got %d", len(payload), len(raw))
	}
	for i := range payload {
		if raw[i] != payload[i] {
			t.Fatalf("byte mismatch at %d", i)
		}
	}
}

func TestWebCacheStoresDownloadedBlock(t *testing.T) {
	UpdateCachedTime()
	payload := make([]byte, BlockSizeInt)
	for i := range payload {
		payload[i] = byte(255 - i%200)
	}

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", BlockSize-1, BlockSize))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	path := filepath.Join(dir, "download.bin")
	hash := protocol.MustHashFromString("CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC")
	settings := NewSettings()
	settings.EnableWebDownload = true
	settings.WebCacheDir = cacheDir
	session := NewSession(settings)

	handle, err := session.AddTransferParams(AddTransferParams{
		Hash:        hash,
		CreateTime:  CurrentTimeMillis(),
		Size:        BlockSize,
		Handler:     disk.NewDesktopFileHandler(path),
		HttpSources: []string{server.URL},
	})
	if err != nil {
		t.Fatalf("add transfer: %v", err)
	}
	registerTransferFileCleanup(t, handle)
	transfer := handle.transfer
	block := data.NewPieceBlock(0, 0)

	deadline := CurrentTime() + Seconds(5)
	for CurrentTime() < deadline {
		session.SecondTick(CurrentTime(), 100)
		session.processDiskTasks()
		if transfer.picker.HavePiece(0) {
			break
		}
	}
	if !transfer.picker.HavePiece(0) {
		t.Fatal("expected piece downloaded")
	}
	if requests != 1 {
		t.Fatalf("expected 1 http request, got %d", requests)
	}

	cachePath := webCachePath(cacheDir, hash, block)
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("cache file missing: %v", err)
	}

	requests = 0
	transfer.picker.RestorePiece(0)
	transfer.picker.DownloadPiece(0)

	deadline = CurrentTime() + Seconds(5)
	for CurrentTime() < deadline {
		session.SecondTick(CurrentTime(), 100)
		session.processDiskTasks()
		if transfer.picker.HavePiece(0) {
			break
		}
	}
	if requests != 0 {
		t.Fatalf("expected cache hit without http request, got %d requests", requests)
	}
}

func TestHttpSourceLastPieceBlockRange(t *testing.T) {
	UpdateCachedTime()
	block := data.NewPieceBlock(0, BlocksPerPiece-1)
	wantSize := block.Size(PieceSize)
	if wantSize != 140*1024 {
		t.Fatalf("last block size=%d, want 143360", wantSize)
	}
	payload := make([]byte, wantSize)
	for i := range payload {
		payload[i] = byte(i % 199)
	}

	var gotRange string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRange = r.Header.Get("Range")
		rng := block.Range(PieceSize)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", rng.Left, rng.Right-1, PieceSize))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	settings := NewSettings()
	settings.EnableWebDownload = true
	session := NewSession(settings)
	handle, err := session.AddTransferParams(AddTransferParams{
		Hash:       protocol.MustHashFromString("DDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD"),
		CreateTime: CurrentTimeMillis(),
		Size:       PieceSize,
		Handler:    disk.NewDesktopFileHandler(filepath.Join(t.TempDir(), "last.bin")),
	})
	if err != nil {
		t.Fatalf("add transfer: %v", err)
	}
	registerTransferFileCleanup(t, handle)

	raw, err := handle.transfer.fetchHttpBlock(server.URL+"/file.bin", block)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	rng := block.Range(PieceSize)
	wantHeader := fmt.Sprintf("bytes=%d-%d", rng.Left, rng.Right-1)
	if gotRange != wantHeader {
		t.Fatalf("Range %q, want %q", gotRange, wantHeader)
	}
	if len(raw) != wantSize {
		t.Fatalf("payload %d, want %d", len(raw), wantSize)
	}
}

func TestNormalizeHTTPSourceURL(t *testing.T) {
	got, err := normalizeHTTPSourceURL("https://example.com/files/a.bin")
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if got != "https://example.com/files/a.bin" {
		t.Fatalf("unexpected url %q", got)
	}
	if _, err := normalizeHTTPSourceURL("ftp://example.com/a"); err == nil {
		t.Fatal("expected error for ftp scheme")
	}
}

func TestPeerInfoWebSourceLabel(t *testing.T) {
	info := PeerInfo{SourceFlag: int(PeerWeb)}
	if got := info.SourceString(); got != "web" {
		t.Fatalf("unexpected label %q", got)
	}
}
