# KADV6 协议草案

## 1. 范围

本文档定义 `goed2k` 中一个隔离的、仅支持 IPv6 的 DHT 覆盖网络 `KADV6`。

目标：

- 允许 IPv6 节点完成 bootstrap、节点发现、元数据发布和元数据搜索
- 在逻辑上尽量接近经典 Kad 的路由模型
- 不要求与现有 `kad4` 实现进行二进制兼容
- 保持第一版协议足够小，便于分阶段实现和测试

非目标：

- 不做 `kad4 <-> kadv6` 桥接
- 不做 `kadv6 -> kad4` 传播
- 不在 `kadv6` 中支持 IPv4 传输
- 不直接复用经典 Kad 的报文格式
- 不做文件传输中继或代理

`KADV6` 是一个独立覆盖网络。节点可以只加入 `kad4`、只加入 `kadv6`，或者同时各自运行独立 tracker 加入两个网络，但第一版中两个 overlay 不交换状态。

## 2. 传输

- 传输层：仅 `UDP/IPv6`
- 监听网络：`udp6`
- 所有多跳 DHT 流量都使用 UDP
- 所有节点地址都必须是原生 IPv6 地址
- 必须拒绝 IPv4-mapped IPv6 地址

实现规则：

- 仅接受满足 `ip.To16() != nil` 且 `ip.To4() == nil` 的地址

## 3. 线协议约定

- 字节序：除非特别说明，否则全部使用 little-endian
- Hash 长度：16 字节
- 报文封装：`header(1) + opcode(1) + payload(n)`

包头：

| 字段 | 大小 | 值 |
|---|---:|---|
| `ProtocolHeader` | 1 byte | `0xE6` |
| `Opcode` | 1 byte | 见第 8 节 |

协议版本：

- `KADV6Version = 0x01`

版本号会出现在 `Hello`、`BootstrapRes` 等特定 payload 中。

## 4. 核心模型

### 4.1 Node ID

- 长度：128 bit
- 类型：与现有 `protocol.Hash` 的逻辑长度一致
- 路由距离：XOR distance

`KADV6` 继续沿用 128 bit Kad 路由模型。bucket 划分和遍历逻辑可以在思想上接近现有 Kad 实现，但地址和报文编码是完全独立的。

### 4.2 EndpointV6

`KADV6` 中所有可路由联系人都使用 `EndpointV6`。

二进制布局：

| 字段 | 大小 | 说明 |
|---|---:|---|
| `IP` | 16 bytes | 原始 IPv6 地址字节 |
| `UDPPort` | 2 bytes | DHT 使用的 UDP 端口 |
| `TCPPort` | 2 bytes | Peer 流量或服务使用的 TCP 端口 |

Go 结构建议：

```go
type EndpointV6 struct {
    IP      [16]byte
    UDPPort uint16
    TCPPort uint16
}
```

校验规则：

- `IP` 不能是未指定地址 `::`
- `IP` 不能是 IPv4-mapped 地址
- 对于可路由联系人，`UDPPort` 必须非零

字符串形式：

- IPv6 文本显示建议使用 RFC 5952 规范形式
- endpoint 展示格式建议为 `[ipv6]:port`

### 4.3 EntryV6

二进制布局：

| 字段 | 大小 |
|---|---:|
| `ID` | 16 bytes |
| `Endpoint` | 20 bytes |
| `Version` | 1 byte |
| `Flags` | 1 byte |

Flags：

- bit `0`：`Verified`
- bit `1-7`：保留，版本 1 中必须为 0

Go 结构建议：

```go
type EntryV6 struct {
    ID       ID
    Endpoint EndpointV6
    Version  byte
    Verified bool
}
```

## 5. 搜索结果模型

`KADV6` 保留 Kad 风格的搜索结果模型，但扩展 tag 系统，使其可以正确表达 IPv6 source。

### 5.1 SearchEntry

二进制布局：

| 字段 | 大小 |
|---|---:|
| `ID` | 16 bytes |
| `TagCount` | 1 byte |
| `Tags` | variable |

Go 结构建议：

```go
type SearchEntry struct {
    ID   ID
    Tags []Tag
}
```

### 5.2 Tag 编码

每个 tag 都包含显式的值类型和 tag 标识符。

二进制布局：

| 字段 | 大小 | 说明 |
|---|---:|---|
| `Type` | 1 byte | 见第 5.3 节 |
| `ID` | 1 byte | 语义字段 id |
| `Value` | variable | 取决于 `Type` |

对于定长整数类型，`Value` 直接写入。

对于 `string` 和 `bytes`，`Value` 编码为：

