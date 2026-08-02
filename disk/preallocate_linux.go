//go:build linux

package disk

import (
	"os"

	"golang.org/x/sys/unix"
)

func allocateDiskSpace(file *os.File, size int64) error {
	if file == nil || size <= 0 {
		return nil
	}
	return unix.Fallocate(int(file.Fd()), 0, 0, size)
}

func setSparseFile(_ *os.File) error {
	return nil
}
