# goed2k 与 eMule / aMule 兼容性基线及实施路线

本文档是 `github.com/goed2k/core` 后续兼容性工作的执行基线。它记录当前源码能证明的范围、尚未闭环的风险、优先级、验证方式和逐 PR 门禁；不把“存在编解码器或本地单测”等同于“已与真实 eMule/aMule 互操作”。

## 1. 基线、范围与判定原则

- **基线**：`main` 分支提交 `9d7487a`（2026-08-20 审查，含 Server Login 官方 CryptLayer 能力位）。
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
| **客户端 UDP ReAsk 基础格式已覆盖；UDPVer 2+ 扩展仍未完成** | `client_udp.go` 使用 `OP_EMULEPROT`（0xC5）编解码标准 `OP_REASKFILEPING`（hash 16）、`OP_REASKACK`（rank 2）、`OP_FILENOTFOUND`、`OP_QUEUEFULL`。Hello 宣告 `UDPVer=1` 与 `CT_EMULE_UDPPORTS`，不宣告 part status / complete source count。下载侧到期排队来源在已知 UDP 端口时优先 UDP ReAsk，TCP 回退；ACK 更新 rank 并推迟重询，QueueFull/FileNotFound 不增加 `FailCount`。上传侧按 IP+UDP+文件 hash 匹配 `uploadWaiter`，TCP 断开后仍可 ACK 并刷新 lastAsked。旧 6 字节 0xE3 探测已拒绝。 | 与 eMule 基础 ReAsk 互问互答已具备线格式与状态接入；断开后的排队身份可由 UDP 续期。不处理 `OP_PACKEDPROT` 压缩客户端 UDP。 | 高 | 已有 golden bytes、短包/旧探测拒绝、下载侧 ACK/QueueFull/FileNotFound 与上传侧匹配/未知文件/断开后 ACK 测试。UDPVer>3 的 part status、complete source count 仍是后续独立 PR。 |
| **普通块边界为 190 KiB，与主流 180 KiB 不一致** | `constants.go` 的 `BlockSize=190*1024`，`data/peer_request.go` 的 `prBlockSize` 同为 190 KiB；该值进入 picker、磁盘偏移、请求、接收、HTTP Range、`.part.met` gap 和 AICH 重置映射。`aich.go` 的 `AICHBlockSize=180*1024`。 | 边界差异可能影响与标准 BLOCKSIZE/EMBLOCKSIZE=180 KiB 的请求切分、断点区间和 AICH 坏块映射。现阶段证据不足，**不得直接断言一定导致数据损坏**。 | 高 | 先列出全链路边界清单，再用非整块尾部、跨块压缩包、64 位偏移、断点导入和 AICH 恢复做表驱动测试；加入 eMule/aMule 请求区间抓包对照后再改常量。 |
| **Kad LowID 消费/发布已覆盖服务器回调路径；Buddy 隧道与 KADV6 LowID 标签仍待后续** | `protocol/kad/types.go:SourceInfo` 按 eMule `TAG_SOURCETYPE` 区分 HighID（1/4）与 LowID（2/3 或仅 client ID）。`session.mergeKadSearchSources` 对 HighID `AddPeer` 直连，对 LowID 写入 `ServerClientID` 并复用 `Policy.ConnectOnePeer` → `RequestServerCallback`。`PublishSource` 按 `session.clientID`/`IsLowID` 发布 SourceType=1 或 2（LowID 带 `TagClientLowID`，不再固定宣称 HighID）。KADV6 仍拒绝 SourceType=2/3 直连，且本机 LowID 时跳过 IPv6 源发布，不实现 client ID 标签。未实现 FindBuddy TCP 隧道、Kad2、KAD↔KADV6 桥接。 | Kad4 LowID 来源不再被当成假 IP 拨号；本机 LowID 发布可被其他客户端识别为需回调。没有 Buddy 时，双方都是 LowID 或未连服务器仍无法互连。KADV6 LowID 只是不再谎报 HighID。 | 中 | 已有假 tracker/entry 测试：HighID 直连、LowID/type2/type3/仅 client ID 进回调、不可用 type2 不入表、HighID/LowID 发布标签与共存索引。仍需真实 LowID 节点与 eMule/aMule 联调；Buddy/Kad2/KADV6 LowID 标签为后续独立 PR。 |
| **Hello/MiscOptions 能力声明与处理能力不一致** | `peer_connection.go:PrepareHelloAnswer` 现已宣告大文件、SX2 与 `UDPVer=1`（标准 ReAsk 基础格式），并写入 `CT_EMULE_UDPPORTS`。`SupportsPreview`、`MultiPacket` 与 Captcha 仍未声明。`SupportLargeFiles()` 已按位读取。 | 对端可据此发送基础 UDP ReAsk；高 UDPVer 扩展、Preview 与 MultiPacket 仍不应出现。 | 中 | 能力位 golden test 已覆盖已实现位；Preview/MultiPacket 仅在各自闭环后声明。 |

