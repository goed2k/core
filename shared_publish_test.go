package goed2k

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCollectPublishableOfferFilesIncludesImportedShared(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "a.bin")
	if err := os.WriteFile(path, []byte("hello-shared-publish"), 0o644); err != nil {
		t.Fatal(err)
	}
	st := NewSettings()
	s := NewSession(st)
	root, size, pieces, err := ComputeEd2kFileMeta(path)
	if err != nil {
		t.Fatal(err)
	}
	s.sharedStore.Add(&SharedFile{
		Hash:        root,
		FileSize:    size,
		Path:        path,
		Name:        "a.bin",
		PieceHashes: pieces,
		Origin:      SharedOriginImported,
		Completed:   true,
		LastHashAt:  CurrentTime(),
	})
	offers := s.collectPublishableOfferFiles()
	if len(offers) != 1 {
		t.Fatalf("expected 1 offer, got %d", len(offers))
	}
	if !offers[0].Hash.Equal(root) || offers[0].Size != size {
		t.Fatalf("unexpected offer: %+v", offers[0])
	}
}

func TestCollectPublishableOfferFilesDedupesSameHash(t *testing.T) {
	tmp := t.TempDir()
	p1 := filepath.Join(tmp, "x.bin")
	if err := os.WriteFile(p1, []byte("dup"), 0o644); err != nil {
		t.Fatal(err)
	}
	root, size, pieces, err := ComputeEd2kFileMeta(p1)
	if err != nil {
		t.Fatal(err)
	}
	s := NewSession(NewSettings())
	s.sharedStore.Add(&SharedFile{
		Hash: root, FileSize: size, Path: p1, Name: "x.bin",
		PieceHashes: pieces, Origin: SharedOriginImported, Completed: true,
	})
	p2 := filepath.Join(tmp, "y.bin")
	if err := os.WriteFile(p2, []byte("dup"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 同内容同 hash，第二条 Add 失败，仅一条
	s.sharedStore.Add(&SharedFile{
		Hash: root, FileSize: size, Path: p2, Name: "y.bin",
		PieceHashes: pieces, Origin: SharedOriginImported, Completed: true,
	})
	if s.sharedStore.Len() != 1 {
		t.Fatalf("store len=%d", s.sharedStore.Len())
	}
	offers := s.collectPublishableOfferFiles()
	if len(offers) != 1 {
		t.Fatalf("offers=%d", len(offers))
	}
}