| 字段 | 大小 |
|---|---:|
| `Length` | 2 bytes |
| `RawBytes` | `Length` |

这与 legacy Kad tag 格式有意不同。版本 1 优先选择更简单的解码器，而不是二进制兼容。

### 5.3 Tag 类型

| 类型名 | 代码 | 含义 |
|---|---:|---|
| `TagTypeString` | `0x02` | UTF-8 字符串 |
| `TagTypeUint32` | `0x03` | 32 位无符号整数 |
| `TagTypeUint16` | `0x08` | 16 位无符号整数 |
| `TagTypeUint8` | `0x09` | 8 位无符号整数 |
| `TagTypeUint64` | `0x0B` | 64 位无符号整数 |
| `TagTypeBytes` | `0x0C` | 带长度前缀的原始字节 |

### 5.4 Tag ID

预留的 source 相关 tag id：

| Tag ID | 名称 | 类型 | 含义 |
|---|---:|---|---|
| `0xEE` | `TagAddrFamily` | `uint8` | 地址族，`kadv6` 中固定为 `6` |
| `0xF0` | `TagSourceIP6` | `bytes` | 16 字节 IPv6 source 地址 |
| `0xFC` | `TagSourceUDPPort` | `uint16` | source 对外声明的 UDP 端口 |
| `0xFD` | `TagSourcePort` | `uint16` | source 的 TCP 端口 |
| `0xFF` | `TagSourceType` | `uint8` | source 类型 |
| `0xD3` | `TagFileSize` | `uint64` | 文件大小 |
| `0x01` | `TagName` | `string` | 关键词或文件名 |

Source type 取值：

| 值 | 含义 |
|---:|---|
| `1` | direct source |
| `4` | store/publish source |

版本 1 中仅使用地址族 `6`。

### 5.5 Source 提取规则

当 `SearchEntry` 同时满足以下条件时，表示一个直接可用的 IPv6 source：

- `TagAddrFamily = 6`
- `TagSourceIP6` 长度恰好为 16 字节
- `TagSourcePort` 非零

`TagSourceUDPPort` 是可选字段。

建议提供辅助方法：

```go
func (s SearchEntry) SourceAddr() (*net.TCPAddr, bool)
```

## 6. 路由与查找规则

`KADV6` 使用标准 Kad 逻辑模型：

- 128 bit node id
- XOR distance
- k-buckets
- 迭代查找
- bootstrap、hello、find-node、publish、search

推荐初始参数：

| 参数 | 值 |
|---|---:|
| `K` bucket 大小 | `10` |
| 搜索并行因子 `alpha` | `5` |
| RPC 短超时 | `2s` |
| RPC 总超时 | `12s` |
| bucket refresh 间隔 | `15m` |
| bootstrap 重试间隔 | `30s` |

这些值有意与当前实现风格尽量接近，便于保持运行时行为一致。

## 7. Nodes 文件格式

`KADV6` 不复用 legacy `nodes.dat`。

推荐文件名：

- `nodes6.dat`

二进制布局：

| 字段 | 大小 | 说明 |
|---|---:|---|
| `Magic` | 4 bytes | ASCII `KD6N` |
| `Version` | 4 bytes | 当前值 `1` |
| `BootstrapEdition` | 4 bytes | `0` 表示普通节点列表，`1` 表示 router/bootstrap 列表 |
| `ContactCount` | 4 bytes | 联系人数 |
| `Contacts` | variable | 重复的 `EntryV6` |

校验规则：

- `Magic` 必须是 `KD6N`
- `Version` 必须是受支持的版本
- 每个 contact 都必须是合法 IPv6 endpoint
- 重复 endpoint 可以被忽略

这个格式是有意设计成自描述、易扩展的，不兼容旧格式。

## 8. 操作码

版本 1 使用以下 opcode 映射：

| Opcode | 名称 |
|---:|---|
| `0x01` | `BootstrapReq` |
| `0x09` | `BootstrapRes` |
| `0x11` | `HelloReq` |
| `0x19` | `HelloRes` |
| `0x21` | `FindNodeReq` |
| `0x29` | `FindNodeRes` |
| `0x33` | `SearchKeysReq` |
| `0x34` | `SearchSourcesReq` |
| `0x35` | `SearchNotesReq` |
| `0x3B` | `SearchRes` |
| `0x43` | `PublishKeysReq` |
| `0x44` | `PublishSourcesReq` |
| `0x45` | `PublishNotesReq` |
| `0x4B` | `PublishRes` |
| `0x60` | `Ping` |
| `0x61` | `Pong` |

说明：

