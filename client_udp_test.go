package goed2k

import (
	"bytes"
	"net"
	"testing"
	"time"

	"github.com/goed2k/core/protocol"
	clientproto "github.com/goed2k/core/protocol/client"
)

func TestClientUDPReaskCodecGoldenBytes(t *testing.T) {
	hash := protocol.MustHashFromString("0123456789ABCDEF0123456789ABCDEF")

	ping := encodeReaskFilePing(hash)
	if got, want := ping, append([]byte{protocol.EMuleProt, clientUDPReaskFilePing}, hash.Bytes()...); !bytes.Equal(got, want) {
		t.Fatalf("Ping 编码错误: got % X want % X", got, want)
	}
	opcode, payload, ok := parseClientUDP(ping)
	if !ok || opcode != clientUDPReaskFilePing {
		t.Fatalf("Ping 解码失败: ok=%t opcode=%#x", ok, opcode)
	}
	decoded, ok := parseReaskFilePing(payload)
	if !ok || !decoded.Equal(hash) {
		t.Fatalf("Ping hash 解码错误: ok=%t hash=%s", ok, decoded.String())
	}

	ack := encodeReaskAck(0x3412)
	if got, want := ack, []byte{protocol.EMuleProt, clientUDPReaskAck, 0x12, 0x34}; !bytes.Equal(got, want) {
		t.Fatalf("ACK 编码错误: got % X want % X", got, want)
	}
	rank, ok := parseReaskAckRank(ack[2:])
	if !ok || rank != 0x3412 {
		t.Fatalf("ACK rank 解码错误: ok=%t rank=%d", ok, rank)
	}

	if got, want := encodeFileNotFound(), []byte{protocol.EMuleProt, clientUDPFileNotFound}; !bytes.Equal(got, want) {
		t.Fatalf("FileNotFound 编码错误: got % X want % X", got, want)
	}
	if got, want := encodeQueueFull(), []byte{protocol.EMuleProt, clientUDPQueueFull}; !bytes.Equal(got, want) {
		t.Fatalf("QueueFull 编码错误: got % X want % X", got, want)
	}
}

func TestClientUDPReaskRejectsShortUnknownAndLegacyProbe(t *testing.T) {
	session := NewSession(NewSettings())
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 4672}

	// 旧 6 字节 0xE3 探测不再作为兼容路径，必须忽略且不回复。
	legacy := []byte{ed2kUDPHeader, clientUDPReaskFilePing, 0x12, 0x34, 0x56, 0x78}
	if _, _, ok := parseClientUDP(legacy); ok {
		t.Fatal("旧 0xE3 探测不应被解析为标准 ReAsk")
	}
	session.handleClientUDP(addr, legacy)

	if _, _, ok := parseClientUDP([]byte{protocol.EMuleProt}); ok {
		t.Fatal("1 字节短包不应解析")
	}
	if _, ok := parseReaskFilePing(make([]byte, 15)); ok {
		t.Fatal("不足 16 字节的 Ping 载荷应拒绝")
	}
	if _, ok := parseReaskAckRank([]byte{0x01}); ok {
		t.Fatal("不足 2 字节的 ACK 载荷应拒绝")
	}
	if opcode, _, ok := parseClientUDP([]byte{protocol.EMuleProt, 0x00}); !ok || opcode != 0x00 {
		t.Fatal("未知 opcode 应能被解析以便上层忽略")
	}
	session.handleClientUDP(addr, []byte{protocol.EMuleProt, 0x00})
	session.handleClientUDP(addr, []byte{protocol.PackedProt, clientUDPReaskFilePing})
	session.handleClientUDP(addr, nil)
}

