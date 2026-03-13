# ED2K Downloader Protocol Specification

Status: Informational / Implementation Profile  
Target: downloader-oriented ED2K/eMule client implementation  
Reference implementation: `github.com/monkeyWie/goed2k`

## 0. Document Scope

这份文档描述的是一个“可实际工作的 ED2K 下载器实现剖面”，不是完整 eMule 生态的全量 RFC。

它重点说明：

- 一个下载器最少需要实现哪些协议流
- 关键数据结构如何切分和校验
- server、peer、piece、block 之间如何协作
- 实现时最容易踩坑的编码和状态机细节

如果你想快速理解这个仓库的协议基础，建议阅读顺序是：

1. `Overall Architecture`
2. `Data Model`
3. `Wire Encoding Rules`
4. `Server Channel`
5. `Peer Channel`
6. `Piece / Disk / Verification`

## 1. Purpose

本文档定义一个可互操作的 ED2K 下载器实现规范，范围覆盖：

- `ed2k://` 文件链接解析
- 通过 ED2K server 获取文件来源
- 与 peer 建立 eDonkey/eMule TCP 会话
- 完成文件元数据协商、分片请求、数据接收、落盘与哈希校验
- 连接管理、超时、重试、进度统计

本文档不覆盖完整网络生态中的全部功能，尤其不覆盖：

- 关键字搜索
- 文件发布/上传策略
- 完整 Kad DHT 协议
- 评论、预览、AICH、安全身份认证等增强功能

但本文档覆盖的内容，已经足够实现一个实际可用的 ED2K 下载器。

## 2. Terminology

- `client`: 下载器本地节点。
- `server`: ED2K 服务器，只负责目录和 source 分发，不直接传输文件内容。
- `peer`: 持有文件或部分文件的远端客户端。
- `file hash`: 整个文件的 128-bit MD4 哈希。
- `piece`: 文件逻辑分片，固定大小 `9728000` 字节，最后一个 piece 可变长。
- `block`: piece 内的调度单元，固定大小 `190 KiB = 194560` 字节，最后一个 block 可变长。
- `hash set`: 所有 piece 的 MD4 哈希数组。
- `source`: 一个可连接的 peer endpoint。

文中使用 `MUST`、`SHOULD`、`MAY` 时采用 RFC 风格语义。

## 3. Overall Architecture

ED2K 下载链路由两条独立 TCP 通道组成：

1. `client <-> server`
   - 登录
   - 获取文件来源
   - 获取自身 `client id`
   - 可选 server ping / callback

2. `client <-> peer`
   - 握手
   - 文件确认
   - piece 拥有情况交换
   - piece hash set 校验
   - 请求 block
   - 接收原始或压缩数据

最小下载流程如下：

1. 解析 `ed2k://|file|...|/`
2. 新建 transfer，打开目标文件
3. 连接一个 ED2K server
4. 发送 `LoginRequest`
5. 收到 `IdChange`
6. 发送 `GetFileSources`
7. 收到 `FoundFileSources`
8. 与若干 peer 建立 TCP 连接
9. 执行 peer 握手和文件协商
10. 请求 3 个 block
11. 接收 `SendingPart` 或 `CompressedPart`
12. 异步写盘
13. 当 piece 全部 block 到齐后计算 piece MD4
14. piece 校验通过则标记完成，直到整文件完成

## 4. Data Model

### 4.1 File Link Format

文件链接格式：

```text
ed2k://|file|<name>|<size>|<md4>|/
```

字段定义：

- `name`: URL decode 后的文件名
- `size`: 十进制文件大小，单位字节
- `md4`: 32 个十六进制字符，对应 16 字节文件哈希

实现必须进行 URL decode，然后按 `|` 分段解析。

### 4.2 Piece and Block Layout

常量：

- `PieceSize = 9728000`
- `BlockSize = 194560`
- `BlocksPerPiece = PieceSize / BlockSize = 50`
- `RequestQueueSize = 3`
- `PartsInRequest = 3`

文件逻辑上切为若干 piece：

- 非最后 piece 固定 `9728000` 字节
- 最后 piece 长度为 `size % PieceSize`，若整除则仍视为完整 piece

piece 内再切 block：

- 非最后 block 固定 `194560` 字节
- 最后 block 长度由文件末尾截断

block 的绝对偏移定义为：

```text
absolute_offset = piece_index * PieceSize + block_index * BlockSize
```

请求范围使用半开区间：

```text
[begin, end)
```

