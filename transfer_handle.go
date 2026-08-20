package goed2k

import (
	"os"
	"sync"

	"github.com/goed2k/core/protocol"
)

type TransferHandle struct {
	ses      *Session
	mu       *sync.Mutex
	transfer *Transfer
}

func NewTransferHandle(s *Session) TransferHandle {
	return TransferHandle{ses: s, mu: &s.mu}
}

func NewTransferHandleWithTransfer(s *Session, t *Transfer) TransferHandle {
	return TransferHandle{ses: s, mu: &s.mu, transfer: t}
}

func (h TransferHandle) IsValid() bool {
	return h.transfer != nil
}

func (h TransferHandle) GetHash() protocol.Hash {
	if h.transfer == nil {
		return protocol.Invalid
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.transfer.GetHash()
}

func (h TransferHandle) GetCreateTime() int64 {
	if h.transfer == nil {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.transfer.GetCreateTime()
}

func (h TransferHandle) GetSize() int64 {
	if h.transfer == nil {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.transfer.Size()
}

func (h TransferHandle) GetFile() *os.File {
	if h.transfer == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.transfer.GetFile()
}

func (h TransferHandle) GetFilePath() string {
	if h.transfer == nil {
		return ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.transfer.GetFilePath()
}

func (h TransferHandle) Pause() {
	if h.transfer == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.transfer.PauseWithDisconnect()
}

func (h TransferHandle) Resume() {
	if h.transfer == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.transfer.ResumeWithState()
}

func (h TransferHandle) IsPaused() bool {
	if h.transfer == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.transfer.IsPaused()
}

func (h TransferHandle) IsResumed() bool {
	return !h.IsPaused()
}

func (h TransferHandle) IsFinished() bool {
	if h.transfer == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.transfer.IsFinished()
}

func (h TransferHandle) GetResumeData() *protocol.TransferResumeData {
	if h.transfer == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.transfer.ResumeData()
}

func (h TransferHandle) SnapshotResumeData() *protocol.TransferResumeData {
	if h.transfer == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.transfer.snapshotResumeData()
}

func (h TransferHandle) GetStatus() TransferStatus {
	if h.transfer == nil {
		return TransferStatus{}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.transfer.GetStatus()
}

func (h TransferHandle) GetPeersInfo() []PeerInfo {
	if h.transfer == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.transfer.GetPeersInfo()
}

func (h TransferHandle) ActiveConnections() int {
	if h.transfer == nil {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.transfer.ActiveConnections()
}

func (h TransferHandle) PieceSnapshots() []PieceSnapshot {
	if h.transfer == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.transfer.PieceSnapshots()
}

func (h TransferHandle) HttpSources() []string {
	if h.transfer == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.transfer.HttpSources()
}

func (h TransferHandle) MarkResumeDataSaved() {
	if h.transfer == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.transfer.MarkResumeDataSaved()
}

func (h TransferHandle) ResumeDirtyGen() uint64 {
	if h.transfer == nil {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.transfer.ResumeDirtyGen()
}

func (h TransferHandle) MarkResumeSavedIfGen(gen uint64) {
	if h.transfer == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.transfer.markResumeSavedIfGen(gen)
}

func (h TransferHandle) NeedResumeDataSave() bool {
	if h.transfer == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.transfer.NeedResumeDataSave()
}

func (h TransferHandle) DownloadPriority() TransferPriority {
	if h.transfer == nil {
		return TransferPriorityNormal
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.transfer.DownloadPriority()
}
