package goed2k

import (
	"net"
	"slices"

	"github.com/goed2k/core/protocol"
)

const (
	maxUploadTime           = 60 * 60 * 1000
	maxUploadData           = 10 * 1024 * 1024
	minUploadClientsAllowed = 2
	maxUploadClientsAllowed = 250
	// uploadQueuePurgeTimeout 对齐 eMule otherfunctions.h 的 MAX_PURGEQUEUETIME（70 分钟）。
	uploadQueuePurgeTimeout = 70 * 60 * 1000
)

// uploadWaiter 是上传等待队列的稳定身份。
// TCP 断开后仍保留 rank / lastAsked / 文件 hash / IP / UDP / UserHash（若已有），
// 以便同一对端重连或 UDP ReAsk 续上原来的排队位置。
type uploadWaiter struct {
	userHash       protocol.Hash
	fileHash       protocol.Hash
	client         *PeerConnection
	waitStart      int64
	lastAsked      int64
	rank           uint16
	addNextConnect bool
	endpoint       protocol.Endpoint
	udpPort        uint16
	lowID          bool
	friendSlot     bool
	boundToConn    bool
}

type UploadQueue struct {
	session         *Session
	waiting         []*uploadWaiter
	uploading       []*PeerConnection
	lastStartUpload int64
	lastSort        int64
	allowKicking    bool
	lastSlotHighID  bool
	suspended       map[string]bool
}

func NewUploadQueue(session *Session) *UploadQueue {
	return &UploadQueue{
		session:        session,
		waiting:        make([]*uploadWaiter, 0),
		uploading:      make([]*PeerConnection, 0),
		lastSlotHighID: true,
		suspended:      make(map[string]bool),
	}
}

func (q *UploadQueue) AddClientToQueue(client *PeerConnection) {
	if client == nil || client.IsDisconnecting() {
		return
	}
	now := CurrentTime()
	client.lastUploadRequest = now
	if w := q.findWaiter(client); w != nil {
		w.attach(client)
		w.lastAsked = now
		w.captureFromClient(client)
		if q.isSuspended(w.fileHash) {
			client.SendQueueRanking(w.rank)
			return
		}
		maxSlots := q.maxSlots()
		if w.addNextConnect && q.lastSlotHighID {
			maxSlots++
		}
		if w.addNextConnect && len(q.uploading) < maxSlots {
			w.addNextConnect = false
			client.SetUploadAddNextConnect(false)
			q.removeWaiter(w)
			q.addUpNextClient(client)
			q.lastSlotHighID = false
			return
		}
		client.SendQueueRanking(w.rank)
		return
	}
	if q.IsUploading(client) {
		client.SendAcceptUpload()
		return
	}
	src := client.ActiveUploadSource()
	if src == nil || q.isSuspended(src.GetHash()) {
		return
	}
	if q.session != nil && q.session.settings.UploadQueueSize > 0 && len(q.waiting) >= q.session.settings.UploadQueueSize {
		return
	}
	client.ClearUploadWaitStart()
	if len(q.waiting) == 0 && now-q.lastStartUpload >= Seconds(1) && len(q.uploading) < q.maxSlots() {
		q.addUpNextClient(client)
		q.lastStartUpload = now
		return
	}
	client.SetUploadWaitStart(now)
	client.SetUploadState(UploadStateOnQueue)
	w := newUploadWaiterFromClient(client)
	w.waitStart = now
	w.lastAsked = now
	q.waiting = append(q.waiting, w)
	q.sortWaiting()
	client.SendQueueRanking(client.UploadQueueRank())
}

func (q *UploadQueue) Process() {
	if q == nil {
		return
	}
	now := CurrentTime()
	q.purgeExpired(now)
	if len(q.waiting) == 0 || now-q.lastStartUpload < Seconds(1) {
		q.allowKicking = false
	} else if len(q.uploading) < q.maxSlots() {
		q.allowKicking = false
		q.lastStartUpload = now
		q.addUpNextClient(nil)
	} else {
		q.allowKicking = true
	}

	for _, client := range append([]*PeerConnection(nil), q.uploading...) {
		if client == nil {
			continue
		}
		if client.IsDisconnecting() || client.socket == nil {
			q.RemoveFromUploadQueue(client)
			continue
		}
		client.SendBlockData()
	}
	if now-q.lastSort > Minutes(2) {
		q.sortWaiting()
	}
}