### 4.3 Hash Model

ED2K 下载必须基于 MD4。

规则如下：

- 单 piece 文件：
  - `file hash = MD4(file bytes)`
  - `hash set = [file hash]`

- 多 piece 文件：
  - `piece hash[i] = MD4(piece_i_bytes)`
  - `file hash = MD4(piece_hash[0] || piece_hash[1] || ... )`

其中 `||` 表示字节串拼接。

下载器必须在接收完整 piece 后计算 piece MD4，并与远端 `HashSetAnswer` 中对应项比较。

## 5. Wire Encoding Rules

### 5.1 Endianness

除 `Hash` 和原始 byte container 外，所有整数均采用 `little-endian`。

基础类型：

- `u8`
- `u16`
- `u32`
- `u64`
- `i32`
- `Hash = 16 bytes`
- `Endpoint = i32(ip) + u16(port)`

### 5.2 Endpoint Encoding

endpoint 在网络上传输为 6 字节：

```text
4 bytes little-endian IPv4
2 bytes little-endian port
```

例如 IP `1.2.3.4` 在本实现中的 wire 形式是按 `little-endian int32` 编码。

### 5.3 Packet Header

所有 TCP 报文统一使用 6 字节头：

```text
0      1      5      6
+------+------+------+ 
| Prot | Size | Op   |
+------+------+------+
 1 byte 4 byte 1 byte
```

字段定义：

- `Prot`: 协议族
- `Size`: `Op + Body` 的总字节数，不包含 `Prot` 与 `Size` 自身
- `Op`: opcode

因此：

```text
body_length = Size - 1
```

### 5.4 Protocol IDs

- `0xE3`: `EdonkeyHeader` / `EdonkeyProt`
- `0xC5`: `EMuleProt`
- `0xD4`: `PackedProt`, body 经过 zlib 压缩
- `0xE4`: `KademliaHeader`
- `0xE5`: `KadCompressedUDP`

下载器必须至少支持：

- `0xE3`
- `0xC5`
- `0xD4`

### 5.5 Packed Frames

当 `Prot = 0xD4` 时，header 后的 body 为 zlib 压缩数据。

解包规则：

1. 先按 header 读取 `body_length`
2. 对 body 执行 zlib 解压
3. 用相同 opcode 查找 packet handler
4. 优先按 `EMuleProt` 映射，找不到再回退到 `EdonkeyProt`

### 5.6 Service Header vs Payload

某些 packet 的 body 不是单纯结构体，而是：

```text
service fields + raw payload
```

下载器必须特殊处理如下报文：

- `SendingPart32`
- `SendingPart64`
- `CompressedPart32`
- `CompressedPart64`

其反序列化流程是：

1. 先读取固定长度 service header
2. 剩余字节视为该 packet 的 payload
3. payload 可能还需要进一步 zlib 解压

## 6. Tag Encoding

tag 用于 `LoginRequest`、`HelloAnswer`、`ExtHello` 等场景。

### 6.1 Tag Wire Format

本实现支持的最小 tag 集合如下：

- `0x02`: string
- `0x03`: uint32
- `0x11..0x20`: short string inline，长度为 `type - 0x11 + 1`

tag 编码：

```text
tag_type_with_has_id
tag_id
tag_value
```

其中最高位 `0x80` 表示显式携带 `tag_id`。

### 6.2 Tag List

tag list 编码为：

```text
u32 count
tag[0]
tag[1]
...
```

### 6.3 Common Tag IDs Used by This Profile

server login / hello 中常用 tag：

- `0x01`: client name
- `0x11`: client version
- `0x20`: server capability flags
- `0xFA`: misc options 1
- `0xFB`: eMule/amule version encoding
- `0xFE`: misc options 2
- `0x55`: mod name or implementation version

下载器不需要理解所有 tag，但必须能够：

- 正确解析未知 tag
- 保留长度语义
- 至少识别本 profile 发送的 tag 集

## 7. Server Channel

### 7.1 Connection Lifecycle

server 连接状态机：

1. TCP connect
2. 立即发送 `LoginRequest`
3. 等待 `IdChange`
4. `IdChange` 到达后视为 server 握手完成
5. 周期性或按需发送 `GetFileSources`
6. 连接断开时可按策略重连

### 7.2 Server Packet Catalog

