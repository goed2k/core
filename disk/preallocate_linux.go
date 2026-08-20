//go:build linux

package disk

import (
	"os"

	"golang.org/x/sys/unix"
)

const preallocateKind = PreallocateLinuxFallocate

func allocateDiskSpace(file *os.File, size int64) error {
	if file == nil || size <= 0 {
		return nil
	}
	return unix.Fallocate(int(file.Fd()), 0, 0, size)
}

func setSparseFile(_ *os.File) error {
	// Linux 上 Truncate 扩展的新区域通常是空洞，无需额外 ioctl。
	return nil
}
