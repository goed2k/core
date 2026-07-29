# Changelog

本文件记录 `github.com/goed2k/core` 的版本变更。

格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.0.0/)，版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## [Unreleased]

### 新增

- GitHub Actions CI：push/PR 自动运行 `go vet`、`-race` 单元测试与构建
- 外网联调测试默认跳过（`GOED2K_RUN_LIVE_TESTS=1` 手动启用）
- KADV6 IPv6 联调测试（`GOED2K_RUN_KADV6_INTEGRATION=1` 手动启用）
- `applyState` 恢复 Secure Ident 密钥（`LoadIdentity`）
- KAD 发布单测（`session_kad_publish_test.go`）
- CLI/TUI：`--crypt-layer`、`--sec-ident`、`--identity-key`
- `Client.LoadIdentity` 公开 API

## [0.1.0] - 2026-07-29

### 新增

- **KADV6（IPv6 DHT）**：独立 Kademlia v6 叠加层，含路由表、RPC、遍历、搜源与发布（`kadv6_tracker.go`、`session_kadv6.go`、`session_kadv6_publish.go`）
- **KADV6 下载管线**：搜索结果并入 Transfer Policy，支持 IPv6 `DialAddr` 拨号（`PeerKADV6` 来源标志）
- **Server / Kad Callback**：低 ID 场景下的服务器回调与 Kad `CallbackReq` / `FindBuddyReq`（`session_callback.go`）
- **协议混淆（CryptLayer）**：eMule TCP 混淆握手与加解密层（`protocol_obfuscation.go`）
- **AICH**：高级智能损坏处理，损坏块定位与恢复（`aich.go`、`protocol/client/aich.go`）
- **Secure Ident**：安全身份密钥、握手与验签（默认关闭，`secure_ident.go`）
- **上传压缩**：zlib 压缩上传数据块
- **下载限速**：`MaxDownloadRateKB` 全局限速（`download_rate_limiter.go`）
- **Kad 搜索增强**：关键字/Notes 搜索、Collection 链接解析（`search.go`、`emule_link.go`）
- **任务优先级**：上传/下载优先级持久化（`transfer_priority.go`）
- **IP 过滤与封禁**：CIDR 规则过滤与 `BanPeer`（`ipfilter.go`、`ban_peer.go`）
- **分类路由**：按扩展名将下载路由到不同目录（`category.go`）
- **Web API**：`cmd/goed2k-web` REST 服务（任务 CRUD、状态查询）
- **TUI 增强**：KADV6 开关、节点配置、peer 来源统计
- **`.part.met` 导出**：JSON 格式旁注元数据（`part_met.go`）
- **UPnP 扩展**：映射混淆 TCP 端口与 KADV6 UDP 端口（`upnp.go`）
- **Server 增强**：`server.met` 元数据、TCP Status、扩展 IdChange、UDP GlobServStat

### 修复

- 默认关闭 CryptLayer 时，入站连接不再强制包装 `ObfuscatedConn`（修复本地上传回归）

### 已知限制

- KADV6 与经典 Kad **不互通**，需分别启用
- 纯 IPv6 来源不参与 `AnswerSources2` 广播
- Secure Ident / CryptLayer 默认关闭，TUI 暂未暴露开关
- KADV6 发布在无可用 IPv6 端点时静默跳过
- `.part.met` 为 goed2k JSON 格式，非 eMule 原生二进制

## [0.0.2] - 2026-04-09

### 新增

- `PeerInfo` 增加 Hello Misc 字段（`HelloMisc1` / `HelloMisc2`）
- 库 API 文档更新（`docs/library-api-CN.md`）

## [0.0.1]

- 初始公开版本，模块路径 `github.com/goed2k/core`

[0.1.0]: https://github.com/goed2k/core/compare/v0.0.2...v0.1.0
[0.0.2]: https://github.com/goed2k/core/compare/v0.0.1...v0.0.2
[0.0.1]: https://github.com/goed2k/core/releases/tag/v0.0.1
