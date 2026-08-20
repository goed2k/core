package goed2k

import (
	"net"
	"time"

	"github.com/goed2k/core/internal/logx"
	"github.com/goed2k/core/protocol"
	kadv6proto "github.com/goed2k/core/protocol/kadv6"
)

func localOutboundIPv6() net.IP {
	c, err := net.DialTimeout("udp6", "[2001:4860:4860::8888]:53", 400*time.Millisecond)
	if err != nil {
		return nil
	}
	defer c.Close()
	u, ok := c.LocalAddr().(*net.UDPAddr)
	if !ok {
		return nil
	}
	ip := u.IP.To16()
	if ip == nil || ip.To4() != nil || ip.IsUnspecified() || ip.IsLoopback() {
		return nil
	}
	return ip
}

// kadv6PublishEndpoint 返回用于 KADV6 发布源（TCP）的本机可达 IPv6 地址；绑定具体网卡时用监听地址，否则用出站 IPv6。
func (s *Session) kadv6PublishEndpoint() *net.TCPAddr {
	s.mu.Lock()
	port := s.settings.ListenPort
	listener := s.listener
	s.mu.Unlock()
	if port <= 0 {
		return nil
	}
	var ip net.IP
	if listener != nil {
		if a, ok := listener.Addr().(*net.TCPAddr); ok && a.IP != nil {
			if a.IP.To4() == nil && !a.IP.IsUnspecified() && !a.IP.IsLoopback() {
				ip = a.IP.To16()
			}
		}
	}
	if ip == nil {
		ip = localOutboundIPv6()
	}
	if ip == nil || ip.To4() != nil {
		return nil
	}
	return &net.TCPAddr{IP: ip, Port: port}
}

func kadv6TagsForSharedFile(name string, size int64) []kadv6proto.Tag {
	tags := []kadv6proto.Tag{
		{Type: kadv6proto.TagTypeString, ID: kadv6proto.TagName, String: name},
	}
	if size > 0 {
		tags = append(tags, kadv6proto.Tag{Type: kadv6proto.TagTypeUint64, ID: kadv6proto.TagFileSize, UInt64: uint64(size)})
	}
	return tags
}

func kadv6PublishAddrsEqual(a, b *net.TCPAddr) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Port == b.Port && a.IP.Equal(b.IP)
}

func (s *Session) noteKadv6Publish(tcpAddr *net.TCPAddr, now int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if tcpAddr != nil {
		s.lastKadv6PublishTCPAddr = cloneTCPAddr(tcpAddr)
	}
	s.lastKadv6PeriodicPublishAt = now
}

func (s *Session) publishSingleTransferKADV6(tracker *KADV6Tracker, tcpAddr *net.TCPAddr, t *Transfer) {
	if tracker == nil || t == nil || tcpAddr == nil {
		return
	}
	hash := t.GetHash()
	if clientID := s.GetClientID(); clientID != 0 && IsLowID(clientID) {
		logx.Debug("kadv6 publish source skipped: local lowid", "hash", hash.String())
	} else if !tracker.PublishSource(hash, tcpAddr, t.Size()) {
		logx.Debug("kadv6 publish source skipped or failed", "hash", hash.String())
	}
	keyword := pickKadKeyword(t.FileName())
	if keyword == "" {
		return
	}
	keywordHash, err := protocol.HashFromData([]byte(keyword))
	if err != nil {
		return
	}
	entry := kadv6proto.SearchEntry{
		ID:   kadv6proto.NewID(hash),
		Tags: kadv6TagsForSharedFile(t.FileName(), t.Size()),
	}
	if !tracker.PublishKeyword(keywordHash, entry) {
		logx.Debug("kadv6 publish keyword skipped or failed", "keyword", keyword, "hash", hash.String())
	}
}

// PublishTransferToKADV6 在任务已完成时向 KADV6 发布文件源与（可选）关键字索引，需 EnableDHTv6 且已设置 KADV6Tracker。
func (s *Session) PublishTransferToKADV6(t *Transfer) {
	if s == nil || t == nil {
		return
	}
	if !s.settings.EnableDHTv6 {
		return
	}
	tracker := s.dhtv6Tracker
	if tracker == nil {
		return
	}
	if t.IsPaused() || t.IsAborted() || !t.isKadPublishable() {
		return
	}
	tcpAddr := s.kadv6PublishEndpoint()
	if tcpAddr == nil {
		return
	}
	s.publishSingleTransferKADV6(tracker, tcpAddr, t)
	s.noteKadv6Publish(tcpAddr, CurrentTime())
}

