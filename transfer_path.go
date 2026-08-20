package goed2k

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var emuleTempPartName = regexp.MustCompile(`(?i)^\d{3}\.part$`)

const emuleTempPartExt = ".part"

// EmuleTempPartPath 返回 eMule/aMule Temp 目录下的 NNN.part 路径（slot 为 1..999）。
func EmuleTempPartPath(tempDir string, slot int) string {
	return filepath.Join(tempDir, fmt.Sprintf("%03d%s", slot, emuleTempPartExt))
}

// AllocEmuleTempPartSlot 在 tempDir 中分配最小可用三位编号槽位。
func AllocEmuleTempPartSlot(tempDir string) (slot int, partPath string, err error) {
	if tempDir == "" {
		return 0, "", fmt.Errorf("temp dir is empty")
	}
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return 0, "", err
	}
	for slot := 1; slot <= 999; slot++ {
		partPath = EmuleTempPartPath(tempDir, slot)
		if _, err := os.Stat(partPath); os.IsNotExist(err) {
			return slot, partPath, nil
		}
	}
	return 0, "", fmt.Errorf("no free emule temp slot in %s", tempDir)
}

// ResolveEmuleDownloadPath 根据设置决定下载数据文件路径。
// UseEmuleTempLayout 为 true 时使用 NNN.part；否则为 outDir/清洗后的 filename。
// ED2K 链接文件名在 filepath.Join 之前经过 SanitizeDownloadFilename。
func ResolveEmuleDownloadPath(settings Settings, outDir, filename string) (path string, cleanup func(), err error) {
	filename = SanitizeDownloadFilename(filename)
	if !settings.UseEmuleTempLayout {
		return filepath.Join(outDir, filename), func() {}, nil
	}
	slot, partPath, err := AllocEmuleTempPartSlot(outDir)
	if err != nil {
		return "", nil, err
	}
	_ = slot
	return partPath, func() {
		_ = os.Remove(partPath)
		_ = os.Remove(partPath + ".met")
		_ = os.Remove(partPath + ".met.json")
	}, nil
}

// ImportEmulePartMetFromSlot 从 eMule 风格 NNN.part 旁注导入（若存在）。
func ImportEmulePartMetFromSlot(partPath string) (PartMetInfo, error) {
	slotName := filepath.Base(partPath)
	if !strings.HasSuffix(strings.ToLower(slotName), emuleTempPartExt) {
		return PartMetInfo{}, fmt.Errorf("not an emule part file: %s", partPath)
	}
	base := strings.TrimSuffix(partPath, filepath.Ext(partPath))
	if n, err := strconv.Atoi(filepath.Base(base)); err == nil && n > 0 {
		_ = n
	}
	return ImportPartMet(partPath)
}

func isEmuleTempPartPath(path string) bool {
	if path == "" {
		return false
	}
	return emuleTempPartName.MatchString(filepath.Base(path))
}

func uniqueCompletedPath(dir, name string) string {
	dest := filepath.Join(dir, name)
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		return dest
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 1; i < 1000; i++ {
		cand := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, i, ext))
		if _, err := os.Stat(cand); os.IsNotExist(err) {
			return cand
		}
	}
	return dest + ".new"
}

func removeEmulePartSidecars(partPath string) {
	for _, suffix := range []string{".met", ".met.json", ".part.met", ".part.met.json"} {
		_ = os.Remove(partPath + suffix)
	}
}

func copyFileReplace(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dest + ".tmp." + strconv.Itoa(os.Getpid())
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(tmp)
		if copyErr != nil {
			return copyErr
		}
		if syncErr != nil {
			return syncErr
		}
		return closeErr
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Remove(src)
}

// promoteEmulePartFile 把已完成的 NNN.part 搬到 destDir/清洗后的文件名。
// dest 已存在且大小匹配时视为崩溃重试成功，删除残留临时文件。
// 同卷优先 Rename；跨卷回退为复制后删除源。
func promoteEmulePartFile(src, destDir, filename string, fileSize int64) (string, error) {
	if !isEmuleTempPartPath(src) {
		return src, fmt.Errorf("not an emule temp part: %s", src)
	}
	filename = SanitizeDownloadFilename(filename)
	if filename == "" || filename == "." || filename == ".." {
		return "", fmt.Errorf("empty final name")
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(destDir, filename)
	if same, err := sameFilePath(src, dest); err == nil && same {
		removeEmulePartSidecars(src)
		return dest, nil
	}
	if info, err := os.Stat(dest); err == nil {
		if fileSize > 0 && info.Size() == fileSize {
			_ = os.Remove(src)
			removeEmulePartSidecars(src)
			return dest, nil
		}
		dest = uniqueCompletedPath(destDir, filename)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if err := os.Rename(src, dest); err != nil {
		if err := copyFileReplace(src, dest); err != nil {
			return "", err
		}
	}
	removeEmulePartSidecars(src)
	return dest, nil
}

func sameFilePath(a, b string) (bool, error) {
	absA, err := filepath.Abs(a)
	if err != nil {
		return false, err
	}
	absB, err := filepath.Abs(b)
	if err != nil {
		return false, err
	}
	return filepath.Clean(absA) == filepath.Clean(absB), nil
}
