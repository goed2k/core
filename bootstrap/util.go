package bootstrap

import "strings"

// SplitCommaList splits a comma-separated list and trims empty entries.
func SplitCommaList(value string) []string {
	parts := make([]string, 0, 4)
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			parts = append(parts, item)
		}
	}
	return parts
}
