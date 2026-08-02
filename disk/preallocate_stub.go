//go:build !linux

package disk

import "os"

func allocateDiskSpace(_ *os.File, _ int64) error {
	return nil
}

func setSparseFile(_ *os.File) error {
	return nil
}