## 4. P1：可靠性、迁移与平台一致性

| 缺口 | 代码证据 | 用户影响 | 复杂度 | 验证方式 |
|---|---|---|---|---|
| **Server Login 混淆能力位已覆盖；UDP/混淆端口不是 Login 标签** | aMule `ServerConnect.cpp` 的 `OP_LOGINREQUEST` 固定写入 4 个标签：`CT_NAME`、`CT_VERSION`、`CT_SERVER_FLAGS`、`CT_EMULE_VERSION`。`NewLoginRequestWith` 按同一文件的 `IsClientCryptLayerSupported/Requested/Required` 映射，在 `CT_SERVER_FLAGS` 置位 `SRVCAP_SUPPORTCRYPT/REQUESTCRYPT/REQUIRECRYPT`。登录 IP 仍为 0，不声明 `CapableIPInLogin`。不写入 Hello `ET_UDPPORT`（0x21）、`ET_COMMENTS`（0x24）或 server.met `ST_TCPPORTOBFUSCATION`（0x97）。客户端 UDP 端口继续由 Hello `CT_EMULE_UDPPORTS` 告知对端；服务器混淆端口由 `IdChange` / `server.met` 下发。 | 支持混淆的服务器可按官方能力位决定是否接受/要求 CryptLayer。臆造 Login 端口标签会被官方客户端/服务器忽略或误解析，因此明确不做。 | 中 | 已有表驱动覆盖关闭/启用/仅要求/同时要求的能力位、固定 4 标签、禁止 0x21/0x24/0x97，以及 Put/Get 与组包往返。仍需真实服务器检查 IdChange/混淆回连。 |
| **`.part.met` 不是下载中的自动兼容状态源** | `part_met.go:ExportPartMet` 仅由显式 API 调用；日常续传主要走 `client_state.go`。`resumeFromGaps` 对二进制导入只标记完全没有 gap 的 piece，不构造 `DownloadedBlocks`，因此部分 piece 的已下载区间会丢失。 | 与 eMule/aMule 交叉接管任务时会丢部分进度；异常退出前未手动导出时二进制 `.part.met` 可能过期。 | 中高 | 从带多段 gap 的真实 `.part.met` 导入并逐字节核对保留区间；在分片/块完成和节流周期原子更新；模拟崩溃恢复及双客户端交叉续传。 |
| **AICH 根哈希主动请求已覆盖；MultiPacket 捆绑与真实联调仍待后续** | 收到匹配的 `FileStatusAnswer`/`HashSetAnswer` 后，若对端 Hello `AICHVersion!=0` 且任务尚无根，连接会发送一次 `OP_AICHFILEHASHREQ`（0x9E）。`AICHFileHashAnswer` 拒绝文件 hash 不匹配、零根和冲突根，只接受首次合法根；`SetAICHRootHash` 不覆盖已有根。缺根时坏片挂起恢复并补请求根；根到达后对挂起分片发送既有 `AICHRequest` 块哈希请求。不把根请求塞进 MultiPacket。 | 链接或状态未带 AICH 根时，可向支持 AICH 的来源补齐根并启动局部恢复，而不必整片重下。对端不回答则该连接不再重试，其他 AICH 来源仍可请求。 | 中 | 已有状态机测试：缺根 FileStatus 请求一次、无 AICH/已有根/hash 不匹配不请求、应答保存/拒绝/冲突、根到达后补块哈希、坏片缺根挂起。仍需真实 eMule/aMule 联调；MultiPacket 内嵌根请求属独立项。 |
| **MultiPacket EXT2 线格式未经真实协议证明** | `protocol/client/multipacket.go` 将完整已编码帧整体 zlib 压缩；测试只做本实现 Pack/Unpack 自洽。`peer_connection_p1.go:tryCoalesceOutgoingMultiPacket` 为空，出站禁用。 | 本地往返通过不能证明符合 eMule 线格式；贸然开启可能导致解包失败、连接中断或与 CryptLayer 时序冲突。 | 高 | 先收集 eMule/aMule golden 抓包和源码定义，建立单向 fixture 测试，不能只做自身 round-trip；分别覆盖明文、CryptLayer、半包、尾随帧和未知子包，再决定是否启用出站。 |
| **Windows/macOS 稀疏、预分配和 Windows 文件名规则缺口** | `disk/preallocate_linux.go` 仅 Linux 使用 `fallocate`；`disk/preallocate_stub.go` 在所有非 Linux 平台静默成功但不执行真实预分配/稀疏标记。`transfer_path.go` 和 `client.go:AddLink` 直接以远端文件名 `filepath.Join`，未见 Windows 保留名、非法字符和尾随点空格清洗。 | 配置显示成功但磁盘策略未生效；恶意或不合法 ED2K 文件名可能创建失败、路径行为不一致或任务无法恢复。 | 中高 | Windows 使用系统稀疏/分配 API、macOS 使用可证明的预分配路径；分别检查实际分配块数。为 `CON`、`NUL`、冒号、反斜杠、尾随点空格、重复名和超长名建立跨平台测试。 |
| **KADV6 关键路径依赖真实 IPv6 环境验证** | `session_kadv6_integration_test.go` 的常规测试使用文档地址和内存合并；由 `GOED2K_RUN_KADV6_INTEGRATION=1` 启用的 live 测试虽检查本机 IPv6 出站地址，但仍只验证本地发布索引，没有公网 bootstrap、远端搜索或拨号。 | 常规 CI 通过不能证明公网 IPv6 bootstrap、发布、搜索和拨号可用。 | 中 | 在具备原生 IPv6 的独立 CI job 运行双节点和公开节点测试，记录路由收敛、发布可见性、来源拨号及无 IPv6 时的明确降级。 |
| **Settings、bootstrap Config 与 ClientState 存在漂移** | `settings.go` 包含临时布局、Kad 发布、磁盘、Web 下载等字段；`bootstrap/config.go` 只映射其中一部分；`client_state.go` 只持久化部分运行配置，版本接受列表还跳过了版本 5/6。 | CLI、库、daemon 或重启后的行为可能不一致，新字段容易静默采用默认值。 | 中 | 建立字段清单和映射测试：默认值、CLI/env→Settings、Settings→状态快照→恢复逐项核对；明确哪些字段刻意不持久化并写入迁移测试。 |

