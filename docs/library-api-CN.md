# goed2k 库 API 使用指南

本文面向在应用中**嵌入** `github.com/goed2k/core` 的开发者，说明典型调用顺序、主要类型与常用方法。协议细节见仓库内其他专题文档。

## 导入与模块路径

```go
import "github.com/goed2k/core"
```

包名默认为 `goed2k`（与目录 `core` 无关，以 `go.mod` 中 `module` 为准）。

## 核心概念

| 概念 | 说明 |
|------|------|
| `Client` | 高层入口：监听、连接服务器、任务、DHT、搜索、状态持久化、事件订阅。 |
| `Session` | 会话与传输内核；多数场景通过 `client.Session()` 访问配置与底层能力。 |
| `TransferHandle` | 单个下载任务的句柄；由 `AddLink` / `AddTransfer` / `FindTransfer` 等返回。 |
| `Settings` / `NewSettings` | 监听端口、客户端标识、连接与队列限制等；`NewClient(settings)` 前配置。 |

## 推荐生命周期

1. `settings := goed2k.NewSettings()`，按需修改端口、名称等。  
2. `client := goed2k.NewClient(settings)`。  
3. 可选：`client.SetStatePath("state.json")` 或 `SetStateStore(...)`，再 `LoadState` 恢复任务。  
4. `client.Start()` — 启动内部循环（监听、定时器、I/O）。  
5. `Connect` / `ConnectServers` 连接 ED2K 服务器；若用 DHT：`EnableDHT()`、`LoadDHTNodesDat`、`AddDHTBootstrapNodes` 等。  
6. `AddLink` / `AddTransfer` 添加任务。  
7. 通过 `Status()`、`SubscribeStatus`、`SubscribeTransferProgress` 或 `TransferHandle` 查询/订阅。  
8. 退出：`Stop()` 或 `Close()`；`Wait()` 会阻塞至所有传输结束或客户端停止。

**注意**：`Start()` 之后才能可靠连接服务器、添加任务；未 `Start` 时部分操作可能无效。

## Client：常用 API 分组

### 启动与停止

| 方法 | 作用 |
|------|------|
| `Start() error` | 启动客户端主循环。 |
| `Stop() error` | 停止运行。 |
| `Close()` | 释放资源；常与 `defer` 配合。 |
| `Wait() error` | 阻塞；全部任务完成或 `Stop` 后返回（`ErrClientStopped` 表示主动停止）。 |

### ED2K 服务器

| 方法 | 作用 |
|------|------|
| `Connect(serverAddr string) error` | 连接单个服务器 `host:port`。 |
| `ConnectServers(addrs ...string) error` | 连接多个（内部会配置并尝试连接）。 |
| `LoadServerMet(path string) ([]ServerMetEntry, error)` | 仅解析 `server.met`。 |
| `ConnectServerMet(path string) error` | 加载并连接列表中的服务器。 |
| `ConnectServerLink(linkValue string) error` | 从 `serverlist://` 类链接加载并连接。 |
| `ServerAddress() string` | 当前偏好/记录的服务器地址（若有）。 |
| `ConnectSavedServer() error` | 使用已保存地址重连。 |

### 下载任务

| 方法 | 作用 |
|------|------|
| `AddLink(linkValue, outputDir string) (TransferHandle, string, error)` | 解析 `ed2k://` 等链接并创建任务。 |
| `AddTransfer(AddTransferParams) (TransferHandle, error)` | 以参数结构体添加任务（哈希、大小、路径等）。 |
| `FindTransfer(hash) TransferHandle` | 按文件哈希查找句柄（无效时 `IsValid()==false`）。 |
| `Transfers() []TransferHandle` | 当前所有任务句柄。 |
| `PauseTransfer` / `ResumeTransfer` / `RemoveTransfer` | 暂停、继续、删除（可是否删文件）。 |
| `SetTransferUploadPriority` | 设置该任务的上传优先级。 |

### 上传与好友槽

| 方法 | 作用 |
|------|------|
| `SuspendUpload` / `ResumeUpload` | 按文件哈希挂起/恢复上传侧逻辑。 |
| `SetFriendSlot(hash, enabled bool)` | 是否为该**用户 Hash** 保留好友上传槽（会持久化到状态中）。 |

### DHT（Kad）

| 方法 | 作用 |
|------|------|
| `EnableDHT() *DHTTracker` | 启用并返回跟踪器（已启用则返回已有）。 |
| `LoadDHTNodesDat(path ...string) error` | 从 `nodes.dat` 等加载引导节点。 |
| `AddDHTBootstrapNodes(nodes ...string) error` | 添加引导地址。 |
| `DHTStatus() DHTStatus` | 节点数、监听端口等摘要。 |
| `PublishDHTSource` / `PublishDHTKeyword` / `PublishDHTNotes` | 发布源或搜索条目（需已启用 DHT）。 |
| `SearchDHTKeywords` | 按关键字哈希搜索（回调形式）。 |
| `SetDHTStoragePoint(address string) error` | 存储端点相关配置。 |

