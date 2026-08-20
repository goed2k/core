# goed2k 与 eMule / aMule 兼容性基线及实施路线

本文档是 `github.com/goed2k/core` 后续兼容性工作的执行基线。它记录当前源码能证明的范围、尚未闭环的风险、优先级、验证方式和逐 PR 门禁；不把“存在编解码器或本地单测”等同于“已与真实 eMule/aMule 互操作”。

## 1. 基线、范围与判定原则

- **基线**：`main` 分支提交 `5f37efc`（2026-08-20，含 #32 `NNN.part` 释放后搬运）。本路线图可做项已收口。
- **范围**：仅审查 `core`；`daemon`、`webui` 和独立 ED2K 服务端不在本文兼容结论内。
- **参照对象**：官方 eMule、aMule 的经典 ED2K/eMule 线协议与常见文件生命周期。
- **证据等级**：
  - **已覆盖**：源码中有完整入口、状态流转和自动化测试，但在真实客户端矩阵完成前仍不宣称“完全兼容”。
  - **部分覆盖**：已有结构、编解码或局部处理，业务闭环、线格式或跨平台验证仍缺失。
  - **缺失/待核对**：没有实现，或现有实现不能由真实线协议证据支持。
- **优先级**：P0 会破坏主要互操作闭环或产生错误能力声明；P1 影响迁移、可靠性或平台一致性；P2 是高级协议、产品完整性与长期回归体系。

## 2. 当前兼容范围

以下能力已有可追踪实现，可以作为后续修复的回归基线，但仍需第 5 节的真实互操作矩阵给出最终兼容结论。

| 范围 | 当前结论 | 主要证据 |
|---|---|---|
| ED2K 服务器 TCP | Login、IdChange、GetSources、Search、Callback、OfferFiles 等基础路径已存在 | `server_connection.go`、`protocol/server/`、`session_callback.go` |
| 客户端 TCP 基础传输 | Hello、文件状态、HashSet、32/64 位请求及普通/压缩数据帧已有处理 | `peer_connection.go`、`protocol/client/packet_combiner.go` |
| 来源交换 | SX1、SX2 及仓库自有 IPv6 扩展已有编解码和策略合并 | `peer_connection_p1.go`、`protocol/client/source_exchange*.go`、`policy.go` |
| Kad4 | 路由、节点加载、来源/关键字/Notes 搜索与发布路径已存在 | `dht_tracker.go`、`kad_*`、`protocol/kad/`、`session_kad_publish.go` |
| KADV6 | 独立 IPv6 叠加层已有路由、来源发布/搜索及下载策略接入 | `kadv6_tracker.go`、`kadv6_*`、`protocol/kadv6/`、`session_kadv6.go` |
| 文件完整性与续传 | ED2K 分片哈希、AICH 局部恢复、goed2k state、二进制及 JSON `.part.met` 读写已存在 | `transfer.go`、`aich.go`、`client_state.go`、`part_met.go`、`protocol/emule_partmet.go` |
| 安全与连接 | CryptLayer、SecIdent、IP 过滤及封禁已有实现和本地测试 | `protocol_obfuscation.go`、`secure_ident.go`、`ipfilter.go`、`ban_peer.go` |

上述“已存在”只说明代码路径可用。尤其是上传队列身份、UDPVer 2+、MultiPacket、Kad Buddy/Kad2 和跨平台文件行为，不能从本地回环测试推导为真实客户端兼容。

## 3. P0：主要互操作闭环

