package goed2k

import (
	"io"
	"os"
	"path/filepath"

	"github.com/monkeyWie/goed2k/data"
	"github.com/monkeyWie/goed2k/protocol"
)

// SharedOrigin 表示共享文件来源：下载完成入库或本地导入。
type SharedOrigin int

const (
	SharedOriginDownloaded SharedOrigin = iota
	SharedOriginImported
)

// SharedFile 表示可共享的文件资源元数据（与下载任务 Transfer 分离）。
// FileSize 为字节大小（与 Transfer.Size() 对应，避免与 Size() 方法同名冲突）。
type SharedFile struct {
	Hash        protocol.Hash
	FileSize    int64
	Path        string
	Name        string
	PieceHashes []protocol.Hash
	Origin      SharedOrigin
	Completed   bool
	LastHashAt  int64
}

// FileLabel 用于协议中的展示名（通常为文件名）。
func (s *SharedFile) FileLabel() string {
	if s == nil {
		return ""
	}
	if s.Name != "" {
		return s.Name
	}
	return filepath.Base(s.Path)
}

// GetHash 返回 ed2k 根哈希。
func (s *SharedFile) GetHash() protocol.Hash {
	if s == nil {
		return protocol.Invalid
	}
	return s.Hash
}

// Size 返回文件大小。
func (s *SharedFile) Size() int64 {
	if s == nil {
		return 0
	}
	return s.FileSize
}

// UploadPriority 导入文件默认普通优先级。
func (s *SharedFile) UploadPriority() UploadPriority {
	return UploadPriorityNormal
}

// AvailablePieces 已完成文件视为拥有全部分片。
func (s *SharedFile) AvailablePieces() protocol.BitField {
	if s == nil || !s.Completed || s.FileSize <= 0 {
		return protocol.NewBitField(0)
	}
	n := int(DivCeil(s.FileSize, PieceSize))
	bits := protocol.NewBitField(n)
	for i := 0; i < n; i++ {
		bits.SetBit(i)
	}
	return bits
}

// UploadHashSet 返回分片哈希列表；与 Transfer 行为一致。
func (s *SharedFile) UploadHashSet() []protocol.Hash {
	if s == nil {
		return nil
	}
	if len(s.PieceHashes) > 0 {
		out := make([]protocol.Hash, len(s.PieceHashes))
		copy(out, s.PieceHashes)
		return out
	}
	if s.FileSize <= PieceSize {
		return []protocol.Hash{s.Hash}
	}
	return nil
}

// CanUpload 是否可向其他 peer 上传数据。
func (s *SharedFile) CanUpload() bool {
	return s != nil && s.Completed && s.Path != "" && s.FileSize > 0
}

// CanUploadRange 检查请求区间是否完全落在已拥有分片内。
func (s *SharedFile) CanUploadRange(begin, end int64) bool {
	if !s.CanUpload() || end <= begin || begin < 0 || end > s.FileSize {
		return false
	}
	reqs, err := data.MakePeerRequests(begin, end, s.FileSize)
	if err != nil {
		return false
	}
	bits := s.AvailablePieces()
	for _, req := range reqs {
		if !bits.GetBit(req.Piece) {
			return false
		}
	}
	return true
}

// ReadRange 从本地路径读取区间数据。
func (s *SharedFile) ReadRange(begin, end int64) ([]byte, error) {
	if s == nil || !s.CanUpload() {
		return nil, NewError(NoTransfer)
	}
	if end <= begin || begin < 0 || end > s.FileSize {
		return nil, NewError(IllegalArgument)
	}
	f, err := os.Open(s.Path)
	if err != nil {
		return nil, NewError(IOException)
	}
	defer f.Close()
	if _, err := f.Seek(begin, io.SeekStart); err != nil {
		return nil, NewError(IOException)
	}
	buf := make([]byte, end-begin)
	if _, err := io.ReadFull(f, buf); err != nil {
		return nil, NewError(IOException)
	}
	return buf, nil
}
