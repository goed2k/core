package goed2k

import "sort"

// TransferPriority 控制下载任务在会话内分配 peer 连接槽的相对顺序（数值越大越优先）。
type TransferPriority int

const (
	TransferPriorityVeryLow TransferPriority = iota
	TransferPriorityLow
	TransferPriorityNormal
	TransferPriorityHigh
	TransferPriorityVeryHigh
)

func (p TransferPriority) SortKey() int {
	return int(p)
}

// Label 返回 P0-P4 优先级标记（P4 最高）。
func (p TransferPriority) Label() string {
	switch p {
	case TransferPriorityVeryLow:
		return "P0"
	case TransferPriorityLow:
		return "P1"
	case TransferPriorityNormal:
		return "P2"
	case TransferPriorityHigh:
		return "P3"
	case TransferPriorityVeryHigh:
		return "P4"
	default:
		return "P2"
	}
}

// TextLabel 返回可读优先级文字。
func (p TransferPriority) TextLabel() string {
	switch p {
	case TransferPriorityVeryLow:
		return "very low"
	case TransferPriorityLow:
		return "low"
	case TransferPriorityNormal:
		return "normal"
	case TransferPriorityHigh:
		return "high"
	case TransferPriorityVeryHigh:
		return "very high"
	default:
		return "normal"
	}
}

func sortTransfersByDownloadPriority(transfers []*Transfer) []*Transfer {
	if len(transfers) < 2 {
		return transfers
	}
	out := append([]*Transfer(nil), transfers...)
	sort.SliceStable(out, func(i, j int) bool {
		pi := out[i].DownloadPriority().SortKey()
		pj := out[j].DownloadPriority().SortKey()
		if pi != pj {
			return pi > pj
		}
		return out[i].GetCreateTime() < out[j].GetCreateTime()
	})
	return out
}
