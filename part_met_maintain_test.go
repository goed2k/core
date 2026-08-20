package goed2k

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/goed2k/core/data"
	"github.com/goed2k/core/protocol"
)

func TestWritePartMetAtomicReplacesExisting(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "movie.avi.part.met")
	if err := writePartMetAtomic(target, []byte("eMule")); err != nil {
		t.Fatalf("first write: %v", err)
	}
	info := PartMetInfo{
		Hash:     protocol.EMule,
		FileSize: PieceSize,
		Filename: "movie.avi",
		Resume: &protocol.TransferResumeData{
			Hashes: []protocol.Hash{protocol.EMule},
			Pieces: protocol.NewBitField(1),
		},
	}
	if err := ExportPartMet(filepath.Join(dir, "movie.avi"), info); err != nil {
		t.Fatalf("replace export: %v", err)
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !protocol.IsEmulePartMetBytes(raw) {
		t.Fatalf("expected replaced emule binary, header %#x", raw[0])
	}
	if _, err := os.Stat(target + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("successful write should not leave tmp")
	}
}

func TestRecoverPartMetPromotesValidTmp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "crash.bin")
	info := PartMetInfo{
		Hash:     protocol.EMule,
		FileSize: PieceSize,
		Filename: "crash.bin",
		Resume: &protocol.TransferResumeData{
			Hashes: []protocol.Hash{protocol.EMule},
			Pieces: protocol.NewBitField(1),
			DownloadedBlocks: []data.PieceBlock{
				data.NewPieceBlock(0, 0),
			},
		},
	}
	if err := ExportPartMet(path, info); err != nil {
		t.Fatalf("export: %v", err)
	}
	target := path + ".part.met"
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target+".tmp", raw, 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ImportPartMet(path)
	if err != nil {
		t.Fatalf("import after crash: %v", err)
	}
	if got.Resume == nil || len(got.Resume.DownloadedBlocks) != 1 {
		t.Fatalf("tmp was not promoted: %+v", got.Resume)
	}
	if _, err := os.Stat(target + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("promoted tmp should be gone")
	}
}

func TestRecoverPartMetKeepsGoodFileAndDropsBadTmp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keep.bin")
	info := PartMetInfo{
		Hash:     protocol.EMule,
		FileSize: PieceSize,
		Filename: "keep.bin",
		Resume: &protocol.TransferResumeData{
			Hashes: []protocol.Hash{protocol.EMule},
			Pieces: protocol.NewBitField(1),
		},
	}
	info.Resume.Pieces.SetBit(0)
	if err := ExportPartMet(path, info); err != nil {
		t.Fatalf("export: %v", err)
	}
	target := path + ".part.met"
	if err := os.WriteFile(target+".tmp", []byte("truncated"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ImportPartMet(path)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if got.Resume == nil || !got.Resume.Pieces.GetBit(0) {
		t.Fatalf("good file should win %+v", got.Resume)
	}
	if _, err := os.Stat(target + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("corrupt tmp should be removed")
	}
}

func TestRecoverPartMetDropsCorruptTmpWhenTargetMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gone.bin")
	target := path + ".part.met"
	if err := os.WriteFile(target+".tmp", []byte{0xE0}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ImportPartMet(path); err == nil {
		t.Fatal("missing target and corrupt tmp should fail import")
	}
	if _, err := os.Stat(target + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("corrupt tmp should be deleted")
	}
}

func TestClientFlushPartMetThrottlesAndForceWrites(t *testing.T) {
	dir := t.TempDir()
	settings := NewSettings()
	settings.ListenPort = 0
	client := NewClient(settings)
	registerClientTransferFileCleanup(t, client)
	client.SetPartMetFlushInterval(time.Hour)

	handle, path, err := client.AddLink("ed2k://|file|auto.bin|19456000|31D6CFE0D16AE931B73C59D7E0C089C1|/", dir)
	if err != nil {
		t.Fatalf("add link: %v", err)
	}
	if err := client.FlushPartMet(true); err != nil {
		t.Fatalf("initial flush: %v", err)
	}
	if _, err := os.Stat(path + ".part.met"); err != nil {
		t.Fatalf("add/flush should write part.met: %v", err)
	}

	block := data.NewPieceBlock(0, 0)
	if _, err := handle.transfer.pm.WriteBlock(block, make([]byte, BlockSize)); err != nil {
		t.Fatalf("write block: %v", err)
	}
	handle.transfer.picker.WeHaveBlock(block)
	handle.transfer.needSaveResumeData = true

	now := time.Now()
	if err := client.flushPartMet(now, false); err != nil {
		t.Fatalf("throttled flush: %v", err)
	}
	got, err := ImportPartMet(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Resume != nil && len(got.Resume.DownloadedBlocks) != 0 {
		t.Fatalf("throttle should skip new block, got %+v", got.Resume.DownloadedBlocks)
	}
	if !handle.NeedResumeDataSave() {
		t.Fatal("skipped flush must keep dirty flag")
	}

	if err := client.flushPartMet(now, true); err != nil {
		t.Fatalf("force flush: %v", err)
	}
	got, err = ImportPartMet(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Resume == nil || len(got.Resume.DownloadedBlocks) != 1 || got.Resume.DownloadedBlocks[0] != block {
		t.Fatalf("force flush should keep block: %+v", got.Resume)
	}
	if handle.transfer != nil && handle.transfer.handler != nil {
		_ = handle.transfer.handler.Close()
	}
}

func TestClientStopFlushesDirtyPartMet(t *testing.T) {
	dir := t.TempDir()
	settings := NewSettings()
	settings.ListenPort = 0
	client := NewClient(settings)
	registerClientTransferFileCleanup(t, client)
	client.SetPartMetFlushInterval(time.Hour)

	handle, path, err := client.AddLink("ed2k://|file|stop.bin|19456000|31D6CFE0D16AE931B73C59D7E0C089C2|/", dir)
	if err != nil {
		t.Fatalf("add link: %v", err)
	}
	block := data.NewPieceBlock(1, 0)
	if _, err := handle.transfer.pm.WriteBlock(block, make([]byte, BlockSize)); err != nil {
		t.Fatalf("write block: %v", err)
	}
	handle.transfer.picker.WeHaveBlock(block)
	handle.transfer.needSaveResumeData = true

	if err := client.Stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	got, err := ImportPartMet(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Resume == nil || len(got.Resume.DownloadedBlocks) != 1 || got.Resume.DownloadedBlocks[0] != block {
		t.Fatalf("stop should flush dirty part.met: %+v", got.Resume)
	}
	if handle.transfer != nil && handle.transfer.handler != nil {
		_ = handle.transfer.handler.Close()
	}
}