| Opcode | Protocol | Direction | Name |
|---|---:|---|---|
| `0x01` | `0xE3` | C->S | `LoginRequest` |
| `0x14` | `0xE3` | C->S | `GetList` |
| `0x19` | `0xE3` | C->S | `GetFileSources` |
| `0x1C` | `0xE3` | C->S | `CallbackRequest` |
| `0x34` | `0xE3` | S->C | `Status` |
| `0x35` | `0xE3` | S->C | `CallbackRequestIncoming` |
| `0x36` | `0xE3` | S->C | `CallbackRequestFailed` |
| `0x38` | `0xE3` | S->C | `Message` |
| `0x40` | `0xE3` | S->C | `IdChange` |
| `0x42` | `0xE3` | S->C | `FoundFileSources` |

### 7.3 LoginRequest

Body:

```text
Hash        16 bytes
Endpoint     6 bytes
u32 tag_count
Tag[tag_count]
```

推荐发送的 tag：

- `ctVersion (0x11)`: `0x3c`
- `ctServerFlags (0x20)`: capability bitmap
- `ctName (0x01)`: client name
- `ctEMuleVersion (0xFB)`: implementation version encoding

本 profile 使用的 capability bit：

- `0x0001`: zlib capable
- `0x0004`: aux port present
- `0x0008`: new tags
- `0x0010`: unicode
- `0x0100`: large files

### 7.4 IdChange

Body:

```text
i32 client_id
[i32 tcp_flags]
[i32 aux_port]
```

后两个字段在部分 server 上可能缺失，因此实现必须允许变长解析。

`client_id` 用于判断 high-id / low-id。若为 low-id，server 可能需要 callback 协助建立连接。

### 7.5 GetFileSources

Body:

```text
Hash file_hash
if large_file:
    i32 0
    i32 low_part
    i32 high_part
else:
    i32 low_part
```

规则：

- 小文件仅发送 `low_part`
- 大文件发送一个额外的 `0` 作为 large-file 标记，再跟 `low/high`

### 7.6 FoundFileSources

Body:

```text
Hash file_hash
u8 source_count
Endpoint[source_count]
```

实现要求：

- 收到 source 后加入本地 peer policy
- 对 low-id source 可直接忽略，或走 callback 机制
- 同一个 source 多次到达时应去重，只合并来源标记

### 7.7 Status / GetList / Message

- `Status`: server 在线用户数和文件数
- `GetList`: 相当于 ping / keepalive
- `Message`: 文本消息，可仅记录日志

### 7.8 Source Refresh Policy

下载器不应只在启动时请求一次 source。

推荐策略：

- 当活跃连接数小于连接上限时，周期性请求 source
- 当 `active == 0` 且无候选 peer 时，应更积极地提前下一次请求
- server 断线后应允许重连

## 8. Peer Channel

### 8.1 Connection Lifecycle

典型流程：

1. 建立 TCP 连接
2. 主动发送 `Hello`
3. 收到 `Hello` 或 `HelloAnswer`
4. 发送 `FileRequest`
5. 收到 `FileAnswer`
6. 发送 `FileStatusRequest`
7. 收到 `FileStatusAnswer`
8. 大文件发送 `HashSetRequest`
9. 收到 `HashSetAnswer`
10. 发送 `StartUpload`
11. 收到 `AcceptUpload`
12. 发送 `RequestParts32/64`
13. 收到 `SendingPart` 或 `CompressedPart`
14. 重复直到 piece / file 完成

### 8.2 Peer Packet Catalog

| Opcode | Protocol | Direction | Name |
|---|---:|---|---|
| `0x01` | `0xE3` | both | `Hello` |
| `0x4C` | `0xE3` | both | `HelloAnswer` |
| `0x01` | `0xC5` | both | `ExtHello` |
| `0x02` | `0xC5` | both | `ExtHelloAnswer` |
| `0x58` | `0xE3` | both | `FileRequest` |
| `0x59` | `0xE3` | both | `FileAnswer` |
| `0x4F` | `0xE3` | both | `FileStatusRequest` |
| `0x50` | `0xE3` | both | `FileStatusAnswer` |
| `0x48` | `0xE3` | both | `NoFileStatus` |
| `0x51` | `0xE3` | both | `HashSetRequest` |
| `0x52` | `0xE3` | both | `HashSetAnswer` |
| `0x54` | `0xE3` | downloader->peer | `StartUpload` |
| `0x55` | `0xE3` | peer->downloader | `AcceptUpload` |
| `0x56` | `0xE3` | both | `CancelTransfer` |
| `0x57` | `0xE3` | peer->downloader | `OutOfParts` |
| `0x60` | `0xC5` | peer->downloader | `QueueRanking` |
| `0x47` | `0xE3` | downloader->peer | `RequestParts32` |
| `0xA3` | `0xC5` | downloader->peer | `RequestParts64` |
| `0x46` | `0xE3` | peer->downloader | `SendingPart32` |
| `0xA2` | `0xC5` | peer->downloader | `SendingPart64` |
| `0x40` | `0xC5` | peer->downloader | `CompressedPart32` |
| `0xA1` | `0xC5` | peer->downloader | `CompressedPart64` |

