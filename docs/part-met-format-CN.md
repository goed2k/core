# `.part.met` 格式说明

goed2k 通过 `ExportPartMet` 导出 **eMule/aMule 兼容的二进制** `<path>.part.met`，并额外写出 goed2k JSON 旁注 `<path>.part.met.json` 供调试。`ImportPartMet` / `ParsePartMetBytes` 可自动识别两种格式。

## 文件位置

若下载目标为 `/data/movie.avi`：

| 文件 | 格式 |
|------|------|
| `/data/movie.avi.part.met` | eMule 二进制（主格式，可与 aMule 交叉续传） |
| `/data/movie.avi.part.met.json` | goed2k JSON 旁注 |

二进制布局见 [aMule part.met 文档](https://amule-org.github.io/docs/developer/file-formats/part-met)。

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