func (q *UploadQueue) RemoveFromUploadQueue(client *PeerConnection) bool {
	if client == nil {
		return false
	}
	removed := false
	if idx := slices.Index(q.uploading, client); idx >= 0 {
		q.uploading = append(q.uploading[:idx], q.uploading[idx+1:]...)
		removed = true
	}
	if q.RemoveFromWaitingQueue(client) {
		removed = true
	}
	if removed {
		client.SetUploadState(UploadStateNone)
		client.ClearUploadBlockRequests()
	}
	return removed
}

// onClientDisconnect 在 TCP 断开时丢掉上传槽，但保留可持久化的等待身份。
func (q *UploadQueue) onClientDisconnect(client *PeerConnection) {
	if q == nil || client == nil {
		return
	}
	if idx := slices.Index(q.uploading, client); idx >= 0 {
		q.uploading = append(q.uploading[:idx], q.uploading[idx+1:]...)
		client.SetUploadState(UploadStateNone)
		client.ClearUploadBlockRequests()
	}
	w := q.findAttachedWaiter(client)
	if w == nil {
		return
	}
	w.captureFromClient(client)
	if !w.canPersist() {
		q.removeWaiter(w)
		client.SetUploadState(UploadStateNone)
		client.SetUploadQueueRank(0)
		client.ClearUploadWaitStart()
		return
	}
	w.client = nil
	client.SetUploadState(UploadStateNone)
}

func (q *UploadQueue) RemoveFromWaitingQueue(client *PeerConnection) bool {
	if client == nil {
		return false
	}
	w := q.findWaiter(client)
	if w == nil {
		return false
	}
	q.removeWaiter(w)
	client.SetUploadState(UploadStateNone)
	client.SetUploadQueueRank(0)
	client.ClearUploadWaitStart()
	return true
}

func (q *UploadQueue) IsOnUploadQueue(client *PeerConnection) bool {
	return q.findWaiter(client) != nil
}

func (q *UploadQueue) IsUploading(client *PeerConnection) bool {
	return slices.Index(q.uploading, client) >= 0
}

// FindWaitingByIPUDP 按 UDP 来源 IP+端口和文件 hash 查找等待项，包括已断开 TCP 的身份。
// 多个匹配时返回 multiple=true 且 waiter=nil，迫使对端走 TCP。
func (q *UploadQueue) FindWaitingByIPUDP(addr *net.UDPAddr, hash protocol.Hash) (waiter *uploadWaiter, multiple bool) {
	if q == nil || addr == nil {
		return nil, false
	}
	var found *uploadWaiter
	count := 0
	for _, current := range q.waiting {
		if current == nil || !uploadWaiterMatchesUDP(current, addr) {
			continue
		}
		if current.fileHash.Equal(protocol.Invalid) || !current.fileHash.Equal(hash) {
			continue
		}
		count++
		found = current
	}
	if count > 1 {
		return nil, true
	}
	return found, false
}

// RefreshWaitingByIPUDP 查找并刷新匹配的等待项。multiple 时不刷新，迫使对端走 TCP。
func (q *UploadQueue) RefreshWaitingByIPUDP(addr *net.UDPAddr, hash protocol.Hash) (rank uint16, found bool, multiple bool) {
	waiter, multiple := q.FindWaitingByIPUDP(addr, hash)
	if multiple {
		return 0, false, true
	}
	if waiter == nil {
		return 0, false, false
	}
	return q.RefreshWaitingAsk(waiter), true, false
}

// RefreshWaitingAsk 刷新等待项的最近询问时间并返回当前 rank。
func (q *UploadQueue) RefreshWaitingAsk(waiter *uploadWaiter) uint16 {
	if q == nil || waiter == nil {
		return 0
	}
	now := CurrentTime()
	waiter.lastAsked = now
	if waiter.client != nil {
		waiter.client.lastUploadRequest = now
		return waiter.client.UploadQueueRank()
	}
	return waiter.rank
}

