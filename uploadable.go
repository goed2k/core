package goed2k

import "github.com/monkeyWie/goed2k/protocol"

// UploadableResource 上传所需的最小能力（Transfer 与 SharedFile 均实现）。
type UploadableResource interface {
	GetHash() protocol.Hash
	FileLabel() string
	Size() int64
	UploadPriority() UploadPriority
	AvailablePieces() protocol.BitField
	UploadHashSet() []protocol.Hash
	CanUpload() bool
	CanUploadRange(begin, end int64) bool
	ReadRange(begin, end int64) ([]byte, error)
}

// FileLabel 用于上传时文件名展示。
func (t *Transfer) FileLabel() string {
	if t == nil {
		return ""
	}
	return t.FileName()
}

var _ UploadableResource = (*Transfer)(nil)
var _ UploadableResource = (*SharedFile)(nil)
