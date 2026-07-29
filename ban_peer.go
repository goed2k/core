package goed2k

import (
	"net"

	"github.com/goed2k/core/protocol"
)

func peerIP(pe Peer) net.IP {
	if pe.DialAddr != nil && pe.DialAddr.IP != nil {
		return pe.DialAddr.IP
	}
	if pe.Endpoint.Defined() {
		return Int2Address(pe.Endpoint.IP())
	}
	return nil
}

func (s *Session) SetIPFilter(filter *IPFilter) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.ipFilter = filter
	s.mu.Unlock()
}

func (s *Session) IPFilter() *IPFilter {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ipFilter
}

func (s *Session) isIPFiltered(ip net.IP) bool {
	if s == nil || ip == nil {
		return false
	}
	s.mu.Lock()
	filter := s.ipFilter
	s.mu.Unlock()
	return filter != nil && filter.Contains(ip)
}

func (s *Session) isPeerBanned(pe Peer) bool {
	if s == nil {
		return false
	}
	key := banKeyForPeer(pe)
	if key == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.bannedPeers[key]
	return ok
}

func (s *Session) BanPeer(endpoint protocol.Endpoint) {
	if s == nil || !endpoint.Defined() {
		return
	}
	s.mu.Lock()
	if s.bannedPeers == nil {
		s.bannedPeers = make(map[string]protocol.Endpoint)
	}
	s.bannedPeers[endpoint.String()] = endpoint
	s.mu.Unlock()
}

func (s *Session) UnbanPeer(endpoint protocol.Endpoint) {
	if s == nil || !endpoint.Defined() {
		return
	}
	s.mu.Lock()
	delete(s.bannedPeers, endpoint.String())
	s.mu.Unlock()
}

func (s *Session) BannedPeers() []protocol.Endpoint {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.bannedPeers) == 0 {
		return nil
	}
	out := make([]protocol.Endpoint, 0, len(s.bannedPeers))
	for _, ep := range s.bannedPeers {
		out = append(out, ep)
	}
	return out
}

func (s *Session) applyBannedPeers(endpoints []protocol.Endpoint) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bannedPeers = make(map[string]protocol.Endpoint, len(endpoints))
	for _, ep := range endpoints {
		if ep.Defined() {
			s.bannedPeers[ep.String()] = ep
		}
	}
}

func (s *Session) snapshotBannedPeers() []protocol.Endpoint {
	return s.BannedPeers()
}

func banKeyForPeer(pe Peer) string {
	if pe.Endpoint.Defined() {
		return pe.Endpoint.String()
	}
	if pe.DialAddr != nil {
		return pe.DialAddr.String()
	}
	return ""
}

func (s *Session) removeBannedPeerFromTransfers(endpoint protocol.Endpoint) {
	if s == nil || !endpoint.Defined() {
		return
	}
	s.mu.Lock()
	transfers := make([]*Transfer, 0, len(s.transfers))
	for _, t := range s.transfers {
		transfers = append(transfers, t)
	}
	s.mu.Unlock()
	for _, t := range transfers {
		if peer := t.policy.FindPeer(endpoint); peer != nil {
			t.policy.removePeer(*peer)
		}
	}
}

func (p *Policy) removePeer(peer Peer) {
	pos, found := -1, false
	for i := range p.peers {
		if p.peers[i].Equal(peer) {
			pos = i
			found = true
			break
		}
	}
	if found && pos >= 0 {
		p.peers = append(p.peers[:pos], p.peers[pos+1:]...)
	}
}

func (p Policy) isPeerAllowed(pe Peer) bool {
	if p.transfer == nil || p.transfer.session == nil {
		return true
	}
	sess := p.transfer.session
	if sess.isPeerBanned(pe) {
		return false
	}
	if ip := peerIP(pe); ip != nil && sess.isIPFiltered(ip) {
		return false
	}
	return true
}
