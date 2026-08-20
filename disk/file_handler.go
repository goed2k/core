package disk

import (
	"errors"
	"os"
	"sync"
)

type FileHandler interface {
	File() *os.File
	Path() string
	Close() error
	DeleteFile() error
	// Preallocate 将文件扩展到逻辑大小 size。
	// sparse 为 true 时尽量标记稀疏后再 Truncate；为 false 时尽量占盘。
	// 真实能力见 PreallocateSemantics：非 Linux/Windows 仅 Truncate，不保证稀疏或占盘。
	Preallocate(size int64, sparse bool) error
}

type DesktopFileHandler struct {
	path   string
	file   *os.File
	sealed bool
	mu     sync.Mutex
}

func NewDesktopFileHandler(path string) *DesktopFileHandler {
	return &DesktopFileHandler{path: path}
}

func (h *DesktopFileHandler) ensureFile(flag int) (*os.File, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.file != nil {
		return h.file, nil
	}
	if h.sealed {
		return nil, errors.New("file handler is sealed")
	}
	f, err := os.OpenFile(h.path, flag, 0o644)
	if err != nil {
		return nil, err
	}
	h.file = f
	return h.file, nil
}

func (h *DesktopFileHandler) File() *os.File {
	f, _ := h.ensureFile(os.O_RDWR | os.O_CREATE)
	return f
}

func (h *DesktopFileHandler) Path() string {
	return h.path
}

func (h *DesktopFileHandler) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.file == nil {
		return nil
	}
	err := h.file.Close()
	h.file = nil
	return err
}

// Seal 禁止 File() 在关闭后按旧路径 O_CREATE 重建（完成搬运窗口）。
func (h *DesktopFileHandler) Seal() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sealed = true
}

// Unseal 允许再次按当前 path 打开。
func (h *DesktopFileHandler) Unseal() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sealed = false
}

// SetPath 在文件已关闭时改写落盘路径（完成后重命名 NNN.part 用），并解除 seal。
func (h *DesktopFileHandler) SetPath(path string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.file != nil {
		return errors.New("cannot set path while file is open")
	}
	h.path = path
	h.sealed = false
	return nil
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
	// 先标记稀疏或尝试占盘，再保证逻辑大小。占盘/稀疏失败时仍 Truncate，
	// 这样下载文件一定有正确 Size，调用方能区分“仅扩逻辑大小”和“预留失败”。
	var extraErr error
	if sparse {
		extraErr = setSparseFile(file)
	} else {
		extraErr = allocateDiskSpace(file, size)
	}
	if err := file.Truncate(size); err != nil {
		return err
	}
	return extraErr
}
