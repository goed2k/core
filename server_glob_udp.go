package goed2k

import (
	"crypto/rand"
	"encoding/binary"
	"net"
	"time"
)

// eD2k 客户端向服务器 UDP（通常为 TCP+4）请求全局统计，与 ed2ksrv OP_GLOBSERVSTATREQ/RES 对应。
const (
	ed2kUDPHeader     byte = 0xe3
	opGlobServStatReq byte = 0x96
	opGlobServStatRes byte = 0x97
	serverUDPPortOffset    = 4
	globUDPThrottleMs      = int64(45000)
)

// SetServerMetadata 记录 server.met 中的名称与描述（identifier 为 host:port）。
func (s *Session) SetServerMetadata(identifier, name, description string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.serverMetMeta == nil {
		s.serverMetMeta = make(map[string]serverMetEntryMeta)
	}
	s.serverMetMeta[identifier] = serverMetEntryMeta{Name: name, Description: description}
}

func (s *Session) handleGlobServStatUDP(addr *net.UDPAddr, buf []byte) {
	if len(buf) < 26 || buf[0] != ed2kUDPHeader || buf[1] != opGlobServStatRes {
		return
	}
	challenge := binary.LittleEndian.Uint32(buf[2:6])
	users := binary.LittleEndian.Uint32(buf[6:10])
	files := binary.LittleEndian.Uint32(buf[10:14])
	maxU := binary.LittleEndian.Uint32(buf[14:18])
	soft := binary.LittleEndian.Uint32(buf[18:22])
	hard := binary.LittleEndian.Uint32(buf[22:26])
	s.mu.Lock()
	id, ok := s.globUDPChallenge[challenge]
	if ok {
		delete(s.globUDPChallenge, challenge)
	}
	s.mu.Unlock()
	if !ok {
		return
	}
	s.mu.Lock()
	if s.udpServerStats == nil {
		s.udpServerStats = make(map[string]serverUDPStats)
	}
	s.udpServerStats[id] = serverUDPStats{
		Users: users, Files: files, MaxUsers: maxU, SoftFiles: soft, HardFiles: hard, Valid: true,
	}
	s.mu.Unlock()
	_ = addr
}

func (s *Session) globUDPWriteConn() *net.UDPConn {
	if s.dhtTracker != nil {
		return s.dhtTracker.UDPConn()
	}
	return s.serverStatUDPConn
}

// EnsureServerStatUDPListener 在未启用 DHT 时绑定 UDP 端口以收发 GlobServStat（启用 DHT 时由 DHT 的 UDP 套接字接收）。
func (s *Session) EnsureServerStatUDPListener() error {
	s.mu.Lock()
	if s.dhtTracker != nil || s.settings.UDPPort <= 0 {
		s.mu.Unlock()
		return nil
	}
	if s.serverStatUDPConn != nil {
		s.mu.Unlock()
		return nil
	}
	dynamic := s.listenPortWasDynamic
	udpPort := s.settings.UDPPort
	s.mu.Unlock()
	if dynamic {
		// 动态 TCP 监听时用随机 UDP，避免与并行测试/本机其他进程抢占固定 UDP 端口
		udpPort = 0
	}
	conn, err := net.ListenUDP("udp", &net.UDPAddr{Port: udpPort})
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.serverStatUDPConn = conn
	if s.serverStatUDPStop == nil {
		s.serverStatUDPStop = make(chan struct{})
	}
	stop := s.serverStatUDPStop
	s.mu.Unlock()
	go s.serverStatUDPReadLoop(conn, stop)
	return nil
}

func (s *Session) serverStatUDPReadLoop(conn *net.UDPConn, stop <-chan struct{}) {
	buf := make([]byte, 2048)
	for {
		select {
		case <-stop:
			return
		default:
		}
		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			select {
			case <-stop:
				return
			default:
				continue
			}
		}
		if n >= 26 {
			s.handleGlobServStatUDP(addr, buf[:n])
		}
	}
}

func (s *Session) closeServerStatUDPListener() {
	s.mu.Lock()
	ch := s.serverStatUDPStop
	conn := s.serverStatUDPConn
	s.serverStatUDPConn = nil
	s.serverStatUDPStop = nil
	s.mu.Unlock()
	if ch != nil {
		close(ch)
	}
	if conn != nil {
		_ = conn.Close()
	}
}

func (s *Session) maybePollServerGlobUDP(now int64) {
	c := s.globUDPWriteConn()
	if c == nil {
		return
	}
	for _, sc := range s.activeServerConnections() {
		if sc == nil || !sc.IsHandshakeCompleted() {
			continue
		}
		if sc.lastGlobUDPQuery != 0 && now-sc.lastGlobUDPQuery < globUDPThrottleMs {
			continue
		}
		addr := sc.GetAddress()
		if addr == nil {
			continue
		}
		var b [4]byte
		if _, err := rand.Read(b[:]); err != nil {
			continue
		}
		challenge := binary.LittleEndian.Uint32(b[:])
		if challenge == 0 {
			challenge = 1
		}
		s.mu.Lock()
		if s.globUDPChallenge == nil {
			s.globUDPChallenge = make(map[uint32]string)
		}
		s.globUDPChallenge[challenge] = sc.GetIdentifier()
		s.mu.Unlock()
		udpAddr := &net.UDPAddr{IP: addr.IP, Port: addr.Port + serverUDPPortOffset}
		pkt := make([]byte, 6)
		pkt[0], pkt[1] = ed2kUDPHeader, opGlobServStatReq
		binary.LittleEndian.PutUint32(pkt[2:6], challenge)
		if _, err := c.WriteToUDP(pkt, udpAddr); err == nil {
			sc.lastGlobUDPQuery = now
		}
	}
}
