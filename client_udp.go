package goed2k

import (
	"encoding/binary"
	"net"

	"github.com/goed2k/core/internal/logx"
	"github.com/goed2k/core/protocol"
)

const (
	// clientUDPReaskVersion 是本实现完整支持并在 Hello 中宣告的 UDP ReAsk 版本。
	// 1 = 基础格式：Ping 仅 16 字节文件 hash，ACK 仅 2 字节 queue rank，以及空载荷的 FileNotFound/QueueFull。
	// 不宣告 2+（complete source count）或 4+（part status / extended info）。
	clientUDPReaskVersion = 1

	clientUDPReaskFilePing byte = 0x90
	clientUDPReaskAck      byte = 0x91
	clientUDPFileNotFound  byte = 0x92
	clientUDPQueueFull     byte = 0x93

	// eMule 在等待人数 + 50 超过队列上限时，对不在队列中的 ReAsk 回复 QueueFull。
	clientUDPQueueFullSlack = 50
)

// encodeClientUDP 编码 eMule 客户端 UDP 帧：[OP_EMULEPROT 0xC5][opcode][payload]。
// UDP 无长度字段；也不使用 OP_PACKEDPROT（0xD4），与 eMule ClientUDPSocket 一致。
func encodeClientUDP(opcode byte, payload []byte) []byte {
	pkt := make([]byte, 2+len(payload))
	pkt[0] = protocol.EMuleProt
	pkt[1] = opcode
	copy(pkt[2:], payload)
	return pkt
}

func encodeReaskFilePing(hash protocol.Hash) []byte {
	return encodeClientUDP(clientUDPReaskFilePing, hash.Bytes())
}

func encodeReaskAck(rank uint16) []byte {
	var payload [2]byte
	binary.LittleEndian.PutUint16(payload[:], rank)
	return encodeClientUDP(clientUDPReaskAck, payload[:])
}

func encodeFileNotFound() []byte {
	return encodeClientUDP(clientUDPFileNotFound, nil)
}

func encodeQueueFull() []byte {
	return encodeClientUDP(clientUDPQueueFull, nil)
}

func parseClientUDP(buf []byte) (opcode byte, payload []byte, ok bool) {
	if len(buf) < 2 || buf[0] != protocol.EMuleProt {
		return 0, nil, false
	}
	return buf[1], buf[2:], true
}

func isED2KUDPProtocol(prot byte) bool {
	return prot == protocol.EdonkeyProt || prot == protocol.EMuleProt
}

func parseReaskFilePing(payload []byte) (protocol.Hash, bool) {
	if len(payload) < 16 {
		return protocol.Invalid, false
	}
	hash, err := protocol.HashFromBytes(payload[:16])
	if err != nil {
		return protocol.Invalid, false
	}
	return hash, true
}

func parseReaskAckRank(payload []byte) (uint16, bool) {
	if len(payload) < 2 {
		return 0, false
	}
	// 基础格式 rank 在载荷开头；若对端误发 UDPVer>3 的 part status，rank 仍写在末尾。
	return binary.LittleEndian.Uint16(payload[len(payload)-2:]), true
}

// handleClientUDP 处理 eMule 客户端 UDP 报文（与 Kad 共用 UDP 端口）。
// 只接受 OP_EMULEPROT；旧 6 字节 0xE3 探测、未知 opcode 与短包一律忽略。
func (s *Session) handleClientUDP(addr *net.UDPAddr, buf []byte) {
	if s == nil || addr == nil {
		return
	}
	opcode, payload, ok := parseClientUDP(buf)
	if !ok {
		return
	}
	switch opcode {
	case clientUDPReaskFilePing:
		s.handleUDPReaskFilePing(addr, payload)
	case clientUDPReaskAck:
		s.handleUDPReaskAck(addr, payload)
	case clientUDPQueueFull:
		s.handleUDPQueueFull(addr)
	case clientUDPFileNotFound:
		s.handleUDPFileNotFound(addr)
	}
}

