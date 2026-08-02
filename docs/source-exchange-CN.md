# 客户端来源交换（Source Exchange / SX2）

本文档说明 `goed2k` 中与 **OP_REQUESTSOURCES2 (0x83)**、**OP_ANSWERSOURCES2 (0x84)** 相关的实现：报文布局、注册位置、运行时行为与涉及源码路径。

## 背景

在 ED2K/eMule 客户端 TCP 连接上，除文件请求与分片传输外，还可通过 **Source Exchange** 向当前对端索取同一文件的其他高 ID 来源，以扩充本地 `Policy` 中的候选 peer。实现需与 **eMule / aMule** 主流客户端二进制兼容，否则对端可能断连或解析错位。

## 操作码与协议族

| 操作码 | 名称 | 协议头 |
|--------|------|--------|
| `0x83` | `OP_REQUESTSOURCES2` | `OP_EMULEPROT`（`0xC5`） |
| `0x84` | `OP_ANSWERSOURCES2` | `OP_EMULEPROT` |

注册位置：`protocol/client/packet_combiner.go`（`Register(PK(EMuleProt, …), …)`）。

## 报文布局（little-endian）

整型字段与 aMule `CMemFile::WriteUInt16` / `WriteUInt32` 一致，均为 **小端**。

### RequestSources2（请求）

总长度 **19 字节**：

1. `uint8`：SX2 版本，实现中与 `SOURCEEXCHANGE2_VERSION` 一致，当前为 **4**。
2. `uint16`：保留字段，**0**。
3. `16` 字节：文件 ED2K hash。

参考：aMule `src/DownloadClient.cpp`（独立 EMule 包发送 `OP_REQUESTSOURCES2`）、`src/PartFile.cpp` `CreateSrcInfoPacket` 中写入顺序。

### AnswerSources2（应答）

1. `uint8`：条目格式版本（1–4，实现侧发送为 **4**）。
2. `16` 字节：文件 hash。
3. `uint16`：来源数量 `n`（最大读取校验 **500**）。
4. 重复 `n` 次「来源条目」，每条长度依版本而定：
   - **v1**：`uint32 UserID` + `uint16 TCPPort` + `uint32 ServerIP` + `uint16 ServerPort`（共 12 字节）。
   - **v2/v3**：在 v1 基础上追加 **16 字节 UserHash**。
   - **v4**：在 v2/v3 基础上再追加 **`uint8` CryptOptions**。

参考：aMule `src/ClientTCPSocket.cpp` 收到 `OP_ANSWERSOURCES2` 时先读版本字节与 hash，再进入 `PartFile::AddClientSources`；发送侧见 `src/PartFile.cpp` `CreateSrcInfoPacket`。

**UserID 与 IP**：Hybrid 编码在 v3+ 与旧版之间存在字节序差异；实现中提供 `SwapUint32`，与 `wxUINT32_SWAP_ALWAYS` 一致，并在应答/解析时按对端 `MiscOptions` 中的 **SourceExchange1Ver**（Hello Tag **0xFA**）是否在旧区间选择编码方式。

## 代码结构

| 区域 | 路径 | 说明 |
|------|------|------|
| 类型与编解码 | `protocol/client/source_exchange.go` | `RequestSources2`、`AnswerSources2`、`SourceExchangeEntry`、`SwapUint32` |
| 注册 | `protocol/client/packet_combiner.go` | `opRequestSources2`、`opAnswerSources2` |
| 单元测试 | `protocol/client/source_exchange_test.go` | golden、`Put`/`Get` 往返、空列表 |
| Hello 能力 | `peer_connection.go` | Tag **0xFA** / **0xFE** → `remotePeerInfo.Misc1` / `Misc2` |
| 下载侧发送 | `peer_connection.go` | `SetHashSet` 之后、`SendStartUpload` 前调用 `maybeSendRequestSources2`；需对端 `SourceExchange1Ver > 0`；**每连接仅发送一次** |
| 接收与合并 | `peer_connection.go` | `HandleRequestSources2` / `HandleAnswerSources2` |
| 候选来源 | `policy.go` | `PeersForSourceExchange(exclude, limit)`，`SourceExchangePeerLimit = 50` |
| 合并策略 | `policy_source_exchange.go` | `MergeSourceExchangePeers` |
| 来源标志 | `peer_info.go` | `PeerSourceExchange`（`0x10`），日志标签 `"sx"` |
| 集成测试 | `source_exchange_loop_test.go` | Policy 排除/limit、Answer 解包与合并 |

