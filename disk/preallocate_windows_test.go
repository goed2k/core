//go:build windows

package disk

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"golang.org/x/sys/windows"
)

func skipIfVolumeLacksSparse(t *testing.T, path string) {
	t.Helper()
	volume := make([]uint16, windows.MAX_PATH+1)
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatalf("utf16: %v", err)
	}
	if err := windows.GetVolumePathName(name, &volume[0], uint32(len(volume))); err != nil {
		t.Skipf("无法读取卷根路径: %v", err)
	}
	var flags uint32
	if err := windows.GetVolumeInformation(&volume[0], nil, 0, nil, nil, &flags, nil, 0); err != nil {
		t.Skipf("无法读取卷信息: %v", err)
	}
	if flags&windows.FILE_SUPPORTS_SPARSE_FILES == 0 {
		t.Skip("当前卷不支持 NTFS 稀疏文件")
	}
}

func windowsFileSizes(t *testing.T, file *os.File) (alloc, eof int64) {
	t.Helper()
	var buf [64]byte
	err := windows.GetFileInformationByHandleEx(
		windows.Handle(file.Fd()),
		windows.FileStandardInfo,
		&buf[0],
		uint32(len(buf)),
	)
	runtime.KeepAlive(file)
	if err != nil {
		t.Fatalf("GetFileInformationByHandleEx: %v", err)
	}
	alloc = int64(binary.LittleEndian.Uint64(buf[0:8]))
	eof = int64(binary.LittleEndian.Uint64(buf[8:16]))
	return alloc, eof
}

func windowsIsSparse(t *testing.T, path string) bool {
	t.Helper()
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatalf("utf16: %v", err)
	}
	attrs, err := windows.GetFileAttributes(name)
	if err != nil {
		t.Fatalf("GetFileAttributes: %v", err)
	}
	return attrs&windows.FILE_ATTRIBUTE_SPARSE_FILE != 0
}

func TestWindowsSparseSetsAttributeWithoutHugeAlloc(t *testing.T) {
	dir := t.TempDir()
	skipIfVolumeLacksSparse(t, dir)
	path := filepath.Join(dir, "sparse.bin")
	handler := NewDesktopFileHandler(path)
	defer func() { _ = handler.Close() }()

	const size int64 = 1 << 20 // 1 MiB，不依赖 >4GB 盘
	if err := handler.Preallocate(size, true); err != nil {
		t.Fatalf("preallocate sparse: %v", err)
	}
	if !windowsIsSparse(t, path) {
		t.Fatal("expected FILE_ATTRIBUTE_SPARSE_FILE")
	}
	file := handler.File()
	if file == nil {
		t.Fatal("expected open file")
	}
	alloc, eof := windowsFileSizes(t, file)
	if eof != size {
		t.Fatalf("eof=%d want %d", eof, size)
	}
	if alloc >= size {
		t.Fatalf("sparse allocation %d should be well below logical size %d", alloc, size)
	}
}

func TestWindowsFullPreallocateReservesClusters(t *testing.T) {
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
	alloc, eof := windowsFileSizes(t, file)
	if eof != size {
		t.Fatalf("eof=%d want %d", eof, size)
	}
	if alloc < size {
		t.Fatalf("allocation %d should reserve at least %d bytes", alloc, size)
	}
}
