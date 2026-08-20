# `.part.met` 格式说明

goed2k 通过 `ExportPartMet` 导出 **eMule/aMule 兼容的二进制** `<path>.part.met`，并额外写出 goed2k JSON 旁注 `<path>.part.met.json` 供调试。`ImportPartMet` / `ParsePartMetBytes` 可自动识别两种格式。

## 文件位置

若下载目标为 `/data/movie.avi`：

| 文件 | 格式 |
|------|------|
| `/data/movie.avi.part.met` | eMule 二进制（主格式，可与 aMule 交叉续传） |
| `/data/movie.avi.part.met.json` | goed2k JSON 旁注 |

二进制布局见 [aMule part.met 文档](https://amule-org.github.io/docs/developer/file-formats/part-met)。

## 二进制导入语义

`ImportPartMet` 解析 eMule gap（`[Start, End)` 缺失区间）后映射到本实现续传模型：

- 完全不被 gap 覆盖的 piece 记入 `completed_pieces`。
- 未完成 piece 中，按当前 `BlockSize`（190 KiB）整块已下载的区间记入 `downloaded_blocks`；尾片短块按实际文件长度夹紧。
- 半块或被 gap 切开的块不计入（保守；180 KiB 统一后再评估不对齐尾部）。
- 下载中 `Client` 会在任务创建、脏进度节流周期和 `Stop` 时原子写出 `.part.met`（先写 `.tmp` 再替换）。崩溃留下的合法 `.tmp` 会在下次导入时提升；损坏 `.tmp` 不会覆盖已有合法文件。
- 仍可用 `ExportPartMet` / `FlushPartMet` 显式写出。`client_state.go` 继续保存 goed2k 自身状态。

## JSON 旁注字段说明

| 字段 | 类型 | 说明 |
|------|------|------|
| `format` | string | 固定为 `goed2k.part.met` |
| `version` | int | 当前为 `1` |
| `file_hash` | string | ED2K 根哈希（十六进制） |
| `file_size` | int | 文件总大小（字节） |
| `piece_hashes` | string[] | ED2K 分片哈希（十六进制） |
| `completed_pieces` | bool[] | 已完成分片位图，与 `piece_hashes` 等长 |
| `downloaded_blocks` | object[] | 进行中的块，`piece` / `block` 为索引 |
| `known_peers` | string[] | 已知来源端点，形如 `1.2.3.4:4662` |

## 示例

```json
{
  "format": "goed2k.part.met",
  "version": 1,
  "piece_hashes": ["D6D7A8E8..."],
  "completed_pieces": [true, false, false],
  "downloaded_blocks": [
    {"piece": 1, "block": 0}
  ],
  "known_peers": ["203.0.113.10:4662"]
}
```

## API

```go
err := goed2k.ExportPartMet("/path/to/file.avi", goed2k.PartMetInfo{
    Hash:     fileHash,
    FileSize: size,
    Filename: "file.avi",
    Resume:   resumeData,
})
info, err := goed2k.ImportPartMet("/path/to/file.avi")
```

`resumeData` 通常来自 `TransferHandle.GetResumeData()` 或 `ClientState` 中的 `resume_data`。