func (s *Session) publishSingleSharedFileKADV6(tracker *KADV6Tracker, tcpAddr *net.TCPAddr, sf *SharedFile) {
	if tracker == nil || sf == nil || tcpAddr == nil || !sf.Completed {
		return
	}
	hash := sf.Hash
	if clientID := s.GetClientID(); clientID != 0 && IsLowID(clientID) {
		logx.Debug("kadv6 publish source skipped: local lowid", "hash", hash.String())
	} else if !tracker.PublishSource(hash, tcpAddr, sf.Size()) {
		logx.Debug("kadv6 publish source skipped or failed", "hash", hash.String())
	}
	keyword := pickKadKeyword(sf.FileLabel())
	if keyword == "" {
		return
	}
	keywordHash, err := protocol.HashFromData([]byte(keyword))
	if err != nil {
		return
	}
	entry := kadv6proto.SearchEntry{
		ID:   kadv6proto.NewID(hash),
		Tags: kadv6TagsForSharedFile(sf.FileLabel(), sf.Size()),
	}
	if !tracker.PublishKeyword(keywordHash, entry) {
		logx.Debug("kadv6 publish keyword skipped or failed", "keyword", keyword, "hash", hash.String())
	}
}

func (s *Session) publishAllFinishedTransfersKADV6(tcpAddr *net.TCPAddr) {
	if !s.settings.EnableDHTv6 || s.dhtv6Tracker == nil || tcpAddr == nil {
		return
	}
	tracker := s.dhtv6Tracker
	seen := make(map[string]struct{})
	for _, t := range s.snapshotTransfers() {
		if t == nil || t.IsPaused() || t.IsAborted() || !t.isKadPublishable() {
			continue
		}
		s.publishSingleTransferKADV6(tracker, tcpAddr, t)
		seen[t.GetHash().String()] = struct{}{}
	}
	if s.sharedStore == nil {
		return
	}
	for _, sf := range s.sharedStore.List() {
		if sf == nil || !sf.Completed || !validateSharedFileOnDisk(sf) {
			continue
		}
		if _, ok := seen[sf.Hash.String()]; ok {
			continue
		}
		s.publishSingleSharedFileKADV6(tracker, tcpAddr, sf)
		seen[sf.Hash.String()] = struct{}{}
	}
}

func (s *Session) publishAllFinishedTransfersKADV6AfterServerChange() {
	if !s.settings.EnableDHTv6 || s.dhtv6Tracker == nil {
		return
	}
	tcpAddr := s.kadv6PublishEndpoint()
	if tcpAddr == nil {
		return
	}
	s.publishAllFinishedTransfersKADV6(tcpAddr)
	s.noteKadv6Publish(tcpAddr, CurrentTime())
}

func (s *Session) maybePeriodicKadv6Publish(now int64) {
	if !s.settings.EnableDHTv6 || s.dhtv6Tracker == nil {
		return
	}
	tcpAddr := s.kadv6PublishEndpoint()
	if tcpAddr == nil {
		return
	}
	s.mu.Lock()
	lastAddr := s.lastKadv6PublishTCPAddr
	lastAt := s.lastKadv6PeriodicPublishAt
	s.mu.Unlock()

	if !kadv6PublishAddrsEqual(tcpAddr, lastAddr) {
		s.publishAllFinishedTransfersKADV6(tcpAddr)
		s.noteKadv6Publish(tcpAddr, now)
		return
	}
	if lastAt == 0 {
		s.mu.Lock()
		if s.lastKadv6PeriodicPublishAt == 0 {
			s.lastKadv6PeriodicPublishAt = now
		}
		s.mu.Unlock()
		return
	}
	if now-lastAt < kadPeriodicPublishInterval {
		return
	}
	s.publishAllFinishedTransfersKADV6(tcpAddr)
	s.noteKadv6Publish(tcpAddr, now)
}