### 8.3 Hello / HelloAnswer

`Hello` = `u8 hash_length` + `HelloAnswer`

`HelloAnswer` body:

```text
Hash user_hash
Endpoint client_point
TagList properties
Endpoint server_point
```

下载器至少应发送：

- client name
- mod name
- client version
- misc options 1
- misc options 2
- eMule-compatible version tag

远端 `HelloAnswer` 到达后，应立即进入目标文件协商，而不是等待额外 application-level ack。

### 8.4 ExtHello / ExtHelloAnswer

Body:

```text
u8 version
u8 protocol_version
[TagList properties]
```

这是 eMule 扩展握手，用于附加能力协商。  
下载器实现可以仅保留最小 tag 集，但必须能正确收发该报文。

### 8.5 FileRequest

Body:

```text
Hash file_hash
```

语义：声明后续交互针对哪个文件。

### 8.6 FileAnswer

Body:

```text
Hash file_hash
u16 name_len
byte[name_len] file_name
```

远端必须返回相同 `file_hash`。若不匹配，连接应立即关闭。

### 8.7 FileStatusRequest

Body:

```text
Hash file_hash
```

在 eDonkey 传统命名中，此 opcode 常被称为 `SetReqFileID`，但对下载器而言其实是“请求文件状态”。

### 8.8 FileStatusAnswer

Body:

```text
Hash file_hash
BitField remote_pieces
```

bitfield 编码：

```text
u16 bit_count
byte[ceil(bit_count / 8)] raw_bits
```

位序采用最高位在前：

```text
bit 0 => 0x80
bit 1 => 0x40
...
```

下载器必须用它来判断远端是否持有某 piece。

### 8.9 HashSetRequest / HashSetAnswer

`HashSetRequest` body:

```text
Hash file_hash
```

`HashSetAnswer` body:

```text
Hash file_hash
u16 piece_count
Hash[piece_count] piece_hashes
```

下载器收到 `HashSetAnswer` 后必须执行三个校验：

1. `file_hash` 必须等于目标 transfer 的 hash
2. `MD4(piece_hash[0] || piece_hash[1] || ...)` 必须等于 `file_hash`
3. `piece_count` 必须等于本地根据文件大小推导出的 piece 数

任一失败都应关闭连接。

对于单 piece 文件，本 profile 允许直接将 `hash set` 视为只含一个 `file hash` 的数组。

### 8.10 StartUpload / AcceptUpload / QueueRanking

`StartUpload` body:

```text
Hash file_hash
```

语义不是“我要上传”，而是“请你开始向我这个 downloader 提供该文件的数据”。

返回分两种：

- `AcceptUpload`
  - 零长度 body
  - 表示可以进入 block 请求

- `QueueRanking`
  - body 为 `u16 rank`
  - 表示自己被放入上传队列，尚未获准取数

下载器在 `QueueRanking` 场景下可以：

- 保持连接等待
- 也可以关闭后稍后重试

本 profile 选择关闭连接并走其他 peer。

### 8.11 NoFileStatus / OutOfParts / CancelTransfer

- `NoFileStatus`: 远端不存在该文件，必须断开
- `OutOfParts`: 远端没有可供当前请求的分块，必须断开或换源
- `CancelTransfer`: 主动取消当前文件会话

## 9. Part Request and Payload Transfer

### 9.1 RequestParts32

Body:

```text
Hash file_hash
u32 begin[3]
u32 end[3]
```

### 9.2 RequestParts64

Body:

```text
Hash file_hash
u64 begin[3]
u64 end[3]
```

规则：

- 每个请求固定携带 3 组 range
- 未使用的槽位可填零
- 小于 `2^32` 的文件可使用 32 位版本
- 大文件必须使用 64 位版本

每个 range 对应一个 block 的绝对字节区间 `[begin, end)`。

### 9.3 SendingPart32 / SendingPart64

service header:

