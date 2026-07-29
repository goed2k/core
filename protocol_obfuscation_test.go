package goed2k

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"testing"

	"github.com/goed2k/core/protocol"
)

func TestDeriveClientClientKeyRoundTrip(t *testing.T) {
	peerHash, err := protocol.RandomHash(true)
	if err != nil {
		t.Fatal(err)
	}
	const randomPart uint32 = 0xAABBCCDD
	sendKey := deriveClientClientKey(peerHash, magicValueRequester, randomPart)
	recvKey := deriveClientClientKey(peerHash, magicValueServer, randomPart)
	if len(sendKey) != 16 || len(recvKey) != 16 {
		t.Fatalf("unexpected key length send=%d recv=%d", len(sendKey), len(recvKey))
	}
	if bytes.Equal(sendKey, recvKey) {
		t.Fatal("send and recv keys should differ")
	}
	sendCipher, err := newRC4StreamCipher(sendKey, rc4DiscardBytes)
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte("eMule obfuscated payload 0123456789")
	enc := make([]byte, len(plain))
	sendCipher.cipher.XORKeyStream(enc, plain)
	out := make([]byte, len(enc))
	sendCipher2, err := newRC4StreamCipher(sendKey, rc4DiscardBytes)
	if err != nil {
		t.Fatal(err)
	}
	sendCipher2.cipher.XORKeyStream(out, enc)
	if !bytes.Equal(plain, out) {
		t.Fatalf("round-trip mismatch: %q vs %q", plain, out)
	}
}

func TestClientClientHandshakeCodec(t *testing.T) {
	peerHash, err := protocol.RandomHash(true)
	if err != nil {
		t.Fatal(err)
	}
	const randomPart uint32 = 0x01020304
	req, sendKey, recvKey, err := encodeClientClientHandshakeRequest(peerHash, randomPart)
	if err != nil {
		t.Fatal(err)
	}
	if len(req) < 12 {
		t.Fatalf("request too short: %d", len(req))
	}
	if isProtocolMarker(req[0]) {
		t.Fatalf("marker must not be protocol header: 0x%02x", req[0])
	}
	gotRandom := binary.LittleEndian.Uint32(req[1:5])
	if gotRandom != randomPart {
		t.Fatalf("random part: got 0x%08x want 0x%08x", gotRandom, randomPart)
	}
	resp, err := buildClientClientResponse(recvKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := decodeClientClientHandshakeResponse(sendKey, recvKey, resp); err != nil {
		t.Fatal(err)
	}
}

func TestClientClientHandshakeOverPipe(t *testing.T) {
	serverHash, err := protocol.RandomHash(true)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	done := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		wrapped := WrapIncomingObfuscatedConn(conn, serverHash, true, false)
		buf := make([]byte, 1)
		_, err = io.ReadFull(wrapped, buf)
		done <- err
	}()
	clientConn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer clientConn.Close()
	obfConn, err := NewOutgoingClientObfuscatedConn(clientConn, serverHash)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := obfConn.Write([]byte{0x42}); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestObfuscatedConnWriteRead(t *testing.T) {
	localHash, err := protocol.RandomHash(true)
	if err != nil {
		t.Fatal(err)
	}
	remoteHash, err := protocol.RandomHash(true)
	if err != nil {
		t.Fatal(err)
	}
	clientConn, serverConn := net.Pipe()
	errCh := make(chan error, 1)
	go func() {
		out, err := NewOutgoingClientObfuscatedConn(clientConn, remoteHash)
		if err != nil {
			errCh <- err
			return
		}
		payload := []byte{protocol.EMuleProt, 0x01, 0x02, 0x03}
		if _, err := out.Write(payload); err != nil {
			errCh <- err
			return
		}
		errCh <- nil
	}()
	in := WrapIncomingObfuscatedConn(serverConn, remoteHash, true, false)
	buf := make([]byte, 16)
	n, err := io.ReadFull(in, buf[:4])
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("read %d bytes", n)
	}
	if buf[0] != protocol.EMuleProt {
		t.Fatalf("expected decrypted header 0x%02x got 0x%02x", protocol.EMuleProt, buf[0])
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	_ = localHash
}

func TestObfuscationDialAddrUsesPeerPort(t *testing.T) {
	base := &net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 4662}
	local := NewSettings()
	local.ListenPort = 4711
	local.ObfuscationTCPPort = 4999
	got := obfuscationDialAddr(base, local)
	if got == nil {
		t.Fatal("expected obfuscation dial addr")
	}
	if got.Port != 4665 {
		t.Fatalf("port: got %d want 4665", got.Port)
	}
	if got.IP.String() != base.IP.String() {
		t.Fatalf("ip: got %s want %s", got.IP, base.IP)
	}
}

func TestPeerWantsObfuscation(t *testing.T) {
	st := NewSettings()
	if peerWantsObfuscation(nil, st) {
		t.Fatal("default settings should not force obfuscation")
	}
	st.EnableCryptLayer = true
	if !peerWantsObfuscation(nil, st) {
		t.Fatal("EnableCryptLayer should request obfuscation")
	}
	peer := &Peer{CryptOptions: cryptOptionRequested}
	if !peerWantsObfuscation(peer, NewSettings()) {
		t.Fatal("peer crypt option should request obfuscation")
	}
}

func TestCryptOptionsForLocal(t *testing.T) {
	st := NewSettings()
	opts := cryptOptionsForLocal(st)
	if opts&cryptOptionSupported == 0 {
		t.Fatal("expected supported bit")
	}
	if opts&cryptOptionRequested != 0 {
		t.Fatal("requested should be off by default")
	}
	st.EnableCryptLayer = true
	opts = cryptOptionsForLocal(st)
	if opts&cryptOptionRequested == 0 {
		t.Fatal("expected requested bit when enabled")
	}
}