// IsNearlyFull 对齐 eMule：等待人数 + 50 超过队列上限时，对未知请求方回复 QueueFull。
func (q *UploadQueue) IsNearlyFull() bool {
	if q == nil || q.session == nil {
		return false
	}
	size := q.session.settings.UploadQueueSize
	if size <= 0 {
		return false
	}
	return len(q.waiting)+clientUDPQueueFullSlack > size
}

func (q *UploadQueue) sortWaiting() {
	slices.SortStableFunc(q.waiting, func(a, b *uploadWaiter) int {
		if a == nil || b == nil {
			return 0
		}
		scoreA := a.score(q.session)
		scoreB := b.score(q.session)
		if scoreA != scoreB {
			if scoreA > scoreB {
				return -1
			}
			return 1
		}
		waitA := a.effectiveWaitStart()
		waitB := b.effectiveWaitStart()
		if waitA == waitB {
			return a.compareEndpoint(b)
		}
		if waitA < waitB {
			return -1
		}
		return 1
	})
	q.lastSort = CurrentTime()
	q.recomputeRanks()
}

func (q *UploadQueue) recomputeRanks() {
	for i, waiter := range q.waiting {
		if waiter == nil {
			continue
		}
		waiter.rank = uint16(i + 1)
		if waiter.client != nil {
			waiter.client.SetUploadQueueRank(waiter.rank)
		}
	}
}

func (q *UploadQueue) addUpNextClient(direct *PeerConnection) {
	var client *PeerConnection
	if direct != nil {
		client = direct
		if w := q.findWaiter(direct); w != nil {
			q.removeWaiter(w)
		}
	} else {
		if len(q.waiting) == 0 {
			return
		}
		best := -1
		for i, candidate := range q.waiting {
			if candidate == nil {
				continue
			}
			if q.isSuspended(candidate.fileHash) || !candidate.canTakeSlot() {
				if !q.isSuspended(candidate.fileHash) {
					candidate.addNextConnect = true
					if candidate.client != nil {
						candidate.client.SetUploadAddNextConnect(true)
					}
				}
				continue
			}
			best = i
			break
		}
		if best < 0 {
			return
		}
		chosen := q.waiting[best]
		client = chosen.client
		q.waiting = append(q.waiting[:best], q.waiting[best+1:]...)
		q.recomputeRanks()
	}
	if client == nil || q.IsUploading(client) {
		return
	}
	client.SetUploadState(UploadStateUploading)
	client.SetUploadQueueRank(0)
	client.SetUploadAddNextConnect(false)
	client.SetUploadStartTime(CurrentTime())
	client.ResetUploadSession()
	q.uploading = append(q.uploading, client)
	q.lastSlotHighID = !client.IsUploadLowID()
	if client.socket != nil {
		client.SendAcceptUpload()
	}
}

func (q *UploadQueue) CheckForTimeOver(client *PeerConnection) bool {
	if !q.allowKicking || client == nil {
		return false
	}
	if client.FriendSlot() {
		return false
	}
	if src := client.ActiveUploadSource(); src != nil && src.UploadPriority() == UploadPriorityPowerShare {
		vips := 0
		for _, current := range q.uploading {
			if current == nil {
				continue
			}
			if current.FriendSlot() {
				vips++
				continue
			}
			if curSrc := current.ActiveUploadSource(); curSrc != nil && curSrc.UploadPriority() == UploadPriorityPowerShare {
				vips++
			}
		}
		if vips <= q.maxSlots()/2 {
			return false
		}
	}
	if client.UploadStartDelay() > maxUploadTime || client.UploadSession() > maxUploadData {
		q.allowKicking = false
		return true
	}
	return false
}

func (q *UploadQueue) maxSlots() int {
	if q == nil || q.session == nil {
		return minUploadClientsAllowed
	}
	if q.session.settings.MaxUploadRateKB <= 0 {
		slotAllocation := q.session.settings.SlotAllocationKB
		if slotAllocation <= 0 {
			slotAllocation = 3
		}
		nMaxSlots := int(q.session.accumulator.UploadRate()/1024)/slotAllocation + 2
		if nMaxSlots < minUploadClientsAllowed {
			nMaxSlots = minUploadClientsAllowed
		}
		if nMaxSlots > maxUploadClientsAllowed {
			nMaxSlots = maxUploadClientsAllowed
		}
		return nMaxSlots
	}
	slotAllocation := q.session.settings.SlotAllocationKB
	if slotAllocation <= 0 {
		slotAllocation = 3
	}
	nMaxSlots := int(float64(q.session.settings.MaxUploadRateKB)/float64(slotAllocation) + 0.5)
	if nMaxSlots < minUploadClientsAllowed {
		nMaxSlots = minUploadClientsAllowed
	}
	if nMaxSlots > maxUploadClientsAllowed {
		nMaxSlots = maxUploadClientsAllowed
	}
	return nMaxSlots
}

