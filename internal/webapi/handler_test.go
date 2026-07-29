package webapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ed2k "github.com/goed2k/core"
	"github.com/goed2k/core/protocol"
)

type fakeClient struct {
	status    ed2k.ClientStatus
	transfers []ed2k.TransferSnapshot
	addLink   func(linkValue, outputDir string) (ed2k.TransferHandle, string, error)
	pause     func(hash protocol.Hash) error
	resume    func(hash protocol.Hash) error
	remove    func(hash protocol.Hash, deleteFile bool) error
}

func (f *fakeClient) Status() ed2k.ClientStatus {
	return f.status
}

func (f *fakeClient) TransferSnapshots() []ed2k.TransferSnapshot {
	return f.transfers
}

func (f *fakeClient) AddLink(linkValue, outputDir string) (ed2k.TransferHandle, string, error) {
	if f.addLink != nil {
		return f.addLink(linkValue, outputDir)
	}
	return ed2k.TransferHandle{}, "", nil
}

func (f *fakeClient) PauseTransfer(hash protocol.Hash) error {
	if f.pause != nil {
		return f.pause(hash)
	}
	return nil
}

func (f *fakeClient) ResumeTransfer(hash protocol.Hash) error {
	if f.resume != nil {
		return f.resume(hash)
	}
	return nil
}

func (f *fakeClient) RemoveTransfer(hash protocol.Hash, deleteFile bool) error {
	if f.remove != nil {
		return f.remove(hash, deleteFile)
	}
	return nil
}

func TestHandlerStatus(t *testing.T) {
	h := NewHandler(&fakeClient{
		status: ed2k.ClientStatus{
			DownloadRate:  1024,
			UploadRate:    512,
			TotalDone:     10,
			TotalReceived: 20,
			TotalWanted:   100,
			Transfers:     make([]ed2k.TransferSnapshot, 2),
			Peers:         make([]ed2k.ClientPeerSnapshot, 3),
			Servers:       make([]ed2k.ServerSnapshot, 1),
		},
	}, ".", "", "")

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", rec.Code, http.StatusOK)
	}
	var resp statusResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.DownloadRate != 1024 || resp.TransferCount != 2 || resp.PeerCount != 3 || resp.ServerCount != 1 {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestHandlerListTransfers(t *testing.T) {
	hash := protocol.MustHashFromString("0123456789ABCDEF0123456789ABCDEF")
	h := NewHandler(&fakeClient{
		transfers: []ed2k.TransferSnapshot{
			{
				Hash:             hash,
				FileName:         "demo.mp4",
				Size:             1000,
				DownloadPriority: ed2k.TransferPriorityHigh,
				Status: ed2k.TransferStatus{
					State:         ed2k.Downloading,
					TotalDone:     100,
					TotalReceived: 200,
					TotalWanted:   1000,
				},
			},
		},
	}, ".", "", "")

	req := httptest.NewRequest(http.MethodGet, "/transfers", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status code = %d", rec.Code)
	}
	var items []transferResponse
	if err := json.NewDecoder(rec.Body).Decode(&items); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(items) != 1 || items[0].FileName != "demo.mp4" || items[0].Priority != "P3" {
		t.Fatalf("unexpected items: %+v", items)
	}
}

func TestHandlerAddTransfer(t *testing.T) {
	var gotLink, gotDir string
	h := NewHandler(&fakeClient{
		addLink: func(linkValue, outputDir string) (ed2k.TransferHandle, string, error) {
			gotLink = linkValue
			gotDir = outputDir
			return ed2k.TransferHandle{}, "/downloads/demo.mp4", nil
		},
	}, "/downloads", "", "")

	body := `{"link":"ed2k://|file|demo.mp4|1000|0123456789ABCDEF0123456789ABCDEF|/"}`
	req := httptest.NewRequest(http.MethodPost, "/transfers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status code = %d body=%s", rec.Code, rec.Body.String())
	}
	if gotLink == "" || gotDir != "/downloads" {
		t.Fatalf("addLink called with link=%q dir=%q", gotLink, gotDir)
	}
}

func TestHandlerTransferActions(t *testing.T) {
	hash := protocol.MustHashFromString("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	var paused, resumed, removed bool
	h := NewHandler(&fakeClient{
		pause: func(got protocol.Hash) error {
			if got != hash {
				t.Fatalf("pause hash mismatch")
			}
			paused = true
			return nil
		},
		resume: func(got protocol.Hash) error {
			resumed = true
			return nil
		},
		remove: func(got protocol.Hash, deleteFile bool) error {
			removed = true
			return nil
		},
	}, ".", "", "")

	hashHex := hash.String()
	for _, action := range []struct {
		path string
		check func() bool
	}{
		{"/transfers/" + hashHex + "/pause", func() bool { return paused }},
		{"/transfers/" + hashHex + "/resume", func() bool { return resumed }},
		{"/transfers/" + hashHex + "/delete", func() bool { return removed }},
	} {
		req := httptest.NewRequest(http.MethodPost, action.path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%s status = %d", action.path, rec.Code)
		}
		if !action.check() {
			t.Fatalf("%s action not recorded", action.path)
		}
	}
}

func TestHandlerBasicAuth(t *testing.T) {
	h := NewHandler(&fakeClient{}, ".", "admin", "secret")

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/status", nil)
	req.SetBasicAuth("admin", "secret")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestHandlerNotFound(t *testing.T) {
	h := NewHandler(&fakeClient{}, ".", "", "")
	req := httptest.NewRequest(http.MethodGet, "/unknown", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
