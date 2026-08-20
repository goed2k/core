package data

const (
	pieceSize      int64 = 9728000
	blockSize      int64 = 180 * 1024
	blocksPerPiece       = int((pieceSize + blockSize - 1) / blockSize)
)

type Range struct {
	Left  int64
	Right int64
}

func MakeRange(left, right int64) Range {
	return Range{Left: left, Right: right}
}

type PieceBlock struct {
	PieceIndex int
	PieceBlock int
}

func NewPieceBlock(pieceIndex, pieceBlock int) PieceBlock {
	return PieceBlock{PieceIndex: pieceIndex, PieceBlock: pieceBlock}
}

func (p PieceBlock) BlocksOffset() int64 {
	return int64(p.PieceIndex*blocksPerPiece + p.PieceBlock)
}

// FileOffset 是块在文件中的起始字节。完整分片末块只有 140 KiB，
// 因此不能用 BlocksOffset()*blockSize。
func (p PieceBlock) FileOffset() int64 {
	return int64(p.PieceIndex)*pieceSize + int64(p.PieceBlock)*blockSize
}

func MakePieceBlock(offset int64) PieceBlock {
	piece := int(offset / pieceSize)
	start := int(offset % pieceSize)
	return PieceBlock{
		PieceIndex: piece,
		PieceBlock: int(int64(start) / blockSize),
	}
}

func (p PieceBlock) Range(size int64) Range {
	begin := p.FileOffset()
	pieceEnd := int64(p.PieceIndex+1) * pieceSize
	end := min(begin+blockSize, min(pieceEnd, size))
	if end < begin {
		end = begin
	}
	return MakeRange(begin, end)
}

func (p PieceBlock) Size(totalSize int64) int {
	r := p.Range(totalSize)
	return int(r.Right - r.Left)
}

func (p PieceBlock) Compare(other PieceBlock) int {
	if p.BlocksOffset() < other.BlocksOffset() {
		return -1
	}
	if p.BlocksOffset() > other.BlocksOffset() {
		return 1
	}
	return 0
}

func min(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
