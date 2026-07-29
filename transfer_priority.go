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
