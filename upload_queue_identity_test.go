package goed2k

import (
	"bytes"
	"testing"

	"github.com/goed2k/core/protocol"
)

func newUploadableTestTransfer(t *testing.T) (*Session, *Transfer) {
	t.Helper()
	return newTestTransfer(t)
}

func newQueuedUploadPeer(t *testing.T, session *Session, transfer *Transfer, ip string, port int, userHash protocol.Hash, udpPort uint16) *PeerConnection {
	t.Helper()
	endpoint, err := protocol.EndpointFromString(ip, port)
	if err != nil {
		t.Fatalf("构造地址失败: %v", err)
	}
	peer := NewPeerWithSource(endpoint, true, int(PeerIncoming))
	peer.UDPPort = udpPort
	conn := NewPeerConnection(session, endpoint, transfer, &peer)
	conn.remoteHash = userHash
	return conn
}

func fillUploadSlots(queue *UploadQueue) {
	queue.lastStartUpload = CurrentTime()
	for len(queue.uploading) < queue.maxSlots() {
		queue.uploading = append(queue.uploading, &PeerConnection{Connection: NewConnection(queue.session)})
	}
}

func TestUploadQueueKeepsWaitIdentityAfterTCPDisconnect(t *testing.T) {
	session, transfer := newUploadableTestTransfer(t)
	queue := session.UploadQueue()
	fillUploadSlots(queue)

	userHash := protocol.MustHashFromString("11111111111111111111111111111111")
	conn := newQueuedUploadPeer(t, session, transfer, "10.0.0.1", 4662, userHash, 4672)
	queue.AddClientToQueue(conn)
	if !queue.IsOnUploadQueue(conn) {
		t.Fatal("首次请求应进入等待队列")
	}
	waitStart := conn.UploadWaitStart()
	rank := conn.UploadQueueRank()
	if waitStart == 0 || rank == 0 {
		t.Fatalf("等待起点或 rank 未建立: start=%d rank=%d", waitStart, rank)
	}

	queue.onClientDisconnect(conn)
	if len(queue.waiting) != 1 {
		t.Fatalf("断开 TCP 后应保留等待身份: waiting=%d", len(queue.waiting))
	}
	if queue.waiting[0].client != nil {
		t.Fatal("断开后等待项应分离连接指针")
	}
	if queue.waiting[0].waitStart != waitStart || queue.waiting[0].rank != rank {
		t.Fatalf("断开后身份被重置: start=%d rank=%d", queue.waiting[0].waitStart, queue.waiting[0].rank)
	}
	if !queue.IsOnUploadQueue(conn) {
		t.Fatal("原连接仍应能按 UserHash 找到等待身份")
	}
}

func TestUploadQueueReconnectPreservesWaitStartAndRank(t *testing.T) {
	session, transfer := newUploadableTestTransfer(t)
	queue := session.UploadQueue()
	fillUploadSlots(queue)

	userHash := protocol.MustHashFromString("22222222222222222222222222222222")
	first := newQueuedUploadPeer(t, session, transfer, "10.0.0.2", 4662, userHash, 4672)
	queue.AddClientToQueue(first)
	waitStart := first.UploadWaitStart()
	rank := first.UploadQueueRank()
	queue.onClientDisconnect(first)

	second := newQueuedUploadPeer(t, session, transfer, "10.0.0.2", 4663, userHash, 4672)
	queue.AddClientToQueue(second)
	if second.UploadWaitStart() != waitStart {
		t.Fatalf("重连后等待起点应保持: got=%d want=%d", second.UploadWaitStart(), waitStart)
	}
	if second.UploadQueueRank() != rank {
		t.Fatalf("重连后 rank 应保持: got=%d want=%d", second.UploadQueueRank(), rank)
	}
	if len(queue.waiting) != 1 || queue.waiting[0].client != second {
		t.Fatal("重连应附着到同一等待身份，而不是新建条目")
	}
}

func TestUploadQueueCancelRemovesPersistentIdentity(t *testing.T) {
	session, transfer := newUploadableTestTransfer(t)
	queue := session.UploadQueue()
	fillUploadSlots(queue)

	userHash := protocol.MustHashFromString("33333333333333333333333333333333")
	conn := newQueuedUploadPeer(t, session, transfer, "10.0.0.3", 4662, userHash, 4672)
	queue.AddClientToQueue(conn)
	oldStart := conn.UploadWaitStart()
	queue.RemoveFromUploadQueue(conn)
	if len(queue.waiting) != 0 {
		t.Fatal("Cancel/显式移除应删除等待身份")
	}

	again := newQueuedUploadPeer(t, session, transfer, "10.0.0.3", 4662, userHash, 4672)
	queue.AddClientToQueue(again)
	if again.UploadWaitStart() == 0 || again.UploadWaitStart() == oldStart {
		t.Fatalf("取消后再次请求应重新计时: old=%d new=%d", oldStart, again.UploadWaitStart())
	}
}

