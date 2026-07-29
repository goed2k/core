package goed2k

import (
	"fmt"
	"strings"
)

// ParseCategoriesConfig 解析 TUI/CLI 分类配置字符串。
// 格式：name:ext1,ext2:dir;name2:ext:dir2（扩展名可带或不带点）
func ParseCategoriesConfig(raw string) ([]Category, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ";")
	out := make([]Category, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		segments := strings.Split(part, ":")
		if len(segments) != 3 {
			return nil, fmt.Errorf("invalid category segment %q, want name:ext:dir", part)
		}
		name := strings.TrimSpace(segments[0])
		exts := strings.TrimSpace(segments[1])
		dir := strings.TrimSpace(segments[2])
		if name == "" || exts == "" || dir == "" {
			return nil, fmt.Errorf("invalid category segment %q", part)
		}
		out = append(out, Category{
			Name:          name,
			AutoExtension: exts,
			OutputDir:     dir,
		})
	}
	return out, nil
}

// FormatCategoriesConfig 将分类列表编码为配置字符串。
func FormatCategoriesConfig(categories []Category) string {
	if len(categories) == 0 {
		return ""
	}
	parts := make([]string, 0, len(categories))
	for _, cat := range categories {
		if strings.TrimSpace(cat.Name) == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s:%s:%s", cat.Name, cat.AutoExtension, cat.OutputDir))
	}
	return strings.Join(parts, ";")
}

// DefaultIdentityKeyPath 返回 SecIdent 默认密钥路径。
func DefaultIdentityKeyPath() string {
	return "goed2k-identity.pem"
}

// EnsureIdentityKeyForSecIdent 在启用 SecIdent 时确保 identity 路径非空。
func EnsureIdentityKeyForSecIdent(settings *Settings) string {
	if settings == nil || !settings.EnableSecIdent {
		return ""
	}
	if strings.TrimSpace(settings.IdentityKeyPath) == "" {
		settings.IdentityKeyPath = DefaultIdentityKeyPath()
	}
	return settings.IdentityKeyPath
}
