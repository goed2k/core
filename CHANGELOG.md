# Changelog

本文件记录 `github.com/goed2k/core` 的版本变更。

格式基于 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.0.0/)，版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## [Unreleased]

### 新增

- 下载中自动维护 eMule 二进制 `.part.met`：任务创建、脏进度节流和 `Stop` 时原子写出；崩溃留下的合法 `.tmp` 可在导入时恢复。
- ED2K 链接文件名落盘前清洗：去掉路径穿越；Windows 另替换非法字符、控制字符、保留设备名和尾随点/空格。
- `disk.PreallocateSemantics`：公开当前平台预分配真实能力（Linux fallocate / Windows NTFS / 其余仅 Truncate）。
- Settings/bootstrap 策略字段对齐：临时布局、磁盘、Web 下载、Kad 部分发布可通过 Config/env 映射；CLI 从 `DefaultConfig` 起步以免零值覆盖默认值。`ClientState` v8 持久化该子集；从未发布的状态版本 5/6 按 v7 兼容 JSON 升级。恢复限速时同步 `downloadLimiter`。自动保存失败可通过 `LastAutoSaveError` 观测并打 Warn；失败仍按间隔重试，避免每 tick 刷盘。
- KADV6 发布端点支持注入本机 IPv6 探测器；默认 `go test` 用文档地址且禁用公网探测。可选 `GOED2K_RUN_KADV6_INTEGRATION=1` 走真实出站地址。
- `UseEmuleTempLayout` 任务完成后把 `NNN.part` 重命名到清洗后的最终文件名；`IncomingDir` 来自 eMule 配置或 Settings，空则留在临时目录。目标冲突且大小不同时使用 `name (n).ext`，同大小视为崩溃重试。
- 服务器搜索支持最小布尔查询：`OR` / `NOT` / `-word`，默认 AND，左结合；过滤条件仍 AND。不支持括号。默认 AND 词仍按原标点切开以对齐 Kad 索引；OR/NOT/`-word` 操作数保持整词。`TokenizeSearchQuery` 跳过 NOT 操作数。

### 变更

- Windows 上 `UseSparseFiles` 会先 `FSCTL_SET_SPARSE` 再 Truncate；`PreallocateDiskSpace` 会设置 `FileAllocationInfo`。非 Linux/Windows 明确只 Truncate，不保证稀疏或占盘。
- 下载块粒度统一为 eMule `EMBLOCKSIZE` 180 KiB：picker / 磁盘偏移 / HTTP Range / `.part.met` / resume 与 AICH 共用同一边界。完整 9.5 MiB 分片为 53 块（52×180 KiB + 末块 140 KiB）。`ClientState` 升到 v9，旧 190 KiB `DownloadedBlocks` 按字节并集重映射，只保留被完整覆盖的新块；已完成 piece 位图保持。磁盘读写改用 `PieceBlock.FileOffset()`，不再用 `BlocksOffset()*BlockSize`。

### 修复

- 二进制 `.part.met` 导入会把未完成分片中已下完的整块写入 `DownloadedBlocks`，不再只保留完全没有 gap 的 piece。半块仍丢弃。

## [0.1.3] - 2026-08-02

### 新增

- **HTTP 下载源 / Web 缓存**：`Client.AddHttpSource`、HTTP Range 并行拉块、`WebCacheDir` 本地块缓存、`PeerWeb` 源标签
- **Sparse / 预分配磁盘**：`FileHandler.Preallocate`、`PreallocateDiskSpace` / `UseSparseFiles` 设置；eMule 配置 `AllocFull` / `SparseFiles` 映射
- **P1 eMule/aMule 兼容**：SX1 (0x81/0x82)、Preview 入站、MultiPacket 入站合并、GetSourcesObfu、UDP ReAsk、eMule 配置导入、NNN.part 布局、KAD 部分发布
- **P0 eMule/aMule 兼容**：eMule 二进制 `.part.met` 导入导出、Source Exchange v5（IPv6 扩展）、`ipfilter.dat` 解析
- **兼容性文档**：`docs/emule-amule-compat-CN.md` 已实现/差距对照
- **互操作回归**：SecIdent 双端上传、CryptLayer 强制模式、IPv6 SX 条目合并测试

### 变更

- `ExportPartMet` 签名改为接收 `PartMetInfo`（含 hash、size、resume、http_sources）
- `IPFilter` 支持按 AccessLevel 与 FilterLevel（默认 127）判定
- `ClientTransferState` / `.part.met.json` 持久化 `http_sources`
- 出站 MultiPacket 合并暂时禁用（与 CryptLayer 握手冲突，待后续修复）

### 修复

