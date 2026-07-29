package goed2k

import (
	"github.com/goed2k/core/protocol"
)

const callbackRequestCooldownSec = 60

// RequestServerCallback 向已连接服务器请求对低 ID 来源的回调穿透，并记录待关联的传输。
func (s *Session) RequestServerCallback(t *Transfer, clientID int32) bool {
	if s == nil || t == nil || clientID == 0 || IsLowID(s.GetClientID()) {
		return false
	}
	now := CurrentTime()
	s.mu.Lock()
	if last, ok := s.callbackCooldown[clientID]; ok && now < last+Seconds(callbackRequestCooldownSec) {
		s.mu.Unlock()
		return false
	}
	s.callbackCooldown[clientID] = now
	s.callbacks[clientID] = t.GetHash()
	servers := make([]*ServerConnection, 0, len(s.serverConnections)+1)
	seen := make(map[*ServerConnection]struct{}, len(s.serverConnections)+1)
	if s.serverConnection != nil {
		servers = append(servers, s.serverConnection)
		seen[s.serverConnection] = struct{}{}
	}
	for _, sc := range s.serverConnections {
		if sc == nil {
			continue
		}
		if _, ok := seen[sc]; ok {
			continue
		}
		servers = append(servers, sc)
		seen[sc] = struct{}{}
	}
	s.mu.Unlock()

	sent := false
	for _, sc := range servers {
		if sc == nil || !sc.IsHandshakeCompleted() {
			continue
		}
		sc.SendCallbackRequest(clientID)
		sent = true
	}
	return sent
}

// OnCallbackRequestIncoming 收到服务器转发的回调请求：向对方发起出站 TCP（用于上传）。
func (s *Session) OnCallbackRequestIncoming(point protocol.Endpoint) {
	if s == nil || !point.Defined() {
		return
	}
	s.mu.Lock()
	pc := NewPeerConnection(s, point, nil, nil)
	s.connections = append(s.connections, pc)
	s.mu.Unlock()
	if err := pc.Connect(); err != nil {
		pc.Close(NotConnected)
	}
}

// tryAttachCallbackPeer 入站连接来自我们请求的 callback 时，按低 ID client ID 关联传输。
func (s *Session) tryAttachCallbackPeer(pc *PeerConnection, remoteClientID int32) {
	if s == nil || pc == nil || remoteClientID == 0 || !IsLowID(remoteClientID) {
		return
	}
	if pc.transfer != nil {
		return
	}
	s.mu.Lock()
	hash, ok := s.callbacks[remoteClientID]
	if !ok {
		s.mu.Unlock()
		return
	}
	delete(s.callbacks, remoteClientID)
	t := s.transfers[hash]
	s.mu.Unlock()
	if t == nil {
		return
	}
	if err := t.AttachIncomingPeer(pc); err != nil {
		return
	}
	pc.SendFileRequest(t.GetHash())
}