| 缺口 | 代码证据 | 用户影响 | 复杂度 | 验证方式 |
|---|---|---|---|---|
| **下载侧 TCP QueueRanking/重询语义已覆盖；上传侧跨连接排队身份已覆盖** | 下载侧会把远端 rank、排队状态和下一次重询期限保存在逻辑 `Peer` 中；收到 `QueueRanking` 后允许关闭 TCP，但 `Policy` 不增加 `FailCount`，并按 eMule `FILEREASKTIME`（29 分钟）阻止提前重连；同一来源重询会更新 rank，`AcceptUpload` 或实际下载开始会清理状态。上传侧等待项改为不绑定活跃 `*PeerConnection` 的 `uploadWaiter`：有 UserHash 时 TCP 断开只分离连接，保留 waitStart/rank/lastAsked/文件 hash/IP/UDP；同一 UserHash 重连附着原记录；UDP ReAsk 按 IP+UDP+文件 hash 查找，断开后仍可 ACK。`CancelTransfer` 删除身份；超过 eMule `MAX_PURGEQUEUETIME`（70 分钟）未重询则清除。无 UserHash 的连接仍绑定套接字，断开即删。上传中的槽位仍要求存活 TCP。旧连接延迟断开只匹配自身指针，不会拆掉已重连的新连接。 | 下载侧正常排队不再被误记为坏来源，也不会因 TCP 关闭立即重连；本客户端作为上传方时，远端按期重连或 UDP ReAsk 可延续等待时间，而不会因 TCP 关闭被当成新请求排到队尾。**TCP 断开本身不是缺陷。** | 高 | 已有可控时钟状态机覆盖下载侧重询，以及上传侧断开保留、重连保序、旧连接延迟断开不拆新身份、取消重建、超时清除、队列满仍接受旧身份、无 UserHash 断开即删、断开后 UDP ACK/FileNotFound/QueueFull。仍需 eMule/aMule 长队列抓包联调。 |
| **客户端 UDP ReAsk 基础格式已覆盖；UDPVer 2+ 扩展仍未完成** | `client_udp.go` 使用 `OP_EMULEPROT`（0xC5）编解码标准 `OP_REASKFILEPING`（hash 16）、`OP_REASKACK`（rank 2）、`OP_FILENOTFOUND`、`OP_QUEUEFULL`。Hello 宣告 `UDPVer=1` 与 `CT_EMULE_UDPPORTS`，不宣告 part status / complete source count。下载侧到期排队来源在已知 UDP 端口时优先 UDP ReAsk，TCP 回退；ACK 更新 rank 并推迟重询，QueueFull/FileNotFound 不增加 `FailCount`。上传侧按 IP+UDP+文件 hash 匹配 `uploadWaiter`，TCP 断开后仍可 ACK 并刷新 lastAsked。旧 6 字节 0xE3 探测已拒绝。 | 与 eMule 基础 ReAsk 互问互答已具备线格式与状态接入；断开后的排队身份可由 UDP 续期。不处理 `OP_PACKEDPROT` 压缩客户端 UDP。 | 高 | 已有 golden bytes、短包/旧探测拒绝、下载侧 ACK/QueueFull/FileNotFound 与上传侧匹配/未知文件/断开后 ACK 测试。UDPVer 2+ 本路线图不做，需独立立项。 |
| **普通块边界已统一为 180 KiB（完整分片末块 140 KiB）** | `BlockSize` / `prBlockSize` / `AICHBlockSize` 均为 `180*1024`；`BlocksPerPiece=53`。磁盘用 `FileOffset()`（`piece*PieceSize+block*BlockSize`）。正好整片的文件最后一片使用 53 块，不再误标成 1 块。v8 及更早 state 的 `DownloadedBlocks` 按 190 KiB 重映射；未完整覆盖的新块丢弃。完成 piece 位图不改。未改 MultiPacket 出站。 | 与 eMule/aMule 请求切分、AICH 坏块和 `.part.met` gap 对齐。旧 state 中未完整覆盖新块的进度会丢弃并重下。 | 高 | 已有 53 块/末块 140 KiB、FileOffset≠BlocksOffset*BlockSize、整片文件 last-piece 块数、AICH 一对一、190→180 重映射、v8 升级、HTTP 末块 Range、part.met 末块不跨片测试。 |
| **Kad LowID 消费/发布已覆盖服务器回调路径；Buddy 隧道与 KADV6 LowID 标签仍待后续** | `protocol/kad/types.go:SourceInfo` 按 eMule `TAG_SOURCETYPE` 区分 HighID（1/4）与 LowID（2/3 或仅 client ID）。`session.mergeKadSearchSources` 对 HighID `AddPeer` 直连，对 LowID 写入 `ServerClientID` 并复用 `Policy.ConnectOnePeer` → `RequestServerCallback`。`PublishSource` 按 `session.clientID`/`IsLowID` 发布 SourceType=1 或 2（LowID 带 `TagClientLowID`，不再固定宣称 HighID）。KADV6 仍拒绝 SourceType=2/3 直连，且本机 LowID 时跳过 IPv6 源发布，不实现 client ID 标签。未实现 FindBuddy TCP 隧道、Kad2、KAD↔KADV6 桥接。 | Kad4 LowID 来源不再被当成假 IP 拨号；本机 LowID 发布可被其他客户端识别为需回调。没有 Buddy 时，双方都是 LowID 或未连服务器仍无法互连。KADV6 LowID 只是不再谎报 HighID。 | 中 | 已有假 tracker/entry 测试：HighID 直连、LowID/type2/type3/仅 client ID 进回调、不可用 type2 不入表、HighID/LowID 发布标签与共存索引。仍需真实 LowID 节点与 eMule/aMule 联调。Buddy/Kad2 刻意不做；KADV6 LowID 标签需官方证据，本路线图不做。 |
| **Hello/MiscOptions 能力声明与处理能力不一致** | `peer_connection.go:PrepareHelloAnswer` 现已宣告大文件、SX2 与 `UDPVer=1`（标准 ReAsk 基础格式），并写入 `CT_EMULE_UDPPORTS`。`SupportsPreview`、`MultiPacket` 与 Captcha 仍未声明。`SupportLargeFiles()` 已按位读取。 | 对端可据此发送基础 UDP ReAsk；高 UDPVer 扩展与 Preview 仍不应出现。MultiPacket 按第 12 项永不宣告。 | 中 | 能力位 golden test 已覆盖已实现位。Captcha UI / MultiPacket 出站刻意不做，故不宣告。 |

