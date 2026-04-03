package goed2k

import (
	"net"
	"time"

	"github.com/monkeyWie/goed2k/internal/logx"
	"github.com/monkeyWie/goed2k/protocol"
	kadproto "github.com/monkeyWie/goed2k/protocol/kad"
)

var kadPeriodicPublishInterval = Minutes(30)

func localOutboundIPv4() net.IP {
	c, err := net.DialTimeout("udp4", "8.8.8.8:53", 400*time.Millisecond)
	if err != nil {
		return nil
	}
	defer c.Close()
	u, ok := c.LocalAddr().(*net.UDPAddr)
	if !ok {
		return nil
	}
	if ip4 := u.IP.To4(); ip4 != nil {
		return ip4
	}
	return nil
}

// kadPublishEndpoint 返回用于 KAD 发布源（TCP）的本机可达地址；与 eMule 类似，绑定具体网卡时用监听地址，否则用出站 IPv4。
func (s *Session) kadPublishEndpoint() protocol.Endpoint {
	s.mu.Lock()
	port := s.settings.ListenPort
	listener := s.listener
	s.mu.Unlock()
	if port <= 0 {
		return protocol.Endpoint{}
	}
	var ip net.IP
	if listener != nil {
		if a, ok := listener.Addr().(*net.TCPAddr); ok && a.IP != nil {
			if !a.IP.IsUnspecified() && !a.IP.IsLoopback() {
				ip = a.IP.To4()
			}
		}
	}
	if ip == nil {
		ip = localOutboundIPv4()
	}
	if ip == nil {
		return protocol.Endpoint{}
	}
	ep, err := protocol.EndpointFromString(ip.String(), port)
	if err != nil {
		return protocol.Endpoint{}
	}
	return ep
}

func kadTagsForSharedFile(name string, size int64) []kadproto.Tag {
	tags := []kadproto.Tag{
		{Type: kadproto.TagTypeString, ID: protocol.FTFilename, String: name},
		{Type: kadproto.TagTypeUint32, ID: protocol.FTFileSize, UInt64: uint64(uint32(size))},
	}
	if size > 0xffffffff {
		tags = append(tags, kadproto.Tag{Type: kadproto.TagTypeUint32, ID: protocol.FTFileSizeHi, UInt64: uint64(uint64(size) >> 32)})
	}
	return tags
}

func (s *Session) noteKadPublish(ep protocol.Endpoint, now int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastKadPublishEndpoint = ep
	s.lastKadPeriodicPublishAt = now
}

func (s *Session) publishSingleTransferKAD(tracker *DHTTracker, ep protocol.Endpoint, t *Transfer) {
	if tracker == nil || t == nil || !ep.Defined() {
		return
	}
	hash := t.GetHash()
	if !tracker.PublishSource(hash, ep, t.Size()) {
		logx.Debug("kad publish source skipped or failed", "hash", hash.String())
	}
	keyword := pickKadKeyword(t.FileName())
	if keyword == "" {
		return
	}
	keywordHash, err := protocol.HashFromData([]byte(keyword))
	if err != nil {
		return
	}
	entry := kadproto.SearchEntry{
		ID:   kadproto.NewID(hash),
		Tags: kadTagsForSharedFile(t.FileName(), t.Size()),
	}
	if !tracker.PublishKeyword(keywordHash, entry) {
		logx.Debug("kad publish keyword skipped or failed", "keyword", keyword, "hash", hash.String())
	}
}

// PublishTransferToKAD 在任务已完成时向 KAD 发布文件源与（可选）关键字索引，需 EnableDHT 且已设置 DHTTracker。
func (s *Session) PublishTransferToKAD(t *Transfer) {
	if s == nil || t == nil {
		return
	}
	if !s.settings.EnableDHT {
		return
	}
	tracker := s.dhtTracker
	if tracker == nil {
		return
	}
	if t.IsPaused() || t.IsAborted() || !t.isFinishedForSharePublish() {
		return
	}
	ep := s.kadPublishEndpoint()
	if !ep.Defined() {
		return
	}
	s.publishSingleTransferKAD(tracker, ep, t)
	now := CurrentTime()
	s.noteKadPublish(ep, now)
}

func (s *Session) publishAllFinishedTransfersKAD(ep protocol.Endpoint) {
	if !s.settings.EnableDHT || s.dhtTracker == nil || !ep.Defined() {
		return
	}
	tracker := s.dhtTracker
	for _, t := range s.snapshotTransfers() {
		if t == nil || t.IsPaused() || t.IsAborted() || !t.isFinishedForSharePublish() {
			continue
		}
		s.publishSingleTransferKAD(tracker, ep, t)
	}
}

func (s *Session) publishAllFinishedTransfersKADAfterServerChange() {
	if !s.settings.EnableDHT || s.dhtTracker == nil {
		return
	}
	ep := s.kadPublishEndpoint()
	if !ep.Defined() {
		return
	}
	s.publishAllFinishedTransfersKAD(ep)
	s.noteKadPublish(ep, CurrentTime())
}

func (s *Session) maybePeriodicKadPublish(now int64) {
	if !s.settings.EnableDHT || s.dhtTracker == nil {
		return
	}
	ep := s.kadPublishEndpoint()
	if !ep.Defined() {
		return
	}
	s.mu.Lock()
	lastEp := s.lastKadPublishEndpoint
	lastAt := s.lastKadPeriodicPublishAt
	s.mu.Unlock()

	if !ep.Equal(lastEp) {
		s.publishAllFinishedTransfersKAD(ep)
		s.noteKadPublish(ep, now)
		return
	}
	if lastAt == 0 {
		s.mu.Lock()
		if s.lastKadPeriodicPublishAt == 0 {
			s.lastKadPeriodicPublishAt = now
		}
		s.mu.Unlock()
		return
	}
	if now-lastAt < kadPeriodicPublishInterval {
		return
	}
	s.publishAllFinishedTransfersKAD(ep)
	s.noteKadPublish(ep, now)
}
