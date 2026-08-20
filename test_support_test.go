package goed2k

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("GOED2K_RUN_KADV6_INTEGRATION") != "1" {
		localOutboundIPv6Detect = func() net.IP { return nil }
	}
	os.Exit(m.Run())
}

// skipUnlessLiveNetwork 跳过依赖外网 ED2K 服务器的联调测试。
// 本地或 CI 默认不运行；需显式设置 GOED2K_RUN_LIVE_TESTS=1。
// KADV6 发布/合并单测不依赖本机公网 IPv6，见 session_kadv6_*_test.go。
// 可选真实出站探测：GOED2K_RUN_KADV6_INTEGRATION=1 go test -run KADV6PublishSearchPipelineLive。
func skipUnlessLiveNetwork(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("跳过：短测试模式不运行外网联调")
	}
	if os.Getenv("GOED2K_RUN_LIVE_TESTS") != "1" {
		t.Skip("跳过：外网联调测试需设置 GOED2K_RUN_LIVE_TESTS=1")
	}
}

// skipUnlessKADV6Integration 跳过需要本机 IPv6 出站探测的可选联调。
func skipUnlessKADV6Integration(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("跳过：短测试模式不运行 KADV6 联调")
	}
	if os.Getenv("GOED2K_RUN_KADV6_INTEGRATION") != "1" {
		t.Skip("跳过：KADV6 联调测试需设置 GOED2K_RUN_KADV6_INTEGRATION=1")
	}
	if probeLocalOutboundIPv6() == nil {
		t.Skip("跳过：本机无可用 IPv6 出站地址")
	}
}

func jed2kServerMetFixturePath(t *testing.T) string {
	t.Helper()
	path := filepath.Join("testdata", "server.met")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("跳过：内嵌 server.met fixture 不可用 (%s): %v", path, err)
	}
	return path
}

// registerTransferFileCleanup 在测试结束时关闭传输任务占用的文件句柄（Windows 上 TempDir 清理需要）。
func registerTransferFileCleanup(t *testing.T, handles ...TransferHandle) {
	t.Helper()
	t.Cleanup(func() {
		for _, handle := range handles {
			if !handle.IsValid() || handle.transfer == nil || handle.transfer.handler == nil {
				continue
			}
			_ = handle.transfer.handler.Close()
		}
	})
}

// registerClientTransferFileCleanup 关闭 Client 上所有传输任务的文件句柄。
func registerClientTransferFileCleanup(t *testing.T, clients ...*Client) {
	t.Helper()
	t.Cleanup(func() {
		for _, client := range clients {
			if client == nil || client.session == nil {
				continue
			}
			for _, handle := range client.session.GetTransfers() {
				if !handle.IsValid() || handle.transfer == nil || handle.transfer.handler == nil {
					continue
				}
				_ = handle.transfer.handler.Close()
			}
		}
	})
}
