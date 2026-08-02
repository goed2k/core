package goed2k

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAllocEmuleTempPartSlot(t *testing.T) {
	dir := t.TempDir()
	slot, path, err := AllocEmuleTempPartSlot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if slot != 1 || filepath.Base(path) != "001.part" {
		t.Fatalf("unexpected slot %d path %s", slot, path)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	slot2, path2, err := AllocEmuleTempPartSlot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if slot2 != 2 || filepath.Base(path2) != "002.part" {
		t.Fatalf("expected slot 2, got %d %s", slot2, path2)
	}
}

func TestResolveEmuleDownloadPathDefault(t *testing.T) {
	dir := t.TempDir()
	path, cleanup, err := ResolveEmuleDownloadPath(NewSettings(), dir, "movie.avi")
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	if filepath.Base(path) != "movie.avi" {
		t.Fatalf("unexpected path %s", path)
	}
}

func TestResolveEmuleDownloadPathTempLayout(t *testing.T) {
	dir := t.TempDir()
	st := NewSettings()
	st.UseEmuleTempLayout = true
	path, cleanup, err := ResolveEmuleDownloadPath(st, dir, "movie.avi")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if filepath.Base(path) != "001.part" {
		t.Fatalf("expected 001.part, got %s", path)
	}
}