- opcode 值尽量贴近经典 Kad，便于维护者理解
- 但 payload 是 `KADV6` 自己的格式，不能交给 legacy Kad 解码器处理

## 9. 报文定义

### 9.1 BootstrapReq

Payload：

- 空

用途：

- 向一个已知 bootstrap 节点请求初始联系人列表

### 9.2 BootstrapRes

Payload：

| 字段 | 大小 |
|---|---:|
| `ID` | 16 bytes |
| `TCPPort` | 2 bytes |
| `Version` | 1 byte |
| `Count` | 2 bytes |
| `Contacts` | variable，重复 `EntryV6` |

规则：

- `Count` 应由实现限制，建议最大值 `20`

### 9.3 HelloReq / HelloRes

Payload：

| 字段 | 大小 |
|---|---:|
| `ID` | 16 bytes |
| `TCPPort` | 2 bytes |
| `Version` | 1 byte |
| `TagCount` | 1 byte |
| `Tags` | variable |

当前版本没有强制要求的 tag，`TagCount` 可以为 `0`。

用途：

- 确认节点身份
- 声明 TCP 服务端口
- 为后续扩展预留能力，不改固定头部

### 9.4 FindNodeReq

Payload：

| 字段 | 大小 |
|---|---:|
| `SearchType` | 1 byte |
| `Target` | 16 bytes |
| `Receiver` | 16 bytes |

规则：

- node lookup 场景下 `SearchType` 必须为 `0x0B`
- `Receiver` 是发送方当前已知的目标 peer id；如果未知可填 zero-id

### 9.5 FindNodeRes

Payload：

| 字段 | 大小 |
|---|---:|
| `Target` | 16 bytes |
| `Count` | 2 bytes |
| `Results` | variable，重复 `EntryV6` |

### 9.6 SearchSourcesReq

Payload：

| 字段 | 大小 |
|---|---:|
| `Target` | 16 bytes |
| `StartPos` | 2 bytes |
| `Size` | 8 bytes |

用途：

- 按文件 hash 和可选 size 提示查询 source

### 9.7 SearchKeysReq

Payload：

| 字段 | 大小 |
|---|---:|
| `Target` | 16 bytes |
| `StartPos` | 2 bytes |

### 9.8 SearchNotesReq

Payload：

| 字段 | 大小 |
|---|---:|
| `Target` | 16 bytes |
| `StartPos` | 2 bytes |

### 9.9 SearchRes

Payload：

| 字段 | 大小 |
|---|---:|
| `Source` | 16 bytes |
| `Target` | 16 bytes |
| `Count` | 2 bytes |
| `Results` | variable，重复 `SearchEntry` |

规则：

- 每包推荐最大 entry 数：`50`
- UDP 发送前推荐总包长限制：约 `8 KiB`

### 9.10 PublishSourcesReq

Payload：

| 字段 | 大小 |
|---|---:|
| `FileID` | 16 bytes |
| `Source` | variable，一个 `SearchEntry` |

必需 source tag：

- `TagAddrFamily = 6`
- `TagSourceIP6`
- `TagSourcePort`
- `TagSourceType`

可选 source tag：

- `TagSourceUDPPort`
- `TagFileSize`

### 9.11 PublishKeysReq

Payload：

| 字段 | 大小 |
|---|---:|
| `KeywordID` | 16 bytes |
| `Count` | 2 bytes |
| `Sources` | variable，重复 `SearchEntry` |

### 9.12 PublishNotesReq

Payload：

| 字段 | 大小 |
|---|---:|
| `FileID` | 16 bytes |
| `Count` | 2 bytes |
| `Notes` | variable，重复 `SearchEntry` |

### 9.13 PublishRes

Payload：

| 字段 | 大小 |
|---|---:|
| `FileID` | 16 bytes |
| `Count` | 1 byte |

用途：

- 确认一个或多个 entry 已被存储

### 9.14 Ping

Payload：

- 空

### 9.15 Pong

Payload：

| 字段 | 大小 |
|---|---:|
| `UDPPort` | 2 bytes |

用途：

- 最基础的存活探测
- 确认对端当前使用的 UDP 端口

## 10. 发布与搜索语义

### 10.1 Source Publish

节点将自己发布为 source 时，应当：

1. 使用文件 hash 作为 `FileID`
2. 发布一个包含自身 IPv6 地址和 TCP 端口的 `SearchEntry`
3. 如果已知文件大小，带上 `TagFileSize`
4. 在向最近节点发送前，先本地存储一份

版本 1 中只存储直接 IPv6 source。

### 10.2 Keyword Publish

Keyword entry 是以 keyword hash 为 key 的应用层元数据记录。

最小元数据要求：