## 5. P2：高级能力与长期质量

| 缺口 | 代码证据 | 用户影响 | 复杂度 | 验证方式 |
|---|---|---|---|---|
| **高级搜索与 KADV6 集成搜索不完整** | `session.go:startDHTSearch` 只启动 Kad4 关键字搜索；服务器查询使用固定字段，未覆盖完整布尔树。虽然 `Client.SearchDHTv6Keywords` API 存在，统一搜索任务没有接入。 | IPv6-only 结果不出现在常规搜索中，复杂筛选与 eMule/aMule 结果不一致。 | 中高 | 以相同查询对比 Server、Kad4、KADV6 的合并/去重；为布尔树、类型、扩展名、大小和来源数建立 golden query。 |
| **Buddy/PeerCache 生命周期缺失** | `protocol/kad/` 有 Buddy 相关标签/报文，`session_callback.go` 有部分 callback/find buddy 路径；未见完整 Buddy 选举、保活、重连状态机，也未见 PeerCache 实现。 | LowID/NAT 场景回调可靠性和缓存加速能力低于完整客户端。 | 高 | 双 NAT/LowID 环境做 Buddy 建立、失效、替换和回调测试；PeerCache 需先按独立威胁模型与真实协议 fixture 验证。 |
| **Kad2 与 Kad4↔KADV6 桥接缺失** | `protocol/kad/` 与 `protocol/kadv6/` 是独立实现；`docs/kadv6-protocol*.md` 明确当前无桥接，仓库未见 Kad2 状态机。 | 无法参与对应网络或跨叠加层共享来源。 | 很高 | 先形成协议设计和安全边界 RFC；使用独立互操作节点验证路由、发布、搜索、去环和隐私，不与其他 P0/P1 改动同 PR。 |
| **`NNN.part` 完成搬运缺失** | `transfer_path.go` 能分配 `NNN.part`，但未见完成后原子搬运到 Incoming/最终文件名的路径。 | eMule 临时目录模式下，完成文件仍保留临时名，迁移体验不完整。 | 中 | 跨卷、目标已存在、非法文件名、崩溃重试和 Windows 文件占用测试；数据与 `.met` 清理必须保持可恢复。 |
| **真实互操作与 fuzz 矩阵不足** | `protocol_e2e_test.go`、`phase_h_test.go` 以本实现双端/本地 fixture 为主；仓库未建立覆盖关键解析器的持续 fuzz corpus，也没有多版本 eMule/aMule 矩阵。 | 自洽实现可能双方共同犯错，边界包、畸形包和版本差异难以及时发现。 | 高（持续） | 建立 Windows eMule、Linux/macOS aMule、多版本、HighID/LowID、明文/混淆、大文件矩阵；对 Hello、标签、Queue/ReAsk、MultiPacket、Kad、`.part.met` 等解析器运行持续 fuzz，并归档抓包 fixture。 |

