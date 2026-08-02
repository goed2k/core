package disk

import (
	"errors"
	"os"
)

type FileHandler interface {
	File() *os.File
	Path() string
	Close() error
	DeleteFile() error
	// Preallocate 将文件扩展到 size。sparse 为 true 时仅扩展逻辑大小（稀疏文件），
	// 为 false 时尽量在磁盘上预分配连续空间。
	Preallocate(size int64, sparse bool) error
}

type DesktopFileHandler struct {
	path string
	file *os.File
}

func NewDesktopFileHandler(path string) *DesktopFileHandler {
	return &DesktopFileHandler{path: path}
}

func (h *DesktopFileHandler) ensureFile(flag int) (*os.File, error) {
	if h.file != nil {
		return h.file, nil
	}
	f, err := os.OpenFile(h.path, flag, 0o644)
	if err != nil {
		return nil, err
	}
	h.file = f
	return h.file, nil
}

func (h *DesktopFileHandler) File() *os.File {
	if h.file != nil {
		return h.file
	}
	f, _ := h.ensureFile(os.O_RDWR | os.O_CREATE)
	return f
}

func (h *DesktopFileHandler) Path() string {
	return h.path
}

func (h *DesktopFileHandler) Close() error {
	if h.file == nil {
		return nil
	}
	err := h.file.Close()
	h.file = nil
	return err
}

func (h *DesktopFileHandler) DeleteFile() error {
	_ = h.Close()
	if err := os.Remove(h.path); err != nil {
		return errors.New("unable to delete file")
	}
	return nil
}

func (h *DesktopFileHandler) Preallocate(size int64, sparse bool) error {
	if size <= 0 {
		return nil
	}
	file, err := h.ensureFile(os.O_RDWR | os.O_CREATE)
	if err != nil {
		return err
	}
	if err := file.Truncate(size); err != nil {
		return err
	}
	if sparse {
		return setSparseFile(file)
	}
	return allocateDiskSpace(file, size)
}
