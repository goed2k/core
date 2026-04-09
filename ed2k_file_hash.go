package goed2k

import (
	"io"
	"os"

	"github.com/monkeyWie/goed2k/protocol"
)

// ComputeEd2kFileMeta 从本地文件计算 ed2k 根哈希、大小与分片哈希列表（与 eMule 分片规则一致）。
func ComputeEd2kFileMeta(path string) (root protocol.Hash, size int64, pieceHashes []protocol.Hash, err error) {
	f, err := os.Open(path)
	if err != nil {
		return protocol.Invalid, 0, nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return protocol.Invalid, 0, nil, err
	}
	size = fi.Size()
	if size == 0 {
		return protocol.Invalid, 0, nil, os.ErrInvalid
	}
	numPieces := int(DivCeil(size, PieceSize))
	pieceHashes = make([]protocol.Hash, 0, numPieces)
	buf := make([]byte, PieceSize)
	for i := 0; i < numPieces; i++ {
		remain := size - int64(i)*PieceSize
		n := PieceSize
		if remain < PieceSize {
			n = remain
		}
		ni := int(n)
		if _, err := io.ReadFull(f, buf[:ni]); err != nil {
			return protocol.Invalid, 0, nil, err
		}
		h, err := protocol.HashFromData(buf[:ni])
		if err != nil {
			return protocol.Invalid, 0, nil, err
		}
		pieceHashes = append(pieceHashes, h)
	}
	root = protocol.HashFromHashSet(pieceHashes)
	return root, size, pieceHashes, nil
}
