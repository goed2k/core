package goed2k

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/goed2k/core/data"
	"github.com/goed2k/core/protocol"
)

var webDownloadPeer = Peer{SourceFlag: int(PeerWeb)}

type httpSourceManager struct {
	mu        sync.Mutex
	sources   []string
	active    int
	failCount map[string]int
}

func newHTTPSourceManager(urls []string) *httpSourceManager {
	mgr := &httpSourceManager{
		sources:   make([]string, 0, len(urls)),
		failCount: make(map[string]int),
	}
	for _, raw := range urls {
		if normalized, err := normalizeHTTPSourceURL(raw); err == nil {
			mgr.sources = append(mgr.sources, normalized)
		}
	}
	return mgr
}

func normalizeHTTPSourceURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("empty url")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("unsupported scheme %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return "", errors.New("missing host")
	}
	return parsed.String(), nil
}

func (m *httpSourceManager) Add(url string) error {
	if m == nil {
		return errors.New("nil http source manager")
	}
	normalized, err := normalizeHTTPSourceURL(url)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.sources {
		if existing == normalized {
			return nil
		}
	}
	m.sources = append(m.sources, normalized)
	return nil
}

func (m *httpSourceManager) Sources() []string {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.sources...)
}

func (t *Transfer) AddHttpSource(rawURL string) error {
	if t == nil {
		return errors.New("nil transfer")
	}
	if t.httpSources == nil {
		t.httpSources = newHTTPSourceManager(nil)
	}
	maxSources := 4
	if t.session != nil && t.session.settings.MaxHttpSources > 0 {
		maxSources = t.session.settings.MaxHttpSources
	}
	current := t.httpSources.Sources()
	if len(current) >= maxSources {
		return fmt.Errorf("http source limit reached (%d)", maxSources)
	}
	if err := t.httpSources.Add(rawURL); err != nil {
		return err
	}
	t.needSaveResumeData = true
	return nil
}

func (t *Transfer) HttpSources() []string {
	if t == nil || t.httpSources == nil {
		return nil
	}
	return t.httpSources.Sources()
}

func (t *Transfer) tickHttpSources() {
	if t == nil || t.session == nil || !t.session.settings.EnableWebDownload {
		return
	}
	if t.IsPaused() || t.IsAborted() || t.IsFinished() || t.pm == nil || t.httpSources == nil {
		return
	}
	sources := t.httpSources.Sources()
	if len(sources) == 0 {
		return
	}
	maxConcurrent := t.session.settings.MaxConcurrentHttpBlocks
	if maxConcurrent <= 0 {
		maxConcurrent = 2
	}
	t.httpSources.mu.Lock()
	slots := maxConcurrent - t.httpSources.active
	t.httpSources.mu.Unlock()
	if slots <= 0 {
		return
	}

	webPeer := &webDownloadPeer
	blocks := make([]data.PieceBlock, 0, slots)
	t.picker.PickPiecesWithAvailability(&blocks, slots, webPeer, PeerSpeedFast, nil)
	for i, block := range blocks {
		if !t.picker.MarkAsWriting(block) {
			continue
		}
		source := sources[i%len(sources)]
		t.startHttpFetch(source, block, webPeer)
	}
}

func (t *Transfer) startHttpFetch(sourceURL string, block data.PieceBlock, webPeer *Peer) {
	t.httpSources.mu.Lock()
	t.httpSources.active++
	t.httpSources.mu.Unlock()

	go func() {
		defer func() {
			t.httpSources.mu.Lock()
			t.httpSources.active--
			t.httpSources.mu.Unlock()
		}()

		payload, err := t.fetchHttpBlock(sourceURL, block)
		if err != nil {
			t.httpSources.mu.Lock()
			t.httpSources.failCount[sourceURL]++
			t.httpSources.mu.Unlock()
			t.picker.AbortDownload(block, webPeer)
			return
		}
		if len(payload) != block.Size(t.size) {
			t.picker.AbortDownload(block, webPeer)
			return
		}

		wasFinished := t.picker.IsPieceFinished(block.PieceIndex)
		t.throttleDownloadWrite(len(payload))
		if t.session != nil {
			t.session.SubmitDiskTask(NewAsyncWrite(block, payload, t))
			if t.picker.IsPieceFinished(block.PieceIndex) && !wasFinished {
				t.QueuePieceHash(block.PieceIndex)
			}
		}
	}()
}

func (t *Transfer) fetchHttpBlock(sourceURL string, block data.PieceBlock) ([]byte, error) {
	if cached, ok := t.readWebCache(block); ok {
		return cached, nil
	}
	expectedSize := block.Size(t.size)
	rng := block.Range(t.size)
	req, err := http.NewRequest(http.MethodGet, sourceURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", rng.Left, rng.Right-1))
	req.Header.Set("User-Agent", "goed2k/1.0")

	timeout := 30 * time.Second
	if t.session != nil && t.session.settings.HttpRequestTimeoutSec > 0 {
		timeout = time.Duration(t.session.settings.HttpRequestTimeoutSec) * time.Second
	}
	client := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("stopped after %d redirects", len(via))
			}
			return nil
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http status %s", resp.Status)
	}

	payload, err := io.ReadAll(io.LimitReader(resp.Body, int64(expectedSize)+1))
	if err != nil {
		return nil, err
	}
	if len(payload) != expectedSize {
		return nil, fmt.Errorf("unexpected block size %d want %d", len(payload), expectedSize)
	}
	_ = t.writeWebCache(block, payload)
	return payload, nil
}

func (t *Transfer) webCacheDir() string {
	if t == nil || t.session == nil {
		return ""
	}
	return strings.TrimSpace(t.session.settings.WebCacheDir)
}

func webCachePath(cacheDir string, hash protocol.Hash, block data.PieceBlock) string {
	return filepath.Join(cacheDir, hash.String(), fmt.Sprintf("%d_%d.part", block.PieceIndex, block.PieceBlock))
}

func (t *Transfer) readWebCache(block data.PieceBlock) ([]byte, bool) {
	cacheDir := t.webCacheDir()
	if cacheDir == "" {
		return nil, false
	}
	path := webCachePath(cacheDir, t.hash, block)
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	if len(raw) != block.Size(t.size) {
		return nil, false
	}
	return raw, true
}

func (t *Transfer) writeWebCache(block data.PieceBlock, payload []byte) error {
	cacheDir := t.webCacheDir()
	if cacheDir == "" || len(payload) == 0 {
		return nil
	}
	path := webCachePath(cacheDir, t.hash, block)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
