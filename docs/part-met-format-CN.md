# `.part.met` JSON 旁注格式

goed2k 通过 `ExportPartMet` 导出的 `<下载文件路径>.part.met` 为 **JSON 文本**，用于与外部工具或手工检查互通；**不保证**与 eMule 原生二进制 `.part.met` 字节级兼容。

## 文件位置

若下载目标为 `/data/movie.avi`，旁注文件为 `/data/movie.avi.part.met`。

## 字段说明

| 字段 | 类型 | 说明 |
|------|------|------|
| `format` | string | 固定为 `goed2k.part.met` |
| `version` | int | 当前为 `1` |
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
err := goed2k.ExportPartMet("/path/to/file.avi", resumeData)
```

`resumeData` 通常来自 `TransferHandle.GetResumeData()` 或 `ClientState` 中的 `resume_data`。
