package goed2k

import (
	"net"

	"github.com/goed2k/core/protocol"
)

// serverSourceExchangeIDs 返回当前主服务器在 SX1 应答中使用的 IP/端口。
func (s *Session) serverSourceExchangeIDs() (serverIP uint32, serverPort uint16) {
	if s == nil {
		return 0, 0
	}
	s.mu.Lock()
	sc := s.serverConnection
	s.mu.Unlock()
	if sc == nil {
		return 0, 0
	}
	addr := sc.GetAddress()
	if addr == nil {
		return 0, 0
	}
	ip4 := addr.IP.To4()
	if ip4 == nil {
		return 0, uint16(addr.Port)
	}
	ep := protocol.EndpointFromInet(&net.TCPAddr{IP: ip4, Port: addr.Port})
	if !ep.Defined() {
		return 0, uint16(addr.Port)
	}
	return uint32(ep.IP()), uint16(ep.Port())
}