func (q *UploadQueue) isSuspended(hash protocol.Hash) bool {
	if q == nil || hash.Equal(protocol.Invalid) {
		return false
	}
	return q.suspended[hash.String()]
}

func (q *UploadQueue) ResumeUpload(hash protocol.Hash) {
	if q == nil || hash.Equal(protocol.Invalid) {
		return
	}
	delete(q.suspended, hash.String())
}

func (q *UploadQueue) SuspendUpload(hash protocol.Hash, terminate bool) uint16 {
	if q == nil || hash.Equal(protocol.Invalid) {
		return 0
	}
	if !terminate {
		q.suspended[hash.String()] = true
	}
	removed := uint16(0)
	for _, client := range append([]*PeerConnection(nil), q.uploading...) {
		if client == nil {
			continue
		}
		src := client.ActiveUploadSource()
		if src == nil || !src.GetHash().Equal(hash) {
			continue
		}
		q.RemoveFromUploadQueue(client)
		if !terminate {
			client.SetUploadState(UploadStateOnQueue)
			client.SetUploadWaitStart(CurrentTime())
			q.waiting = append(q.waiting, newUploadWaiterFromClient(client))
			q.sortWaiting()
			client.SendQueueRanking(client.UploadQueueRank())
		}
		removed++
	}
	return removed
}

func (q *UploadQueue) findAttachedWaiter(client *PeerConnection) *uploadWaiter {
	if q == nil || client == nil {
		return nil
	}
	for _, waiter := range q.waiting {
		if waiter != nil && waiter.client == client {
			return waiter
		}
	}
	return nil
}

func (q *UploadQueue) findWaiter(client *PeerConnection) *uploadWaiter {
	if attached := q.findAttachedWaiter(client); attached != nil {
		return attached
	}
	if client == nil || uploadIdentityHash(client).Equal(protocol.Invalid) {
		return nil
	}
	for _, waiter := range q.waiting {
		if waiter == nil || waiter.boundToConn {
			continue
		}
		if waiter.userHash.Equal(client.remoteHash) {
			return waiter
		}
	}
	return nil
}

func (q *UploadQueue) removeWaiter(waiter *uploadWaiter) {
	if q == nil || waiter == nil {
		return
	}
	idx := slices.Index(q.waiting, waiter)
	if idx < 0 {
		return
	}
	q.waiting = append(q.waiting[:idx], q.waiting[idx+1:]...)
	q.recomputeRanks()
}

func (q *UploadQueue) purgeExpired(now int64) {
	if q == nil || now <= 0 {
		return
	}
	dst := q.waiting[:0]
	changed := false
	for _, waiter := range q.waiting {
		if waiter == nil {
			changed = true
			continue
		}
		asked := waiter.lastAsked
		if asked == 0 {
			asked = waiter.waitStart
		}
		if asked > 0 && now-asked > uploadQueuePurgeTimeout {
			if waiter.client != nil {
				waiter.client.SetUploadState(UploadStateNone)
				waiter.client.SetUploadQueueRank(0)
				waiter.client.ClearUploadWaitStart()
			}
			changed = true
			continue
		}
		dst = append(dst, waiter)
	}
	if changed {
		q.waiting = dst
		q.recomputeRanks()
	}
}

func newUploadWaiterFromClient(client *PeerConnection) *uploadWaiter {
	w := &uploadWaiter{client: client}
	w.captureFromClient(client)
	if client != nil {
		w.waitStart = client.UploadWaitStart()
		w.lastAsked = client.lastUploadRequest
		w.rank = client.UploadQueueRank()
		w.addNextConnect = client.UploadAddNextConnect()
	}
	return w
}

