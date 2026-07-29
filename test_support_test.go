package goed2k

import (
	"os"
	"path/filepath"
	"testing"
)

// skipUnlessLiveNetwork 跳过依赖外网 ED2K 服务器的联调测试。
// 本地或 CI 默认不运行；需显式设置 GOED2K_RUN_LIVE_TESTS=1。
// KADV6 IPv6 联调见 session_kadv6_integration_test.go（GOED2K_RUN_KADV6_INTEGRATION=1）。
func skipUnlessLiveNetwork(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("跳过：短测试模式不运行外网联调")
	}
	if os.Getenv("GOED2K_RUN_LIVE_TESTS") != "1" {
		t.Skip("跳过：外网联调测试需设置 GOED2K_RUN_LIVE_TESTS=1")
	}
}

func jed2kServerMetFixturePath(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "jed2k", "core", "src", "main", "resources", "server.met")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("跳过：外部 jed2k fixture 不可用 (%s): %v", path, err)
	}
	return path
}
