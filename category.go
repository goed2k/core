package goed2k

import (
	"path/filepath"
	"strings"
)

// Category 按文件扩展名将下载任务路由到指定输出目录。
type Category struct {
	Name          string
	OutputDir     string
	AutoExtension string // 逗号分隔扩展名，如 ".mp4,.mkv" 或 "mp4,mkv"
}

// MatchCategory 根据文件名扩展名查找匹配的分类。
func MatchCategory(categories []Category, filename string) *Category {
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" {
		return nil
	}
	for i := range categories {
		cat := &categories[i]
		for _, rule := range strings.Split(cat.AutoExtension, ",") {
			rule = strings.TrimSpace(strings.ToLower(rule))
			if rule == "" {
				continue
			}
			if !strings.HasPrefix(rule, ".") {
				rule = "." + rule
			}
			if ext == rule {
				return cat
			}
		}
	}
	return nil
}

// ResolveCategoryOutputDir 按扩展名选择输出目录，无匹配时返回 defaultDir。
func ResolveCategoryOutputDir(categories []Category, filename, defaultDir string) string {
	if cat := MatchCategory(categories, filename); cat != nil {
		if dir := strings.TrimSpace(cat.OutputDir); dir != "" {
			return dir
		}
	}
	return defaultDir
}
