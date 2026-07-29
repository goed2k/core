package goed2k

import (
	"errors"
	"net"
	"strconv"

	"github.com/goed2k/core/protocol"
)

type Peer struct {
	LastConnected  int64
	NextConnection int64
	FailCount      int
	Connectable    bool
	SourceFlag     int
	Connection     any
	Endpoint       protocol.Endpoint
	// ServerClientID 非零表示服务器来源的低 ID 用户 ID（Endpoint 的 IP 字段实为 client ID）。
	ServerClientID int32
	// DialAddr 可选；非 nil 时优先用于 TCP 拨号（如 KADV6 纯 IPv6 来源），与 Endpoint 可并存（IPv4 时常同步）。
	DialAddr *net.TCPAddr
}

func NewPeer(ep protocol.Endpoint) Peer {
	return Peer{Endpoint: ep}
}

func NewPeerWithSource(ep protocol.Endpoint, conn bool, sourceFlag int) Peer {
	return Peer{Endpoint: ep, Connectable: conn, SourceFlag: sourceFlag}
}

// NewPeerFromTCPAddr 从 TCP 地址构造 Peer：IPv4 时填充 Endpoint；IPv6 时仅填 DialAddr（Policy 排序用 DialAddr 字符串键）。
func NewPeerFromTCPAddr(addr *net.TCPAddr, connectable bool, sourceFlag int) Peer {
	if addr == nil {
		return Peer{}
	}
	p := Peer{Connectable: connectable, SourceFlag: sourceFlag, DialAddr: cloneTCPAddr(addr)}
	if ip4 := addr.IP.To4(); ip4 != nil {
		p.Endpoint = protocol.EndpointFromInet(&net.TCPAddr{IP: ip4, Port: addr.Port})
	}
	return p
}

func cloneTCPAddr(a *net.TCPAddr) *net.TCPAddr {
	if a == nil {
		return nil
	}
	cp := *a
	if a.IP != nil {
		cp.IP = append(net.IP(nil), a.IP...)
	}
	return &cp
}

func peerSortKey(p Peer) string {
	if p.ServerClientID != 0 {
		return "s:" + strconv.FormatInt(int64(p.ServerClientID), 10)
	}
	if p.DialAddr != nil {
		return "d:" + p.DialAddr.String()
	}
	if p.Endpoint.Defined() {
		return "e:" + p.Endpoint.String()
	}
	return ""
}

func (p Peer) Compare(other Peer) int {
	a, b := peerSortKey(p), peerSortKey(other)
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func (p Peer) Equal(other Peer) bool {
	return p.Compare(other) == 0
}

// HasDialableAddress 是否具备可尝试 TCP 的地址（IPv4 Endpoint、DialAddr，或可通过服务器回调的低 ID）。
func (p Peer) HasDialableAddress() bool {
	if p.ServerClientID != 0 {
		return true
	}
	if p.Endpoint.Defined() {
		return true
	}
	if p.DialAddr == nil || p.DialAddr.Port == 0 || p.DialAddr.IP == nil || p.DialAddr.IP.IsUnspecified() {
		return false
	}
	return true
}

// CanEncodeAnswerSources2 当前 AnswerSources2 v4 条目仅支持 IPv4 hybrid uint32；纯 IPv6 无法编码。
func (p Peer) CanEncodeAnswerSources2() bool {
	if p.DialAddr != nil {
		return p.DialAddr.IP.To4() != nil
	}
	return p.Endpoint.Defined()
}

// EffectiveEndpointForSX 返回用于 SX 条目中 UserID 的 IPv4 Endpoint（含 IPv4-mapped IPv6 映射为 IPv4）。
func (p Peer) EffectiveEndpointForSX() (protocol.Endpoint, bool) {
	if !p.CanEncodeAnswerSources2() {
		return protocol.Endpoint{}, false
	}
	if p.DialAddr != nil {
		if ip4 := p.DialAddr.IP.To4(); ip4 != nil {
			return protocol.EndpointFromInet(&net.TCPAddr{IP: ip4, Port: p.DialAddr.Port}), true
		}
	}
	if p.Endpoint.Defined() {
		return p.Endpoint, true
	}
	return protocol.Endpoint{}, false
}

// isFilteredPeerTCPAddr 过滤回环、私网、RFC4193 等（与 IsLocalAddress 对 IPv4 的语义对齐扩展至 IPv6）。
func isFilteredPeerTCPAddr(a *net.TCPAddr) bool {
	if a == nil || a.IP == nil {
		return true
	}
	if ip4 := a.IP.To4(); ip4 != nil {
		ep := protocol.EndpointFromInet(&net.TCPAddr{IP: ip4})
		return IsLocalAddress(ep.IP())
	}
	ip := a.IP.To16()
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsPrivate()
}

// peerDialTCPAddr 用于拨号：优先 DialAddr，否则由 Endpoint 转换。
func (p *Peer) peerDialTCPAddr() (*net.TCPAddr, error) {
	if p == nil {
		return nil, errors.New("nil peer")
	}
	if p.DialAddr != nil {
		return p.DialAddr, nil
	}
	if p.Endpoint.Defined() {
		return p.Endpoint.ToTCPAddr()
	}
	return nil, errors.New("no dial address")
}
