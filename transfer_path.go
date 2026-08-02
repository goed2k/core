package goed2k

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

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
// UseEmuleTempLayout 为 true 时使用 NNN.part；否则为 outDir/filename。
func ResolveEmuleDownloadPath(settings Settings, outDir, filename string) (path string, cleanup func(), err error) {
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