## 运行时行为摘要

- **发送请求**：在已确认对端分片位图并完成 `SetHashSet` 后发起；若对端 Hello 未声明来源交换能力（`SourceExchange1Ver == 0`），不发送。
- **处理请求**：在存在 **`Transfer` 上传上下文**（本连接正在为该 hash 提供上传）时，从 `policy` 选取可连接、非当前 socket、非局域网/无效地址的 peer，组装 **v4** 应答；若无候选来源，**不发送空包**（与 aMule 常见行为一致）。**仅 SharedFile 上传**而无 `Transfer.policy` 时不应答。
- **处理应答**：hash 与当前下载任务一致时，将条目转为 `Peer`（`Connectable: true`，`SourceFlag` 含 `PeerSourceExchange`），过滤后与本地/对端/回环冲突的 endpoint，再 `MergeSourceExchangePeers`。

## 测试

- `go test ./protocol/client -run Source`：协议层 golden 与往返。
- `go test . -run 'SourceExchange|PeersFor|AnswerSources2'`：goed2k 包内 Policy 与合并路径。

## 与 KADV6 / IPv6 的关系

- **[protocol/kadv6](../protocol/kadv6)** 实现的是 **KADV6 的 UDP** 报文与节点/搜索条目（含 [`EndpointV6`](../protocol/kadv6/types.go)、[`SearchEntry.SourceAddr()`](../protocol/kadv6/types.go) 提取 **IPv6 TCP** 源）。**不包含** EMule 的 TCP 扩展操作码；Source Exchange 仍只在 **TCP + `OP_EMULEPROT`** 上收发。
- **分层**：KADV6 负责 IPv6 DHT 上的发现/发布；与对端建立 ED2K **TCP** 连接后，仍走同一套 `PeerConnection` → `RequestSources2` / `AnswerSources2` → `Policy`。
- **AnswerSources2 与 IPv4/v5 IPv6**：
  - **v4（版本 4）**：`UserID` 为 **IPv4 hybrid `uint32`**，与 aMule 一致。
  - **v5（goed2k 扩展，版本 5）**：在 v4 条目后追加 **16 字节 IPv6**；纯 IPv6 来源使用 `UserID=0` 且填充 IPv6 字段。对端需在 Hello 标签 **`0x3B`** 声明 `SourceExchange2Ver >= 5` 才会收到 v5 应答。
  - **应答侧**（[`buildSourceExchangeEntries`](../peer_connection.go)）：v4 时仅广播可映射为 IPv4 的来源；v5 时额外包含纯 IPv6 `Peer.DialAddr`。
  - **解析侧**：v5 应答合并为带 `DialAddr` 的 `Peer`，由 `PeerConnection.Connect` 优先拨号。
- **双栈拨号**：`Peer` 可选携带 [`DialAddr *net.TCPAddr`](../peer.go)（例如由 [`PeerFromKADV6SearchEntry`](../peer_kadv6.go) 从 KADV6 `SearchEntry` 构造）；`PeerConnection.Connect` 优先使用该地址拨号，以便在 **`protocol.Endpoint` 仍为 IPv4-only** 的前提下连接 **IPv6 TCP** 对端。详见 [kadv6-implementation-plan.md](kadv6-implementation-plan.md) 第 10 节。

## 参考源码（aMule）

- `src/DownloadClient.cpp`：独立 `OP_REQUESTSOURCES2` 包载荷。
- `src/PartFile.cpp`：`CreateSrcInfoPacket`、`AddClientSources`（SX2 分支）。
- `src/ClientTCPSocket.cpp`：`case OP_ANSWERSOURCES2`。

（eMule 官方树中名称类似，操作码与 `opcodes.h` 中 `OP_REQUESTSOURCES2` / `OP_ANSWERSOURCES2` 一致。）