## 4. P1：可靠性、迁移与平台一致性

| 缺口 | 代码证据 | 用户影响 | 复杂度 | 验证方式 |
|---|---|---|---|---|
| **Server Login 混淆能力位已覆盖；UDP/混淆端口不是 Login 标签** | aMule `ServerConnect.cpp` 的 `OP_LOGINREQUEST` 固定写入 4 个标签：`CT_NAME`、`CT_VERSION`、`CT_SERVER_FLAGS`、`CT_EMULE_VERSION`。`NewLoginRequestWith` 按同一文件的 `IsClientCryptLayerSupported/Requested/Required` 映射，在 `CT_SERVER_FLAGS` 置位 `SRVCAP_SUPPORTCRYPT/REQUESTCRYPT/REQUIRECRYPT`。登录 IP 仍为 0，不声明 `CapableIPInLogin`。不写入 Hello `ET_UDPPORT`（0x21）、`ET_COMMENTS`（0x24）或 server.met `ST_TCPPORTOBFUSCATION`（0x97）。客户端 UDP 端口继续由 Hello `CT_EMULE_UDPPORTS` 告知对端；服务器混淆端口由 `IdChange` / `server.met` 下发。 | 支持混淆的服务器可按官方能力位决定是否接受/要求 CryptLayer。臆造 Login 端口标签会被官方客户端/服务器忽略或误解析，因此明确不做。 | 中 | 已有表驱动覆盖关闭/启用/仅要求/同时要求的能力位、固定 4 标签、禁止 0x21/0x24/0x97，以及 Put/Get 与组包往返。仍需真实服务器检查 IdChange/混淆回连。 |
| **`.part.met` 精确导入与运行时自动写出已覆盖** | `resumeFromGaps` 对无 gap 的 piece 置位，并对有 gap 的 piece 把当前 `BlockSize`（180 KiB，完整分片末块 140 KiB）下整块已下载区间写入 `DownloadedBlocks`。导出 gap 用 `PieceBlock.Range`，完整分片末块不会跨进下一片。`Client` 在创建任务、节流周期和 `Stop` 时原子写出 `.part.met`；`writePartMetAtomic` 先写 `.tmp` 再替换，导入前 `recoverPartMetSidecar` 提升合法 tmp、删除损坏 tmp，不覆盖已有合法文件。日常 `client_state.go` 仍保存自身状态。 | 交叉接管保留整块进度；异常退出后仍可用旁注 `.part.met` 恢复到最近一次节流写出。小于当前块长的尾部仍可能丢失。 | 中高 | 已有多 gap、半块拒绝、尾片短块、空/全文件 gap、eMule 二进制部分 gap 导入、损坏/未知格式拒绝、导出再导入保留块、完成量统计、导入块不与原 gap 重叠、完整分片末块不跨片，以及原子替换、tmp 提升/丢弃、节流跳过、Stop 强制写出测试。 |
| **AICH 根哈希主动请求已覆盖；MultiPacket 捆绑与真实联调仍待后续** | 收到匹配的 `FileStatusAnswer`/`HashSetAnswer` 后，若对端 Hello `AICHVersion!=0` 且任务尚无根，连接会发送一次 `OP_AICHFILEHASHREQ`（0x9E）。`AICHFileHashAnswer` 拒绝文件 hash 不匹配、零根和冲突根，只接受首次合法根；`SetAICHRootHash` 不覆盖已有根。缺根时坏片仍整片重下，同时向 AICH 来源补请求根，避免无应答时永久挂起。根到达后对已挂起的块哈希请求按连接串行派出（每个连接同时只挂一个 `pendingAICHPiece`）。不把根请求塞进 MultiPacket。 | 链接或状态未带 AICH 根时，可在握手阶段补齐根；之后坏片才能走 AICH 局部恢复。根未到之前的坏片回落整片重下。对端不回答则该连接不再重试根请求，其他 AICH 来源仍可请求。 | 中 | 已有状态机测试：缺根 FileStatus 请求一次、无 AICH/已有根/hash 不匹配不请求、应答保存/拒绝/冲突、根到达后补块哈希、缺根坏片不挂起、忙连接不覆盖分片索引。仍需真实 eMule/aMule 联调；MultiPacket 内嵌根请求属独立项。 |
| **MultiPacket EXT2 已核对：不做出站/宣告** | eMule 源码证明 `OP_MULTIPACKET_EXT2` 载荷是 FileIdentifier + 子 opcode（如请求文件名/状态），不是完整 TCP 帧再 zlib。本仓库 `PackMultiPacketExt2` 把已编码帧整体压缩，线格式错误。`tryCoalesceOutgoingMultiPacket` 为空，Hello 不宣告 MultiPacket。 | 不会对 eMule/aMule 发出错误 MultiPacket。入站 expand 维持现状。错误格式的 Pack/Unpack 仅自洽，不能当互操作证据。 | 高 | 已用官方源码核对载荷结构；刻意不实现正确 EXT2 出站（需独立 FileIdentifier 子包状态机，超出本路线图）。 |
| **Windows 文件名清洗与跨平台预分配语义已覆盖；macOS 仍仅 Truncate** | `SanitizeDownloadFilename` 在 `filepath.Join` 前去掉穿越与 `..`；仅 Windows 替换 `<>:"|?*`、控制字符、保留设备名和尾随点/空格，Unix 合法名（如冒号）不改。`disk.PreallocateSemantics` 明确三类能力：Linux `fallocate`、Windows NTFS `FSCTL_SET_SPARSE`/`FileAllocationInfo`、其余平台仅 Truncate 且不保证稀疏/占盘。`UseSparseFiles` 优先于 `PreallocateDiskSpace`。未实现 macOS `F_PREALLOCATE`。 | Windows 上非法/保留名可落盘；Settings 不再假装非 Linux 已 fallocate。macOS 仍只扩逻辑大小。清洗后不同非法名可能映射到同一文件名。 | 中 | 已有表驱动映射、保留名、穿越、超长截断、Unix 不改合法名，以及 Windows 稀疏属性/分配簇、Linux `st_blocks` 测试；均不依赖 >4GB 盘。macOS 可证明占盘为后续独立项。 |
| **KADV6 发布/合并单测已不依赖本机公网 IPv6；真实叠加层 CI 仍待后续** | `kadv6PublishEndpoint` 可通过 `Session.detectOutboundIPv6` 注入文档地址；`ListenPort=0` 或探测器返回 nil 时确定性跳过。`TestMain` 默认把 `localOutboundIPv6Detect` 换成空实现，常规 `go test` 不会拨号 `2001:4860:4860::8888`。可选 live：`GOED2K_RUN_KADV6_INTEGRATION=1`。未覆盖公网 bootstrap、远端搜索或拨号。 | 常规 CI 即可证明发布端点选择与本地索引闭环，且无 IPv6 主机也不会因 400ms 探测超时变慢。公网 IPv6 路由/搜索仍不能由本机探测代替。 | 中 | 已有注入探测器、ListenPort=0、探测器 nil、文档地址发布索引、单元测试不发现公网 IPv6。Integration 工作流保留可选 `kadv6_ipv6` job。具备原生 IPv6 的双节点/公开节点 job 仍为后续项。 |
| **Settings / bootstrap / state 映射已覆盖可持久化策略；过程字段仍刻意不落盘** | `BuildSettings` 映射临时布局、Kad 部分发布、磁盘预分配/稀疏、Web 下载与速率。CLI `bootstrapConfig` 从 `DefaultConfig` 起步并叠加 `GOED2K_*`，避免零值把默认 true 的策略打成 false。`ClientState` 升到 v9：v8 写入 `settings` 子集，v9 重映射 190→180 KiB 块索引；`migrateClientState` 接受 0–9。`Policy.IsConnectCandidate` 使用 `Settings.MaxFailCount`（默认 20，`<=0` 回落 20）。`Policy.AddPeer`/`ErasePeers` 使用 `Settings.MaxPeerListSize`（默认 100，`<=0` 回落包常量 100）。不持久化 Logger、UserAgent、端口、DHT 开关、连接池与超时、`MaxPeerListSize`。 | 重启后磁盘/Web/速率策略可恢复。来源失败阈值与名单上限可按本次进程 Settings 调整。端口与 DHT 仍由本次进程配置决定。 | 中 | 已有默认值对齐、`MaxFailCount=2` 拒绝 / 默认 20 边界，以及 `MaxPeerListSize=2` 拒绝 / 无 session 默认 100 / `<=0` 回落测试。Kad/KADV6 路由 `FailCount>=20` 是独立阈值。 |