func (w *uploadWaiter) captureFromClient(client *PeerConnection) {
	if w == nil || client == nil {
		return
	}
	hash := uploadIdentityHash(client)
	w.userHash = hash
	w.fileHash = uploadFileHash(client)
	w.endpoint = client.Endpoint()
	w.udpPort = clientAdvertisedUDPPort(client)
	w.lowID = client.IsUploadLowID()
	w.friendSlot = client.FriendSlot()
	w.addNextConnect = w.addNextConnect || client.UploadAddNextConnect()
	w.boundToConn = hash.Equal(protocol.Invalid)
}

func (w *uploadWaiter) attach(client *PeerConnection) {
	if w == nil || client == nil {
		return
	}
	w.client = client
	if w.waitStart != 0 {
		client.SetUploadWaitStart(w.waitStart)
	}
	client.SetUploadState(UploadStateOnQueue)
	client.SetUploadQueueRank(w.rank)
	client.SetUploadAddNextConnect(w.addNextConnect)
}

func (w *uploadWaiter) canPersist() bool {
	return w != nil && !w.boundToConn && !w.userHash.Equal(protocol.Invalid)
}

func (w *uploadWaiter) canTakeSlot() bool {
	if w == nil || w.client == nil {
		return false
	}
	if w.lowID || w.client.IsUploadLowID() {
		return w.client.IsUploadConnected()
	}
	return true
}

func (w *uploadWaiter) effectiveWaitStart() int64 {
	if w == nil {
		return 0
	}
	if w.client != nil && w.client.UploadWaitStart() != 0 {
		return w.client.UploadWaitStart()
	}
	return w.waitStart
}

func (w *uploadWaiter) score(session *Session) uint32 {
	if w == nil {
		return 0
	}
	if w.client != nil {
		return w.client.UploadScore()
	}
	if w.friendSlot && !w.lowID {
		return 0x0FFFFFFF
	}
	waitStart := w.waitStart
	if waitStart == 0 {
		return 0
	}
	base := float64(CurrentTime()-waitStart) / 1000.0
	if session != nil && session.Credits() != nil && !w.userHash.Equal(protocol.Invalid) {
		base *= session.Credits().ScoreRatio(w.userHash)
	}
	if base < 0 {
		return 0
	}
	return uint32(base)
}

func (w *uploadWaiter) compareEndpoint(other *uploadWaiter) int {
	if w == nil || other == nil {
		return 0
	}
	left := w.endpoint
	right := other.endpoint
	if w.client != nil {
		left = w.client.Endpoint()
	}
	if other.client != nil {
		right = other.client.Endpoint()
	}
	return left.Compare(right)
}

func uploadIdentityHash(client *PeerConnection) protocol.Hash {
	if client == nil {
		return protocol.Invalid
	}
	return client.remoteHash
}

func uploadFileHash(client *PeerConnection) protocol.Hash {
	if client == nil {
		return protocol.Invalid
	}
	if src := client.ActiveUploadSource(); src != nil {
		return src.GetHash()
	}
	return protocol.Invalid
}

func clientAdvertisedUDPPort(client *PeerConnection) uint16 {
	if client == nil {
		return 0
	}
	if client.peerInfo != nil && client.peerInfo.UDPPort != 0 {
		return client.peerInfo.UDPPort
	}
	return client.remotePeerInfo.UDPPort
}

func uploadWaiterMatchesUDP(waiter *uploadWaiter, addr *net.UDPAddr) bool {
	if waiter == nil || addr == nil {
		return false
	}
	if waiter.client != nil {
		return uploadClientMatchesUDP(waiter.client, addr)
	}
	return uploadEndpointMatchesUDP(waiter.endpoint, waiter.udpPort, addr)
}

func uploadEndpointMatchesUDP(endpoint protocol.Endpoint, udpPort uint16, addr *net.UDPAddr) bool {
	if addr == nil || udpPort == 0 || !endpoint.Defined() {
		return false
	}
	ip4 := addr.IP.To4()
	if ip4 == nil {
		return false
	}
	want := protocol.EndpointFromInet(&net.TCPAddr{IP: ip4, Port: endpoint.Port()})
	if endpoint.IP() != want.IP() {
		return false
	}
	return int(udpPort) == addr.Port
}
