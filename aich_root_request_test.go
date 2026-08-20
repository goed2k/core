package goed2k

import (
	"testing"

	"github.com/goed2k/core/protocol"
	clientproto "github.com/goed2k/core/protocol/client"
)

func unpackPeerPackets(t *testing.T, raws [][]byte) []protocol.Serializable {
	t.Helper()
	combiner := clientproto.NewPacketCombiner()
	out := make([]protocol.Serializable, 0, len(raws))
	for _, raw := range raws {
		_, packet, err := combiner.UnpackFrame(raw)
		if err != nil {
			t.Fatalf("unpack frame: %v", err)
		}
		out = append(out, packet)
	}
	return out
}

func collectAICHFileHashRequests(packets []protocol.Serializable) []*clientproto.AICHFileHashRequest {
	var out []*clientproto.AICHFileHashRequest
	for _, packet := range packets {
		if req, ok := packet.(*clientproto.AICHFileHashRequest); ok {
			out = append(out, req)
		}
	}
	return out
}

func collectAICHRequests(packets []protocol.Serializable) []*clientproto.AICHRequest {
	var out []*clientproto.AICHRequest
	for _, packet := range packets {
		if req, ok := packet.(*clientproto.AICHRequest); ok {
			out = append(out, req)
		}
	}
	return out
}

func newAICHPeer(t *testing.T, session *Session, transfer *Transfer, aichVersion int) *PeerConnection {
	t.Helper()
	endpoint, err := protocol.EndpointFromString("1.2.3.4", 4662)
	if err != nil {
		t.Fatalf("endpoint: %v", err)
	}
	conn := NewPeerConnection(session, endpoint, transfer, nil)
	conn.remotePeerInfo.Misc1.AICHVersion = aichVersion
	transfer.connections = append(transfer.connections, conn)
	return conn
}

func fileStatusFor(transfer *Transfer) *clientproto.FileStatusAnswer {
	return &clientproto.FileStatusAnswer{
		Hash:     transfer.GetHash(),
		BitField: protocol.NewBitField(transfer.numPieces),
	}
}

func TestFileStatusRequestsAICHRootWhenMissing(t *testing.T) {
	session, transfer := newTestTransfer(t)
	conn := newAICHPeer(t, session, transfer, 1)

	conn.HandleFileStatusAnswer(fileStatusFor(transfer))
	reqs := collectAICHFileHashRequests(unpackPeerPackets(t, conn.PendingPackets()))
	if len(reqs) != 1 {
		t.Fatalf("缺根且对端支持 AICH 时应发送 1 个根哈希请求，got %d", len(reqs))
	}
	if !reqs[0].Hash.Equal(transfer.GetHash()) {
		t.Fatalf("请求 hash 不匹配: got %s want %s", reqs[0].Hash.String(), transfer.GetHash().String())
	}

	conn.HandleFileStatusAnswer(fileStatusFor(transfer))
	reqs = collectAICHFileHashRequests(unpackPeerPackets(t, conn.PendingPackets()))
	if len(reqs) != 1 {
		t.Fatalf("同一连接不得重复请求根哈希: got %d", len(reqs))
	}
}

func TestFileStatusSkipsAICHRootWithoutSupportOrExistingRoot(t *testing.T) {
	t.Run("对端未宣告 AICH", func(t *testing.T) {
		session, transfer := newTestTransfer(t)
		conn := newAICHPeer(t, session, transfer, 0)
		conn.HandleFileStatusAnswer(fileStatusFor(transfer))
		if reqs := collectAICHFileHashRequests(unpackPeerPackets(t, conn.PendingPackets())); len(reqs) != 0 {
			t.Fatalf("未宣告 AICH 时不得请求根哈希: got %d", len(reqs))
		}
	})
	t.Run("任务已有根哈希", func(t *testing.T) {
		session, transfer := newTestTransfer(t)
		transfer.SetAICHRootHash(ComputeAICHHash([]byte("existing-root")))
		conn := newAICHPeer(t, session, transfer, 1)
		conn.HandleFileStatusAnswer(fileStatusFor(transfer))
		if reqs := collectAICHFileHashRequests(unpackPeerPackets(t, conn.PendingPackets())); len(reqs) != 0 {
			t.Fatalf("已有根哈希时不得再请求: got %d", len(reqs))
		}
	})
	t.Run("FileStatus 文件 hash 不匹配", func(t *testing.T) {
		session, transfer := newTestTransfer(t)
		conn := newAICHPeer(t, session, transfer, 1)
		status := fileStatusFor(transfer)
		status.Hash = protocol.MustHashFromString("00000000000000000000000000000001")
		conn.HandleFileStatusAnswer(status)
		if reqs := collectAICHFileHashRequests(unpackPeerPackets(t, conn.PendingPackets())); len(reqs) != 0 {
			t.Fatalf("文件 hash 不匹配时不得请求根哈希: got %d", len(reqs))
		}
	})
}

