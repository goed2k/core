package goed2k

import (
	"github.com/goed2k/core/protocol"
	kadproto "github.com/goed2k/core/protocol/kad"
)

func (s *Session) mergeKadSearchSources(hash protocol.Hash, transfer *Transfer, results []kadproto.SearchEntry) {
	if s == nil || transfer == nil || len(results) == 0 {
		return
	}
	s.mu.Lock()
	current := s.transfers[hash]
	s.mu.Unlock()
	if current == nil || current != transfer {
		return
	}
	var callbacks []Peer
	var directs []protocol.Endpoint
	for _, entry := range results {
		info, ok := entry.SourceInfo()
		if !ok {
			continue
		}
		switch info.Kind {
		case kadproto.SourceKindCallback:
			if info.ClientID == 0 {
				continue
			}
			peer := NewPeerWithSource(protocol.Endpoint{}, true, int(PeerDHT))
			peer.ServerClientID = info.ClientID
			callbacks = append(callbacks, peer)
		case kadproto.SourceKindDirect:
			if info.Endpoint.Defined() {
				directs = append(directs, info.Endpoint)
			}
		}
	}
	if len(callbacks) > 0 {
		if current.session != nil {
			current.session.mu.Lock()
		}
		for _, peer := range callbacks {
			_, _ = current.policy.AddPeer(peer)
		}
		if current.session != nil {
			current.session.mu.Unlock()
		}
	}
	for _, endpoint := range directs {
		_ = current.AddPeer(endpoint, int(PeerDHT))
	}
}
