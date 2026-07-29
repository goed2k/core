package goed2k

import (
	"net"
	"testing"
	"time"

	"github.com/goed2k/core/protocol"
	serverproto "github.com/goed2k/core/protocol/server"
)

func TestFoundFileSourcesAddsLowIDPeer(t *testing.T) {
	session := NewSession(NewSettings())
	session.clientID = 0x0100007f
	hash := protocol.MustHashFromString("23A8CEFF57A7A32D562D649ED7893796")
	transfer, err := NewTransfer(session, AddTransferParams{
		Hash:       hash,
		CreateTime: CurrentTimeMillis(),
		Size:       1024,
	})
	if err != nil {
		t.Fatalf("new transfer: %v", err)
	}
	session.transfers[hash] = transfer

	sc := NewServerConnection("test", nil, session)
	packet := serverproto.FoundFileSources{
		Hash:    hash,
		Sources: []protocol.Endpoint{protocol.NewEndpoint(12345, 4662)},
	}
	applyFoundFileSources(sc, &packet)

	if transfer.policy.Size() != 1 {
		t.Fatalf("expected 1 peer, got %d", transfer.policy.Size())
	}
	peer := transfer.policy.peers[0]
	if peer.ServerClientID != 12345 {
		t.Fatalf("expected ServerClientID 12345, got %d", peer.ServerClientID)
	}
	if peer.SourceFlag&int(PeerServer) == 0 {
		t.Fatal("expected PeerServer flag")
	}
}

func applyFoundFileSources(sc *ServerConnection, value *serverproto.FoundFileSources) {
	if sc == nil || value == nil {
		return
	}
	if transfer := sc.session.LookupTransfer(value.Hash); transfer != nil {
		for _, ep := range value.Sources {
			if IsLowID(ep.IP()) {
				peer := NewPeerWithSource(protocol.Endpoint{}, true, int(PeerServer))
				peer.ServerClientID = ep.IP()
				if transfer.session != nil {
					transfer.session.mu.Lock()
				}
				_, _ = transfer.policy.AddPeer(peer)
				if transfer.session != nil {
					transfer.session.mu.Unlock()
				}
			} else {
				_ = transfer.AddPeer(ep, int(PeerServer))
			}
		}
	}
}

func TestRequestServerCallbackRateLimit(t *testing.T) {
	session := NewSession(NewSettings())
	session.clientID = 0x0100007f
	hash := protocol.MustHashFromString("23A8CEFF57A7A32D562D649ED7893796")
	transfer, err := NewTransfer(session, AddTransferParams{
		Hash:       hash,
		CreateTime: CurrentTimeMillis(),
		Size:       1024,
	})
	if err != nil {
		t.Fatalf("new transfer: %v", err)
	}

	sc := NewServerConnection("srv", nil, session)
	sc.handshakeCompleted = true
	session.serverConnection = sc
	session.serverConnections["srv"] = sc

	clientID := int32(999)
	if !session.RequestServerCallback(transfer, clientID) {
		t.Fatal("expected first callback request to succeed")
	}
	if got := session.callbacks[clientID]; !got.Equal(hash) {
		t.Fatalf("expected callback hash %s, got %s", hash, got)
	}
	if session.RequestServerCallback(transfer, clientID) {
		t.Fatal("expected rate-limited callback request to be rejected")
	}
}

func TestPolicyConnectOnePeerUsesCallbackForLowIDSource(t *testing.T) {
	session := NewSession(NewSettings())
	session.clientID = 0x0100007f
	hash := protocol.MustHashFromString("23A8CEFF57A7A32D562D649ED7893796")
	transfer, err := NewTransfer(session, AddTransferParams{
		Hash:       hash,
		CreateTime: CurrentTimeMillis(),
		Size:       1024,
	})
	if err != nil {
		t.Fatalf("new transfer: %v", err)
	}
	sc := NewServerConnection("srv", nil, session)
	sc.handshakeCompleted = true
	session.serverConnection = sc

	peer := NewPeerWithSource(protocol.Endpoint{}, true, int(PeerServer))
	peer.ServerClientID = 4242
	_, _ = transfer.policy.AddPeer(peer)

	connected, err := transfer.policy.ConnectOnePeer(CurrentTime())
	if err != nil {
		t.Fatalf("connect one peer: %v", err)
	}
	if !connected {
		t.Fatal("expected callback connect to succeed")
	}
	if _, ok := session.callbacks[4242]; !ok {
		t.Fatal("expected pending callback for low-id peer")
	}
}

func TestTryAttachCallbackPeer(t *testing.T) {
	session := NewSession(NewSettings())
	hash := protocol.MustHashFromString("23A8CEFF57A7A32D562D649ED7893796")
	transfer, err := NewTransfer(session, AddTransferParams{
		Hash:       hash,
		CreateTime: CurrentTimeMillis(),
		Size:       1024,
	})
	if err != nil {
		t.Fatalf("new transfer: %v", err)
	}
	session.transfers[hash] = transfer
	session.callbacks[12345] = hash

	pc := NewIncomingPeerConnection(session, &loopbackConn{})
	session.tryAttachCallbackPeer(pc, 12345)
	if pc.transfer != transfer {
		t.Fatal("expected incoming peer attached to transfer")
	}
	if _, ok := session.callbacks[12345]; ok {
		t.Fatal("expected callback entry removed after attach")
	}
}

type loopbackConn struct{}

func (loopbackConn) Read([]byte) (int, error)         { return 0, nil }
func (loopbackConn) Write([]byte) (int, error)        { return 0, nil }
func (loopbackConn) Close() error                     { return nil }
func (loopbackConn) LocalAddr() net.Addr              { return nil }
func (loopbackConn) RemoteAddr() net.Addr             { return nil }
func (loopbackConn) SetDeadline(time.Time) error      { return nil }
func (loopbackConn) SetReadDeadline(time.Time) error  { return nil }
func (loopbackConn) SetWriteDeadline(time.Time) error { return nil }
