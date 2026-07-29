package goed2k

import (
	"net"
	"sort"
)

const defaultServerPingTTLMs = int64(5 * 60 * 1000)

type ServerConnectionCandidate struct {
	Identifier string
	Address    *net.TCPAddr
}

type ServerConnectionPolicy struct {
	reconnectSecondsTimeout int64
	maxReconnects           int
	iteration               int
	identifier              string
	address                 *net.TCPAddr
	nextConnectTime         int64
	pingRTTMs               int64
	pingTTLMs               int64
	pingUpdated             int64
}

func NewServerConnectionPolicy(reconnectSecondsTimeout int64, maxReconnects int) ServerConnectionPolicy {
	p := ServerConnectionPolicy{
		reconnectSecondsTimeout: reconnectSecondsTimeout,
		maxReconnects:           maxReconnects,
		pingTTLMs:               defaultServerPingTTLMs,
		pingRTTMs:               -1,
	}
	p.RemoveConnectCandidates()
	return p
}

func (p *ServerConnectionPolicy) SetConnectCandidate(identifier string, address *net.TCPAddr, currentSessionTime int64) {
	p.iteration = 0
	p.identifier = identifier
	p.address = address
	p.nextConnectTime = currentSessionTime
}

func (p *ServerConnectionPolicy) SetServerConnectionFailed(identifier string, address *net.TCPAddr, currentSessionTime int64) {
	if p.identifier == "" || p.identifier != identifier {
		p.iteration = 0
		p.identifier = identifier
		p.address = address
	} else {
		p.iteration++
	}
	if p.HasCandidate() && p.HasIterations() {
		p.nextConnectTime = currentSessionTime + int64(p.iteration)*p.reconnectSecondsTimeout*1000
	} else {
		p.nextConnectTime = -1
	}
}

func (p ServerConnectionPolicy) HasCandidate() bool {
	return p.identifier != "" && p.address != nil
}

func (p ServerConnectionPolicy) HasIterations() bool {
	return p.iteration < p.maxReconnects
}

func (p *ServerConnectionPolicy) RemoveConnectCandidates() {
	p.iteration = p.maxReconnects
	p.identifier = ""
	p.address = nil
	p.nextConnectTime = -1
}

func (p ServerConnectionPolicy) GetConnectCandidate(currentSessionTime int64) *ServerConnectionCandidate {
	if p.nextConnectTime != -1 && p.nextConnectTime <= currentSessionTime {
		return &ServerConnectionCandidate{
			Identifier: p.identifier,
			Address:    p.address,
		}
	}
	return nil
}

// SetPingResult 记录 UDP 探测往返时延；ttlMs 为结果有效期（毫秒），0 表示使用默认 TTL。
func (p *ServerConnectionPolicy) SetPingResult(rttMs, ttlMs, now int64) {
	if p == nil {
		return
	}
	if rttMs < 0 {
		return
	}
	p.pingRTTMs = rttMs
	p.pingUpdated = now
	if ttlMs > 0 {
		p.pingTTLMs = ttlMs
	} else if p.pingTTLMs <= 0 {
		p.pingTTLMs = defaultServerPingTTLMs
	}
}

// EffectivePingRTT 返回未过期的 ping 时延，未知或已过期返回 -1。
func (p ServerConnectionPolicy) EffectivePingRTT(now int64) int64 {
	if p.pingRTTMs < 0 || p.pingUpdated == 0 {
		return -1
	}
	ttl := p.pingTTLMs
	if ttl <= 0 {
		ttl = defaultServerPingTTLMs
	}
	if now-p.pingUpdated > ttl {
		return -1
	}
	return p.pingRTTMs
}

func compareServerPing(lhs, rhs ServerConnectionPolicy, now int64) bool {
	lhsPing := lhs.EffectivePingRTT(now)
	rhsPing := rhs.EffectivePingRTT(now)
	if lhsPing < 0 && rhsPing < 0 {
		return lhs.identifier < rhs.identifier
	}
	if lhsPing < 0 {
		return false
	}
	if rhsPing < 0 {
		return true
	}
	if lhsPing != rhsPing {
		return lhsPing < rhsPing
	}
	return lhs.identifier < rhs.identifier
}

func sortServerIdentifiersByPing(session *Session, identifiers []string, now int64) {
	if session == nil || len(identifiers) < 2 {
		return
	}
	session.mu.Lock()
	policies := make(map[string]*ServerConnectionPolicy, len(identifiers))
	for _, id := range identifiers {
		policies[id] = session.serverConnectionPolicy[id]
	}
	session.mu.Unlock()
	sort.SliceStable(identifiers, func(i, j int) bool {
		var lhs, rhs ServerConnectionPolicy
		if p := policies[identifiers[i]]; p != nil {
			lhs = *p
		} else {
			lhs = NewServerConnectionPolicy(5, 5)
			lhs.identifier = identifiers[i]
		}
		if p := policies[identifiers[j]]; p != nil {
			rhs = *p
		} else {
			rhs = NewServerConnectionPolicy(5, 5)
			rhs.identifier = identifiers[j]
		}
		return compareServerPing(lhs, rhs, now)
	})
}
