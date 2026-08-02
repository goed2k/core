# goed2k 与 eMule / aMule 兼容性说明

本文档描述 **goed2k**（`github.com/goed2k/core`）相对官方 **eMule** 与 **aMule** 客户端的能力对照：已实现项、部分实现项、以及主要差距与后续方向。

**适用范围**：当前 `main` 分支及 P0/P1 兼容分支（`cursor/p0-emule-compat-6407`、`cursor/p1-emule-compat-6407`，PR #11 / #12）。

**图例**：

| 标记 | 含义 |
|------|------|
| ✅ | 已实现，可与主流 eMule/aMule 互操作 |
| 🟡 | 部分实现（协议或 API 已有，业务流程/ UI / 自动化不完整） |
| ❌ | 未实现或刻意不做 |
| ⚙️ | 默认关闭，需配置启用 |

---

## 1. 总览

goed2k 定位为 **ED2K/eMule 协议库 + 终端下载管理器**，核心下载、找源、KAD、服务器、混淆、AICH 等主路径已可用。与完整 eMule/aMule 相比，差距主要集中在：

- 图形界面与插件生态（goed2k 仅有 TUI）
- 部分 eMule 扩展协议（Captcha、出站 MultiPacket、完整 Preview UI）
- 与 eMule 完全一致的目录/临时文件生命周期（`NNN.part` 重命名、下载中自动写 `.part.met`）
- Kad2、KAD↔KADV6 互通、IRC、Web 服务等周边功能

---

## 2. 客户端标识与握手

| 能力 | eMule/aMule | goed2k | 说明 |
|------|-------------|--------|------|
| 客户端名称 / Mod 名 | 可配置 | ✅ `goed2k` | `Settings.ClientName` / `ModName` |
| User Hash（16 字节） | 随机/固定 | ✅ | `Settings.UserAgent` |
| Hello / HelloAnswer | 标准 | ✅ | `peer_connection.go` |
| ExtHello / ExtHelloAnswer | eMule 扩展 | ✅ | 含版本、SecIdent 等标签 |
| Misc 选项（AICH、Unicode、SX、压缩等） | 位域标签 `0xFA` | ✅ | `MiscOptions` |
| Misc2（大文件、SX2、Captcha 声明等） | 位域 `0xFE` | 🟡 | Captcha 仅声明位，无实际处理 |
| Source Exchange 能力协商（tag `0x3B`） | v2–v5 | ✅ | 含 goed2k 扩展 **v5 IPv6** |
| Preview 能力宣告 | 可选 | 🟡 | 协议支持；Hello **暂不宣告**（CryptLayer CI 互操作） |
| MultiPacket 能力宣告 | 可选 | 🟡 | 入站展开已实现；出站合并 **已禁用** |

---

## 3. 客户端 ↔ 客户端（TCP）

### 3.1 下载 / 上传主流程

| 能力 | 状态 | 关键实现 |
|------|------|----------|
| FileRequest / FileAnswer | ✅ | `peer_connection.go` |
| FileStatusRequest / Answer（BitField） | ✅ | |
| HashSetRequest / Answer | ✅ | 大文件分片哈希集 |
| StartUpload / AcceptUpload / QueueRanking | ✅ | `upload_queue.go` |
| RequestParts32 / RequestParts64 | ✅ | 按文件大小自动选择 |
| SendingPart32/64、CompressedPart32/64 | ✅ | 上传 zlib 压缩（可配置） |
| CancelTransfer / OutOfParts | ✅ | |
| 上传队列与积分（Credits） | ✅ | `client_credits.go`；可仅统计已验证 peer |
| 好友槽（Friend Slot） | ✅ | `session.go` / `peer_connection.go` |

### 3.2 来源交换（Source Exchange）

| 版本 | 操作码 | 状态 | 说明 |
|------|--------|------|------|
| **SX1** | `0x81` / `0x82` | ✅ | 旧客户端回退；`source_exchange_sx1.go`、`peer_connection_p1.go` |
| **SX2 v4** | `0x83` / `0x84` | ✅ | UserHash + CryptOptions；`source_exchange.go` |
| **SX2 v5（IPv6）** | 同上 + 16B IPv6 | ✅ | Hello `0x3B` 协商；`DialAddr` 双栈拨号 |
| 应答时填充 ServerIP/ServerPort（SX1） | ✅ | `session_server_exchange.go` |
| 合并来源进 Policy | ✅ | `policy.go`；过滤局域网/自身/低质量地址 |
| 纯 SharedFile 上传时 SX 应答 | 🟡 | 仅应答本机单源，不广播其他 peer |

