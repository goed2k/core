package goed2k

import (
	"path"
	"runtime"
	"strings"
	"unicode/utf8"
)

// downloadFilenameMaxBytes 是落盘文件名（单层分量）的上限。
// NTFS 分量与常见 Unix NAME_MAX 均为 255 字节量级；超长名截断，避免创建失败。
const downloadFilenameMaxBytes = 255

var windowsReservedDeviceNames = map[string]struct{}{
	"CON": {}, "PRN": {}, "AUX": {}, "NUL": {},
	"COM0": {}, "COM1": {}, "COM2": {}, "COM3": {}, "COM4": {},
	"COM5": {}, "COM6": {}, "COM7": {}, "COM8": {}, "COM9": {},
	"LPT0": {}, "LPT1": {}, "LPT2": {}, "LPT3": {}, "LPT4": {},
	"LPT5": {}, "LPT6": {}, "LPT7": {}, "LPT8": {}, "LPT9": {},
}

// SanitizeDownloadFilename 将 ED2K 链接中的文件名清洗为可安全 filepath.Join 的单层名字。
//
// 所有平台都会去掉路径分隔与 ".." 穿越，只保留最后一层分量。
// 仅在 Windows 上替换非法字符 <>:"|?*、控制字符、保留设备名，并去掉尾随点/空格。
// Unix 上合法字符（如冒号、竖线）保持不变，避免过度改名。
func SanitizeDownloadFilename(name string) string {
	return sanitizeDownloadFilename(name, runtime.GOOS)
}

func sanitizeDownloadFilename(name string, goos string) string {
	name = strings.ReplaceAll(name, "\x00", "")
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(name)
	if isPlaceholderDownloadFilename(name) {
		return "_"
	}
	if goos == "windows" {
		name = sanitizeWindowsFilename(name)
	}
	name = truncateDownloadFilename(name, downloadFilenameMaxBytes)
	if goos == "windows" {
		// 截断/去尾后可能重新变成保留设备名或空名，必须再清洗一次。
		name = sanitizeWindowsFilename(name)
	}
	if isPlaceholderDownloadFilename(name) {
		return "_"
	}
	return name
}

func isPlaceholderDownloadFilename(name string) bool {
	return name == "" || name == "." || name == ".." || name == "/" || name == "\\"
}

func sanitizeWindowsFilename(name string) string {
	name = strings.Map(replaceWindowsIllegalRune, name)
	name = strings.TrimRight(name, " .")
	if name == "" || name == "." || name == ".." {
		return "_"
	}
	stem, ext := splitFilenameStem(name)
	if _, reserved := windowsReservedDeviceNames[strings.ToUpper(stem)]; reserved {
		name = stem + "_" + ext
	}
	if name == "" {
		return "_"
	}
	return name
}

func replaceWindowsIllegalRune(r rune) rune {
	if r < 32 || strings.ContainsRune(`<>:"|?*`, r) {
		return '_'
	}
	return r
}

func splitFilenameStem(name string) (stem, ext string) {
	i := strings.IndexByte(name, '.')
	if i < 0 {
		return name, ""
	}
	return name[:i], name[i:]
}

func truncateDownloadFilename(name string, maxBytes int) string {
	if maxBytes <= 0 || len(name) <= maxBytes {
		return name
	}
	ext := ""
	if i := strings.LastIndexByte(name, '.'); i > 0 && i < len(name)-1 {
		ext = name[i:]
		if len(ext) >= maxBytes {
			return cutUTF8(name, maxBytes)
		}
		name = name[:i]
	}
	return cutUTF8(name, maxBytes-len(ext)) + ext
}

func cutUTF8(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	s = s[:maxBytes]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
