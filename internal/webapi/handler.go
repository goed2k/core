package webapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	ed2k "github.com/goed2k/core"
	"github.com/goed2k/core/protocol"
)

// ClientAPI 是 Web API 所需的客户端操作子集。
type ClientAPI interface {
	Status() ed2k.ClientStatus
	TransferSnapshots() []ed2k.TransferSnapshot
	AddLink(linkValue, outputDir string) (ed2k.TransferHandle, string, error)
	PauseTransfer(hash protocol.Hash) error
	ResumeTransfer(hash protocol.Hash) error
	RemoveTransfer(hash protocol.Hash, deleteFile bool) error
}

// Handler 提供基于 net/http 的 REST API。
type Handler struct {
	Client        ClientAPI
	DefaultOutDir string
	User          string
	Pass          string
}

// NewHandler 创建 API 处理器。user/pass 均非空时启用 Basic Auth。
func NewHandler(client ClientAPI, defaultOutDir, user, pass string) *Handler {
	return &Handler{
		Client:        client,
		DefaultOutDir: defaultOutDir,
		User:          user,
		Pass:          pass,
	}
}

// ServeHTTP 分发 REST 路由。
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(r) {
		w.Header().Set("WWW-Authenticate", `Basic realm="goed2k"`)
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	path := strings.Trim(r.URL.Path, "/")
	switch {
	case path == "status" && r.Method == http.MethodGet:
		h.handleStatus(w, r)
	case path == "transfers" && r.Method == http.MethodGet:
		h.handleListTransfers(w, r)
	case path == "transfers" && r.Method == http.MethodPost:
		h.handleAddTransfer(w, r)
	case strings.HasPrefix(path, "transfers/") && r.Method == http.MethodPost:
		h.handleTransferAction(w, r, path)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

func (h *Handler) authorize(r *http.Request) bool {
	if h.User == "" && h.Pass == "" {
		return true
	}
	user, pass, ok := r.BasicAuth()
	return ok && user == h.User && pass == h.Pass
}

func (h *Handler) handleStatus(w http.ResponseWriter, _ *http.Request) {
	status := h.Client.Status()
	writeJSON(w, http.StatusOK, statusResponse{
		DownloadRate:  status.DownloadRate,
		UploadRate:    status.UploadRate,
		TotalDone:     status.TotalDone,
		TotalReceived: status.TotalReceived,
		TotalWanted:   status.TotalWanted,
		TransferCount: len(status.Transfers),
		PeerCount:     len(status.Peers),
		ServerCount:   len(status.Servers),
	})
}

func (h *Handler) handleListTransfers(w http.ResponseWriter, _ *http.Request) {
	snapshots := h.Client.TransferSnapshots()
	items := make([]transferResponse, 0, len(snapshots))
	for _, snap := range snapshots {
		items = append(items, newTransferResponse(snap))
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handler) handleAddTransfer(w http.ResponseWriter, r *http.Request) {
	link, err := readTransferLink(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	outDir := strings.TrimSpace(h.DefaultOutDir)
	if outDir == "" {
		outDir = "."
	}
	_, targetPath, err := h.Client.AddLink(link, outDir)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{
		"path": targetPath,
		"link": link,
	})
}

func (h *Handler) handleTransferAction(w http.ResponseWriter, r *http.Request, path string) {
	parts := strings.Split(path, "/")
	if len(parts) != 3 {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	hash, err := protocol.HashFromString(parts[1])
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid hash")
		return
	}
	switch parts[2] {
	case "pause":
		if err := h.Client.PauseTransfer(hash); err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
	case "resume":
		if err := h.Client.ResumeTransfer(hash); err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
	case "delete":
		if err := h.Client.RemoveTransfer(hash, false); err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
	default:
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func readTransferLink(r *http.Request) (string, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return "", err
	}
	raw := strings.TrimSpace(string(body))
	if raw == "" {
		return "", errors.New("empty body")
	}
	if strings.HasPrefix(raw, "{") {
		var req addTransferRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return "", errors.New("invalid json body")
		}
		raw = strings.TrimSpace(req.Link)
		if raw == "" {
			return "", errors.New("link is required")
		}
	}
	if !strings.HasPrefix(strings.ToLower(raw), "ed2k://") {
		return "", errors.New("invalid ed2k link")
	}
	return raw, nil
}

type statusResponse struct {
	DownloadRate  int   `json:"download_rate"`
	UploadRate    int   `json:"upload_rate"`
	TotalDone     int64 `json:"total_done"`
	TotalReceived int64 `json:"total_received"`
	TotalWanted   int64 `json:"total_wanted"`
	TransferCount int   `json:"transfer_count"`
	PeerCount     int   `json:"peer_count"`
	ServerCount   int   `json:"server_count"`
}

type addTransferRequest struct {
	Link string `json:"link"`
}

type transferResponse struct {
	Hash             string  `json:"hash"`
	FileName         string  `json:"file_name"`
	FilePath         string  `json:"file_path"`
	Size             int64   `json:"size"`
	State            string  `json:"state"`
	Paused           bool    `json:"paused"`
	DownloadRate     int     `json:"download_rate"`
	TotalDone        int64   `json:"total_done"`
	TotalReceived    int64   `json:"total_received"`
	TotalWanted      int64   `json:"total_wanted"`
	ActivePeers      int     `json:"active_peers"`
	Priority         string  `json:"priority"`
	PriorityLabel    string  `json:"priority_label"`
	DonePercent      float64 `json:"done_percent"`
	ReceivedPercent  float64 `json:"received_percent"`
}

func newTransferResponse(snap ed2k.TransferSnapshot) transferResponse {
	donePct := 0.0
	recvPct := 0.0
	if snap.Status.TotalWanted > 0 {
		donePct = float64(snap.Status.TotalDone) * 100 / float64(snap.Status.TotalWanted)
		recvPct = float64(snap.Status.TotalReceived) * 100 / float64(snap.Status.TotalWanted)
	}
	return transferResponse{
		Hash:            snap.Hash.String(),
		FileName:        snap.FileName,
		FilePath:        snap.FilePath,
		Size:            snap.Size,
		State:           string(snap.Status.State),
		Paused:          snap.Status.Paused,
		DownloadRate:    snap.Status.DownloadRate,
		TotalDone:       snap.Status.TotalDone,
		TotalReceived:   snap.Status.TotalReceived,
		TotalWanted:     snap.Status.TotalWanted,
		ActivePeers:     snap.ActivePeers,
		Priority:        snap.DownloadPriority.Label(),
		PriorityLabel:   snap.DownloadPriority.TextLabel(),
		DonePercent:     donePct,
		ReceivedPercent: recvPct,
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