详见 [source-exchange-CN.md](source-exchange-CN.md)。

### 3.3 预览（Preview）

| 能力 | 状态 | 说明 |
|------|------|------|
| RequestPreview / PreviewAnswer（`0x90`/`0x91`） | 🟡 | 编解码与 handler 完整：`preview.go`、`peer_connection_p1.go` |
| 上传侧应答（按分片读盘） | ✅ | `CanUploadRange` + `ReadRange` |
| 下载侧请求 | 🟡 | 目前仅在 `AcceptUpload` 后请求 **piece 0** |
| 缓存预览数据 | ✅ | `Transfer.StorePreviewPiece` / `PreviewPiece` |
| TUI / API 展示预览 | ❌ | 无 UI |

### 3.4 MultiPacket（合并发送）

| 能力 | 状态 | 说明 |
|------|------|------|
| OP_MULTIPACKET_EXT2（`0xA9`）解压入站 | ✅ | `expandIncomingMultiPackets`、`multipacket.go` |
| 出站 zlib 合并多帧 | ❌ | **刻意禁用**（与 CryptLayer 握手在 CI/部分环境冲突） |
| PackMultiPacketExt2 API | ✅ | 可供外部或后续启用 |

### 3.5 安全与完整性

| 能力 | 状态 | 说明 |
|------|------|------|
| **CryptLayer** 协议混淆 | ✅ ⚙️ | RC4、端口+3、独立混淆监听；`protocol_obfuscation.go` |
| CryptLayer 强制模式 | ✅ ⚙️ | `CryptLayerRequired` |
| **SecIdent** 安全身份 | ✅ ⚙️ | RSA 2048、挑战应答；`secure_ident.go` |
| **AICH** 损坏检测与恢复 | ✅ | 根哈希、分块哈希、自动恢复；`aich.go` |
| FileComment / Rating | 🟡 | 出站评论、入站解析；rating 无业务/UI |
| Captcha | ❌ | Hello 可声明，无处理器 |

### 3.6 其他客户端 TCP

| 能力 | 状态 | 说明 |
|------|------|------|
| 聊天（Chat） | ❌ | eMule 已废弃的服务器聊天相关 opcode 未实现 |
| 浏览共享文件（View Shared Files） | ❌ | Hello 声明 `NoViewSharedFiles` |

---

## 4. 客户端 ↔ 服务器（TCP）

| 能力 | 状态 | 说明 |
|------|------|------|
| LoginRequest / IdChange | ✅ | 含 `ObfuscationTCPPort`、`ReportedIP` |
| GetServerList（`0x14`） | ✅ | |
| **GetSources**（`0x19`） | ✅ | 低 ID / 高 ID 分别入 Policy |
| **GetSourcesObfu**（`0x23`） | ✅ | `EnableCryptLayer` 时并行请求；`found_file_sources_obfu.go` |
| **FoundSourcesObfu**（`0x44`） | ✅ | 解析 CryptOptions + UserHash |
| OfferFiles（`0x15`） | ✅ | 完成传输 + 共享库 |
| SearchRequest / SearchMore / SearchResult | ✅ | `search.go` |
| CallbackRequest / CallbackRequestIncoming | ✅ | 低 ID 穿透；`session_callback.go` |
| Server Status / Message | ✅ | |
| 服务器 TCP CryptLayer | ✅ ⚙️ | 优先连混淆端口 |
| 多服务器并发与 Ping 排序 | ✅ | `server_connection_policy.go` |
| `server.met` 加载（本地/URL/ed2k） | ✅ | |
| 服务器搜索高级选项（布尔树全功能） | 🟡 | 基础搜索可用，复杂查询树覆盖有限 |

---

## 5. UDP

| 能力 | 状态 | 说明 |
|------|------|------|
| KAD UDP（与客户端共用端口） | ✅ | `dht_tracker.go` |
| GlobServStat（服务器 UDP 统计） | ✅ | `server_glob_udp.go` |
| 客户端 UDP ReAsk（`0x90`/`0x91`） | 🟡 | 收发与 `SendUDPReaskPing` API：`client_udp.go` |
| ReAsk 结果驱动防火墙/低 ID 策略 | ❌ | `IsUDPReachable()` 存在，下载管线未使用 |
| Kad UDP 混淆 | ❌ | eMule 规范中 Kad UDP 尚不可混淆 |
| 全局搜索 UDP（GlobSearch 等） | ❌ | 仅 TCP 搜索 + KAD |

