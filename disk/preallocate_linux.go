//go:build linux

package disk

import (
	"os"
	"syscall"
)

func allocateDiskSpace(file *os.File, size int64) error {
	if file == nil || size <= 0 {
		return nil
	}
	_, _, errno := syscall.Syscall6(
		syscall.SYS_FALLOCATE,
		file.Fd(),
		0,
		0,
		uintptr(size),
		0,
		0,
	)
	if errno != 0 {
		return errno
	}
	return nil
}

func setSparseFile(_ *os.File) error {
	return nil
}
