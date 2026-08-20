package goed2k

const (
	PieceSize int64 = 9728000
	// BlockSize 对齐 eMule EMBLOCKSIZE / aMule BLOCKSIZE（180 KiB）。
	// 完整 9.28MiB 分片为 53 块（52×180KiB + 末块 140KiB）；磁盘偏移必须用 FileOffset，
	// 不能用 BlocksOffset*BlockSize。
	BlockSize        int64 = 180 * 1024
	BlockSizeInt           = int(BlockSize)
	BlocksPerPiece         = int((PieceSize + BlockSize - 1) / BlockSize)
	HighestLowIDED2K int64 = 16777216
	RequestQueueSize       = 3
	PartsInRequest         = 3
)

// blocksInLastPieceForSize 返回最后一片的块数。
// 文件长度正好是整片倍数时，最后一片仍是完整 53 块，不能把余数 0 当成 1 块。
func blocksInLastPieceForSize(size int64) int {
	if size <= 0 {
		return 1
	}
	rem := size % PieceSize
	if rem == 0 {
		return BlocksPerPiece
	}
	n := int(DivCeil(rem, BlockSize))
	if n < 1 {
		return 1
	}
	return n
}
