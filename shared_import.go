package goed2k

import (
	"os"
	"path/filepath"
	"strings"
)

func isSkippableSharedFileName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return true
	}
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".tmp"), strings.HasSuffix(lower, ".temp"):
		return true
	case strings.HasSuffix(lower, ".part"), strings.HasSuffix(lower, ".partial"):
		return true
	case strings.HasSuffix(lower, "~"), strings.HasPrefix(lower, ".~"):
		return true
	case strings.HasSuffix(lower, ".download"), strings.HasSuffix(lower, ".crdownload"):
		return true
	case lower == ".ds_store", lower == "thumbs.db":
		return true
	}
	return false
}

func normalizeSharedPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func validateSharedFileOnDisk(sf *SharedFile) bool {
	if sf == nil || sf.Path == "" {
		return false
	}
	fi, err := os.Stat(sf.Path)
	if err != nil || fi.IsDir() {
		return false
	}
	if fi.Size() != sf.FileSize {
		return false
	}
	return true
}

// pathIsUnderRoot 判断 filePath 是否位于 rootDir 目录之下（含根目录的直接子路径，不含 root 自身作为文件路径的歧义）。
// 用于移除共享目录时清理该目录下索引的共享文件。
func pathIsUnderRoot(filePath, rootDir string) bool {
	if filePath == "" || rootDir == "" {
		return false
	}
	f, err := filepath.Abs(filePath)
	if err != nil {
		return false
	}
	r, err := filepath.Abs(rootDir)
	if err != nil {
		return false
	}
	f = filepath.Clean(f)
	r = filepath.Clean(r)
	rel, err := filepath.Rel(r, f)
	if err != nil {
		return false
	}
	if rel == "." {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