---

## 6. Kademlia（KAD / KADV6）

| 能力 | eMule/aMule | goed2k | 说明 |
|------|-------------|--------|------|
| Kad4（IPv4）路由与 RPC | ✅ | ✅ | `dht_tracker.go`、`protocol/kad` |
| Bootstrap、`nodes.dat` | ✅ | ✅ | 本地/URL/多源 |
| 搜源（Publish / Search Sources） | ✅ | ✅ | 并入 Transfer Policy |
| 关键字搜索 | ✅ | ✅ | |
| Notes 搜索与发布 | ✅ | ✅ | 完成时发布 `FileComment` |
| 完成文件周期发布（~30min） | ✅ | ✅ | `session_kad_publish.go` |
| **部分完成文件发布源** | 可配置 | ✅ | `PartialKadPublish` + `isKadPublishable()` |
| Callback / FindBuddy | 部分 | 🟡 | RPC 存在，非完整 buddy 体验 |
| **KADV6**（IPv6 DHT） | aMule 等有扩展 | ✅ | 独立叠加层；`kadv6_tracker.go` |
| KAD ↔ KADV6 互通 | — | ❌ | 两套网络需分别启用 |
| Kad2 协议 | 新版本 | ❌ | 仅 Kad4 + KADV6 |

详见 [kadv6-protocol-CN.md](kadv6-protocol-CN.md)。

---

## 7. 文件、目录与配置格式

| 格式 / 行为 | eMule/aMule | goed2k | 状态 |
|-------------|-------------|--------|------|
| ED2K 文件链接（含 AICH、分片 hash） | ✅ | ✅ | `emule_link.go` |
| Collection 链接 / 文件 | ✅ | ✅ | `client.go` |
| **二进制 `.part.met`** | ✅ | ✅ | P0：`protocol/emule_partmet.go`、`part_met.go` |
| goed2k JSON `.part.met.json` 旁注 | — | ✅ | 导出时可选写出；导入自动识别 |
| 下载中自动维护 `.part.met` | ✅ | ❌ | 续传走 `state.json`；需手动 `ExportPartMet` |
| **`NNN.part` 临时布局** | Temp 目录 | 🟡 | P1：`transfer_path.go`；`UseEmuleTempLayout` + `AddLink` |
| 完成后 `NNN.part` → 最终文件名 | ✅ | ❌ | 无 Incoming 目录搬运 |
| **`preferences.ini` / `amule.conf`** | ✅ | 🟡 | P1：`emule_import.go`；无 CLI/TUI 入口 |
| **`ipfilter.dat`**（PeerGuardian / AntiP2P） | ✅ | ✅ | P0：`ipfilter.go`；AccessLevel + FilterLevel |
| `state.json`（任务/积分/DHT/共享） | — | ✅ | goed2k 自有格式 |
| `server.met` / `nodes.dat` | ✅ | ✅ | |
| IP 过滤接入连接策略 | ✅ | ✅ | `policy.go` |

`.part.met` 格式说明见 [part-met-format-CN.md](part-met-format-CN.md)。

---

## 8. 网络、共享与其它客户端能力

| 能力 | eMule/aMule | goed2k |
|------|-------------|--------|
| UPnP 端口映射 | 可选 | ✅ TCP + 混淆 TCP + KADV6 UDP |
| 显式 IPv6 入站（tcp6） | 部分 | ✅ |
| 下载/上传限速 | ✅ | ✅ |
| 分类（按扩展名路由目录） | ✅ | ✅ |
| 任务上传/下载优先级 | ✅ | ✅ |
| 本地共享库 + 扫描目录 | ✅ | ✅ |
| Server/KAD 发布共享文件 | ✅ | ✅ |
| Web 界面 | 官方无 / 插件 | ❌（独立 `goed2k/daemon` + `goed2k/webui`） |
| IRC / 消息 | 有 | ❌ |
| 内置浏览器 / 媒体播放器 | 有 | ❌ |

---

## 9. P0 / P1 已完成兼容项（分支对照）

### P0（PR #11）

| # | 项 | 状态 |
|---|-----|------|
| 1 | eMule 二进制 `.part.met` 读写 | ✅ |
| 2 | Source Exchange v5（IPv6 扩展） | ✅ |
| 3 | `ipfilter.dat`（PeerGuardian + AccessLevel） | ✅ |
| 4 | SecIdent / CryptLayer 互操作回归测试 | ✅ |

### P1（PR #12）

