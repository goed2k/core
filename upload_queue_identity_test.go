package goed2k

import (
	"bytes"
	"net"
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

	currentCachedTime.Add(Seconds(5))
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

func TestUploadQueueDropsWaiterWithoutStableIdentity(t *testing.T) {
	session, transfer := newUploadableTestTransfer(t)
	queue := session.UploadQueue()
	fillUploadSlots(queue)

	conn := newQueuedUploadPeer(t, session, transfer, "10.0.0.8", 4662, protocol.Invalid, 0)
	queue.AddClientToQueue(conn)
	if !queue.IsOnUploadQueue(conn) {
		t.Fatal("无稳定标识时仍应按连接入队")
	}
	queue.onClientDisconnect(conn)
	if len(queue.waiting) != 0 {
		t.Fatal("既无 UserHash 也无 UDP 端口的等待项在断开后不得残留")
	}
}

func TestUploadQueueDropsConnectionBoundWaiterEvenWithUDPPort(t *testing.T) {
	session, transfer := newUploadableTestTransfer(t)
	queue := session.UploadQueue()
	fillUploadSlots(queue)

	conn := newQueuedUploadPeer(t, session, transfer, "10.0.0.8", 4662, protocol.Invalid, 4672)
	queue.AddClientToQueue(conn)
	queue.onClientDisconnect(conn)
	if len(queue.waiting) != 0 {
		t.Fatal("没有 UserHash 时即使有 UDP 端口也不得跨连接保留排队身份")
	}
}

func TestUploadQueueSuspendedFileDoesNotPromoteOnReconnect(t *testing.T) {
	session, transfer := newUploadableTestTransfer(t)
	queue := session.UploadQueue()
	fillUploadSlots(queue)

	userHash := protocol.MustHashFromString("10101010101010101010101010101010")
	conn := newQueuedUploadPeer(t, session, transfer, "10.0.0.11", 4662, userHash, 4672)
	queue.AddClientToQueue(conn)
	waitStart := conn.UploadWaitStart()
	queue.onClientDisconnect(conn)
	queue.waiting[0].addNextConnect = true
	queue.SuspendUpload(transfer.GetHash(), false)
	queue.uploading = queue.uploading[:len(queue.uploading)-1]
	queue.lastSlotHighID = true

	again := newQueuedUploadPeer(t, session, transfer, "10.0.0.11", 4663, userHash, 4672)
	queue.AddClientToQueue(again)
	if queue.IsUploading(again) {
		t.Fatal("挂起文件的已有身份重连不得被提升到上传槽")
	}
	if again.UploadWaitStart() != waitStart {
		t.Fatalf("挂起后重连仍应附着原等待身份: got=%d want=%d", again.UploadWaitStart(), waitStart)
	}

	queue.Process()
	if queue.IsUploading(again) {
		t.Fatal("Process 不得把挂起文件的等待项提升到上传槽")
	}
}

func TestUploadUDPReaskFileNotFoundAndQueueFullAfterDisconnect(t *testing.T) {
	session, transfer, local, remote, peerAddr := newUDPReaskLoopback(t)
	defer local.Close()
	defer remote.Close()
	session.settings.UploadQueueSize = 1
	queue := session.UploadQueue()
	fillUploadSlots(queue)

	userHash := protocol.MustHashFromString("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	endpoint, err := protocol.EndpointFromString("127.0.0.1", 4662)
	if err != nil {
		t.Fatalf("构造地址失败: %v", err)
	}
	peer := NewPeerWithSource(endpoint, true, int(PeerIncoming))
	peer.UDPPort = uint16(peerAddr.Port)
	conn := NewPeerConnection(session, endpoint, transfer, &peer)
	conn.remoteHash = userHash
	queue.AddClientToQueue(conn)
	queue.onClientDisconnect(conn)

	unknown := protocol.MustHashFromString("BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB")
	session.handleClientUDP(peerAddr, encodeReaskFilePing(unknown))
	if pkt := readUDPPacket(t, remote); !bytes.Equal(pkt, encodeFileNotFound()) {
		t.Fatalf("未知文件应回 FileNotFound: got % X", pkt)
	}

	stranger, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("监听陌生 UDP 失败: %v", err)
	}
	defer stranger.Close()
	session.handleClientUDP(stranger.LocalAddr().(*net.UDPAddr), encodeReaskFilePing(transfer.GetHash()))
	if pkt := readUDPPacket(t, stranger); !bytes.Equal(pkt, encodeQueueFull()) {
		t.Fatalf("队列将满且非等待者应回 QueueFull: got % X", pkt)
	}
}

func TestOldConnectionDisconnectDoesNotDetachReconnectedWaiter(t *testing.T) {
	session, transfer := newUploadableTestTransfer(t)
	queue := session.UploadQueue()
	fillUploadSlots(queue)

	userHash := protocol.MustHashFromString("99999999999999999999999999999999")
	first := newQueuedUploadPeer(t, session, transfer, "10.0.0.10", 4662, userHash, 4672)
	queue.AddClientToQueue(first)
	waitStart := first.UploadWaitStart()
	queue.onClientDisconnect(first)

	second := newQueuedUploadPeer(t, session, transfer, "10.0.0.10", 4663, userHash, 4672)
	queue.AddClientToQueue(second)
	first.OnDisconnect(QueueRanking)
	if len(queue.waiting) != 1 || queue.waiting[0].client != second {
		t.Fatal("旧连接延迟断开不得拆掉已重连的新等待项")
	}
	if second.UploadWaitStart() != waitStart {
		t.Fatalf("旧连接断开后等待起点被改动: got=%d want=%d", second.UploadWaitStart(), waitStart)
	}
}

func TestUploadQueueRejectsStealOfLiveWaiter(t *testing.T) {
	session, transfer := newUploadableTestTransfer(t)
	queue := session.UploadQueue()
	fillUploadSlots(queue)

	userHash := protocol.MustHashFromString("ABABABABABABABABABABABABABABABAB")
	owner := newQueuedUploadPeer(t, session, transfer, "10.0.0.11", 4662, userHash, 4672)
	queue.AddClientToQueue(owner)
	waitStart := owner.UploadWaitStart()
	rank := owner.UploadQueueRank()

	thief := newQueuedUploadPeer(t, session, transfer, "10.0.0.12", 4662, userHash, 4673)
	queue.AddClientToQueue(thief)
	if len(queue.waiting) != 1 || queue.waiting[0].client != owner {
		t.Fatal("已附着的等待身份不得被同 UserHash 的另一连接抢占")
	}
	if owner.UploadWaitStart() != waitStart || owner.UploadQueueRank() != rank {
		t.Fatal("被抢占尝试后原等待项的起点或 rank 被改动")
	}
}

func TestOnDisconnectParksPersistentWaitIdentity(t *testing.T) {
	session, transfer := newUploadableTestTransfer(t)
	queue := session.UploadQueue()
	fillUploadSlots(queue)

	userHash := protocol.MustHashFromString("88888888888888888888888888888888")
	conn := newQueuedUploadPeer(t, session, transfer, "10.0.0.9", 4662, userHash, 4672)
	queue.AddClientToQueue(conn)
	waitStart := conn.UploadWaitStart()
	conn.OnDisconnect(QueueRanking)
	if len(queue.waiting) != 1 || queue.waiting[0].client != nil {
		t.Fatal("OnDisconnect 应分离连接并保留等待身份")
	}
	if queue.waiting[0].waitStart != waitStart {
		t.Fatalf("OnDisconnect 不得重置等待起点: got=%d want=%d", queue.waiting[0].waitStart, waitStart)
	}
}

func TestUploadQueueProcessDoesNotDeleteReattachedIdentity(t *testing.T) {
	session, transfer := newUploadableTestTransfer(t)
	queue := session.UploadQueue()
	fillUploadSlots(queue)

	userHash := protocol.MustHashFromString("12121212121212121212121212121212")
	first := newQueuedUploadPeer(t, session, transfer, "10.0.0.13", 4662, userHash, 4672)
	queue.AddClientToQueue(first)
	waitStart := first.UploadWaitStart()
	queue.onClientDisconnect(first)

	second := newQueuedUploadPeer(t, session, transfer, "10.0.0.13", 4663, userHash, 4672)
	queue.AddClientToQueue(second)
	if len(queue.waiting) != 1 || queue.waiting[0].client != second {
		t.Fatal("重连后应附着到同一等待身份")
	}

	first.uploadState = UploadStateUploading
	queue.uploading = append([]*PeerConnection{first}, queue.uploading...)
	queue.Process()
	if len(queue.waiting) != 1 || queue.waiting[0].client != second {
		t.Fatal("旧上传连接的 Process 清理不得按 UserHash 删除已重连身份")
	}
	if second.UploadWaitStart() != waitStart {
		t.Fatalf("Process 后等待起点被改动: got=%d want=%d", second.UploadWaitStart(), waitStart)
	}
}

func TestUploadQueueDetachedScoreKeepsFilePriority(t *testing.T) {
	const sessionTime int64 = 90_000_000
	previousTime := currentCachedTime.Swap(sessionTime)
	t.Cleanup(func() { currentCachedTime.Store(previousTime) })

	session, transfer := newUploadableTestTransfer(t)
	transfer.SetUploadPriority(UploadPriorityPowerShare)
	queue := session.UploadQueue()
	fillUploadSlots(queue)

	userHash := protocol.MustHashFromString("13131313131313131313131313131313")
	conn := newQueuedUploadPeer(t, session, transfer, "10.0.0.14", 4662, userHash, 4672)
	queue.AddClientToQueue(conn)
	waitStart := sessionTime - Seconds(10)
	conn.SetUploadWaitStart(waitStart)
	queue.waiting[0].waitStart = waitStart
	attached := queue.waiting[0].score(session)
	if attached == 0 {
		t.Fatal("PowerShare 附着评分不应为 0")
	}
	queue.onClientDisconnect(conn)
	detached := queue.waiting[0].score(session)
	if detached != attached {
		t.Fatalf("断开后应保留文件优先级因子: detached=%d attached=%d", detached, attached)
	}
}
