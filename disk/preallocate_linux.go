//go:build linux

package disk

import (
	"errors"
	"os"
	"syscall"
)

func allocateDiskSpace(file *os.File, size int64) error {
	if file == nil || size <= 0 {
		return nil
	}
	length := uint64(size)
	if length > uint64(^uintptr(0)) {
		return errors.New("preallocate size exceeds uintptr range")
	}
	_, _, errno := syscall.Syscall6(
		syscall.SYS_FALLOCATE,
		file.Fd(),
		0,
		0,
		uintptr(length),
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
