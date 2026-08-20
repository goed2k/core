package goed2k

import (
	"testing"

	"github.com/goed2k/core/data"
	"github.com/goed2k/core/protocol"
	clientproto "github.com/goed2k/core/protocol/client"
)

func attachRemoteQueueTestPeer(t *testing.T, session *Session, transfer *Transfer, endpoint protocol.Endpoint) (*Peer, *PeerConnection) {
	t.Helper()
	if err := transfer.AddPeer(endpoint, int(PeerServer)); err != nil {
		t.Fatalf("添加来源失败: %v", err)
	}
	peer := transfer.policy.FindPeer(endpoint)
	if peer == nil {
		t.Fatal("策略中未找到来源")
	}
	conn := NewPeerConnection(session, endpoint, transfer, peer)
	transfer.connections = append(transfer.connections, conn)
	session.connections = append(session.connections, conn)
	transfer.policy.SetConnection(peer, conn)
	return peer, conn
}

func TestQueueRankingDisconnectPreservesRemoteQueueWithoutFailure(t *testing.T) {
	const sessionTime int64 = 10_000_000
	session, transfer := newTestTransfer(t)
	previousTime := currentCachedTime.Swap(sessionTime)
	t.Cleanup(func() { currentCachedTime.Store(previousTime) })
	endpoint, err := protocol.EndpointFromString("2.3.4.5", 4662)
	if err != nil {
		t.Fatalf("构造来源地址失败: %v", err)
	}
	peer, conn := attachRemoteQueueTestPeer(t, session, transfer, endpoint)
	peer.FailCount = 3

	// 在连接存活时插入排序更靠前的来源，覆盖 []Peer 扩容后旧指针失效的场景。
	earlier, err := protocol.EndpointFromString("1.2.3.4", 4662)
	if err != nil {
		t.Fatalf("构造额外来源地址失败: %v", err)
	}
	if err := transfer.AddPeer(earlier, int(PeerSourceExchange)); err != nil {
		t.Fatalf("插入额外来源失败: %v", err)
	}

	conn.HandleQueueRanking(&clientproto.QueueRanking{Rank: 17})
	if !conn.IsDisconnecting() || conn.DisconnectCode().Code() != QueueRanking.Code() {
		t.Fatal("收到 QueueRanking 后应保留原因并关闭 TCP")
	}
	conn.OnDisconnect(QueueRanking)

	stored := transfer.policy.FindPeer(endpoint)
	if stored == nil {
		t.Fatal("正常排队断开后不应删除来源")
	}
	if stored.FailCount != 3 {
		t.Fatalf("正常排队不应增加失败次数，实际为 %d", stored.FailCount)
	}
	if conn.failed {
		t.Fatal("QueueRanking 不应把连接标记为失败")
	}
	rank, queued := stored.RemoteQueueState()
	if !queued || rank != 17 {
		t.Fatalf("远端排队状态未跨连接保存: queued=%t rank=%d", queued, rank)
	}
	if stored.NextConnection != sessionTime+remoteQueueReaskInterval {
		t.Fatalf("重询时间不正确: got=%d want=%d", stored.NextConnection, sessionTime+remoteQueueReaskInterval)
	}
	duplicate := NewPeerWithSource(endpoint, true, int(PeerDHT))
	if added, err := transfer.policy.AddPeer(duplicate); err != nil || added {
		t.Fatalf("重复来源应合并而非新增: added=%t err=%v", added, err)
	}
	if rank, queued := stored.RemoteQueueState(); !queued || rank != 17 {
		t.Fatalf("合并重复来源不应覆盖排队状态: queued=%t rank=%d", queued, rank)
	}
}

func TestRemoteQueueStateUpdatesAndClearsAcrossConnections(t *testing.T) {
	const sessionTime int64 = 20_000_000
	session, transfer := newTestTransfer(t)
	previousTime := currentCachedTime.Swap(sessionTime)
	t.Cleanup(func() { currentCachedTime.Store(previousTime) })
	endpoint, err := protocol.EndpointFromString("3.4.5.6", 4662)
	if err != nil {
		t.Fatalf("构造来源地址失败: %v", err)
	}
	_, first := attachRemoteQueueTestPeer(t, session, transfer, endpoint)
	first.HandleQueueRanking(&clientproto.QueueRanking{Rank: 9})
	first.OnDisconnect(QueueRanking)

	peer := transfer.policy.FindPeer(endpoint)
	second := NewPeerConnection(session, endpoint, transfer, peer)
	transfer.policy.SetConnection(peer, second)
	if rank, queued := peer.RemoteQueueState(); !queued || rank != 9 {
		t.Fatalf("重新连接不应提前丢失排队状态: queued=%t rank=%d", queued, rank)
	}
	second.HandleQueueRanking(&clientproto.QueueRanking{Rank: 4})
	second.OnDisconnect(QueueRanking)

	peer = transfer.policy.FindPeer(endpoint)
	if rank, queued := peer.RemoteQueueState(); !queued || rank != 4 {
		t.Fatalf("重复重询应更新队列排名: queued=%t rank=%d", queued, rank)
	}

	accepted := NewPeerConnection(session, endpoint, transfer, peer)
	transfer.policy.SetConnection(peer, accepted)
	accepted.HandleAcceptUpload()
	if rank, queued := peer.RemoteQueueState(); queued || rank != 0 {
		t.Fatalf("AcceptUpload 应清除远端排队状态: queued=%t rank=%d", queued, rank)
	}
	if peer.NextConnection != 0 {
		t.Fatalf("AcceptUpload 应清除重询期限，实际为 %d", peer.NextConnection)
	}

	peer.markRemoteQueued(2, sessionTime+remoteQueueReaskInterval)
	downloading := NewPeerConnection(session, endpoint, transfer, peer)
	transfer.policy.SetConnection(peer, downloading)
	req, err := data.MakePeerRequest(0, 1)
	if err != nil {
		t.Fatalf("构造下载请求失败: %v", err)
	}
	downloading.ReceiveData(req, false)
	if rank, queued := peer.RemoteQueueState(); queued || rank != 0 {
		t.Fatalf("实际下载开始时应清除远端排队状态: queued=%t rank=%d", queued, rank)
	}
}