func TestDownloadUDPReaskAckUpdatesRankWithoutFailCount(t *testing.T) {
	const sessionTime int64 = 70_000_000
	session, transfer := newTestTransfer(t)
	previousTime := currentCachedTime.Swap(sessionTime)
	t.Cleanup(func() { currentCachedTime.Store(previousTime) })

	endpoint, err := protocol.EndpointFromString("8.8.4.4", 4662)
	if err != nil {
		t.Fatalf("构造来源地址失败: %v", err)
	}
	peer, _ := attachRemoteQueueTestPeer(t, session, transfer, endpoint)
	peer.FailCount = 4
	peer.UDPPort = 4672
	peer.markRemoteQueued(11, sessionTime)
	peer.udpReaskPending = true

	addr := &net.UDPAddr{IP: net.IPv4(8, 8, 4, 4), Port: 4672}
	session.handleClientUDP(addr, encodeReaskAck(3))

	stored := transfer.policy.FindPeer(endpoint)
	rank, queued := stored.RemoteQueueState()
	if !queued || rank != 3 {
		t.Fatalf("ACK 应更新远端 rank: queued=%t rank=%d", queued, rank)
	}
	if stored.FailCount != 4 {
		t.Fatalf("ACK 不得增加 FailCount，实际为 %d", stored.FailCount)
	}
	if stored.udpReaskPending {
		t.Fatal("ACK 后应清除 pending")
	}
	if stored.NextConnection != sessionTime+remoteQueueReaskInterval {
		t.Fatalf("ACK 应推迟下次重询: got=%d want=%d", stored.NextConnection, sessionTime+remoteQueueReaskInterval)
	}
	if !session.IsUDPReachable() {
		t.Fatal("有效 ACK 应标记 UDP 可达")
	}
}

func TestDownloadUDPQueueFullAndFileNotFoundDoNotIncrementFailCount(t *testing.T) {
	const sessionTime int64 = 80_000_000
	session, transfer := newTestTransfer(t)
	previousTime := currentCachedTime.Swap(sessionTime)
	t.Cleanup(func() { currentCachedTime.Store(previousTime) })

	fullEP, err := protocol.EndpointFromString("9.9.9.9", 4662)
	if err != nil {
		t.Fatalf("构造来源地址失败: %v", err)
	}
	_, _ = attachRemoteQueueTestPeer(t, session, transfer, fullEP)
	fnfEP, err := protocol.EndpointFromString("9.9.9.8", 4662)
	if err != nil {
		t.Fatalf("构造来源地址失败: %v", err)
	}
	_, _ = attachRemoteQueueTestPeer(t, session, transfer, fnfEP)
	fullPeer := transfer.policy.FindPeer(fullEP)
	fnfPeer := transfer.policy.FindPeer(fnfEP)
	if fullPeer == nil || fnfPeer == nil {
		t.Fatal("策略中未找到测试来源")
	}
	fullPeer.FailCount = 2
	fullPeer.UDPPort = 4672
	fullPeer.udpReaskPending = true
	fnfPeer.FailCount = 5
	fnfPeer.UDPPort = 4673
	fnfPeer.markRemoteQueued(8, sessionTime)
	fnfPeer.udpReaskPending = true

	session.handleClientUDP(&net.UDPAddr{IP: net.IPv4(9, 9, 9, 9), Port: 4672}, encodeQueueFull())
	if fullPeer.FailCount != 2 {
		t.Fatalf("QueueFull 不得增加 FailCount，实际为 %d", fullPeer.FailCount)
	}
	if rank, queued := fullPeer.RemoteQueueState(); !queued || rank != 0 || !fullPeer.RemoteQueueFull() {
		t.Fatalf("QueueFull 应标记远端队列满: queued=%t rank=%d full=%t", queued, rank, fullPeer.RemoteQueueFull())
	}
	if fullPeer.NextConnection != sessionTime+remoteQueueReaskInterval {
		t.Fatal("QueueFull 应推迟下次重询")
	}

	session.handleClientUDP(&net.UDPAddr{IP: net.IPv4(9, 9, 9, 8), Port: 4673}, encodeFileNotFound())
	if fnfPeer.FailCount != 5 {
		t.Fatalf("FileNotFound 不得增加 FailCount，实际为 %d", fnfPeer.FailCount)
	}
	if rank, queued := fnfPeer.RemoteQueueState(); queued || rank != 0 || fnfPeer.RemoteQueueFull() {
		t.Fatalf("FileNotFound 应取消该文件排队: queued=%t rank=%d full=%t", queued, rank, fnfPeer.RemoteQueueFull())
	}
	if fnfPeer.udpReaskPending {
		t.Fatal("FileNotFound 后应清除 pending")
	}
}

