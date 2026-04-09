# Secure Ident 实现预研（goed2k）

## 现状

- **Peer 身份**：`protocol.Hash` 作为 UserHash/客户端标识出现在 `Hello`/`HelloAnswer`（`peer_connection.go`），对端 `remoteHash` 用于积分与上传排队评分（`PeerConnection.remoteHash`、`Credits.ScoreRatio`）。
- **Credits**：`PeerCreditManager` 按对端 hash 累计上下传字节（`client_credits.go`），与「是否验签」无绑定。
- **Friend slot**：`Session.friendSlots` 按文件 hash 标记好友槽，与 Secure Ident 无直接关系。
- **握手**：`MiscOptions` 含 `SupportSecIdent` 位；当前未实现完整安全握手与签名载荷。

## 与完整 Secure Ident 的差距

1. 无长期 **RSA/ECDSA 密钥对** 持久化；UserHash 多为协议层 16 字节标识，不等同于可验签公钥。
2. 握手与扩展握手中未交换可验证的 **challenge/response** 或证书链。
3. Credits 未与「已验证身份」关联，无法防止伪造 UserHash 刷分。
4. 与 eMule **SecIdent** 二进制报文（签名、密钥长度、算法标识）未对齐。

## 建议数据结构

| 项目 | 说明 |
|------|------|
| `IdentityState` | 本地私钥句柄或封装（不落盘明文私钥时可仅存路径 + 类型） |
| `PeerIdentity` | 对端 UserHash、可选公钥指纹、上次验签时间、信任级别 |
| `ClientState` 扩展 | `identity_version`、`public_key_id` 或密钥存储路径（与现有 `version` 字段区分） |

## 建议实现顺序

1. 密钥生成与 **文件持久化**（权限 0600），启动时加载。
2. 在 **HelloAnswer/扩展握手** 中增加可协商的 SecIdent 版本与公钥/指纹字段（与现有 Tag 列表兼容）。
3. **验签流程**：收到对端签名 → 校验 → 标记 `PeerConnection` 身份状态。
4. **Credits**：仅对已验证 peer 累计或加权（可配置）。
5. 与 **Friend slot**、Kad 发布等交叉审查（避免误将未验证 peer 当高信任）。

## 风险

- 与官方 eMule/aMule 行为不一致时，可能被低 ID 或旧客户端拒绝连接。
- 私钥泄露导致身份冒充；需明确轮换策略。
- 性能：每条连接验签成本；可缓存会话内验证结果。

## 代码落点（参考）

- 握手组装：`PeerConnection.PrepareHelloAnswer`、`SendExtHelloAnswer`
- 对端标识：`PeerConnection.remoteHash`、`RemotePeerInfo`
- 积分：`PeerCreditManager`、`UploadScore`
- 状态持久化：`ClientState` / `client_state.go`

当前仓库 **未实现** 上述运行时逻辑；本文件仅作设计与改造点索引。