### 服务器侧搜索（非 DHT 关键词遍历）

| 方法 | 作用 |
|------|------|
| `StartSearch(SearchParams) (SearchHandle, error)` | 启动搜索。 |
| `StopSearch() error` | 停止搜索。 |
| `SearchSnapshot() SearchSnapshot` | 当前搜索结果快照。 |

### 状态快照与事件（UI / 监控）

| 方法 | 作用 |
|------|------|
| `Status() ClientStatus` | **一次性**拉取全局快照：服务器、所有任务的传输状态、对等端列表、汇总速率等。 |
| `PeerStatuses() []ClientPeerSnapshot` | 仅所有 `ClientPeerSnapshot`（等价于 `Status().Peers` 的常用封装）。 |
| `SubscribeStatus() (<-chan ClientStatusEvent, cancel func())` | 周期性状态事件（带缓冲 channel）；`cancel` 取消订阅。 |
| `SubscribeStatusBuffered(n int)` | 同上，自定义缓冲长度。 |
| `SubscribeTransferProgress()` / `SubscribeTransferProgressBuffered` | 仅在有进度/状态变化时推送任务子集。 |

`ClientStatusEvent` 可调用 `TransferSnapshots()`、`TransferState(hash)` 等解析快照。

### 状态持久化

| 方法 | 作用 |
|------|------|
| `SetStatePath(path string)` | 使用内置 JSON 文件存储。 |
| `SetStateStore(ClientStateStore)` | 自定义存储（需实现 `Load`/`Save`）。 |
| `StatePath() string` | 当前文件路径（若使用文件存储）。 |
| `LoadState(path string) error` | 从路径加载（与 `SetStatePath` 配合）。 |
| `SaveState(path string) error` | 保存到路径。 |
| `SetAutoSaveInterval(d time.Duration)` | 自动保存间隔（内部循环触发）。 |

### 其他

| 方法 | 作用 |
|------|------|
| `Session() *Session` | 访问会话层（监听端口、积分、共享目录等）。 |
| `GetDHTTracker` / `SetDHTTracker` | 获取或注入 `DHTTracker`（高级用法）。 |

## TransferHandle

在持有有效句柄（`IsValid()==true`）时可调用，例如：

- `GetHash` / `GetSize` / `GetFilePath` / `GetCreateTime`
- `Pause` / `Resume` / `Remove`
- `GetStatus` / `GetPeersInfo` — 任务级状态与**当前已连接对等端**列表
- `IsPaused` / `IsFinished` / …（见源码 `transfer_handle.go`）

**对等端信息**：`GetPeersInfo() []PeerInfo` 中每条包含速率、Endpoint、`UserHash`、`NickName`（Hello 解析）、`ModName` / `Version` / `ModVersion` / `StrModVersion`、`Connected`、与该用户累计上下传 `TotalUploaded` / `TotalDownloaded`（与积分一致），以及 Hello 标签 **0xFA / 0xFE** 对应的原始值 `HelloMisc1` / `HelloMisc2`（与 `MiscOptions` / `MiscOptions2` 位域一致）等。

## Session：常见用法

典型用于展示本机信息或积分：

```go
s := client.Session()
port := s.GetListenPort()
name := s.GetClientName()
up, down := s.Credits().TotalsForPeer(peerUserHash)
```

- `Credits()` 返回 `*PeerCreditManager`；`TotalsForPeer(protocol.Hash)` 为与该用户 Hash 的累计上传/下载字节。  
- 监听、UPnP 等多在 `Client.Start` 内部驱动；高级集成可直接使用 `Session` 上导出方法（见 `session.go`）。

## 快照类型速览

- `ClientStatus`：`Servers`、`Transfers`（含每任务 `Peers []PeerInfo`）、`Peers`（扁平的所有对等端）、总进度与速率等。  
- `TransferSnapshot`：单任务文件名、大小、`Status`、`Peers`、`Pieces` 等。  
- `PeerInfo`：见上一节；适合用来做列表 UI 或日志。  
- `ServerSnapshot`：某台 ED2K 服务器的连接与统计信息。  
- `DHTStatus`：Kad 节点规模、是否 firewalled 等（**不是**「ED2K 好友列表」）。

## 示例程序

仓库内 `examples/` 目录：

| 目录 | 说明 |
|------|------|
| `examples/basic` | 最小下载示例。 |
| `examples/multi` | 多任务/多服务器思路。 |
| `examples/status` | 状态相关用法。 |
| `examples/state_store` | 自定义 `ClientStateStore`。 |

## 与其它文档的关系

- [Source Exchange](source-exchange-CN.md)、[Kad v6](kadv6-protocol-CN.md) 等：协议与实现细节。  
- [库能力分阶段说明](library-implementation-phases-CN.md)：路线图与边界。  

若你发现 API 与源码不一致，以 **`go doc` 与当前分支源码为准**，并欢迎提 Issue/PR 更新本文档。
