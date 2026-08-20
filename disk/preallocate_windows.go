//go:build windows

package disk

import (
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

const preallocateKind = PreallocateWindowsNTFS

type fileAllocationInfo struct {
	AllocationSize int64
}

func allocateDiskSpace(file *os.File, size int64) error {
	if file == nil || size <= 0 {
		return nil
	}
	info := fileAllocationInfo{AllocationSize: size}
	err := windows.SetFileInformationByHandle(
		windows.Handle(file.Fd()),
		windows.FileAllocationInfo,
		(*byte)(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	)
	runtime.KeepAlive(file)
	return err
}

func setSparseFile(file *os.File) error {
	if file == nil {
		return nil
	}
	var bytesReturned uint32
	err := windows.DeviceIoControl(
		windows.Handle(file.Fd()),
		windows.FSCTL_SET_SPARSE,
		nil,
		0,
		nil,
		0,
		&bytesReturned,
		nil,
	)
	runtime.KeepAlive(file)
	return err
}
