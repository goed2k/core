package disk

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDesktopFileHandlerPreallocateSparse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sparse.bin")
	handler := NewDesktopFileHandler(path)
	defer func() { _ = handler.Close() }()

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
	defer func() { _ = handler.Close() }()

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

func TestPreallocateSemanticsMatchesGOOS(t *testing.T) {
	t.Parallel()
	got := PreallocateSemantics()
	switch runtime.GOOS {
	case "linux":
		if got != PreallocateLinuxFallocate {
			t.Fatalf("linux semantics=%s", got)
		}
	case "windows":
		if got != PreallocateWindowsNTFS {
			t.Fatalf("windows semantics=%s", got)
		}
	default:
		if got != PreallocateTruncateOnly {
			t.Fatalf("%s semantics=%s, want truncate-only", runtime.GOOS, got)
		}
	}
}

func TestDesktopFileHandlerSetPathRequiresClosed(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "001.part")
	dest := filepath.Join(dir, "final.bin")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	handler := NewDesktopFileHandler(src)
	if handler.File() == nil {
		t.Fatal("expected open file")
	}
	if err := handler.SetPath(dest); err == nil {
		t.Fatal("open handler must reject SetPath")
	}
	if err := handler.Close(); err != nil {
		t.Fatal(err)
	}
	if err := handler.SetPath(dest); err != nil {
		t.Fatal(err)
	}
	if handler.Path() != dest {
		t.Fatalf("path %s", handler.Path())
	}
}

func TestPreallocateZeroSizeNoop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.bin")
	handler := NewDesktopFileHandler(path)
	defer func() { _ = handler.Close() }()
	if err := handler.Preallocate(0, false); err != nil {
		t.Fatalf("preallocate 0: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("zero size should not create file, stat err=%v", err)
	}
}
