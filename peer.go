package goed2k

import (
	"errors"
	"net"
	"strconv"

	"github.com/goed2k/core/protocol"
	clientproto "github.com/goed2k/core/protocol/client"
)

type Peer struct {
	LastConnected   int64
	NextConnection  int64
	FailCount       int
	Connectable     bool
	SourceFlag      int
	Connection      any
	Endpoint        protocol.Endpoint
	remoteQueueRank uint16
	inRemoteQueue   bool
	remoteQueueFull bool
	udpReaskPending bool
	// UDPPort 来自 Hello CT_EMULE_UDPPORTS（0xF9）低 16 位，供标准 UDP ReAsk 使用。
	UDPPort uint16
	// ServerClientID 非零表示服务器来源的低 ID 用户 ID（Endpoint 的 IP 字段实为 client ID）。
	ServerClientID int32
	// DialAddr 可选；非 nil 时优先用于 TCP 拨号（如 KADV6 纯 IPv6 来源），与 Endpoint 可并存（IPv4 时常同步）。
	DialAddr *net.TCPAddr
	// UserHash / CryptOptions 来自 Source Exchange v4，用于协议混淆拨号。
	UserHash     protocol.Hash
	CryptOptions uint8
}

// RemoteQueueState 返回本来源在远端上传队列中的最近排名及排队状态。
// 状态由下载侧 QueueRanking/AcceptUpload 状态机维护，调用方只能读取。
func (p *Peer) RemoteQueueState() (rank uint16, queued bool) {
	if p == nil {
		return 0, false
	}
	return p.remoteQueueRank, p.inRemoteQueue
}

func (p *Peer) markRemoteQueued(rank uint16, nextConnection int64) {
	if p == nil {
		return
	}
	p.remoteQueueRank = rank
	p.inRemoteQueue = true
	p.remoteQueueFull = false
	p.udpReaskPending = false
	p.NextConnection = nextConnection
}

func (p *Peer) markRemoteQueueFull(nextConnection int64) {
	if p == nil {
		return
	}
	p.remoteQueueRank = 0
	p.inRemoteQueue = true
	p.remoteQueueFull = true
	p.udpReaskPending = false
	p.NextConnection = nextConnection
}

func (p *Peer) markRemoteFileNotFound(nextConnection int64) {
	if p == nil {
		return
	}
	p.remoteQueueRank = 0
	p.inRemoteQueue = false
	p.remoteQueueFull = false
	p.udpReaskPending = false
	p.NextConnection = nextConnection
}

func (p *Peer) RemoteQueueFull() bool {
	return p != nil && p.remoteQueueFull
}

func (p *Peer) clearRemoteQueue() {
	if p == nil {
		return
	}
	p.remoteQueueRank = 0
	p.inRemoteQueue = false
	p.remoteQueueFull = false
	p.udpReaskPending = false
	p.NextConnection = 0
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

// CanEncodeAnswerSources2 判断是否可编码进 AnswerSources2（IPv4 或 v5 IPv6）。
func (p Peer) CanEncodeAnswerSources2(sx2Version byte) bool {
	if sx2Version >= clientproto.SourceExchangeIPv6Version {
		if p.DialAddr != nil && p.DialAddr.IP != nil && p.DialAddr.IP.To4() == nil {
			return p.DialAddr.Port > 0 && !p.DialAddr.IP.IsUnspecified()
		}
	}
	if p.DialAddr != nil {
		return p.DialAddr.IP.To4() != nil
	}
	return p.Endpoint.Defined()
}

// EffectiveEndpointForSX 返回用于 SX 条目中 UserID 的 IPv4 Endpoint（含 IPv4-mapped IPv6 映射为 IPv4）。
func (p Peer) EffectiveEndpointForSX() (protocol.Endpoint, bool) {
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

// ToSourceExchangeEntry 将 Peer 编码为 AnswerSources2 条目。
func (p Peer) ToSourceExchangeEntry(sx1Ver int, sx2Version byte, cryptOptions uint8) (clientproto.SourceExchangeEntry, bool) {
	if !p.CanEncodeAnswerSources2(sx2Version) {
		return clientproto.SourceExchangeEntry{}, false
	}
	var entry clientproto.SourceExchangeEntry
	entry.CryptOptions = cryptOptions
	entry.UserHash = p.UserHash
	if ep, ok := p.EffectiveEndpointForSX(); ok {
		uid := uint32(ep.IP())
		if sx1Ver <= 2 {
			uid = clientproto.SwapUint32(uid)
		}
		entry.UserID = uid
		entry.TCPPort = uint16(ep.Port())
	}
	if sx2Version >= clientproto.SourceExchangeIPv6Version && p.DialAddr != nil {
		if ip := p.DialAddr.IP.To16(); ip != nil && ip.To4() == nil {
			copy(entry.IPv6[:], ip)
			if entry.TCPPort == 0 {
				entry.TCPPort = uint16(p.DialAddr.Port)
			}
		}
	}
	if entry.UserID == 0 && !entry.EntryHasIPv6() {
		return clientproto.SourceExchangeEntry{}, false
	}
	return entry, true
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