func TestUploadUDPReaskAckForKnownQueuedFile(t *testing.T) {
	session, transfer, local, remote, peerAddr := newUDPReaskLoopback(t)
	defer local.Close()
	defer remote.Close()

	endpoint, err := protocol.EndpointFromString("127.0.0.1", 4662)
	if err != nil {
		t.Fatalf("构造来源地址失败: %v", err)
	}
	peer, conn := attachRemoteQueueTestPeer(t, session, transfer, endpoint)
	peer.UDPPort = uint16(peerAddr.Port)
	conn.SetUploadState(UploadStateOnQueue)
	conn.SetUploadQueueRank(7)
	session.UploadQueue().waiting = append(session.UploadQueue().waiting, conn)

	session.handleClientUDP(peerAddr, encodeReaskFilePing(transfer.GetHash()))
	pkt := readUDPPacket(t, remote)
	if !bytes.Equal(pkt, encodeReaskAck(7)) {
		t.Fatalf("上传侧匹配文件应回 ACK+rank: got % X", pkt)
	}
	if conn.lastUploadRequest == 0 {
		t.Fatal("匹配等待项后应刷新 last asked")
	}
}

func TestUploadUDPReaskIncompleteTransferIsKnownNotFileNotFound(t *testing.T) {
	session, transfer, local, remote, peerAddr := newUDPReaskLoopback(t)
	defer local.Close()
	defer remote.Close()
	if transfer.CanUpload() {
		t.Fatal("夹具任务尚无已完成分片，本例用于证明 FileNotFound 不等于不能上传")
	}
	session.handleClientUDP(peerAddr, encodeReaskFilePing(transfer.GetHash()))
	_ = remote.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	buf := make([]byte, 64)
	n, _, err := remote.ReadFromUDP(buf)
	if err == nil && bytes.Equal(buf[:n], encodeFileNotFound()) {
		t.Fatal("已知但尚不能上传的文件不得回 FileNotFound，否则对端会取消该来源")
	}
}