```text
Hash file_hash
u32/u64 begin
u32/u64 end
```

header 后面直接跟随 raw payload，长度为：

```text
payload_len = end - begin
```

下载器接收流程：

1. 解析 service header
2. 建立 `PeerRequest(begin, end)`
3. 将后续 payload 写入对应 pending block buffer
4. 到齐后提交写盘

### 9.4 CompressedPart32 / CompressedPart64

service header:

```text
Hash file_hash
u32/u64 begin
u32 compressed_length
```

header 后紧跟压缩后的 payload。

规则：

- `compressed_length` 是解压前的目标原始长度
- payload 本身是 zlib 压缩流
- 下载器必须在单个 block 完整收齐后再解压
- 解压结果长度应等于目标 block 的原始长度

### 9.5 Payload-to-Block Mapping

根据 `begin` 反推出 `PieceBlock`：

```text
piece_index = begin / PieceSize
block_index = (begin % PieceSize) / BlockSize
```

同一 pending block 需要记录：

- `expected size`
- `received size`
- `buffer`
- `compressed / uncompressed`

若接收到的数据无法匹配任何 pending block，下载器应丢弃该 payload，并最终重发请求或断开该 peer。

## 10. Downloader State Machine

### 10.1 Transfer State

transfer 最小状态集：

- `LOADING_RESUME_DATA`
- `DOWNLOADING`
- `FINISHED`

暂停和终止是额外布尔状态，不单独作为核心状态。

### 10.2 Per-Piece State

piece 状态：

- `None`
- `Downloading`
- `Have`

block 状态：

- `None`
- `Requested`
- `Writing`
- `Finished`

### 10.3 Piece Picking Rules

推荐策略：

1. 先从已处于 `Downloading` 的 piece 中补足空闲 block
2. 若还不够，再选择新的 piece
3. 每个 peer 一次只保留最多 `3` 个 pending block
4. 在 end-game 场景可允许重复请求少量 block

### 10.4 Peer Selection Rules

本地 peer policy 至少需要维护：

- endpoint
- 是否 connectable
- fail count
- last connected time
- next allowed connection time
- source flags
- 当前连接引用

下载器必须：

- 去重 peer
- 对失败 peer 做指数级或线性退避
- 限制全局 peer 列表大小
- 在活跃连接不足时持续补充新 peer

## 11. Disk Pipeline

### 11.1 Required Flow

网络层和磁盘层必须解耦。

建议流程：

1. 收到完整 block
2. 标记该 block 为 `Writing`
3. 提交异步写盘任务
4. 写盘完成后标记该 block 为 `Finished`
5. 如果整个 piece 已齐，提交异步哈希任务
6. piece MD4 正确则 `WeHave(piece)`
7. 否则恢复 piece 并重新下载

### 11.2 Piece Hash Validation

piece 校验失败时：

- 不应把该 piece 计入完成量
- 应将该 piece 的 block 状态整体回滚
- 应允许从其他 peer 重新请求

### 11.3 Resume Data

resume data 至少需要记录：

- `hash set`
- 已完成 piece bitfield
- 已完成 block 列表

恢复时可以：

- 直接恢复完整 piece 为 `Have`
- 对已落盘但未完成 piece 的 block 做重新注册

## 12. Timeouts and Retry Strategy

下载器至少需要三类超时：

- `server ping timeout`
- `peer idle timeout`
- `connect retry backoff`

推荐：

- peer 在 `N` 秒无接收数据则关闭
- server 周期性 `GetList`
- server 断开后按重连策略回退
- 当前无 active peer 且无 connect candidate 时，立即或短间隔重新获取 source

## 13. Minimal Interoperable Downloader Algorithm

```text
parse ed2k link
open output file
create transfer
connect server
send LoginRequest
wait IdChange

loop:
    if need more sources:
        send GetFileSources

    for each new source:
        connect peer
        send Hello

    for each peer:
        process incoming packets

        Hello/HelloAnswer -> send FileRequest
        FileAnswer -> send FileStatusRequest
        FileStatusAnswer:
            if multi-piece:
                send HashSetRequest
            else:
                synthesize hashset [file_hash]
                send StartUpload
        HashSetAnswer -> validate -> send StartUpload
        AcceptUpload -> request 3 blocks
        SendingPart/CompressedPart -> fill block buffer
        block complete -> async write
        piece complete -> async hash
        piece hash ok -> mark piece have

    refresh progress
    reconnect / retry as needed

stop when all pieces are have
```

