# 本地共享库与周边能力：分阶段实现说明

本文档按**功能演进**归纳仓库中已实现的多阶段工作（与迭代顺序大致对应，便于查阅代码与测试；并非严格的版本发布编号）。

---

## 阶段 0：基线与客户端状态

**目标**：为任务、积分、DHT、共享等提供统一的持久化与恢复能力。

**要点**：

- `client_state.go`：`ClientState` 版本演进，含 `transfers`、`credits`、`dht`、`shared_dirs`、`shared_files` 等字段。
- `Client` 在配置状态存储后加载/保存，共享相关变更通过 `saveStateIfConfigured` 等路径落盘。

**相关文件**：`client_state.go`、`client.go`、`examples/state_store/`。

---

## 阶段 1：共享库内存模型（SharedStore / SharedFile）

**目标**：在会话内维护与下载任务分离的「可共享文件」索引。

**要点**：

- `shared_store.go`：`SharedStore` 按 hash 去重，支持 `Add` / `Remove` / `Get` / `List` / `ReplaceAll`。
- `shared_file.go`：`SharedFile` 描述路径、大小、分片哈希、`SharedOrigin`（下载完成入库 / 本地导入）等。
- `Session` 构造时初始化 `sharedStore`（见 `session.go`）。

**相关测试**：`shared_store_test.go`。

---

## 阶段 2：导入、目录注册与扫描

**目标**：允许用户将本地文件纳入共享库，并支持批量目录扫描。

**要点**：

- `session_shared.go`：`AddSharedDir` / `RemoveSharedDir` / `ListSharedDirs`。
- `ImportSharedFile`：计算 ED2K 根哈希与分片元数据（`ComputeEd2kFileMeta`），写入 `SharedStore`。
- `RescanSharedDirs`：遍历已注册目录下的普通文件并逐个导入（跳过目录名规则见 `isSkippableSharedFileName` 等）。

**对外 API**：`client_shared.go` 中 `Client.ImportSharedFile`、`RescanSharedDirs`、`AddSharedDir` 等与 `Session` 对齐，并在变更后触发状态保存与 UI 事件。

---

## 阶段 3：上传与入站连接（仅共享文件）

**目标**：使「仅有共享文件、无对应下载任务」时仍能响应对端上传请求。

**要点**：

- `SharedFile` 实现 `UploadableResource`（`uploadable.go`）：分片位图、`ReadRange`、`UploadHashSet` 等与 `Transfer` 对齐的接口。
- `session_shared.go`：`attachIncomingSharedUpload` 将 `PeerConnection` 绑定到 `SharedFile`。
- `peer_connection.go`：`attachUploadByHash` 在 `Transfer` 与 `SharedStore` 之间解析上传资源；`HandleRequestSources2` 应答来源时仅对 `*Transfer` 使用 `Policy`（纯共享文件无 peer 策略表时与 aMule 类似可不广播）。

---

## 阶段 4：ED2K Server 侧发布（OfferFiles）

**目标**：在已连接服务器、获得有效 `clientID` 后，向服务器声明本机可提供的文件列表。

**要点**：

- `shared_publish.go`：`collectPublishableOfferFiles` 合并「已完成且可发布的 `Transfer`」与 `SharedStore` 中校验通过的文件，按 hash 去重，生成 `serverproto.OfferFile` 列表。
- `session.go`：`OnServerIDChange` 在握手完成后若有可发布条目则 `SendOfferFiles`；对未完成任务触发 `RequestSourcesNow` 等。
- `PublishTransferToServer`：单个已完成任务向当前服务器补发一条 offer。

---

## 阶段 5：Kademlia（DHT）侧发布

**目标**：在启用 DHT 且存在 `DHTTracker` 时，发布文件源与可选关键字索引，并支持周期性刷新。

**要点**：

- `session_kad_publish.go`：`kadPublishEndpoint`（监听地址或出站 IPv4）、`PublishTransferToKAD`、`publishSingleSharedFileKAD`、`publishAllFinishedTransfersKADAfterServerChange`、`maybePeriodicKadPublish` 等。
- 发布内容包含 `PublishSource` 与基于文件名的 `PublishKeyword`（`pickKadKeyword`、`kadTagsForSharedFile`）。

**相关文档**：Kad 协议说明见 [kadv6-protocol-CN.md](kadv6-protocol-CN.md)。

---

## 阶段 6：共享状态持久化与下载完成自动入库

**目标**：重启后恢复共享目录与共享文件元数据；下载完成的文件在条件允许时自动进入共享库。