## 5. P2：高级能力与长期质量

| 缺口 | 代码证据 | 用户影响 | 复杂度 | 验证方式 |
|---|---|---|---|---|
| **高级搜索：Server 最小布尔与 KADV6 纳入 StartSearch 已覆盖；括号仍待后续** | `SearchRequest` 解析 `OR`/`NOT`/`-word`，左结合默认 AND；过滤条件仍 AND。`SearchScopeDHT`/`All` 在 tracker 可用时同时启动 Kad4 与 KADV6 `SearchKeywords`，按文件 hash 合并去重，共用 `SearchResultKAD`。无节点时不拨号、不把 DHT 标忙。括号/引号仍不做。 | 常规搜索可合并 IPv6 关键字命中。复杂括号式仍与 eMule 不一致。 | 中高 | 已有 OR/NOT 编码、KADV6 标签转换、Kad4/KADV6 去重、MaxSize 过滤、空 tracker 不忙、双后端 pending 测试。不依赖公网/本机 IPv6。 |
| **Buddy/PeerCache 生命周期缺失** | `protocol/kad/` 有 Buddy 相关标签/报文，`session_callback.go` 有部分 callback/find buddy 路径；未见完整 Buddy 选举、保活、重连状态机，也未见 PeerCache 实现。 | LowID/NAT 场景回调可靠性和缓存加速能力低于完整客户端。 | 高 | 双 NAT/LowID 环境做 Buddy 建立、失效、替换和回调测试；PeerCache 需先按独立威胁模型与真实协议 fixture 验证。 |
| **Kad2 与 Kad4↔KADV6 桥接缺失** | `protocol/kad/` 与 `protocol/kadv6/` 是独立实现；`docs/kadv6-protocol*.md` 明确当前无桥接，仓库未见 Kad2 状态机。 | 无法参与对应网络或跨叠加层共享来源。 | 很高 | 先形成协议设计和安全边界 RFC；使用独立互操作节点验证路由、发布、搜索、去环和隐私，不与其他 P0/P1 改动同 PR。 |
| **`NNN.part` 完成后重命名已覆盖最小闭环** | 仅 `UseEmuleTempLayout`：`finished()` 只排队 `AsyncRelease`；`OnReleaseFile` 在句柄关闭后把 `001`–`999.part` 搬到 Settings 已有 `IncomingDir`（空则 part 所在目录，不发明默认路径）。目标已存在一律 `name (n).ext`，不按同大小当崩溃重试。同卷 `Rename`，仅 EXDEV/Windows 跨卷回退 copy（Linux errno 17 不当跨卷）。旁注删除 `.met` / `.part.met`。`FinalName` 随 state 保存。恢复已完成 `.part` 会再 promote。未做跨卷事务日志或 Windows 占用重试队列。 | 临时布局完成后不再长期停在 `001.part`。目标冲突不覆盖已有文件。未完成释放不搬运。 | 中 | 已有同目录改名、Incoming 冲突、同大小不覆盖、EXDEV copy、非跨卷失败保留源、`finished()` 须等释放、关闭布局不搬、恢复已完成 `.part`、未完成不搬、Linux EEXIST 测试。 |
| **真实互操作与 fuzz 矩阵不足** | `protocol_e2e_test.go`、`phase_h_test.go` 以本实现双端/本地 fixture 为主；仓库未建立覆盖关键解析器的持续 fuzz corpus，也没有多版本 eMule/aMule 矩阵。 | 自洽实现可能双方共同犯错，边界包、畸形包和版本差异难以及时发现。 | 高（持续） | 建立 Windows eMule、Linux/macOS aMule、多版本、HighID/LowID、明文/混淆、大文件矩阵；对 Hello、标签、Queue/ReAsk、MultiPacket、Kad、`.part.met` 等解析器运行持续 fuzz，并归档抓包 fixture。 |
| **其余曾标可做、本路线图明确不做或需用户决策** | `IncomingDir` 不写入可持久化 Settings：bootstrap Overlay 会用空 Config 冲掉导入值，且用户要求不发明全局默认路径。括号/引号搜索需独立语法与 Kad 分词设计，不是最小布尔补丁。UDPVer 2+ part status 需新线格式与 Hello 能力位，不能混进已覆盖的 UDPVer=1。macOS `F_PREALLOCATE` 需 Darwin CI 与占盘证据，当前门禁只有 Ubuntu/Windows。KADV6 公网 job、完整互操作/fuzz 矩阵依赖专用环境，超出本仓库常规 CI。KADV6 LowID 标签、Kad 路由 FailCount 是否对齐 Policy 尚无官方证据。 | 重启后 Incoming 仍靠本次进程/导入写入；复杂搜索、高 UDPVer、macOS 占盘与公网 IPv6 不在本路线图交付。 | — | 需新立项或用户明确决策后再做。本路线图不再开这些实现 PR。 |
| **刻意不做** | MultiPacket EXT2 出站（线格式已核对错误，保持禁用且不宣告）。IRC、浏览共享、Kad2、完整 Buddy 隧道、Captcha UI。 | 不会发出错误 MultiPacket；高级社交/NAT/验证码能力低于完整客户端。 | — | 无新实现 PR。 |