## 14. Packet Layout Summary

### 15.1 Server

`LoginRequest`

```text
Hash
Endpoint
u32 tag_count
Tag[tag_count]
```

`GetFileSources`

```text
Hash
i32 low_or_zero
[i32 low]
[i32 high]
```

`FoundFileSources`

```text
Hash
u8 count
Endpoint[count]
```

`IdChange`

```text
i32 client_id
[i32 tcp_flags]
[i32 aux_port]
```

### 15.2 Peer

`Hello`

```text
u8 16
HelloAnswer
```

`HelloAnswer`

```text
Hash
Endpoint
TagList
Endpoint
```

`ExtendedHandshake`

```text
u8 version
u8 protocol_version
[TagList]
```

`FileAnswer`

```text
Hash
u16 name_len
byte[name_len]
```

`FileStatusAnswer`

```text
Hash
BitField
```

`HashSetAnswer`

```text
Hash
u16 piece_count
Hash[piece_count]
```

`RequestParts64`

```text
Hash
u64 begin[3]
u64 end[3]
```

`SendingPart64`

```text
Hash
u64 begin
u64 end
byte[end-begin] payload
```

`CompressedPart64`

```text
Hash
u64 begin
u32 raw_length
byte[...] zlib_payload
```

## 15. Interoperability Notes

- 远端可能同时使用 `0xE3` 与 `0xC5` 两个协议族。
- 某些 server/peer 会发送 `0xD4` 压缩包，下载器必须支持。
- `IdChange` 可能不是固定 12 字节。
- `AcceptUpload` 是零长度 body，不可把零长度当作 EOF。
- `FileStatusRequest` 的名字容易误导，它在老协议里叫 `SetReqFileID`。
- 对大文件必须使用 64 位 `RequestParts` / `SendingPart` / `CompressedPart`。
- 压缩分块必须在完整收齐后再做 zlib 解压。
- 单 piece 文件依然应支持 `HashSetAnswer` 省略的简化路径。

## 16. Security and Robustness Considerations

下载器至少应防御以下问题：

- 非法长度字段导致的内存放大
- bitfield 长度与文件 piece 数不匹配
- hash set 数量不匹配
- payload 越界写入 block buffer
- 未请求数据注入
- 压缩炸弹或异常 zlib 流
- 断连后旧 peer 状态未清理

建议：

- 对 byte container 和 tag 长度设置上限
- 对所有 opcode 建立严格 body 长度检查
- 对每个 pending block 只接受匹配范围的数据
- 对未知包允许跳过，但不得破坏流同步

## 17. Implementation Mapping in This Repository

协议实现入口：

- `ed2k/session.go`
- `ed2k/server_connection.go`
- `ed2k/peer_connection.go`

报文定义：

- `ed2k/protocol/packet_header.go`
- `ed2k/protocol/packet_combiner.go`
- `ed2k/protocol/server/*.go`
- `ed2k/protocol/client/*.go`

调度与落盘：

- `ed2k/transfer.go`
- `ed2k/policy.go`
- `ed2k/piece_picker.go`
- `ed2k/piece_manager.go`
- `ed2k/async_disk.go`

数据模型：

- `ed2k/data/piece_block.go`
- `ed2k/data/peer_request.go`
- `ed2k/protocol/hash.go`
- `ed2k/protocol/bitfield.go`
- `ed2k/protocol/simple_tag.go`

## 18. Conformance Checklist

一个实现若要声称符合本文档，至少必须满足：

- 能解析 `ed2k://|file|...|/`
- 支持 `0xE3`、`0xC5`、`0xD4`
- 支持 server 登录和 source 获取
- 支持 `Hello/FileRequest/FileStatus/HashSet/StartUpload`
- 支持 `RequestParts32/64`
- 支持 `SendingPart32/64` 与 `CompressedPart32/64`
- 使用 MD4 进行 piece/file 校验
- 能处理多 peer 并发下载
- 能在 active peer 归零后重新补 source
- 能把完整文件落盘并校验通过

---

如果只按照本文档实现一个最小 downloader，推荐先实现：

1. `ed2k` link parser
2. TCP framing
3. server 登录和 `FoundFileSources`
4. peer 握手和 `HashSetAnswer`
5. `RequestParts64`
6. `SendingPart64` / `CompressedPart64`
7. piece manager + MD4
8. peer policy + source refresh

完成这 8 步后，即可得到一个能在真实网络中工作的 ED2K 下载器。