## 6. 分步 PR 顺序

每个编号都是独立分支和独立 PR；前一项合并后才从最新 `main` 创建下一分支。不得把多个高风险协议改动塞进同一 PR。

1. **能力位诚实性**：移除未实现 Captcha 声明，修正并测试大文件能力 getter；仅在有真实证据时调整其他位。这是低风险首个实现 PR。
2. **下载侧 TCP QueueRanking/重询语义（已覆盖）**：QueueRanking 不作为普通失败；TCP 可关闭，但逻辑 `Peer` 保留 rank 与远端排队状态，并按 eMule 29 分钟周期重询；AcceptUpload 或实际下载开始时清理。
3. **跨连接上传队列身份（已覆盖）**：等待项按 UserHash 持久化，不绑定活跃 TCP；断开后保留 rank/lastAsked/文件 hash/IP/UDP。重连按 UserHash 附着；UDP ReAsk 按 IP+UDP+文件 hash 续期。取消与 70 分钟超时删除身份。无 UserHash 仍绑定连接。未改上传评分公式、槽位踢人策略或完整 Buddy 回拨。
4. **UDP ReAsk 线协议（基础格式已覆盖）**：已独立实现标准 OP_REASKFILEPING、ACK rank、FileNotFound、QueueFull 编解码，并接入下载侧远端队列重询；Hello 仅宣告 UDPVer=1。后续独立 PR 再做 UDPVer 2+ 扩展，不得与上传队列持久化或 Kad LowID 混在同一 PR。
5. **180 KiB 边界审计与迁移**：先提交边界清单/测试，再在单独 PR 改全链路常量；如证据否定变更则关闭实现 PR 并更新本文。
6. **Kad LowID 消费链路（已覆盖服务器回调）**：`SourceInfo` 保留 SourceType；HighID 直连，LowID 进入既有 `RequestServerCallback`/`Policy` 路径。Buddy 隧道仍属第 16 项。
7. **Kad 发布类型（已覆盖 Kad4）**：根据 `clientID`/`IsLowID` 生成 SourceType=1/2 与 client ID 标签。KADV6 LowID 仅跳过 HighID 源发布，完整 IPv6 LowID 标签待后续。
8. **Server Login 扩展标签（已覆盖官方能力位）**：按 aMule `ServerConnect.cpp` 只保留 4 个 Login 标签，并在 CryptLayer 开关打开时写入 `SRVCAP_*CRYPT`。文档原先假设应上报 UDP/混淆端口，但官方 Login 并不发送这些标签；客户端 UDP 走 Hello，服务器混淆端口走 IdChange/`server.met`。真实服务器联调仍待后续。
9. **`.part.met` 精确导入**：先保留部分 piece 区间；不同时引入自动写入。
10. **`.part.met` 自动维护**：在已验证精确模型上增加节流、原子写和崩溃恢复。
11. **AICH 根请求闭环（已覆盖）**：缺根时向宣告 AICH 的来源发送 `OP_AICHFILEHASHREQ`，校验后保存首次根并恢复挂起分片；不覆盖已有根，不启用 MultiPacket 捆绑。
12. **MultiPacket 真实线协议核对**：先 fixture/抓包；出站启用必须是后续单独 PR。
13. **跨平台文件 PR 组**：Windows 稀疏/预分配、macOS 预分配、文件名清洗分别提交。
14. **KADV6 真实 IPv6 CI**：先增加可选、可诊断的专用 job，再讨论功能扩展。
15. **Settings/state 一致性**：增加映射清单、迁移和 round-trip 测试。
16. **P2 项目**：高级搜索、Buddy/PeerCache、Kad2/桥接、完成搬运、互操作/fuzz 各自立项，不共享实现 PR。

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