## 6. 分步 PR 顺序

每个编号都是独立分支和独立 PR；前一项合并后才从最新 `main` 创建下一分支。不得把多个高风险协议改动塞进同一 PR。

1. **能力位诚实性**：移除未实现 Captcha 声明，修正并测试大文件能力 getter；仅在有真实证据时调整其他位。这是低风险首个实现 PR。
2. **下载侧 TCP QueueRanking/重询语义（已覆盖）**：QueueRanking 不作为普通失败；TCP 可关闭，但逻辑 `Peer` 保留 rank 与远端排队状态，并按 eMule 29 分钟周期重询；AcceptUpload 或实际下载开始时清理。
3. **跨连接上传队列身份（已覆盖）**：等待项按 UserHash 持久化，不绑定活跃 TCP；断开后保留 rank/lastAsked/文件 hash/IP/UDP。重连按 UserHash 附着；UDP ReAsk 按 IP+UDP+文件 hash 续期。取消与 70 分钟超时删除身份。无 UserHash 仍绑定连接。未改上传评分公式、槽位踢人策略或完整 Buddy 回拨。
4. **UDP ReAsk 线协议（基础格式已覆盖）**：已独立实现标准 OP_REASKFILEPING、ACK rank、FileNotFound、QueueFull 编解码，并接入下载侧远端队列重询；Hello 仅宣告 UDPVer=1。后续独立 PR 再做 UDPVer 2+ 扩展，不得与上传队列持久化或 Kad LowID 混在同一 PR。
5. **180 KiB 边界审计与迁移（已覆盖）**：`BlockSize` 与 AICH 统一为 180 KiB；磁盘偏移改 `FileOffset`；state v9 重映射旧 190 KiB 块。未改 MultiPacket、Kad、队列。格式未与 eMule 核对则不做 MultiPacket 出站。
6. **Kad LowID 消费链路（已覆盖服务器回调）**：`SourceInfo` 保留 SourceType；HighID 直连，LowID 进入既有 `RequestServerCallback`/`Policy` 路径。Buddy 隧道仍属第 16 项。
7. **Kad 发布类型（已覆盖 Kad4）**：根据 `clientID`/`IsLowID` 生成 SourceType=1/2 与 client ID 标签。KADV6 LowID 仅跳过 HighID 源发布，完整 IPv6 LowID 标签待后续。
8. **Server Login 扩展标签（已覆盖官方能力位）**：按 aMule `ServerConnect.cpp` 只保留 4 个 Login 标签，并在 CryptLayer 开关打开时写入 `SRVCAP_*CRYPT`。文档原先假设应上报 UDP/混淆端口，但官方 Login 并不发送这些标签；客户端 UDP 走 Hello，服务器混淆端口走 IdChange/`server.met`。真实服务器联调仍待后续。
9. **`.part.met` 精确导入（已覆盖整块进度）**：二进制 gap 转为完成 piece 与 `DownloadedBlocks`；半块不计入。不同时引入自动写入。
10. **`.part.met` 自动维护（已覆盖节流与崩溃恢复）**：创建/脏进度/Stop 原子写出；合法 `.tmp` 可恢复，损坏 `.tmp` 不覆盖旧文件。块粒度见第 5 步。
11. **AICH 根请求闭环（已覆盖）**：缺根时向宣告 AICH 的来源发送 `OP_AICHFILEHASHREQ`，校验后保存首次根；块哈希按连接串行请求。缺根坏片不挂起。不覆盖已有根，不启用 MultiPacket 捆绑。
12. **MultiPacket 真实线协议（已核对、不做出站）**：eMule 源码证明 EXT2 载荷是 FileIdentifier + 子 opcode，不是完整 TCP 帧 zlib。本仓库 Pack/Unpack 线格式错误。出站保持禁用，不宣告 MultiPacket。不为本项再开实现 PR。
13. **跨平台文件（Windows 文件名与预分配语义已覆盖）**：ED2K 文件名清洗与 Windows NTFS 稀疏/占盘、非 Linux 明确 Truncate-only 语义已落地。macOS `F_PREALLOCATE` 仍为后续独立 PR，不得与 180 KiB / MultiPacket / Kad / 队列混提。
14. **KADV6 真实 IPv6 CI（单测已不依赖本机公网 IPv6）**：发布端点可注入，`TestMain` 默认禁用公网探测；可选 `GOED2K_RUN_KADV6_INTEGRATION=1` 仍走真实出站地址。公网 bootstrap/远端搜索的专用 job 仍待后续。
15. **Settings/state 一致性（已覆盖可持久化策略）**：Config/env/CLI 映射与 v8 快照/恢复、从未发布的 5/6 按 v7 兼容升级、自动保存失败可观测且按间隔重试。过程调优字段仍刻意不持久化。
16. **P2 与路线图收口**：Server 最小布尔、KADV6 `StartSearch`、`NNN.part` 释放后搬运、`Settings.MaxFailCount`/`MaxPeerListSize` 接入 Policy 已覆盖。其余曾标可做项（IncomingDir 持久化、括号搜索、UDPVer 2+、macOS 预分配、公网 IPv6 CI、互操作/fuzz）已标明不做或需用户决策。Buddy/Kad2/IRC/浏览共享/Captcha/MultiPacket 出站刻意不做。**本路线图结束，不再从本文自动开实现 PR。**

