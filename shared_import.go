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
