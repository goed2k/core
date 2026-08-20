//go:build !linux && !windows

package disk

import "os"

const preallocateKind = PreallocateTruncateOnly

// allocateDiskSpace 在非 Linux/Windows 上是有意的空操作。
// Preallocate 仍会 Truncate 到目标逻辑大小，但不保证预留簇或生成稀疏孔。
func allocateDiskSpace(_ *os.File, _ int64) error {
	return nil
}

// setSparseFile 在非 Linux/Windows 上是有意的空操作，见 PreallocateSemantics。
func setSparseFile(_ *os.File) error {
	return nil
}