**要点**：

- `ClientSharedFileState` 等结构与 `ClientState` 一并序列化（`client_state.go`）。
- `session_shared.go`：`tryAddCompletedTransferToSharedStore` 在任务完成后尝试加入 `SharedStore`（同 hash 不覆盖）。

---

## 阶段 7：终端 UI（TUI）共享页

**目标**：在交互式下载管理器中查看与管理共享库。

**要点**：

- `cmd/goed2k/tui.go`：`managerPageShared`、共享文件表格 `sharedTable` / `sharedFiles`，与 `Client` 的共享 API 联动。

---

## 阶段 8：客户端来源交换（Source Exchange）

**目标**：在 peer 连接上收发 `OP_REQUESTSOURCES2` / `OP_ANSWERSOURCES2`，将合法来源合并进当前下载任务的 `Policy`。

**说明**：见专文 [source-exchange-CN.md](source-exchange-CN.md)。

### 与 KADV6（`protocol/kadv6`）的衔接

- **UDP 协议包**在 [`protocol/kadv6`](../protocol/kadv6)；**TCP 拨号与 SX** 在 `goed2k` 根包：[`Peer.DialAddr`](../peer.go)、[`PeerFromKADV6SearchEntry` / `Transfer.AddPeerFromKADV6Search`](../peer_kadv6.go)、[`PeerConnection.Connect`](../peer_connection.go) 优先使用 `DialAddr`；经典 `AnswerSources2` 仍仅编码 **IPv4 hybrid**（纯 IPv6 来源不参与 SX 广播）。详见 [source-exchange-CN.md](source-exchange-CN.md)「与 KADV6 / IPv6 的关系」与 [kadv6-implementation-plan.md](kadv6-implementation-plan.md) 第 10 节。

---

## 阶段 9：Secure Ident

**目标**：实现 eMule **SecIdent** 密钥、握手、验签与 Credits 关联。

**要点**：

- `secure_ident.go`：RSA 密钥生成/加载、签名与验签。
- `peer_connection.go`：Hello 扩展 SecIdent 载荷交换。
- `client_state.go`：`IdentityKeyPath` 持久化，`applyState` 时 `LoadIdentity` 恢复密钥。
- `cmd/goed2k`：`--sec-ident`、`--identity-key`；设置页可配置。

**说明**：见 [secure-ident-plan.md](secure-ident-plan.md)。

---

## 阶段 10：KADV6 发布与下载管线

**目标**：IPv6 DHT 搜源、完成发布与 Policy 合并。

**要点**：

- `session_kadv6.go`：搜源结果并入 Transfer。
- `session_kadv6_publish.go`：IPv6 TCP 端点发布、周期刷新。
- `peer_kadv6.go`：`DialAddr` 优先 IPv6 拨号。
- 测试：`session_kadv6_test.go`、`session_kadv6_publish_test.go`、`session_kadv6_integration_test.go`。

---

## 阶段 11：CI 持续集成

**目标**：push/PR 自动回归，防止破坏兼容性。

**要点**：

- `.github/workflows/ci.yml`：`go vet`、`-race` 测试、构建。
- `test_support_test.go`：外网/IPv6 联调默认跳过（`GOED2K_RUN_LIVE_TESTS` / `GOED2K_RUN_KADV6_INTEGRATION`）。

---

## 文档与代码索引

| 主题 | 文档 |
|------|------|
| Source Exchange | [source-exchange-CN.md](source-exchange-CN.md) |
| Kad v6 协议 | [kadv6-protocol-CN.md](kadv6-protocol-CN.md) |
| Kad 实现计划 | [kadv6-implementation-plan.md](kadv6-implementation-plan.md) |
| Secure Ident | [secure-ident-plan.md](secure-ident-plan.md) |

| 主题 | 主要代码入口 |
|------|----------------|
| 共享库 | `shared_store.go`、`shared_file.go`、`session_shared.go` |
| Server 发布 | `shared_publish.go`、`session.go`（`OnServerIDChange`、`PublishTransferToServer`） |
| Kad 发布 | `session_kad_publish.go` |
| KADV6 发布 | `session_kadv6_publish.go` |
| Secure Ident | `secure_ident.go`、`client_state.go` |
| 客户端状态 | `client_state.go`、`client.go` |
| TUI | `cmd/goed2k/tui.go` |
| 来源交换 | `protocol/client/source_exchange.go`、`peer_connection.go`、`policy.go` |
| KADV6 + 双栈 Peer | `protocol/kadv6`、`peer.go`、`peer_kadv6.go` |