- CryptLayer 本地双端联调与混淆拨号修复
- MultiPacket 握手阶段不合并出站帧
- Windows CI：传输/磁盘测试结束关闭文件句柄；`DesktopFileHandler` 并发 `Close` 加锁
- CodeQL：`unix.Fallocate` 替代手动 `uintptr` 转换

## [0.1.2] - 2026-07-29

### 新增

- CLI 状态持久化：默认 `~/.config/goed2k/state.json`，`--state-path` / `--no-state`
- CLI 补全参数：`--server`、`--out-dir`、`--kad`、`--upnp`、`--listen-port`、`--link`、`--setup`、`--timeout` 等
- 公开包 `bootstrap`：CLI / daemon 共享的客户端初始化与后台引导（连服务器、nodes.dat、状态加载）
- `--timeout` / `--link` 模式下 TUI 在任务全部完成或超时后自动退出
- CLI：`--sec-ident-required`、`--max-upload-rate-kb`、`--secure`（CryptLayer + SecIdent 预设）
- `TestEMuleInteropHarness`：本地双端上传与协议线格式回归（含 CryptLayer 混淆端口联调）

### 变更

- **移除** `cmd/goed2k-web` 与 `internal/webapi`：请改用 [goed2k/daemon](https://github.com/goed2k/daemon) + [goed2k/webui](https://github.com/goed2k/webui)
- UPnP 调试日志默认关闭（`upnp.Debug`）
- 连接读缓冲复用，减少每 tick 分配；补充 I/O 超时注释
- TUI：有任务时进入 `/setting` 显示中文锁定提示
- CI：单元测试输出 `-coverprofile` 覆盖率摘要
- Integration 工作流：每日 UTC 03:00 定时运行
- Release 矩阵：linux/windows/darwin × amd64/arm64

### 修复

- Windows CI：`registerClientTransferFileCleanup` 推广，避免 TempDir 清理时文件句柄未关闭
- TUI：`--timeout` 在交互模式下真正生效
- CryptLayer：出站拨号使用对端 `TCP端口+3`（不再误用本地 ListenPort）
- CryptLayer：主监听端口不再强制包装 ObfuscatedConn，可选混淆走独立混淆端口
- CryptLayer：入站/出站握手支持增量读取，与 PumpIO 协同完成后再发送 Hello

## [0.1.1] - 2026-07-29

### 新增

- Go **1.26** 为唯一支持版本；CI 多 OS（Ubuntu/Windows）+ staticcheck + gofmt 检查
- `testdata/server.met` 内嵌 fixture，测试不再依赖外部 jed2k 仓库
- CLI：`--crypt-layer-required`、`--credits-only-verified`、`--max-download-rate-kb`、`--categories`
- TUI 设置页：CryptLayer 强制、Credits 仅验证 peer、分类规则、Identity key
- TUI Setup 向导：CryptLayer / SecIdent 开关
- Web API：`/settings`、`/dht`、`/dhtv6`、`/search`、`/shared` 端点
- KAD 完成发布时自动发布 Notes（任务 `FileComment`）
- 出站 `FileComment` 包（握手完成后）
- 共享文件 `RequestSources2` 应答本机源
- 显式 `tcp6` 入站监听（与 IPv4 并行）
- Release / Integration GitHub Actions 工作流
- 集成测试：`protocol_e2e_test.go`（SecIdent、AICH、CryptLayer、SharedFile SX）
- 分类配置持久化（`client_state` v7）
- `Client.LoadIdentity` / `SettingsSnapshot` / `SharedFileSnapshots` API

### 修复

- 启用 `--sec-ident` 但未指定密钥时自动使用默认路径 `goed2k-identity.pem`
- CHANGELOG v0.1.0 已知限制描述过时（TUI 已暴露 CryptLayer/SecIdent）

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
- Secure Ident / CryptLayer 默认关闭；可通过 CLI/TUI 启用
- KADV6 发布在无可用 IPv6 端点时静默跳过
- `.part.met` 续传元数据以 eMule 二进制为主格式，goed2k JSON 为旁注；下载中不自动维护，需 `ExportPartMet` 或 `state.json`

## [0.0.2] - 2026-04-09

### 新增

- `PeerInfo` 增加 Hello Misc 字段（`HelloMisc1` / `HelloMisc2`）
- 库 API 文档更新（`docs/library-api-CN.md`）

## [0.0.1]

- 初始公开版本，模块路径 `github.com/goed2k/core`

[0.1.3]: https://github.com/goed2k/core/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/goed2k/core/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/goed2k/core/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/goed2k/core/compare/v0.0.2...v0.1.0
[0.0.2]: https://github.com/goed2k/core/compare/v0.0.1...v0.0.2
[0.0.1]: https://github.com/goed2k/core/releases/tag/v0.0.1
