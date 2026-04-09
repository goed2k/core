package goed2k

// MergeSourceExchangePeers 将来源交换得到的 Peer 合并进策略表（按 Endpoint 去重，重复时合并 SourceFlag）。
func (p *Policy) MergeSourceExchangePeers(peers []Peer) int {
	if p == nil || len(peers) == 0 {
		return 0
	}
	added := 0
	for _, pe := range peers {
		if !pe.Endpoint.Defined() {
			continue
		}
		if ok, _ := p.AddPeer(pe); ok {
			added++
		}
	}
	return added
}
