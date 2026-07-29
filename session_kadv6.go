package goed2k

import (
	"github.com/goed2k/core/internal/logx"
	"github.com/goed2k/core/protocol"
	kadv6proto "github.com/goed2k/core/protocol/kadv6"
)

// SendDHTv6SourcesRequest 通过 KADV6 搜索文件源，并将 IPv6 来源并入传输策略表。
func (s *Session) SendDHTv6SourcesRequest(hash protocol.Hash, size int64, transfer *Transfer) bool {
	if s == nil || s.dhtv6Tracker == nil || transfer == nil {
		return false
	}
	return s.dhtv6Tracker.SearchSources(hash, size, func(results []kadv6proto.SearchEntry) {
		s.mergeKADV6SearchResults(hash, transfer, results)
	})
}

func (s *Session) mergeKADV6SearchResults(hash protocol.Hash, transfer *Transfer, results []kadv6proto.SearchEntry) {
	if s == nil || transfer == nil || len(results) == 0 {
		return
	}
	logx.Debug("kadv6 source search result", "hash", hash.String(), "results", len(results))
	s.mu.Lock()
	current := s.transfers[hash]
	s.mu.Unlock()
	if current == nil || current != transfer {
		return
	}
	added := 0
	for _, entry := range results {
		ok, err := current.AddPeerFromKADV6Search(entry)
		if err != nil {
			logx.Debug("kadv6 add peer failed", "hash", hash.String(), "err", err)
			continue
		}
		if ok {
			added++
		}
	}
	if added > 0 {
		logx.Debug("kadv6 peers merged into policy", "hash", hash.String(), "added", added)
	}
}
