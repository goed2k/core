package goed2k

import (
	"encoding/binary"
	"net"

	"github.com/goed2k/core/internal/logx"
)

const (
	clientUDPReaskFilePing byte = 0x90
	clientUDPReaskAck      byte = 0x91
)

// handleClientUDP 处理 eMule 客户端 UDP 报文（与 Kad 共用 UDP 端口）。
func (s *Session) handleClientUDP(addr *net.UDPAddr, buf []byte) {
	if s == nil || addr == nil || len(buf) < 2 {
		return
	}
	if buf[0] != ed2kUDPHeader {
		return
	}
	switch buf[1] {
	case clientUDPReaskFilePing:
		s.replyUDPReaskAck(addr)
	case clientUDPReaskAck:
		s.noteUDPReachable(addr)
	}
}

func (s *Session) replyUDPReaskAck(addr *net.UDPAddr) {
	conn := s.clientUDPConn()
	if conn == nil || addr == nil {
		return
	}
	pkt := []byte{ed2kUDPHeader, clientUDPReaskAck}
	if _, err := conn.WriteToUDP(pkt, addr); err != nil {
		logx.Debug("udp reask ack failed", "addr", addr.String(), "err", err)
	}
}

func (s *Session) noteUDPReachable(addr *net.UDPAddr) {
	if s == nil || addr == nil {
		return
	}
	s.mu.Lock()
	s.udpReachable = true
	s.mu.Unlock()
}

func (s *Session) clientUDPConn() *net.UDPConn {
	if s.dhtTracker != nil {
		return s.dhtTracker.UDPConn()
	}
	return s.serverStatUDPConn
}

// SendUDPReaskPing 向对端 UDP 端口发送 ReAsk 探测（eMule 防火墙检测）。
func (s *Session) SendUDPReaskPing(addr *net.UDPAddr) error {
	conn := s.clientUDPConn()
	if conn == nil || addr == nil {
		return nil
	}
	port := s.settings.UDPPort
	if port <= 0 {
		port = 4662
	}
	pkt := make([]byte, 6)
	pkt[0] = ed2kUDPHeader
	pkt[1] = clientUDPReaskFilePing
	binary.LittleEndian.PutUint16(pkt[2:], uint16(port))
	binary.LittleEndian.PutUint16(pkt[4:], uint16(port))
	_, err := conn.WriteToUDP(pkt, addr)
	return err
}

// IsUDPReachable 返回最近一次 ReAsk 探测是否收到应答。
func (s *Session) IsUDPReachable() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.udpReachable
}