func TestUploadQueuePurgesStaleWaitIdentity(t *testing.T) {
	const sessionTime int64 = 80_000_000
	previousTime := currentCachedTime.Swap(sessionTime)
	t.Cleanup(func() { currentCachedTime.Store(previousTime) })

	session, transfer := newUploadableTestTransfer(t)
	queue := session.UploadQueue()
	fillUploadSlots(queue)

	userHash := protocol.MustHashFromString("44444444444444444444444444444444")
	conn := newQueuedUploadPeer(t, session, transfer, "10.0.0.4", 4662, userHash, 4672)
	queue.AddClientToQueue(conn)
	queue.onClientDisconnect(conn)
	queue.waiting[0].lastAsked = sessionTime - uploadQueuePurgeTimeout - 1

	queue.Process()
	if len(queue.waiting) != 0 {
		t.Fatal("超过 MAX_PURGEQUEUETIME 且未重询的身份应被清除")
	}
}

func TestUploadQueueUDPReaskAfterDisconnectKeepsIdentity(t *testing.T) {
	session, transfer, local, remote, peerAddr := newUDPReaskLoopback(t)
	defer local.Close()
	defer remote.Close()
	transfer.WeHave(0)

	queue := session.UploadQueue()
	fillUploadSlots(queue)
	userHash := protocol.MustHashFromString("55555555555555555555555555555555")
	endpoint, err := protocol.EndpointFromString("127.0.0.1", 4662)
	if err != nil {
		t.Fatalf("构造地址失败: %v", err)
	}
	peer := NewPeerWithSource(endpoint, true, int(PeerIncoming))
	peer.UDPPort = uint16(peerAddr.Port)
	conn := NewPeerConnection(session, endpoint, transfer, &peer)
	conn.remoteHash = userHash
	queue.AddClientToQueue(conn)
	rank := conn.UploadQueueRank()
	queue.onClientDisconnect(conn)

	session.handleClientUDP(peerAddr, encodeReaskFilePing(transfer.GetHash()))
	pkt := readUDPPacket(t, remote)
	if !bytes.Equal(pkt, encodeReaskAck(rank)) {
		t.Fatalf("断开后 UDP ReAsk 仍应按身份回 ACK: got % X want rank=%d", pkt, rank)
	}
	if queue.waiting[0].lastAsked == 0 || queue.waiting[0].client != nil {
		t.Fatal("UDP 重询应刷新 lastAsked 且不要求 TCP 仍存活")
	}
}

func TestUploadQueueFullStillAcceptsExistingIdentity(t *testing.T) {
	session, transfer := newUploadableTestTransfer(t)
	session.settings.UploadQueueSize = 1
	queue := session.UploadQueue()
	fillUploadSlots(queue)

	existingHash := protocol.MustHashFromString("66666666666666666666666666666666")
	strangerHash := protocol.MustHashFromString("77777777777777777777777777777777")
	existing := newQueuedUploadPeer(t, session, transfer, "10.0.0.6", 4662, existingHash, 4672)
	queue.AddClientToQueue(existing)
	queue.onClientDisconnect(existing)

	stranger := newQueuedUploadPeer(t, session, transfer, "10.0.0.7", 4662, strangerHash, 4673)
	queue.AddClientToQueue(stranger)
	if queue.IsOnUploadQueue(stranger) {
		t.Fatal("队列已满时新身份不得入队")
	}

	again := newQueuedUploadPeer(t, session, transfer, "10.0.0.6", 4664, existingHash, 4672)
	queue.AddClientToQueue(again)
	if !queue.IsOnUploadQueue(again) || again.UploadWaitStart() == 0 {
		t.Fatal("队列已满时仍应接受已有 UserHash 的重连")
	}
}

func TestUploadQueueDropsConnectionBoundWaiterOnDisconnect(t *testing.T) {
	session, transfer := newUploadableTestTransfer(t)
	queue := session.UploadQueue()
	fillUploadSlots(queue)

	conn := newQueuedUploadPeer(t, session, transfer, "10.0.0.8", 4662, protocol.Invalid, 4672)
	queue.AddClientToQueue(conn)
	if !queue.IsOnUploadQueue(conn) {
		t.Fatal("无 UserHash 时仍应按连接入队")
	}
	queue.onClientDisconnect(conn)
	if len(queue.waiting) != 0 {
		t.Fatal("没有稳定 UserHash 的等待项在断开后不得残留")
	}
}
