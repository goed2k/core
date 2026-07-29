# Secure Ident 实现说明（goed2k）

## 现状（v0.1.0+）

仓库已实现 eMule **SecIdent** 的运行时逻辑（默认关闭，需显式启用）：

| 能力 | 状态 | 代码入口 |
|------|------|----------|
| RSA 2048 密钥生成与 PEM 持久化（0600） | ✅ | `secure_ident.go`：`GenerateIdentityKeyPair`、`LoadIdentityState` |
| 启动/状态恢复时加载密钥 | ✅ | `Session.LoadIdentity`、`Client.LoadIdentity`、`client_state.go` `applyState` |
| Hello / 扩展握手 SecIdent 载荷 | ✅ | `peer_connection.go` |
| 对端公钥与签名验签 | ✅ | `secure_ident.go`：`verifySecIdentSignature` |
| Credits 仅对已验证 peer 累计（可配置） | ✅ | `settings.CreditsOnlyVerified`、`client_credits.go` |
| CLI / TUI 开关 | ✅ | `--sec-ident`、`--identity-key`；设置页 SecIdent / Identity key |
| 状态持久化 `IdentityKeyPath` / `IdentityVersion` | ✅ | `client_state.go` |

## 启用方式

### 命令行

```bash
goed2k --sec-ident --identity-key ./identity.pem
```

首次运行若密钥文件不存在，`LoadIdentity` 会自动生成。

### 库 API

```go
settings.EnableSecIdent = true
settings.IdentityKeyPath = "/path/to/identity.pem"
client := goed2k.NewClient(settings)
if err := client.LoadIdentity(settings.IdentityKeyPath); err != nil {
    log.Fatal(err)
}
```

### 状态恢复

`Client.LoadState` 在 `IdentityKeyPath` 非空时会调用 `LoadIdentity`，重启后身份自动可用。

## 与 eMule 的差异与限制

1. **默认关闭**：与 CryptLayer 相同，避免与未支持 SecIdent 的旧客户端互连问题。
2. **TUI 设置页**可切换 SecIdent，但修改后需在无任务时保存并重启客户端（与 KAD/端口设置相同）。
3. **CreditsOnlyVerified** 默认 `false`；设为 `true` 时仅对已验签 peer 计分。
4. 尚未与真实 eMule 全版本做大规模互操作回归；生产环境建议先小范围试连。

## 数据结构

| 项目 | 说明 |
|------|------|
| `IdentityState` | 本地 RSA 私钥、公钥 DER、指纹、关联 UserHash |
| `ClientState.IdentityKeyPath` | 密钥 PEM 路径 |
| `ClientState.IdentityVersion` | SecIdent 协议版本号 |

## 代码落点

- 密钥与验签：`secure_ident.go`、`secure_ident_test.go`
- 握手：`peer_connection.go`（`PrepareHelloAnswer`、`handleSecIdent`）
- 协议报文：`protocol/client/sec_ident.go`
- 积分：`client_credits.go`、`PeerCreditManager.ScoreRatio(..., verified bool)`
- 持久化：`client_state.go`、`session.go` `LoadIdentity`

## 相关文档

- [库 API 使用指南](library-api-CN.md)
- [分阶段实现说明](library-implementation-phases-CN.md) 阶段 9
