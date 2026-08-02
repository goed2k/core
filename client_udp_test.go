package goed2k

import (
	"net"
	"testing"
	"time"
)

func TestHandleClientUDPReaskPingRepliesAck(t *testing.T) {
	s := NewSession(NewSettings())
	serverUDP, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer serverUDP.Close()
	s.serverStatUDPConn = serverUDP

	clientUDP, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer clientUDP.Close()

	serverAddr := serverUDP.LocalAddr().(*net.UDPAddr)
	ping := []byte{ed2kUDPHeader, clientUDPReaskFilePing, 0x12, 0x34, 0x56, 0x78}
	if _, err := clientUDP.WriteToUDP(ping, serverAddr); err != nil {
		t.Fatal(err)
	}

	go func() {
		buf := make([]byte, 64)
		n, addr, err := serverUDP.ReadFromUDP(buf)
		if err != nil {
			return
		}
		s.handleClientUDP(addr, buf[:n])
	}()

	_ = serverUDP.SetReadDeadline(time.Now().Add(2 * time.Second))
	_ = clientUDP.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 8)
	n, _, err := clientUDP.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || buf[0] != ed2kUDPHeader || buf[1] != clientUDPReaskAck {
		t.Fatalf("unexpected ack: % X", buf[:n])
	}
}

func TestNoteUDPReachable(t *testing.T) {
	s := NewSession(NewSettings())
	if s.IsUDPReachable() {
		t.Fatal("expected false initially")
	}
	s.noteUDPReachable(&net.UDPAddr{IP: net.IPv4(1, 2, 3, 4), Port: 1})
	if !s.IsUDPReachable() {
		t.Fatal("expected reachable after ack")
	}
}
