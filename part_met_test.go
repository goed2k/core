package goed2k

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/goed2k/core/data"
	"github.com/goed2k/core/protocol"
)

func TestExportImportEmulePartMetRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "movie.avi")
	hash := protocol.EMule
	resume := &protocol.TransferResumeData{
		Hashes: []protocol.Hash{hash, hash},
		Pieces: protocol.NewBitField(2),
		DownloadedBlocks: []data.PieceBlock{
			data.NewPieceBlock(1, 0),
		},
	}
	resume.Pieces.SetBit(0)

	info := PartMetInfo{
		Hash:     hash,
		FileSize: PieceSize * 2,
		Filename: "movie.avi",
		Resume:   resume,
	}
	if err := ExportPartMet(path, info); err != nil {
		t.Fatalf("export: %v", err)
	}
	raw, err := os.ReadFile(path + ".part.met")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !protocol.IsEmulePartMetBytes(raw) {
		t.Fatal("expected emule binary part.met")
	}
	got, err := ImportPartMet(path)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if !got.Hash.Equal(hash) || got.FileSize != PieceSize*2 {
		t.Fatalf("unexpected info %+v", got)
	}
	if got.Resume == nil || got.Resume.Pieces.Len() != 2 || !got.Resume.Pieces.GetBit(0) {
		t.Fatalf("resume pieces %+v", got.Resume.Pieces.Bits())
	}
	if got.Resume.Pieces.GetBit(1) {
		t.Fatal("部分进度的 piece 1 不得标完成")
	}
	if len(got.Resume.DownloadedBlocks) != 1 || got.Resume.DownloadedBlocks[0] != data.NewPieceBlock(1, 0) {
		t.Fatalf("expected piece 1 block 0, got %+v", got.Resume.DownloadedBlocks)
	}
}

func TestImportGoed2kJSONPartMet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.bin")
	info := PartMetInfo{
		Hash:     protocol.EMule,
		FileSize: PieceSize,
		Resume: &protocol.TransferResumeData{
			Hashes: []protocol.Hash{protocol.EMule},
			Pieces: protocol.NewBitField(1),
		},
	}
	info.Resume.Pieces.SetBit(0)
	if err := exportPartMetJSON(path, info); err != nil {
		t.Fatalf("export json: %v", err)
	}
	// 仅 JSON 旁注时主 .part.met 不存在，直接测 ParsePartMetBytes
	raw, err := os.ReadFile(path + ".part.met.json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParsePartMetBytes(raw)
	if err != nil {
		t.Fatalf("parse json: %v", err)
	}
	if !got.Hash.Equal(protocol.EMule) {
		t.Fatalf("hash %+v", got.Hash)
	}
}

func TestResumeFromGapsKeepsPartialPieceBlocks(t *testing.T) {
	hash := protocol.EMule
	hashes := []protocol.Hash{hash, hash}
	fileSize := PieceSize * 2
	// piece 0 完整；piece 1 仅前两个 BlockSize 已下载，其后为 gap。
	gaps := []protocol.PartMetGap{{
		Start: uint64(PieceSize + 2*BlockSize),
		End:   uint64(fileSize),
	}}
	resume := resumeFromGaps(fileSize, hashes, gaps)
	if resume == nil || resume.Pieces.Len() != 2 {
		t.Fatal("expected 2-piece resume")
	}
	if !resume.Pieces.GetBit(0) {
		t.Fatal("无 gap 的 piece 0 应标记完成")
	}
	if resume.Pieces.GetBit(1) {
		t.Fatal("仍有 gap 的 piece 1 不得标记完成")
	}
	if len(resume.DownloadedBlocks) != 2 {
		t.Fatalf("piece 1 应保留 2 个完整块，got %d %+v", len(resume.DownloadedBlocks), resume.DownloadedBlocks)
	}
	if resume.DownloadedBlocks[0] != data.NewPieceBlock(1, 0) || resume.DownloadedBlocks[1] != data.NewPieceBlock(1, 1) {
		t.Fatalf("unexpected blocks %+v", resume.DownloadedBlocks)
	}
	wantDone := uint64(PieceSize + 2*BlockSize)
	if got := transferredFromResume(fileSize, resume); got != wantDone {
		t.Fatalf("transferred %d want %d", got, wantDone)
	}
	assertResumeBlocksOutsideGaps(t, fileSize, resume, gaps)
}

func TestResumeFromGapsSkipsPartialTrailingBlock(t *testing.T) {
	hash := protocol.EMule
	fileSize := PieceSize
	// 只覆盖第一个块的一半：不能把未完成块算进去。
	gaps := []protocol.PartMetGap{{
		Start: uint64(BlockSize / 2),
		End:   uint64(fileSize),
	}}
	resume := resumeFromGaps(fileSize, []protocol.Hash{hash}, gaps)
	if resume.Pieces.GetBit(0) {
		t.Fatal("半块不得把 piece 标完成")
	}
	if len(resume.DownloadedBlocks) != 0 {
		t.Fatalf("半块不得进入 DownloadedBlocks: %+v", resume.DownloadedBlocks)
	}
}