func TestAICHFileHashAnswerStoresRejectsAndConflicts(t *testing.T) {
	root := ComputeAICHHash([]byte("aich-root-payload"))
	conflict := ComputeAICHHash([]byte("other-root-payload"))
	session, transfer := newTestTransfer(t)
	conn := newAICHPeer(t, session, transfer, 1)

	conn.HandleAICHFileHashAnswer(&clientproto.AICHFileHashAnswer{
		Hash:     protocol.MustHashFromString("00000000000000000000000000000001"),
		RootHash: root,
	})
	if _, ok := transfer.AICHRootHash(); ok {
		t.Fatal("文件 hash 不匹配时不得保存根哈希")
	}

	conn.HandleAICHFileHashAnswer(&clientproto.AICHFileHashAnswer{
		Hash:     transfer.GetHash(),
		RootHash: protocol.InvalidAICH,
	})
	if _, ok := transfer.AICHRootHash(); ok {
		t.Fatal("零根哈希必须拒绝")
	}

	conn.HandleAICHFileHashAnswer(&clientproto.AICHFileHashAnswer{
		Hash:     transfer.GetHash(),
		RootHash: root,
	})
	got, ok := transfer.AICHRootHash()
	if !ok || !got.Equal(root) {
		t.Fatalf("首次合法应答应保存根哈希: ok=%v got=%s", ok, got.String())
	}

	conn.HandleAICHFileHashAnswer(&clientproto.AICHFileHashAnswer{
		Hash:     transfer.GetHash(),
		RootHash: conflict,
	})
	got, ok = transfer.AICHRootHash()
	if !ok || !got.Equal(root) {
		t.Fatalf("冲突根不得覆盖已有根: ok=%v got=%s", ok, got.String())
	}

	transfer.SetAICHRootHash(conflict)
	got, ok = transfer.AICHRootHash()
	if !ok || !got.Equal(root) {
		t.Fatalf("SetAICHRootHash 不得覆盖已有根: ok=%v got=%s", ok, got.String())
	}
}

func TestAICHRootAnswerRetriesPendingPieceRecovery(t *testing.T) {
	session, transfer := newTestTransfer(t)
	conn := newAICHPeer(t, session, transfer, 1)
	transfer.aichPendingPiece[0] = true

	root := ComputeAICHHash([]byte("pending-recovery-root"))
	conn.HandleAICHFileHashAnswer(&clientproto.AICHFileHashAnswer{
		Hash:     transfer.GetHash(),
		RootHash: root,
	})
	reqs := collectAICHRequests(unpackPeerPackets(t, conn.PendingPackets()))
	if len(reqs) != 1 {
		t.Fatalf("拿到根后应对挂起分片发送块哈希请求，got %d", len(reqs))
	}
	if !reqs[0].Hash.Equal(transfer.GetHash()) {
		t.Fatalf("块哈希请求 hash 不匹配: %s", reqs[0].Hash.String())
	}
}

func TestPieceHashMismatchWithoutAICHPeerDoesNotHoldPiece(t *testing.T) {
	session, transfer := newTestTransfer(t)
	_ = newAICHPeer(t, session, transfer, 0)
	want := protocol.MustHashFromString("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	transfer.hashSet = []protocol.Hash{want, want}

	transfer.OnPieceHashCompleted(0, protocol.EMule)
	if transfer.aichPendingPiece[0] {
		t.Fatal("没有 AICH 来源时不得把坏片挂起，应回落到整片重下")
	}
}

func TestPieceHashMismatchWithoutRootRequestsRoot(t *testing.T) {
	session, transfer := newTestTransfer(t)
	conn := newAICHPeer(t, session, transfer, 1)
	want := protocol.MustHashFromString("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	transfer.hashSet = []protocol.Hash{want, want}

	transfer.OnPieceHashCompleted(0, protocol.EMule)
	if !transfer.aichPendingPiece[0] {
		t.Fatal("缺根时坏片应挂起 AICH 恢复，而不是立即整片重下")
	}
	reqs := collectAICHFileHashRequests(unpackPeerPackets(t, conn.PendingPackets()))
	if len(reqs) != 1 {
		t.Fatalf("缺根坏片应向支持 AICH 的来源请求根哈希，got %d", len(reqs))
	}
}

func TestAICHFileHashPacketRoundtrip(t *testing.T) {
	t.Parallel()
	hash := protocol.EMule
	root := ComputeAICHHash([]byte("roundtrip-root"))
	combiner := clientproto.NewPacketCombiner()

	reqRaw, err := combiner.Pack("client.AICHFileHashRequest", &clientproto.AICHFileHashRequest{Hash: hash})
	if err != nil {
		t.Fatalf("pack request: %v", err)
	}
	_, reqPacket, err := combiner.UnpackFrame(reqRaw)
	if err != nil {
		t.Fatalf("unpack request: %v", err)
	}
	req, ok := reqPacket.(*clientproto.AICHFileHashRequest)
	if !ok || !req.Hash.Equal(hash) {
		t.Fatalf("request 往返失败: %+v", reqPacket)
	}

	ansRaw, err := combiner.Pack("client.AICHFileHashAnswer", &clientproto.AICHFileHashAnswer{Hash: hash, RootHash: root})
	if err != nil {
		t.Fatalf("pack answer: %v", err)
	}
	_, ansPacket, err := combiner.UnpackFrame(ansRaw)
	if err != nil {
		t.Fatalf("unpack answer: %v", err)
	}
	ans, ok := ansPacket.(*clientproto.AICHFileHashAnswer)
	if !ok || !ans.Hash.Equal(hash) || !ans.RootHash.Equal(root) {
		t.Fatalf("answer 往返失败: %+v", ansPacket)
	}
}