- `TagName`

后续可以通过新增 tag 扩展更多字段。

### 10.3 Notes Publish

Notes 是以文件 hash 为 key 的附加元数据记录。

版本 1 中 notes 保持为通用 `SearchEntry` 结构。

### 10.4 Search 行为

节点执行搜索时应当：

1. 当没有 live node 时先尝试 bootstrap
2. 从最近的已验证节点开始
3. 以 `alpha` 并行度执行迭代查询
4. 对结果做语义去重
5. 当遍历收敛或超时后停止

推荐去重键：

- source：`[ipv6]:tcpPort`
- keyword entry：`entry.ID + ":" + TagName`
- notes：`entry.ID`，若带来源 endpoint 则可用 `entry.ID + source endpoint`

## 11. 状态与持久化

建议持久化以下运行时状态：

- self node id
- known nodes
- router nodes
- last bootstrap time
- last refresh time
- source index
- keyword index
- notes index

推荐持久化节点文本格式：

- `"[2001:db8::1]:4665"`

`KADV6` 状态中不应持久化 IPv4 地址。

## 12. 校验规则

`KADV6` 实现必须拒绝：

- header 不等于 `0xE6` 的报文
- 不支持的 opcode
- 格式错误的长度前缀 tag
- 长度不等于 16 字节的 `TagSourceIP6`
- 使用 IPv4 或 IPv4-mapped 地址的 contact
- `TagAddrFamily != 6` 的 source 记录
- 超过实现安全上限的报文

推荐安全上限：

| 项目 | 限制 |
|---|---:|
| 接收 UDP 包大小 | `8192` bytes |
| 单个路由响应 contact 数 | `20` |
| 单个搜索响应 entry 数 | `50` |
| 单个搜索 entry 的 tag 数 | `64` |

## 13. 兼容性声明

`KADV6` version 1 与以下内容均不兼容：

- 经典 eMule Kad
- aMule Kad
- 当前 `goed2k/protocol/kad` 实现

以下方面是刻意不同的：

- 协议头
- endpoint 大小
- nodes 文件格式
- 变量长度 bytes 的 tag 编码
- source 地址表示方式

这是设计目标的一部分。`KADV6` 是新 overlay，不是在经典 Kad 上做兼容补丁。

## 14. `goed2k` 中的实现映射建议

建议的包和文件布局：

- `protocol/kadv6/types.go`
- `protocol/kadv6/packets.go`
- `protocol/kadv6/packet_combiner.go`
- `kadv6_tracker.go`
- `kadv6_node.go`
- `kadv6_routing.go`
- `kadv6_rpc.go`
- `kadv6_traversal.go`

建议的 tracker API：

```go
type KADV6Tracker struct{}

func NewKADV6Tracker(listenPort int, timeout time.Duration) *KADV6Tracker
func (t *KADV6Tracker) Start() error
func (t *KADV6Tracker) Close()
func (t *KADV6Tracker) AddNode(addr *net.UDPAddr)
func (t *KADV6Tracker) LoadNodesDat(path string) error
func (t *KADV6Tracker) SearchSources(hash protocol.Hash, size int64, cb func([]kadv6.SearchEntry)) bool
func (t *KADV6Tracker) SearchKeywords(hash protocol.Hash, cb func([]kadv6.SearchEntry)) bool
func (t *KADV6Tracker) PublishSource(hash protocol.Hash, endpoint *net.TCPAddr, size int64) bool
```

`PublishSource` 应直接接收原生 IPv6 地址类型，不应复用当前 IPv4-only 的 `protocol.Endpoint`。

## 15. 待定问题

以下内容故意留给后续版本处理：

1. 是否增加经过签名的 publish record 或认证 node id
2. 是否对大搜索结果包做压缩
3. 是否定义更丰富的 keyword / notes tag schema
4. 是否在 `Hello` 中增加 capability flags
5. 是否支持仅用于测试的 scoped local 地址
6. 未来是否需要定义 `kad4 -> kadv6` 的 search bridge

## 16. 总结

`KADV6` version 1 是一个隔离的、原生 IPv6 的 Kad 风格 DHT，具备：

- 仅使用 UDP/IPv6 传输
- 128 bit node id 和 XOR 路由
- 16 字节 IPv6 endpoint
- 独立的 `nodes6.dat`
- 显式支持 IPv6 的 source tag
- 不桥接、不兼容 legacy Kad

这个边界足够清晰，可以把第一版实现控制在较小、可测试、可迭代的范围内，并且符合当前目标：IPv6 节点只与 IPv6 节点通过 `kadv6` 交互，不涉及 `kad4`。