func (s *Session) handleUDPReaskFilePing(addr *net.UDPAddr, payload []byte) {
	hash, ok := parseReaskFilePing(payload)
	if !ok {
		return
	}
	if !s.hasKnownFile(hash) {
		s.sendClientUDP(addr, encodeFileNotFound())
		return
	}
	client, multiple := s.UploadQueue().FindWaitingByIPUDP(addr)
	if multiple {
		// 同 IP 多个 UDP 端口冲突时，eMule 不回答，迫使对端走 TCP。
		return
	}
	if client != nil {
		if src := client.ActiveUploadSource(); src == nil || !src.GetHash().Equal(hash) {
			return
		}
		rank := s.UploadQueue().RefreshWaitingAsk(client)
		s.sendClientUDP(addr, encodeReaskAck(rank))
		return
	}
	if s.UploadQueue().IsNearlyFull() {
		s.sendClientUDP(addr, encodeQueueFull())
	}
}

func (s *Session) handleUDPReaskAck(addr *net.UDPAddr, payload []byte) {
	rank, ok := parseReaskAckRank(payload)
	if !ok {
		return
	}
	s.withPendingUDPPeer(addr, func(peer *Peer) {
		peer.markRemoteQueued(rank, CurrentTime()+remoteQueueReaskInterval)
		s.udpReachable = true
	})
}

func (s *Session) handleUDPQueueFull(addr *net.UDPAddr) {
	s.withPendingUDPPeer(addr, func(peer *Peer) {
		peer.markRemoteQueueFull(CurrentTime() + remoteQueueReaskInterval)
		s.udpReachable = true
	})
}

func (s *Session) handleUDPFileNotFound(addr *net.UDPAddr) {
	s.withPendingUDPPeer(addr, func(peer *Peer) {
		peer.markRemoteFileNotFound(CurrentTime() + remoteQueueReaskInterval)
	})
}

func (s *Session) hasKnownFile(hash protocol.Hash) bool {
	if s == nil || hash.Equal(protocol.Invalid) {
		return false
	}
	if t := s.LookupTransfer(hash); t != nil && !t.IsAborted() {
		return true
	}
	if store := s.SharedStore(); store != nil && store.Get(hash) != nil {
		return true
	}
	return false
}

func (s *Session) withPendingUDPPeer(addr *net.UDPAddr, apply func(*Peer)) {
	if s == nil || addr == nil || apply == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var found *Peer
	matches := 0
	for _, t := range s.transfers {
		if t == nil {
			continue
		}
		for i := range t.policy.peers {
			peer := &t.policy.peers[i]
			if !peerMatchesUDPAddr(peer, addr) {
				continue
			}
			matches++
			found = peer
		}
	}
	if matches != 1 || found == nil || !found.udpReaskPending {
		return
	}
	apply(found)
}

func (s *Session) sendClientUDP(addr *net.UDPAddr, pkt []byte) {
	conn := s.clientUDPConn()
	if conn == nil || addr == nil || len(pkt) == 0 {
		return
	}
	if _, err := conn.WriteToUDP(pkt, addr); err != nil {
		logx.Debug("udp reask send failed", "addr", addr.String(), "err", err)
	}
}

func (s *Session) clientUDPConn() *net.UDPConn {
	if s.dhtTracker != nil {
		return s.dhtTracker.UDPConn()
	}
	return s.serverStatUDPConn
}

// SendUDPReaskFilePing 向对端发送标准 OP_REASKFILEPING（文件 hash，无 UDPVer 扩展字段）。
func (s *Session) SendUDPReaskFilePing(addr *net.UDPAddr, hash protocol.Hash) error {
	conn := s.clientUDPConn()
	if conn == nil || addr == nil {
		return nil
	}
	_, err := conn.WriteToUDP(encodeReaskFilePing(hash), addr)
	return err
}

