package data

import "testing"

func TestBlocksPerPieceIsFiftyThree(t *testing.T) {
	t.Parallel()
	if blocksPerPiece != 53 {
		t.Fatalf("blocksPerPiece=%d, want 53", blocksPerPiece)
	}
	if 52*blockSize+143360 != pieceSize {
		t.Fatalf("last full-piece block must be 140 KiB: 52*%d+143360=%d piece=%d", blockSize, 52*blockSize+143360, pieceSize)
	}
}

func TestFileOffsetNotBlocksOffsetTimesBlockSize(t *testing.T) {
	t.Parallel()
	b := NewPieceBlock(1, 0)
	if b.FileOffset() != pieceSize {
		t.Fatalf("piece 1 block 0 FileOffset=%d, want %d", b.FileOffset(), pieceSize)
	}
	if b.BlocksOffset()*blockSize == b.FileOffset() {
		t.Fatal("BlocksOffset*blockSize must not equal FileOffset after 180 KiB (short last block)")
	}
}

func TestLastBlockOfFullPieceIsShort(t *testing.T) {
	t.Parallel()
	b := NewPieceBlock(0, 52)
	r := b.Range(pieceSize)
	if got := r.Right - r.Left; got != 143360 {
		t.Fatalf("last block size=%d, want 143360", got)
	}
	if r.Left != 52*blockSize {
		t.Fatalf("last block start=%d", r.Left)
	}
	if r.Right != pieceSize {
		t.Fatalf("last block must clamp to piece end, got %d", r.Right)
	}
}

func TestLastBlockDoesNotCrossIntoNextPiece(t *testing.T) {
	t.Parallel()
	b := NewPieceBlock(0, 52)
	r := b.Range(2 * pieceSize)
	if r.Right != pieceSize {
		t.Fatalf("last block of piece 0 must end at %d, got %d", pieceSize, r.Right)
	}
}

func TestMakePieceBlockLastBytes(t *testing.T) {
	t.Parallel()
	got := MakePieceBlock(pieceSize - 1)
	if got.PieceIndex != 0 || got.PieceBlock != 52 {
		t.Fatalf("last byte mapped to %+v", got)
	}
	got = MakePieceBlock(pieceSize)
	if got.PieceIndex != 1 || got.PieceBlock != 0 {
		t.Fatalf("first byte of piece 1 mapped to %+v", got)
	}
}

func TestMakePeerRequestFromLastBlock(t *testing.T) {
	t.Parallel()
	block := NewPieceBlock(0, 52)
	req, err := MakePeerRequestFromBlock(block, 2*pieceSize)
	if err != nil {
		t.Fatal(err)
	}
	if req.Piece != 0 || req.Start != 52*blockSize || req.Length != 143360 {
		t.Fatalf("request %+v", req)
	}
}