func TestFindConnectCandidateHonorsRemoteQueueDeadline(t *testing.T) {
	const deadline int64 = 50_000_000
	policy := NewPolicy(nil)
	endpoint, err := protocol.EndpointFromString("4.5.6.7", 4662)
	if err != nil {
		t.Fatalf("构造来源地址失败: %v", err)
	}
	peer := NewPeerWithSource(endpoint, true, int(PeerServer))
	peer.markRemoteQueued(6, deadline)
	if _, err := policy.AddPeer(peer); err != nil {
		t.Fatalf("添加来源失败: %v", err)
	}

	if candidate := policy.FindConnectCandidate(deadline - 1); candidate != nil {
		t.Fatal("重询期限到达前不应选中来源")
	}
	if candidate := policy.FindConnectCandidate(deadline); candidate == nil || !candidate.Endpoint.Equal(endpoint) {
		t.Fatal("边界时刻应允许重新询问来源")
	}
	if candidate := policy.FindConnectCandidate(deadline + 1); candidate == nil || !candidate.Endpoint.Equal(endpoint) {
		t.Fatal("重询期限到达后应允许重新询问来源")
	}
}

func TestOrdinaryDisconnectStillIncrementsFailCount(t *testing.T) {
	const sessionTime int64 = 30_000_000
	session, transfer := newTestTransfer(t)
	previousTime := currentCachedTime.Swap(sessionTime)
	t.Cleanup(func() { currentCachedTime.Store(previousTime) })
	endpoint, err := protocol.EndpointFromString("5.6.7.8", 4662)
	if err != nil {
		t.Fatalf("构造来源地址失败: %v", err)
	}
	peer, conn := attachRemoteQueueTestPeer(t, session, transfer, endpoint)
	peer.FailCount = 1

	conn.Close(ConnectionTimeout)
	conn.OnDisconnect(ConnectionTimeout)

	stored := transfer.policy.FindPeer(endpoint)
	if stored == nil {
		t.Fatal("普通错误后来源应保留供退避重试")
	}
	if stored.FailCount != 2 {
		t.Fatalf("普通错误仍应增加失败次数，实际为 %d", stored.FailCount)
	}
}

func TestQueuedPeerPauseResumeAndFailedReaskKeepDeadline(t *testing.T) {
	const (
		sessionTime int64 = 40_000_000
		deadline          = sessionTime + remoteQueueReaskInterval
	)
	session, transfer := newTestTransfer(t)
	previousTime := currentCachedTime.Swap(sessionTime)
	t.Cleanup(func() { currentCachedTime.Store(previousTime) })
	endpoint, err := protocol.EndpointFromString("6.7.8.9", 4662)
	if err != nil {
		t.Fatalf("构造来源地址失败: %v", err)
	}
	peer, conn := attachRemoteQueueTestPeer(t, session, transfer, endpoint)
	peer.markRemoteQueued(8, deadline)

	conn.Close(TransferPaused)
	conn.OnDisconnect(TransferPaused)
	stored := transfer.policy.FindPeer(endpoint)
	if rank, queued := stored.RemoteQueueState(); !queued || rank != 8 {
		t.Fatalf("暂停连接不应清除排队身份: queued=%t rank=%d", queued, rank)
	}
	if stored.NextConnection != deadline {
		t.Fatalf("暂停不应提前远端队列期限: got=%d want=%d", stored.NextConnection, deadline)
	}

	transfer.ResumeWithState()
	if stored.NextConnection != deadline {
		t.Fatalf("恢复任务不应绕过远端队列期限: got=%d want=%d", stored.NextConnection, deadline)
	}

	stored.NextConnection = 0 // 模拟到期后已经发起一次 TCP 重询。
	reask := NewPeerConnection(session, endpoint, transfer, stored)
	transfer.policy.SetConnection(stored, reask)
	reask.Close(ConnectionTimeout)
	reask.OnDisconnect(ConnectionTimeout)
	stored = transfer.policy.FindPeer(endpoint)
	if stored.FailCount != 1 {
		t.Fatalf("失败的重询仍应计入普通连接失败，实际为 %d", stored.FailCount)
	}
	if rank, queued := stored.RemoteQueueState(); !queued || rank != 8 {
		t.Fatalf("重询连接失败时应保留排队身份: queued=%t rank=%d", queued, rank)
	}
	if stored.NextConnection != sessionTime+remoteQueueReaskInterval {
		t.Fatalf("失败重询应重新设置安全期限: got=%d want=%d", stored.NextConnection, sessionTime+remoteQueueReaskInterval)
	}
}