func TestResumeFromGapsMultipleGapsInOnePiece(t *testing.T) {
	hash := protocol.EMule
	fileSize := PieceSize
	// 块 1 缺失，块 0 与块 2 完整。
	gaps := []protocol.PartMetGap{{
		Start: uint64(BlockSize),
		End:   uint64(2 * BlockSize),
	}}
	resume := resumeFromGaps(fileSize, []protocol.Hash{hash}, gaps)
	if resume.Pieces.GetBit(0) {
		t.Fatal("中间有 gap 的 piece 不得标完成")
	}
	got := map[int]bool{}
	for _, b := range resume.DownloadedBlocks {
		if b.PieceIndex != 0 {
			t.Fatalf("unexpected piece %d", b.PieceIndex)
		}
		got[b.PieceBlock] = true
	}
	if !got[0] || got[1] || !got[2] {
		t.Fatalf("expected blocks 0 and 2 only, got %+v", resume.DownloadedBlocks)
	}
	assertResumeBlocksOutsideGaps(t, fileSize, resume, gaps)
}

func TestExportImportEmulePartMetPreservesPartialBlocks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "movie.avi")
	hash := protocol.EMule
	resume := &protocol.TransferResumeData{
		Hashes: []protocol.Hash{hash, hash},
		Pieces: protocol.NewBitField(2),
		DownloadedBlocks: []data.PieceBlock{
			data.NewPieceBlock(1, 0),
			data.NewPieceBlock(1, 1),
		},
	}
	resume.Pieces.SetBit(0)
	if err := ExportPartMet(path, PartMetInfo{
		Hash:     hash,
		FileSize: PieceSize * 2,
		Filename: "movie.avi",
		Resume:   resume,
	}); err != nil {
		t.Fatalf("export: %v", err)
	}
	got, err := ImportPartMet(path)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if got.Resume == nil || !got.Resume.Pieces.GetBit(0) || got.Resume.Pieces.GetBit(1) {
		t.Fatalf("pieces %+v", got.Resume.Pieces.Bits())
	}
	if len(got.Resume.DownloadedBlocks) != 2 {
		t.Fatalf("partial blocks lost: %+v", got.Resume.DownloadedBlocks)
	}
	if got.Resume.DownloadedBlocks[0] != data.NewPieceBlock(1, 0) || got.Resume.DownloadedBlocks[1] != data.NewPieceBlock(1, 1) {
		t.Fatalf("unexpected blocks %+v", got.Resume.DownloadedBlocks)
	}
	assertResumeBlocksOutsideGaps(t, PieceSize*2, got.Resume, gapsFromResume(PieceSize*2, resume))
}

func TestResumeFromGapsLastShortPiece(t *testing.T) {
	hash := protocol.EMule
	fileSize := PieceSize + BlockSize + 100
	hashes := []protocol.Hash{hash, hash}

	// 尾片最后 50 字节缺失：完整块保留，短尾块丢弃。
	tailGap := []protocol.PartMetGap{{
		Start: uint64(fileSize - 50),
		End:   uint64(fileSize),
	}}
	resume := resumeFromGaps(fileSize, hashes, tailGap)
	if !resume.Pieces.GetBit(0) || resume.Pieces.GetBit(1) {
		t.Fatalf("pieces %+v", resume.Pieces.Bits())
	}
	if len(resume.DownloadedBlocks) != 1 || resume.DownloadedBlocks[0] != data.NewPieceBlock(1, 0) {
		t.Fatalf("expected last-piece full block only, got %+v", resume.DownloadedBlocks)
	}
	if got := transferredFromResume(fileSize, resume); got != uint64(PieceSize+BlockSize) {
		t.Fatalf("transferred %d", got)
	}
	assertResumeBlocksOutsideGaps(t, fileSize, resume, tailGap)

	// 尾片首块缺失、短尾块完整。
	headGap := []protocol.PartMetGap{{
		Start: uint64(PieceSize),
		End:   uint64(PieceSize + BlockSize),
	}}
	resume = resumeFromGaps(fileSize, hashes, headGap)
	if !resume.Pieces.GetBit(0) || resume.Pieces.GetBit(1) {
		t.Fatalf("pieces after head gap %+v", resume.Pieces.Bits())
	}
	if len(resume.DownloadedBlocks) != 1 || resume.DownloadedBlocks[0] != data.NewPieceBlock(1, 1) {
		t.Fatalf("expected last short block only, got %+v", resume.DownloadedBlocks)
	}
	if got := transferredFromResume(fileSize, resume); got != uint64(PieceSize+100) {
		t.Fatalf("short-tail transferred %d", got)
	}
	assertResumeBlocksOutsideGaps(t, fileSize, resume, headGap)
}