// tryUDPReaskQueuedPeer 对已在远端队列且已知 UDP 端口的来源发送 ReAsk，避免到期后无脑新开 TCP。
// 成功发送后设置 pending 并续期 29 分钟；发送失败时返回 false，由调用方回退 TCP。
func (s *Session) tryUDPReaskQueuedPeer(transfer *Transfer, peer *Peer, sessionTime int64) bool {
	if s == nil || transfer == nil || peer == nil || peer.UDPPort == 0 || peer.udpReaskPending {
		return false
	}
	if peer.ServerClientID != 0 && peer.DialAddr == nil {
		return false
	}
	if s.clientUDPConn() == nil {
		return false
	}
	addr := peerUDPAddr(peer)
	if addr == nil {
		return false
	}
	if err := s.SendUDPReaskFilePing(addr, transfer.GetHash()); err != nil {
		return false
	}
	peer.LastConnected = sessionTime
	peer.udpReaskPending = true
	if peer.NextConnection <= sessionTime {
		peer.NextConnection = sessionTime + remoteQueueReaskInterval
	}
	return true
}

// IsUDPReachable 返回最近一次有效 ReAsk ACK/QueueFull 是否到达（诊断用，不再表示私有探测）。
func (s *Session) IsUDPReachable() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.udpReachable
}

func (s *Session) helloUDPPortsValue() uint32 {
	if s == nil {
		return 0
	}
	port := s.GetUDPPort()
	if port <= 0 {
		return 0
	}
	value := uint32(port)
	return value | (value << 16)
}

func peerUDPAddr(peer *Peer) *net.UDPAddr {
	if peer == nil || peer.UDPPort == 0 {
		return nil
	}
	if peer.DialAddr != nil && peer.DialAddr.IP != nil && !peer.DialAddr.IP.IsUnspecified() {
		ip := append(net.IP(nil), peer.DialAddr.IP...)
		return &net.UDPAddr{IP: ip, Port: int(peer.UDPPort)}
	}
	if peer.Endpoint.Defined() {
		tcp, err := peer.Endpoint.ToTCPAddr()
		if err != nil || tcp == nil || tcp.IP == nil {
			return nil
		}
		return &net.UDPAddr{IP: tcp.IP, Port: int(peer.UDPPort)}
	}
	return nil
}

func peerMatchesUDPAddr(peer *Peer, addr *net.UDPAddr) bool {
	if peer == nil || addr == nil || peer.UDPPort == 0 || int(peer.UDPPort) != addr.Port {
		return false
	}
	return peerIPEqual(peer, addr.IP)
}

func peerIPEqual(peer *Peer, ip net.IP) bool {
	if peer == nil || ip == nil {
		return false
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	if peer.Endpoint.Defined() {
		want := protocol.EndpointFromInet(&net.TCPAddr{IP: ip4, Port: peer.Endpoint.Port()})
		if peer.Endpoint.IP() == want.IP() {
			return true
		}
	}
	if peer.DialAddr != nil && peer.DialAddr.IP != nil {
		if a := peer.DialAddr.IP.To4(); a != nil && a.Equal(ip4) {
			return true
		}
	}
	return false
}

func uploadClientMatchesUDP(client *PeerConnection, addr *net.UDPAddr) bool {
	if client == nil || addr == nil {
		return false
	}
	ip4 := addr.IP.To4()
	if ip4 == nil {
		return false
	}
	want := protocol.EndpointFromInet(&net.TCPAddr{IP: ip4, Port: client.Endpoint().Port()})
	if !client.Endpoint().Defined() || client.Endpoint().IP() != want.IP() {
		if client.peerInfo == nil || !peerIPEqual(client.peerInfo, addr.IP) {
			return false
		}
	}
	udpPort := client.remotePeerInfo.UDPPort
	if client.peerInfo != nil && client.peerInfo.UDPPort != 0 {
		udpPort = client.peerInfo.UDPPort
	}
	if udpPort == 0 {
		return false
	}
	return int(udpPort) == addr.Port
}
