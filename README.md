# goed2k

[![CI](https://github.com/goed2k/core/actions/workflows/ci.yml/badge.svg)](https://github.com/goed2k/core/actions/workflows/ci.yml)

`goed2k` 是一个用 Go 编写的 ED2K/eMule 客户端库，附带一个可交互的终端下载管理器。

![Demo](.docs/demo.gif)

## AI 参与开发

这个项目主要由 AI 辅助完成，使用的工具包括：

- `Codex`
- `ChatGPT 5.4`

实现过程中主要参考了仓库内的两个开源项目：

- `jed2k`
- `amule`

## 特性

`goed2k` 目前已经覆盖了一套可用的 ED2K/eMule 客户端能力，主要包括：

- [x] ED2K 文件下载与多任务并发
- [x] 多个 ED2K server 并发找源与 `server.met` 加载
- [x] KAD（Kad4）bootstrap、搜源与完成发布
- [x] **KADV6**（IPv6 DHT）搜源、发布与 TUI/CLI 开关
- [x] UPNP 端口映射（含混淆 TCP 与 KADV6 UDP）
- [x] 客户端间来源交换（Source Exchange）
- [x] Server / Kad Callback（低 ID 穿透）
- [x] 协议混淆 CryptLayer（`--crypt-layer`，默认关闭）
- [x] AICH 损坏块检测与恢复
- [x] Secure Ident 安全身份（`--sec-ident`、`--identity-key`，默认关闭）
- [x] 上传 zlib 压缩与下载限速
- [x] 本地共享库、Server OfferFiles、KAD/KADV6 发布
- [x] Kad 关键字/Notes 搜索、Collection 链接
- [x] 任务优先级、分类路由、IP 过滤与封禁
- [x] 状态持久化与恢复、`.part.met` JSON 导出
- [x] 可交互终端下载管理器（TUI）

Web 控制台请使用独立仓库 [goed2k/daemon](https://github.com/goed2k/daemon) + [goed2k/webui](https://github.com/goed2k/webui)。守护进程可复用本仓库公开包 `github.com/goed2k/core/bootstrap` 进行客户端初始化。

## 相关文档

- [库 API 使用指南（中文）：嵌入调用、Client/Session、快照与订阅](docs/library-api-CN.md)
- [本地共享库与周边能力：分阶段实现说明（中文）](docs/library-implementation-phases-CN.md)
- [Kademlia v6 协议说明（中文）](docs/kadv6-protocol-CN.md)
- [客户端来源交换（Source Exchange）实现说明（中文）](docs/source-exchange-CN.md)
- [**goed2k 与 eMule/aMule 兼容性说明（中文）**](docs/emule-amule-compat-CN.md)
- [Secure Ident 计划](docs/secure-ident-plan.md)
- [版本变更记录（CHANGELOG）](CHANGELOG.md)

## 开发与测试

```bash
# 默认单元测试（CI 同款，跳过外网联调）
go test -race -count=1 ./...

# 运行外网联调测试（需可访问 ED2K 网络）
GOED2K_RUN_LIVE_TESTS=1 go test -run LiveDownload -count=1 .

# 运行 KADV6 IPv6 联调（需本机 IPv6 出站）
GOED2K_RUN_KADV6_INTEGRATION=1 go test -run KADV6PublishSearchPipelineLive -count=1 .
```

推送至 `main` 或提交 Pull Request 时，GitHub Actions 会自动运行 `go vet`、全量单元测试（含覆盖率）与构建检查。Integration 工作流每日 UTC 03:00 定时运行单元测试。

### 安全与混淆开关（CLI）

```bash
goed2k --crypt-layer --crypt-layer-required --sec-ident --identity-key ./identity.pem \
  --credits-only-verified --max-download-rate-kb 1024 \
  --categories 'video:mp4,mkv:/videos;music:mp3:/music'
```

TUI 设置页（`/setting`）亦可配置上述选项。

### 状态持久化与常用参数

```bash
goed2k --state-path ~/.config/goed2k/state.json \
  --server 'host:port,host:port' \
  --out-dir ./downloads \
  --link 'ed2k://|file|...|/' \
  --setup   # 可选：启动前进入设置向导
```

默认状态文件：`~/.config/goed2k/state.json`（可用 `--no-state` 关闭）。退出时自动保存任务与积分等。

### Web 控制台（daemon + webui）

| 组件 | 仓库 | 说明 |
|------|------|------|
| 守护进程 | [goed2k/daemon](https://github.com/goed2k/daemon) | `goed2kd`，HTTP `/api/v1` + WebSocket 事件 |
| 浏览器 UI | [goed2k/webui](https://github.com/goed2k/webui) | React 控制台，对接 daemon API |

共享初始化逻辑见包 [`bootstrap`](bootstrap/doc.go)（`Config`、`InitClient`、`RunBackground`）。

## 安装

### 可执行文件

```bash
go install github.com/goed2k/core/cmd/goed2k@latest
```

### 作为库

```bash
go get github.com/goed2k/core@v0.1.2
```

守护进程或自定义程序可复用 `github.com/goed2k/core/bootstrap` 进行客户端初始化（见 [bootstrap/doc.go](bootstrap/doc.go)）。

## 快速开始

### 运行终端下载管理器

```bash
goed2k
```

如果你想直接从源码运行：

```bash
go run ./cmd/goed2k
```

## 库使用示例

```go
package main

import (
	"log"

	"github.com/goed2k/core"
)

func main() {
	settings := goed2k.NewSettings()
	settings.ReconnectToServer = true
	settings.EnableUPnP = true

	client := goed2k.NewClient(settings)
	if err := client.Start(); err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	if err := client.ConnectServers("176.123.5.89:4725", "45.82.80.155:5687"); err != nil {
		log.Fatal(err)
	}

	if _, _, err := client.AddLink(
		"ed2k://|file|example-file.mp3|12345678|0123456789ABCDEF0123456789ABCDEF|/",
		"./downloads",
	); err != nil {
		log.Fatal(err)
	}

	if err := client.Wait(); err != nil && err != goed2k.ErrClientStopped {
		log.Fatal(err)
	}
}
```

## License

本项目采用 MIT License。

你可以在保留原始版权声明和许可声明的前提下，自由使用、复制、修改、合并、发布、分发、再许可和销售本项目的副本。项目按“现状”提供，作者不对其适用性或稳定性作任何明示或暗示担保。