func TestResumeFromGapsEmptyOrFullFile(t *testing.T) {
	hash := protocol.EMule
	fileSize := PieceSize * 2
	hashes := []protocol.Hash{hash, hash}

	full := resumeFromGaps(fileSize, hashes, nil)
	if full.Pieces.Len() != 2 || !full.Pieces.GetBit(0) || !full.Pieces.GetBit(1) {
		t.Fatalf("empty gaps should finish all pieces %+v", full.Pieces.Bits())
	}
	if len(full.DownloadedBlocks) != 0 {
		t.Fatalf("complete pieces should not emit blocks %+v", full.DownloadedBlocks)
	}
	if got := transferredFromResume(fileSize, full); got != uint64(fileSize) {
		t.Fatalf("full transferred %d", got)
	}

	empty := resumeFromGaps(fileSize, hashes, []protocol.PartMetGap{{Start: 0, End: uint64(fileSize)}})
	if empty.Pieces.GetBit(0) || empty.Pieces.GetBit(1) {
		t.Fatal("full-file gap must not mark pieces complete")
	}
	if len(empty.DownloadedBlocks) != 0 {
		t.Fatalf("full-file gap must not emit blocks %+v", empty.DownloadedBlocks)
	}
	if got := transferredFromResume(fileSize, empty); got != 0 {
		t.Fatalf("empty transferred %d", got)
	}
}

func assertResumeBlocksOutsideGaps(t *testing.T, fileSize int64, resume *protocol.TransferResumeData, gaps []protocol.PartMetGap) {
	t.Helper()
	if resume == nil {
		t.Fatal("nil resume")
	}
	for _, block := range resume.DownloadedBlocks {
		r := block.Range(fileSize)
		if rangeOverlapsGaps(uint64(r.Left), uint64(r.Right), gaps) {
			t.Fatalf("imported block %+v overlaps gap range [%d,%d)", block, r.Left, r.Right)
		}
	}
}

func TestImportEmuleBinaryPartialGaps(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "from-emule.bin")
	hash := protocol.EMule
	fileSize := PieceSize * 2
	gaps := []protocol.PartMetGap{{
		Start: uint64(PieceSize + BlockSize),
		End:   uint64(fileSize),
	}}
	raw, err := protocol.BuildEmulePartMet(protocol.EmulePartMetOptions{
		Hash:        hash,
		FileSize:    fileSize,
		Filename:    "from-emule.bin",
		Transferred: uint64(PieceSize + BlockSize),
		PieceHashes: []protocol.Hash{hash, hash},
		Gaps:        gaps,
	})
	if err != nil {
		t.Fatalf("build emule binary: %v", err)
	}
	if err := os.WriteFile(path+".part.met", raw, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ImportPartMet(path)
	if err != nil {
		t.Fatalf("import emule binary: %v", err)
	}
	if got.Resume == nil || !got.Resume.Pieces.GetBit(0) || got.Resume.Pieces.GetBit(1) {
		t.Fatalf("pieces %+v", got.Resume.Pieces.Bits())
	}
	if len(got.Resume.DownloadedBlocks) != 1 || got.Resume.DownloadedBlocks[0] != data.NewPieceBlock(1, 0) {
		t.Fatalf("partial gap not preserved: %+v", got.Resume.DownloadedBlocks)
	}
	assertResumeBlocksOutsideGaps(t, fileSize, got.Resume, gaps)
}

func TestParsePartMetBytesRejectsUnknownAndCorrupt(t *testing.T) {
	if _, err := ParsePartMetBytes(nil); err == nil {
		t.Fatal("empty bytes should fail")
	}
	if _, err := ParsePartMetBytes([]byte("not a part.met")); err == nil {
		t.Fatal("unknown text should fail")
	}
	if _, err := ParsePartMetBytes([]byte{0xE0}); err == nil {
		t.Fatal("truncated emule header should fail")
	}
	if _, err := ParsePartMetBytes([]byte(`{"format":"other"}`)); err == nil {
		t.Fatal("unknown json format should fail")
	}
}

func TestGapsFromResumePartialPiece(t *testing.T) {
	resume := &protocol.TransferResumeData{
		Hashes: []protocol.Hash{protocol.EMule},
		Pieces: protocol.NewBitField(1),
		DownloadedBlocks: []data.PieceBlock{
			data.NewPieceBlock(0, 0),
		},
	}
	gaps := gapsFromResume(PieceSize, resume)
	if len(gaps) != 1 {
		t.Fatalf("expected 1 gap, got %d", len(gaps))
	}
	blockEnd := uint64(BlockSize)
	if gaps[0].Start != blockEnd || gaps[0].End != uint64(PieceSize) {
		t.Fatalf("unexpected gaps %+v", gaps)
	}
}