## 7. 多轮审计门禁

所有实现 PR 必须严格依次通过：

1. **实现与测试**：先写可失败的状态机、golden bytes 或跨平台测试，再完成最小实现。
2. **第一次自审**：检查完整 diff、状态机转移、资源生命周期、整数/长度/尾块边界、并发与失败回滚；确认没有顺手改动。
3. **自动验证**：按范围运行 `go test`；涉及并发/网络状态运行 `go test -race -count=1 ./...`；运行 `go vet ./...`、`staticcheck ./...` 和格式检查。真实网络测试必须记录环境和跳过条件。
4. **第二次独立代码审计**：使用独立审计者或 Bugbot，重点检查协议证据、对端不可信输入、状态重入及向后兼容。
5. **修复后复审**：所有审计意见逐条关闭；修复产生的新 diff 必须重新跑相关测试并复审。
6. **CI 全绿**：必需检查全部成功，不能以重跑掩盖稳定复现的失败。
7. **等待 PR 审计无问题**：确认没有未解决 review conversation、待处理 Bugbot 发现或新的阻塞意见。
8. **才允许合并**：合并后更新基线，再从最新 `main` 创建下一分支；禁止预先堆叠下一实现分支。

本文档 PR 自身也执行两轮内容自审：第一轮逐条回到源码核对证据和措辞，第二轮核对优先级、重复项、实施顺序及 Markdown 结构。文档 PR 不运行与内容无关的 Go 代码变更。

## 8. 合并与维护规则

- 本文是路线基线，不是完成度宣传页；“编解码存在”“本地双端通过”“默认关闭”都不能单独证明真实互操作。
- 每个实现 PR 合并时同步更新对应条目的证据、状态和验证结果；不删除历史风险，应记录关闭依据。
- 任何线协议结论至少需要一项外部证据：eMule/aMule 源码定义、真实抓包或跨实现 golden fixture。
- 发现现有前提错误时，先用文档 PR 修正基线，不为追求路线一致性强行修改代码。
- 未满足第 7 节全部门禁时，不合并；PR 无问题只是必要条件，不能替代真实互操作证据。