func TestUploadUDPReaskFileNotFoundForUnknownHash(t *testing.T) {
	session, _, local, remote, peerAddr := newUDPReaskLoopback(t)
	defer local.Close()
	defer remote.Close()

	unknown := protocol.MustHashFromString("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	session.handleClientUDP(peerAddr, encodeReaskFilePing(unknown))
	pkt := readUDPPacket(t, remote)
	if !bytes.Equal(pkt, encodeFileNotFound()) {
		t.Fatalf("未知文件应回 FileNotFound: got % X", pkt)
	}
}

func TestQueuedSourcePrefersUDPReaskOverTCP(t *testing.T) {
	const sessionTime int64 = 90_000_000
	_, transfer, local, remote, peerAddr := newUDPReaskLoopback(t)
	defer local.Close()
	defer remote.Close()
	previousTime := currentCachedTime.Swap(sessionTime)
	t.Cleanup(func() { currentCachedTime.Store(previousTime) })

	endpoint, err := protocol.EndpointFromString("127.0.0.1", 4662)
	if err != nil {
		t.Fatalf("构造来源地址失败: %v", err)
	}
	if err := transfer.AddPeer(endpoint, int(PeerServer)); err != nil {
		t.Fatalf("添加来源失败: %v", err)
	}
	peer := transfer.policy.FindPeer(endpoint)
	peer.UDPPort = uint16(peerAddr.Port)
	peer.markRemoteQueued(5, sessionTime)

	sent, err := transfer.policy.ConnectOnePeer(sessionTime)
	if err != nil || !sent {
		t.Fatalf("到期排队来源应优先 UDP ReAsk: sent=%t err=%v", sent, err)
	}
	if peer.Connection != nil {
		t.Fatal("已知 UDP 端口时不应立刻新开 TCP")
	}
	if !peer.udpReaskPending {
		t.Fatal("出站 UDP ReAsk 后应设置 pending")
	}
	if peer.NextConnection != sessionTime+remoteQueueReaskInterval {
		t.Fatalf("出站 UDP 应推迟下次重询: got=%d", peer.NextConnection)
	}
	pkt := readUDPPacket(t, remote)
	if !bytes.Equal(pkt, encodeReaskFilePing(transfer.GetHash())) {
		t.Fatalf("出站载荷应为标准 ReAsk Ping: got % X", pkt)
	}
	if candidate := transfer.policy.FindConnectCandidate(sessionTime + 1); candidate != nil {
		t.Fatal("UDP 重询后、期限到达前不应再选出同一来源，避免 UDP/TCP 双路径")
	}

	// 上次 UDP 未收到应答时，到期应回退 TCP，而不是无限只发 UDP。
	peer.NextConnection = sessionTime
	if !peer.udpReaskPending {
		t.Fatal("预期仍处于 UDP pending")
	}
	if transfer.session.tryUDPReaskQueuedPeer(transfer, peer, sessionTime) {
		t.Fatal("已有未应答 pending 时 ConnectOnePeer 不应再发 UDP")
	}
}

func TestDownloadUDPReaskIgnoresUnsolicitedAck(t *testing.T) {
	const sessionTime int64 = 91_000_000
	session, transfer := newTestTransfer(t)
	previousTime := currentCachedTime.Swap(sessionTime)
	t.Cleanup(func() { currentCachedTime.Store(previousTime) })
	endpoint, err := protocol.EndpointFromString("11.12.13.14", 4662)
	if err != nil {
		t.Fatalf("构造来源地址失败: %v", err)
	}
	peer, _ := attachRemoteQueueTestPeer(t, session, transfer, endpoint)
	peer.UDPPort = 4672
	peer.markRemoteQueued(6, sessionTime)
	session.handleClientUDP(&net.UDPAddr{IP: net.IPv4(11, 12, 13, 14), Port: 4672}, encodeReaskAck(1))
	if rank, queued := peer.RemoteQueueState(); !queued || rank != 6 {
		t.Fatalf("无 pending 的 ACK 应忽略: queued=%t rank=%d", queued, rank)
	}
}

func TestDownloadUDPReaskAckSelectsPendingPeerAmongSharedSources(t *testing.T) {
	const sessionTime int64 = 92_000_000
	session, first := newTestTransfer(t)
	previousTime := currentCachedTime.Swap(sessionTime)
	t.Cleanup(func() { currentCachedTime.Store(previousTime) })
	second, err := NewTransfer(session, AddTransferParams{
		Hash:       protocol.MustHashFromString("FEDCBA9876543210FEDCBA9876543210"),
		CreateTime: CurrentTimeMillis(),
		Size:       PieceSize * 2,
	})
	if err != nil {
		t.Fatalf("构造第二任务失败: %v", err)
	}
	session.transfers[second.hash] = second

	endpoint, err := protocol.EndpointFromString("12.12.12.12", 4662)
	if err != nil {
		t.Fatalf("构造来源地址失败: %v", err)
	}
	_, _ = attachRemoteQueueTestPeer(t, session, first, endpoint)
	_, _ = attachRemoteQueueTestPeer(t, session, second, endpoint)
	idle := first.policy.FindPeer(endpoint)
	pending := second.policy.FindPeer(endpoint)
	idle.UDPPort = 4672
	idle.markRemoteQueued(9, sessionTime)
	pending.UDPPort = 4672
	pending.markRemoteQueued(4, sessionTime)
	pending.udpReaskPending = true

	session.handleClientUDP(&net.UDPAddr{IP: net.IPv4(12, 12, 12, 12), Port: 4672}, encodeReaskAck(2))
	if rank, queued := idle.RemoteQueueState(); !queued || rank != 9 {
		t.Fatalf("未 pending 的并行下载不应被 ACK 改写: queued=%t rank=%d", queued, rank)
	}
	if rank, queued := pending.RemoteQueueState(); !queued || rank != 2 {
		t.Fatalf("pending 的并行下载应收到 ACK: queued=%t rank=%d", queued, rank)
	}
}

func TestUploadUDPReaskUsesFileHashToDisambiguateWaitingClients(t *testing.T) {
	session, first, local, remote, peerAddr := newUDPReaskLoopback(t)
	defer local.Close()
	defer remote.Close()
	second, err := NewTransfer(session, AddTransferParams{
		Hash:       protocol.MustHashFromString("FEDCBA9876543210FEDCBA9876543210"),
		CreateTime: CurrentTimeMillis(),
		Size:       PieceSize * 2,
	})
	if err != nil {
		t.Fatalf("构造第二任务失败: %v", err)
	}
	session.transfers[second.hash] = second

	endpoint, err := protocol.EndpointFromString("127.0.0.1", 4662)
	if err != nil {
		t.Fatalf("构造来源地址失败: %v", err)
	}
	firstPeer, firstConn := attachRemoteQueueTestPeer(t, session, first, endpoint)
	secondPeer, secondConn := attachRemoteQueueTestPeer(t, session, second, endpoint)
	firstPeer.UDPPort = uint16(peerAddr.Port)
	secondPeer.UDPPort = uint16(peerAddr.Port)
	firstConn.SetUploadState(UploadStateOnQueue)
	firstConn.SetUploadQueueRank(3)
	secondConn.SetUploadState(UploadStateOnQueue)
	secondConn.SetUploadQueueRank(11)
	q := session.UploadQueue()
	q.waiting = append(q.waiting, firstConn, secondConn)

	session.handleClientUDP(peerAddr, encodeReaskFilePing(second.GetHash()))
	pkt := readUDPPacket(t, remote)
	if !bytes.Equal(pkt, encodeReaskAck(11)) {
		t.Fatalf("应按文件 hash 选择等待项: got % X", pkt)
	}
	if secondConn.lastUploadRequest == 0 {
		t.Fatal("匹配的等待项应刷新 last asked")
	}
	if firstConn.lastUploadRequest != 0 {
		t.Fatal("不同文件的等待项不应被刷新")
	}
}

func TestHelloPersistsRemoteUDPPortForLaterReask(t *testing.T) {
	session, transfer := newTestTransfer(t)
	endpoint, err := protocol.EndpointFromString("10.1.2.3", 4662)
	if err != nil {
		t.Fatalf("构造来源地址失败: %v", err)
	}
	peer, conn := attachRemoteQueueTestPeer(t, session, transfer, endpoint)
	hello := clientproto.Hello{
		HashLength: 16,
		HelloAnswer: clientproto.HelloAnswer{
			Hash:  protocol.EMule,
			Point: endpoint,
			Properties: protocol.TagList{
				protocol.NewUInt32Tag(helloTagUDPPorts, 0x1234),
			},
		},
	}
	conn.HandleClientHello(&hello)
	if peer.UDPPort != 0x1234 {
		t.Fatalf("Hello 0xF9 应写入逻辑来源 UDP 端口: got %d", peer.UDPPort)
	}
}

func newUDPReaskLoopback(t *testing.T) (*Session, *Transfer, *net.UDPConn, *net.UDPConn, *net.UDPAddr) {
	t.Helper()
	session, transfer := newTestTransfer(t)
	local, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	remote, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		local.Close()
		t.Fatal(err)
	}
	session.serverStatUDPConn = local
	return session, transfer, local, remote, remote.LocalAddr().(*net.UDPAddr)
}

func readUDPPacket(t *testing.T, conn *net.UDPConn) []byte {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 64)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("读取 UDP 应答失败: %v", err)
	}
	return append([]byte(nil), buf[:n]...)
}
