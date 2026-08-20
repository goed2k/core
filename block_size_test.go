package goed2k

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/goed2k/core/data"
	"github.com/goed2k/core/disk"
	"github.com/goed2k/core/protocol"
)

func TestBlockSizeMatchesAICH(t *testing.T) {
	t.Parallel()
	if BlockSize != 180*1024 {
		t.Fatalf("BlockSize=%d, want 184320", BlockSize)
	}
	if BlockSize != int64(AICHBlockSize) {
		t.Fatalf("BlockSize=%d AICHBlockSize=%d", BlockSize, AICHBlockSize)
	}
	if BlocksPerPiece != AICHBlocksPerPiece {
		t.Fatalf("BlocksPerPiece=%d AICHBlocksPerPiece=%d", BlocksPerPiece, AICHBlocksPerPiece)
	}
	if BlocksPerPiece != 53 {
		t.Fatalf("BlocksPerPiece=%d, want 53", BlocksPerPiece)
	}
	if AICHLastBlockSize != 140*1024 {
		t.Fatalf("AICHLastBlockSize=%d, want 143360", AICHLastBlockSize)
	}
	if PieceSize != int64(BlocksPerPiece-1)*BlockSize+AICHLastBlockSize {
		t.Fatalf("piece layout 52*180KiB+140KiB != PieceSize")
	}
}

func TestBlocksInLastPieceFullPieceFile(t *testing.T) {
	t.Parallel()
	if got := blocksInLastPieceForSize(PieceSize); got != BlocksPerPiece {
		t.Fatalf("exact one piece: last blocks=%d, want %d", got, BlocksPerPiece)
	}
	if got := blocksInLastPieceForSize(2 * PieceSize); got != BlocksPerPiece {
		t.Fatalf("exact two pieces: last blocks=%d, want %d", got, BlocksPerPiece)
	}
	if got := blocksInLastPieceForSize(BlockSize + 100); got != 2 {
		t.Fatalf("short file last blocks=%d", got)
	}
}

func TestAICHBlockMapsOneToOneWithDownloadBlock(t *testing.T) {
	t.Parallel()
	for i := 0; i < BlocksPerPiece; i++ {
		block := data.NewPieceBlock(0, i)
		r := block.Range(PieceSize)
		aichBegin := int64(i) * int64(AICHBlockSize)
		aichEnd := aichBegin + int64(AICHBlockSize)
		if aichEnd > PieceSize {
			aichEnd = PieceSize
		}
		if r.Left != aichBegin || r.Right != aichEnd {
			t.Fatalf("block %d range [%d,%d) != AICH [%d,%d)", i, r.Left, r.Right, aichBegin, aichEnd)
		}
	}
}

func TestRemapDownloadedBlocks190To180KeepsFullyCovered(t *testing.T) {
	t.Parallel()
	// 两个相邻 190 KiB 块覆盖 [0, 389120)，足以完整覆盖新块 0 和 1。
	old := []data.PieceBlock{
		data.NewPieceBlock(0, 0),
		data.NewPieceBlock(0, 1),
	}
	got := remapDownloadedBlocks(old, PieceSize*2, legacyDownloadBlockSize190, protocol.BitField{})
	if len(got) != 2 || got[0] != data.NewPieceBlock(0, 0) || got[1] != data.NewPieceBlock(0, 1) {
		t.Fatalf("got %+v", got)
	}
}

func TestRemapDownloadedBlocks190To180DropsPartial(t *testing.T) {
	t.Parallel()
	old := []data.PieceBlock{data.NewPieceBlock(0, 0)}
	got := remapDownloadedBlocks(old, PieceSize, legacyDownloadBlockSize190, protocol.BitField{})
	if len(got) != 1 || got[0] != data.NewPieceBlock(0, 0) {
		t.Fatalf("single 190 KiB block should keep new block 0 only, got %+v", got)
	}
}

func TestRemapAllOldBlocksCoverFullNewPiece(t *testing.T) {
	t.Parallel()
	old := make([]data.PieceBlock, 50)
	for i := range old {
		old[i] = data.NewPieceBlock(0, i)
	}
	got := remapDownloadedBlocks(old, PieceSize, legacyDownloadBlockSize190, protocol.BitField{})
	if len(got) != BlocksPerPiece {
		t.Fatalf("50 old 190 KiB blocks should cover all %d new blocks, got %d", BlocksPerPiece, len(got))
	}
	for i, b := range got {
		if b != data.NewPieceBlock(0, i) {
			t.Fatalf("block %d: %+v", i, b)
		}
	}
}

func TestRemapLastOldBlockKeepsOnlyCoveredNewTail(t *testing.T) {
	t.Parallel()
	// 旧块 49 覆盖 [9533440, 9728000)，完整覆盖新块 52，但不完整覆盖新块 51。
	old := []data.PieceBlock{data.NewPieceBlock(0, 49)}
	got := remapDownloadedBlocks(old, PieceSize, legacyDownloadBlockSize190, protocol.BitField{})
	if len(got) != 1 || got[0] != data.NewPieceBlock(0, 52) {
		t.Fatalf("expected only new block 52, got %+v", got)
	}
}

func TestRemapSkipsCompletedPieceBlocks(t *testing.T) {
	t.Parallel()
	bits := protocol.NewBitField(2)
	bits.SetBit(0)
	old := []data.PieceBlock{
		data.NewPieceBlock(0, 0),
		data.NewPieceBlock(1, 0),
	}
	got := remapDownloadedBlocks(old, PieceSize*2, legacyDownloadBlockSize190, bits)
	if len(got) != 1 || got[0] != data.NewPieceBlock(1, 0) {
		t.Fatalf("completed piece 0 blocks must be dropped, got %+v", got)
	}
}