| # | 项 | 状态 | 备注 |
|---|-----|------|------|
| 6 | SX1（`0x81`/`0x82`） | ✅ | 与 SX2 自动选择 |
| 7 | 客户端 UDP ReAsk | 🟡 | 协议有，业务未集成 |
| 8 | 下载预览 Preview | 🟡 | 无 UI；仅 piece 0 |
| 9 | MultiPacket 合并 | 🟡 | 仅入站 |
| 10 | eMule 配置导入 | 🟡 | API 有，无 CLI |
| 11 | `.ed2kpart` / `NNN.part` 路径 | 🟡 | 分配槽位，不自动重命名 |
| 12 | GetSourcesObfu | ✅ | |
| 13 | 客户端标识 `goed2k` | ✅ | |
| 14 | 部分完成文件 KAD 发布 | ✅ | `PartialKadPublish` |

---

## 10. 与 eMule / aMule 的主要差距（待办方向）

以下按优先级归纳，便于后续迭代（P2 及以后）。

### 10.1 协议与互操作

| 差距 | 影响 | 建议方向 |
|------|------|----------|
| MultiPacket **出站**合并 | 与部分客户端带宽效率 | 修复 CryptLayer 握手期合并问题后重新启用 |
| Preview 多分片 + UI | 预览体验 | TUI/API 展示 `PreviewPiece` |
| Hello 宣告 Preview/MultiPacket | 与 eMule 能力对齐 | 握手稳定后恢复宣告 |
| Captcha 协议 | 少数 mod 要求 | 低优先级 |
| UDP ReAsk 驱动连接策略 | 防火墙/NAT 判断 | 在 Policy 中参考 `IsUDPReachable()` |
| Kad2 / KAD↔KADV6 桥接 | 网络融合 | 架构级，见 KADV6 文档 |
| 与全版本 eMule 大规模联调 | 回归信心 | 扩展 `protocol_e2e_test.go`、live 测试 |

### 10.2 文件与迁移

| 差距 | 影响 | 建议方向 |
|------|------|----------|
| 下载中自动写 `.part.met` | 与 aMule 交叉续传 | 在 `SecondTick` 或分片完成时增量导出 |
| `NNN.part` 完成后重命名 | 目录布局与 eMule 一致 | `Finished` 时移到 Incoming + 最终名 |
| `ImportEmulePreferences` CLI | 从 eMule 迁移 | `goed2k --import-prefs` |
| NickName 等未映射字段 | 配置完整性 | 扩展 `ApplyEmulePreferences` |

### 10.3 产品与生态

| 差距 | 说明 |
|------|------|
| 图形界面 | eMule/aMule 为 GUI；goed2k 为 TUI + 外部 Web |
| 插件、Mod 脚本 | 无 |
| 自动更新服务器列表 / 智能 Kad 引导 | 部分有（`server.met`、`nodes.dat`），无 eMule 全套智能服务器 |
| 媒体预览、评论社区 | 无 |

---

## 11. 代码与测试索引

| 主题 | 路径 |
|------|------|
| Peer 协议状态机 | `peer_connection.go`、`peer_connection_p1.go` |
| 来源交换协议 | `protocol/client/source_exchange.go`、`source_exchange_sx1.go` |
| 服务器协议 | `server_connection.go`、`protocol/server/` |
| KAD 发布 | `session_kad_publish.go`、`session_kadv6_publish.go` |
| `.part.met` | `protocol/emule_partmet.go`、`part_met.go` |
| IP 过滤 | `ipfilter.go` |
| eMule 配置 / 路径 | `emule_import.go`、`transfer_path.go` |
| 混淆 | `protocol_obfuscation.go` |
| 互操作测试 | `protocol_e2e_test.go`、`phase_h_test.go` |

**建议测试命令**：

```bash
go test -race -count=1 ./...
go test -run 'EMuleInterop|SourceExchange|PartMet|IPFilter|Preview|MultiPacket|Obfu' ./...
```

---

## 12. 参考

- [eMule 官方](https://www.emule-project.net/)
- [aMule 文档](https://amule-org.github.io/docs/)
- 仓库内：[CHANGELOG.md](../CHANGELOG.md)、[source-exchange-CN.md](source-exchange-CN.md)、[part-met-format-CN.md](part-met-format-CN.md)
- aMule 源码参考：`DownloadClient.cpp`、`PartFile.cpp`、`ClientTCPSocket.cpp`（SX2）、`opcodes.h`

---

*文档随 P0/P1 合并进度更新；若代码与本文冲突，以仓库 `main` 及 `CHANGELOG.md` [Unreleased] 为准。*
