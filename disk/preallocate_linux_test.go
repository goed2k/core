//go:build linux

package disk

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestLinuxFullPreallocateReservesBlocks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "full.bin")
	handler := NewDesktopFileHandler(path)
	defer func() { _ = handler.Close() }()

	const size int64 = 256 * 1024 // 256 KiB，不依赖 >4GB 盘
	if err := handler.Preallocate(size, false); err != nil {
		t.Fatalf("preallocate full: %v", err)
	}
	file := handler.File()
	if file == nil {
		t.Fatal("expected open file")
	}
	var st unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &st); err != nil {
		t.Fatalf("fstat: %v", err)
	}
	allocated := st.Blocks * 512
	if allocated < size {
		t.Fatalf("fallocate reserved %d bytes, want at least %d", allocated, size)
	}
}