func TestMigrateV8RemapsDownloadedBlocks(t *testing.T) {
	st := &ClientState{
		Version: 8,
		Transfers: []ClientTransferState{{
			Size: PieceSize,
			ResumeData: &protocol.TransferResumeData{
				DownloadedBlocks: []data.PieceBlock{data.NewPieceBlock(0, 0)},
			},
		}},
	}
	if err := migrateClientState(st); err != nil {
		t.Fatal(err)
	}
	if st.Version != 9 {
		t.Fatalf("version %d", st.Version)
	}
	got := st.Transfers[0].ResumeData.DownloadedBlocks
	if len(got) != 1 || got[0] != data.NewPieceBlock(0, 0) {
		t.Fatalf("remapped %+v", got)
	}
}

func TestMigrateV8KeepsCompletedPieces(t *testing.T) {
	bits := protocol.NewBitField(2)
	bits.SetBit(0)
	st := &ClientState{
		Version: 8,
		Transfers: []ClientTransferState{{
			Size: PieceSize * 2,
			ResumeData: &protocol.TransferResumeData{
				Pieces:           bits,
				DownloadedBlocks: nil,
			},
		}},
	}
	if err := migrateClientState(st); err != nil {
		t.Fatal(err)
	}
	if !st.Transfers[0].ResumeData.Pieces.GetBit(0) || st.Transfers[0].ResumeData.Pieces.GetBit(1) {
		t.Fatalf("completed pieces changed: %+v", st.Transfers[0].ResumeData.Pieces.Bits())
	}
	if len(st.Transfers[0].ResumeData.DownloadedBlocks) != 0 {
		t.Fatalf("empty blocks should stay empty: %+v", st.Transfers[0].ResumeData.DownloadedBlocks)
	}
}

func TestNewTransferExactPieceUsesFullLastPieceBlocks(t *testing.T) {
	t.Parallel()
	session := NewSession(NewSettings())
	transfer, err := NewTransfer(session, AddTransferParams{
		Hash:       protocol.EMule,
		CreateTime: CurrentTimeMillis(),
		Size:       2 * PieceSize,
	})
	if err != nil {
		t.Fatalf("new transfer: %v", err)
	}
	if got := transfer.picker.BlocksInPiece(0); got != BlocksPerPiece {
		t.Fatalf("piece 0 blocks=%d", got)
	}
	if got := transfer.picker.BlocksInPiece(1); got != BlocksPerPiece {
		t.Fatalf("exact two-piece file last piece blocks=%d, want %d", got, BlocksPerPiece)
	}
}

func TestOverlappingDownloadBlocksHalfOpen(t *testing.T) {
	t.Parallel()
	first, last, ok := overlappingDownloadBlocks(0, 0, BlockSize, PieceSize)
	if !ok || first != 0 || last != 0 {
		t.Fatalf("first AICH/block [0,BlockSize) -> [%d,%d] ok=%v", first, last, ok)
	}
	begin := int64(BlocksPerPiece-1) * BlockSize
	first, last, ok = overlappingDownloadBlocks(0, begin, PieceSize, PieceSize)
	if !ok || first != BlocksPerPiece-1 || last != BlocksPerPiece-1 {
		t.Fatalf("last 140KiB -> [%d,%d] ok=%v", first, last, ok)
	}
	first, last, ok = overlappingDownloadBlocks(1, PieceSize, PieceSize+BlockSize, PieceSize)
	if !ok || first != 0 || last != 0 {
		t.Fatalf("piece 1 first block -> [%d,%d] ok=%v", first, last, ok)
	}
}

func TestPieceManagerWritesLastBlockAtFileOffset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "two-piece.bin")
	handler := disk.NewDesktopFileHandler(path)
	t.Cleanup(func() { _ = handler.Close() })
	pm := NewPieceManager(handler, 2, BlocksPerPiece)
	last := data.NewPieceBlock(0, BlocksPerPiece-1)
	firstNext := data.NewPieceBlock(1, 0)
	lastPayload := bytes.Repeat([]byte{0xA1}, last.Size(2*PieceSize))
	nextPayload := bytes.Repeat([]byte{0xB2}, firstNext.Size(2*PieceSize))
	if _, err := pm.WriteBlock(last, lastPayload); err != nil {
		t.Fatalf("write last: %v", err)
	}
	if _, err := pm.WriteBlock(firstNext, nextPayload); err != nil {
		t.Fatalf("write next: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(raw)) < PieceSize+BlockSize {
		t.Fatalf("file len %d", len(raw))
	}
	gotLast := raw[last.FileOffset() : last.FileOffset()+int64(len(lastPayload))]
	if !bytes.Equal(gotLast, lastPayload) {
		t.Fatal("last 140KiB block not written at FileOffset")
	}
	gotNext := raw[firstNext.FileOffset() : firstNext.FileOffset()+int64(len(nextPayload))]
	if !bytes.Equal(gotNext, nextPayload) {
		t.Fatal("piece 1 block 0 not written at PieceSize")
	}
	if last.FileOffset()+int64(len(lastPayload)) != firstNext.FileOffset() {
		t.Fatalf("last block must abut next piece: %d+%d != %d", last.FileOffset(), len(lastPayload), firstNext.FileOffset())
	}
}
