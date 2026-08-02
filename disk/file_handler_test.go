package disk

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDesktopFileHandlerPreallocateSparse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sparse.bin")
	handler := NewDesktopFileHandler(path)

	const size int64 = 1_000_000
	if err := handler.Preallocate(size, true); err != nil {
		t.Fatalf("preallocate sparse: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() != size {
		t.Fatalf("expected size %d, got %d", size, info.Size())
	}
}

func TestDesktopFileHandlerPreallocateFull(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "full.bin")
	handler := NewDesktopFileHandler(path)

	const size int64 = 64 * 1024
	if err := handler.Preallocate(size, false); err != nil {
		t.Fatalf("preallocate full: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() != size {
		t.Fatalf("expected size %d, got %d", size, info.Size())
	}
}
