package goed2k

import (
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"crypto/rc4"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"sync"

	"github.com/goed2k/core/protocol"
)

const (
	magicValueRequester         = 0x22 // 34
	magicValueServer            = 0xCB // 203
	magicValueSync              = 0x835E6FC4
	encryptionMethodObfuscation = 0
	rc4DiscardBytes             = 1024

	cryptOptionRequested = 0x01
	cryptOptionSupported = 0x02
	cryptOptionRequired  = 0x04
	cryptOptionUserHash  = 0x80
)

var (
	errObfuscationHandshake = errors.New("obfuscation handshake failed")
	errObfuscationRequired  = errors.New("obfuscation required")
)

// rc4StreamCipher wraps crypto/rc4 with optional keystream discard.
type rc4StreamCipher struct {
	cipher *rc4.Cipher
}

func newRC4StreamCipher(key []byte, discard int) (*rc4StreamCipher, error) {
	c, err := rc4.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if discard > 0 {
		buf := make([]byte, discard)
		c.XORKeyStream(buf, buf)
	}
	return &rc4StreamCipher{cipher: c}, nil
}

func deriveClientClientKey(userHash protocol.Hash, magic byte, randomKeyPart uint32) []byte {
	buf := make([]byte, 21)
	copy(buf[:16], userHash.Bytes())
	buf[16] = magic
	binary.LittleEndian.PutUint32(buf[17:], randomKeyPart)
	sum := md5.Sum(buf)
	return sum[:]
}

func deriveServerKey(sharedSecret []byte, magic byte) []byte {
	buf := make([]byte, len(sharedSecret)+1)
	copy(buf, sharedSecret)
	buf[len(sharedSecret)] = magic
	sum := md5.Sum(buf)
	return sum[:]
}

func semiRandomNotProtocolMarker() (byte, error) {
	for i := 0; i < 128; i++ {
		b := make([]byte, 1)
		if _, err := rand.Read(b); err != nil {
			return 0, err
		}
		switch b[0] {
		case protocol.EdonkeyProt, protocol.PackedProt, protocol.EMuleProt:
			continue
		default:
			return b[0], nil
		}
	}
	return 0x01, nil
}

func isProtocolMarker(b byte) bool {
	switch b {
	case protocol.EdonkeyProt, protocol.PackedProt, protocol.EMuleProt:
		return true
	default:
		return false
	}
}

type obfHandshakeRole int

const (
	obfRoleNone obfHandshakeRole = iota
	obfRoleOutgoingClient
	obfRoleIncomingClient
	obfRoleOutgoingServer
)

type obfConnState int

const (
	obfStateHandshake obfConnState = iota
	obfStateReady
	obfStatePlain
)

// ObfuscatedConn wraps a TCP connection with eMule Basic Obfuscation (RC4).
type ObfuscatedConn struct {
	net.Conn
	mu         sync.Mutex
	role       obfHandshakeRole
	state      obfConnState
	sendCipher *rc4StreamCipher
	recvCipher *rc4StreamCipher
	localHash  protocol.Hash
	remoteHash protocol.Hash
	required   bool
	handshake  *obfHandshakeMachine
	readBuf       []byte
	handshakeBuf  []byte
}

// NewOutgoingClientObfuscatedConn wraps a TCP connection for client-client obfuscation.
// The handshake is completed lazily on the first Read/Write via PumpIO (non-blocking).
func NewOutgoingClientObfuscatedConn(conn net.Conn, peerUserHash protocol.Hash) (net.Conn, error) {
	if conn == nil || peerUserHash.Equal(protocol.Invalid) {
		return nil, errObfuscationHandshake
	}
	return &ObfuscatedConn{
		Conn:       conn,
		role:       obfRoleOutgoingClient,
		state:      obfStateHandshake,
		remoteHash: peerUserHash,
		handshake:  newOutgoingClientHandshake(peerUserHash),
	}, nil
}

// WrapIncomingObfuscatedConn accepts an inbound connection that may use obfuscation.
func WrapIncomingObfuscatedConn(conn net.Conn, localUserHash protocol.Hash, forceObfuscated, required bool) net.Conn {
	return &ObfuscatedConn{
		Conn:      conn,
		role:      obfRoleIncomingClient,
		state:     obfStateHandshake,
		localHash: localUserHash,
		required:  required,
		handshake: newIncomingClientHandshake(localUserHash, forceObfuscated),
	}
}

// NewOutgoingServerObfuscatedConn performs simplified server obfuscation (DH key exchange).
func NewOutgoingServerObfuscatedConn(conn net.Conn) (net.Conn, error) {
	if conn == nil {
		return nil, errObfuscationHandshake
	}
	oc := &ObfuscatedConn{
		Conn:      conn,
		role:      obfRoleOutgoingServer,
		state:     obfStateHandshake,
		handshake: newOutgoingServerHandshake(),
	}
	if err := oc.completeOutgoingServerHandshake(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return oc, nil
}

func (o *ObfuscatedConn) handshakeComplete() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.state != obfStateHandshake
}

func (o *ObfuscatedConn) Read(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.state == obfStatePlain {
		return o.Conn.Read(p)
	}
	if o.state == obfStateHandshake {
		if err := o.advanceHandshake(); err != nil {
			return 0, err
		}
	}
	if len(o.readBuf) > 0 {
		n := copy(p, o.readBuf)
		o.readBuf = o.readBuf[n:]
		return n, nil
	}
	n, err := o.Conn.Read(p)
	if err != nil || n == 0 {
		return n, err
	}
	if o.recvCipher != nil {
		o.recvCipher.cipher.XORKeyStream(p[:n], p[:n])
	}
	return n, err
}

func (o *ObfuscatedConn) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.state == obfStatePlain {
		return o.Conn.Write(p)
	}
	if o.state == obfStateHandshake {
		if o.role == obfRoleOutgoingClient {
			if err := o.advanceOutgoingClientHandshake(); err != nil {
				return 0, err
			}
		}
		if o.state == obfStateHandshake {
			return 0, io.ErrShortBuffer
		}
	}
	if o.sendCipher == nil {
		return o.Conn.Write(p)
	}
	buf := make([]byte, len(p))
	copy(buf, p)
	o.sendCipher.cipher.XORKeyStream(buf, buf)
	return o.Conn.Write(buf)
}

func (o *ObfuscatedConn) activateCiphers(sendKey, recvKey []byte) error {
	send, err := newRC4StreamCipher(sendKey, rc4DiscardBytes)
	if err != nil {
		return err
	}
	recv, err := newRC4StreamCipher(recvKey, rc4DiscardBytes)
	if err != nil {
		return err
	}
	o.sendCipher = send
	o.recvCipher = recv
	o.state = obfStateReady
	return nil
}

func (o *ObfuscatedConn) completeOutgoingClientHandshake() error {
	req, sendKey, recvKey, err := o.handshake.buildOutgoingRequest()
	if err != nil {
		return err
	}
	if _, err := o.Conn.Write(req); err != nil {
		return err
	}
	resp := make([]byte, 6+16) // magic(4)+method(1)+padlen(1)+max padding
	if _, err := io.ReadFull(o.Conn, resp[:6]); err != nil {
		return err
	}
	recvHandshake, err := newRC4StreamCipher(recvKey, 0)
	if err != nil {
		return err
	}
	recvHandshake.cipher.XORKeyStream(resp[:6], resp[:6])
	if binary.LittleEndian.Uint32(resp[:4]) != magicValueSync {
		return errObfuscationHandshake
	}
	if resp[4] != encryptionMethodObfuscation {
		return errObfuscationHandshake
	}
	padLen := int(resp[5])
	if padLen > 16 {
		return errObfuscationHandshake
	}
	if padLen > 0 {
		padding := make([]byte, padLen)
		if _, err := io.ReadFull(o.Conn, padding); err != nil {
			return err
		}
		recvHandshake.cipher.XORKeyStream(padding, padding)
	}
	return o.activateCiphers(sendKey, recvKey)
}

func (o *ObfuscatedConn) advanceIncomingHandshake() error {
	if o.handshake == nil {
		return errObfuscationHandshake
	}
	for o.state == obfStateHandshake {
		tmp := make([]byte, 256)
		n, err := o.Conn.Read(tmp)
		if n > 0 {
			o.handshakeBuf = append(o.handshakeBuf, tmp[:n]...)
		}
		if len(o.handshakeBuf) == 0 {
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					return nil
				}
				if err == io.EOF {
					return nil
				}
				return err
			}
			return nil
		}
		reader := bytes.NewReader(o.handshakeBuf)
		step, err := o.handshake.readStep(reader)
		consumed := len(o.handshakeBuf) - reader.Len()
		if errors.Is(err, errHandshakeIncomplete) {
			return nil
		}
		o.handshakeBuf = append([]byte(nil), o.handshakeBuf[consumed:]...)
		if err != nil {
			if errors.Is(err, errObfuscationPlain) {
				o.state = obfStatePlain
				if len(step.leftover) > 0 {
					o.readBuf = append(o.readBuf, step.leftover...)
				}
				return nil
			}
			return err
		}
		if step.response != nil {
			if _, err := o.Conn.Write(step.response); err != nil {
				return err
			}
		}
		if step.done {
			if err := o.activateCiphers(step.sendKey, step.recvKey); err != nil {
				return err
			}
			if len(step.leftover) > 0 {
				plain := make([]byte, len(step.leftover))
				copy(plain, step.leftover)
				o.recvCipher.cipher.XORKeyStream(plain, step.leftover)
				o.readBuf = append(o.readBuf, plain...)
			}
			return nil
		}
	}
	return nil
}

func (o *ObfuscatedConn) completeOutgoingServerHandshake() error {
	req, err := o.handshake.buildServerRequest()
	if err != nil {
		return err
	}
	if _, err := o.Conn.Write(req); err != nil {
		return err
	}
	header := make([]byte, 96)
	if _, err := io.ReadFull(o.Conn, header); err != nil {
		return err
	}
	sendKey, recvKey, tail, err := o.handshake.processServerDHResponse(header)
	if err != nil {
		return err
	}
	// Encrypted tail: magic(4)+methods(2)+padlen(1)+padding
	enc := make([]byte, 7)
	need := 7
	if len(tail) > 0 {
		need -= len(tail)
		copy(enc, tail)
	}
	if need > 0 {
		if _, err := io.ReadFull(o.Conn, enc[len(tail):]); err != nil {
			return err
		}
	}
	recvHandshake, err := newRC4StreamCipher(recvKey, 0)
	if err != nil {
		return err
	}
	recvHandshake.cipher.XORKeyStream(enc, enc)
	if binary.LittleEndian.Uint32(enc[:4]) != magicValueSync {
		return errObfuscationHandshake
	}
	padLen := int(enc[6])
	if padLen > 16 {
		return errObfuscationHandshake
	}
	if padLen > 0 {
		padding := make([]byte, padLen)
		if _, err := io.ReadFull(o.Conn, padding); err != nil {
			return err
		}
		recvHandshake.cipher.XORKeyStream(padding, padding)
	}
	// Client answer (delayed with first payload in eMule); send now for simplicity.
	ans, err := buildClientServerAnswer(sendKey)
	if err != nil {
		return err
	}
	if _, err := o.Conn.Write(ans); err != nil {
		return err
	}
	return o.activateCiphers(sendKey, recvKey)
}

type obfHandshakeStep struct {
	response []byte
	sendKey  []byte
	recvKey  []byte
	leftover []byte
	done     bool
}

var errObfuscationPlain = errors.New("plain connection")

var errHandshakeIncomplete = errors.New("handshake incomplete")

type obfHandshakeMachine struct {
	role            obfHandshakeRole
	localHash       protocol.Hash
	forceObfuscated bool
	phase           int
	randomKeyPart   uint32
	sendKey         []byte
	recvKey         []byte
	responseBuf     []byte
	serverDH        *serverDHState
}

func (o *ObfuscatedConn) advanceHandshake() error {
	switch o.role {
	case obfRoleIncomingClient:
		return o.advanceIncomingHandshake()
	case obfRoleOutgoingClient:
		return o.advanceOutgoingClientHandshake()
	default:
		return errObfuscationHandshake
	}
}

func (o *ObfuscatedConn) advanceOutgoingClientHandshake() error {
	h := o.handshake
	if h == nil {
		return errObfuscationHandshake
	}
	if h.phase >= 2 {
		return nil
	}
	if h.phase == 0 {
		req, sendKey, recvKey, err := h.buildOutgoingRequest()
		if err != nil {
			return err
		}
		if _, err := o.Conn.Write(req); err != nil {
			return err
		}
		h.sendKey = sendKey
		h.recvKey = recvKey
		h.phase = 1
	}
	if h.phase != 1 {
		return nil
	}
	tmp := make([]byte, 64)
	n, err := o.Conn.Read(tmp)
	if n > 0 {
		h.responseBuf = append(h.responseBuf, tmp[:n]...)
	}
	if err != nil {
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			return nil
		}
		if len(h.responseBuf) == 0 {
			return err
		}
	}
	if len(h.responseBuf) < 6 {
		return nil
	}
	resp := make([]byte, len(h.responseBuf))
	copy(resp, h.responseBuf)
	recvHandshake, err := newRC4StreamCipher(h.recvKey, 0)
	if err != nil {
		return err
	}
	recvHandshake.cipher.XORKeyStream(resp[:6], h.responseBuf[:6])
	if binary.LittleEndian.Uint32(resp[:4]) != magicValueSync {
		return errObfuscationHandshake
	}
	if resp[4] != encryptionMethodObfuscation {
		return errObfuscationHandshake
	}
	padLen := int(resp[5])
	if padLen > 16 {
		return errObfuscationHandshake
	}
	need := 6 + padLen
	if len(h.responseBuf) < need {
		return nil
	}
	if padLen > 0 {
		padding := make([]byte, padLen)
		copy(padding, h.responseBuf[6:need])
		recvHandshake.cipher.XORKeyStream(padding, padding)
	}
	if err := o.activateCiphers(h.sendKey, h.recvKey); err != nil {
		return err
	}
	if len(h.responseBuf) > need {
		extra := make([]byte, len(h.responseBuf)-need)
		copy(extra, h.responseBuf[need:])
		o.recvCipher.cipher.XORKeyStream(extra, h.responseBuf[need:])
		o.readBuf = append(o.readBuf, extra...)
	}
	h.responseBuf = nil
	h.phase = 2
	return nil
}

func newOutgoingClientHandshake(peerHash protocol.Hash) *obfHandshakeMachine {
	var randomKeyPart uint32
	_ = binary.Read(rand.Reader, binary.LittleEndian, &randomKeyPart)
	sendKey := deriveClientClientKey(peerHash, magicValueRequester, randomKeyPart)
	recvKey := deriveClientClientKey(peerHash, magicValueServer, randomKeyPart)
	return &obfHandshakeMachine{
		role:          obfRoleOutgoingClient,
		randomKeyPart: randomKeyPart,
		sendKey:       sendKey,
		recvKey:       recvKey,
	}
}

func (h *obfHandshakeMachine) buildOutgoingRequest() ([]byte, []byte, []byte, error) {
	marker, err := semiRandomNotProtocolMarker()
	if err != nil {
		return nil, nil, nil, err
	}
	sendHandshake, err := newRC4StreamCipher(h.sendKey, 0)
	if err != nil {
		return nil, nil, nil, err
	}
	plain, err := buildClientClientRequestPayload()
	if err != nil {
		return nil, nil, nil, err
	}
	encrypted := make([]byte, len(plain))
	sendHandshake.cipher.XORKeyStream(encrypted, plain)
	req := make([]byte, 0, 5+len(encrypted))
	req = append(req, marker)
	var rk [4]byte
	binary.LittleEndian.PutUint32(rk[:], h.randomKeyPart)
	req = append(req, rk[:]...)
	req = append(req, encrypted...)
	return req, h.sendKey, h.recvKey, nil
}

func newIncomingClientHandshake(localHash protocol.Hash, forceObfuscated bool) *obfHandshakeMachine {
	return &obfHandshakeMachine{
		role:            obfRoleIncomingClient,
		localHash:       localHash,
		forceObfuscated: forceObfuscated,
	}
}

func (h *obfHandshakeMachine) readStep(r *bytes.Reader) (obfHandshakeStep, error) {
	if h.phase > 0 {
		return obfHandshakeStep{}, errObfuscationHandshake
	}
	marker := make([]byte, 1)
	if err := readFullOrIncomplete(r, marker); err != nil {
		return obfHandshakeStep{}, err
	}
	if !h.forceObfuscated && isProtocolMarker(marker[0]) {
		return obfHandshakeStep{leftover: marker}, errObfuscationPlain
	}
	rk := make([]byte, 4)
	if err := readFullOrIncomplete(r, rk); err != nil {
		return obfHandshakeStep{}, err
	}
	h.randomKeyPart = binary.LittleEndian.Uint32(rk)
	h.recvKey = deriveClientClientKey(h.localHash, magicValueRequester, h.randomKeyPart)
	h.sendKey = deriveClientClientKey(h.localHash, magicValueServer, h.randomKeyPart)
	recvHandshake, err := newRC4StreamCipher(h.recvKey, 0)
	if err != nil {
		return obfHandshakeStep{}, err
	}
	header := make([]byte, 7)
	if err := readFullOrIncomplete(r, header); err != nil {
		return obfHandshakeStep{}, err
	}
	recvHandshake.cipher.XORKeyStream(header, header)
	if binary.LittleEndian.Uint32(header[:4]) != magicValueSync {
		return obfHandshakeStep{}, errObfuscationHandshake
	}
	padLen := int(header[6])
	if padLen > 16 {
		return obfHandshakeStep{}, errObfuscationHandshake
	}
	if padLen > 0 {
		padding := make([]byte, padLen)
		if err := readFullOrIncomplete(r, padding); err != nil {
			return obfHandshakeStep{}, err
		}
		recvHandshake.cipher.XORKeyStream(padding, padding)
	}
	resp, err := buildClientClientResponse(h.sendKey)
	if err != nil {
		return obfHandshakeStep{}, err
	}
	h.phase = 1
	return obfHandshakeStep{
		response: resp,
		sendKey:  h.sendKey,
		recvKey:  h.recvKey,
		done:     true,
	}, nil
}

func readFullOrIncomplete(r *bytes.Reader, buf []byte) error {
	if r.Len() < len(buf) {
		return errHandshakeIncomplete
	}
	_, err := io.ReadFull(r, buf)
	return err
}

func buildClientClientRequestPayload() ([]byte, error) {
	var padLen byte
	if err := binary.Read(rand.Reader, binary.LittleEndian, &padLen); err != nil {
		return nil, err
	}
	padLen %= 16
	plain := make([]byte, 0, 7+int(padLen))
	var sync [4]byte
	binary.LittleEndian.PutUint32(sync[:], magicValueSync)
	plain = append(plain, sync[:]...)
	plain = append(plain, encryptionMethodObfuscation, encryptionMethodObfuscation, padLen)
	if padLen > 0 {
		pad := make([]byte, int(padLen))
		if _, err := rand.Read(pad); err != nil {
			return nil, err
		}
		plain = append(plain, pad...)
	}
	return plain, nil
}

func buildClientClientResponse(sendKey []byte) ([]byte, error) {
	var padLen byte
	if err := binary.Read(rand.Reader, binary.LittleEndian, &padLen); err != nil {
		return nil, err
	}
	padLen %= 16
	plain := make([]byte, 0, 6+int(padLen))
	var sync [4]byte
	binary.LittleEndian.PutUint32(sync[:], magicValueSync)
	plain = append(plain, sync[:]...)
	plain = append(plain, encryptionMethodObfuscation, padLen)
	if padLen > 0 {
		pad := make([]byte, int(padLen))
		if _, err := rand.Read(pad); err != nil {
			return nil, err
		}
		plain = append(plain, pad...)
	}
	sendHandshake, err := newRC4StreamCipher(sendKey, 0)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(plain))
	sendHandshake.cipher.XORKeyStream(out, plain)
	return out, nil
}

func buildClientServerAnswer(sendKey []byte) ([]byte, error) {
	plain, err := buildClientClientResponse(sendKey)
	if err != nil {
		return nil, err
	}
	return plain, nil
}

// server DH (eMule dh768_p, g=2, 96-byte modulus result).
var dh768P = []byte{
	0xF2, 0xBF, 0x52, 0xC5, 0x5F, 0x58, 0x7A, 0xDD, 0x53, 0x71, 0xA9, 0x36,
	0xE8, 0x86, 0xEB, 0x3C, 0x62, 0x17, 0xA3, 0x3E, 0xC3, 0x4C, 0xB4, 0x0D,
	0xC7, 0x3A, 0x41, 0xA6, 0x43, 0xAF, 0xFC, 0xE7, 0x21, 0xFC, 0x28, 0x63,
	0x66, 0x53, 0x5B, 0xDB, 0xCE, 0x25, 0x9F, 0x22, 0x86, 0xDA, 0x4A, 0x91,
	0xB2, 0x07, 0xCB, 0xAA, 0x52, 0x55, 0xD4, 0xF6, 0x1C, 0xCE, 0xAE, 0xD4,
	0x5A, 0xD5, 0xE0, 0x74, 0x7D, 0xF7, 0x78, 0x18, 0x28, 0x10, 0x5F, 0x34,
	0x0F, 0x76, 0x23, 0x87, 0xF8, 0x8B, 0x28, 0x91, 0x42, 0xFB, 0x42, 0x68,
	0x8F, 0x05, 0x15, 0x0F, 0x54, 0x8B, 0x5F, 0x43, 0x6A, 0xF7, 0x0D, 0xF3,
}

type serverDHState struct {
	privateA *big.Int
}

func newOutgoingServerHandshake() *obfHandshakeMachine {
	return &obfHandshakeMachine{
		role:     obfRoleOutgoingServer,
		serverDH: &serverDHState{},
	}
}

func (h *obfHandshakeMachine) buildServerRequest() ([]byte, error) {
	p := new(big.Int).SetBytes(dh768P)
	max := new(big.Int).Lsh(big.NewInt(1), 128)
	a, err := rand.Int(rand.Reader, max)
	if err != nil {
		return nil, err
	}
	if a.Sign() == 0 {
		a.SetInt64(1)
	}
	h.serverDH.privateA = a
	g := big.NewInt(2)
	gA := new(big.Int).Exp(g, a, p)
	gABytes := encodeDHInt(gA, 96)
	marker, err := semiRandomNotProtocolMarker()
	if err != nil {
		return nil, err
	}
	var padLen byte
	_ = binary.Read(rand.Reader, binary.LittleEndian, &padLen)
	padLen %= 16
	req := make([]byte, 0, 1+96+1+int(padLen))
	req = append(req, marker)
	req = append(req, gABytes...)
	req = append(req, padLen)
	if padLen > 0 {
		pad := make([]byte, int(padLen))
		_, _ = rand.Read(pad)
		req = append(req, pad...)
	}
	return req, nil
}

func (h *obfHandshakeMachine) processServerDHResponse(gBBytes []byte) (sendKey, recvKey, tail []byte, err error) {
	p := new(big.Int).SetBytes(dh768P)
	gB := new(big.Int).SetBytes(gBBytes)
	shared := new(big.Int).Exp(gB, h.serverDH.privateA, p)
	secret := encodeDHInt(shared, 96)
	sendKey = deriveServerKey(secret, magicValueRequester)
	recvKey = deriveServerKey(secret, magicValueServer)
	return sendKey, recvKey, nil, nil
}

func encodeDHInt(v *big.Int, size int) []byte {
	b := v.Bytes()
	if len(b) > size {
		b = b[len(b)-size:]
	}
	out := make([]byte, size)
	copy(out[size-len(b):], b)
	return out
}

func peerWantsObfuscation(peer *Peer, settings Settings) bool {
	if settings.CryptLayerRequired || settings.EnableCryptLayer {
		return true
	}
	if peer == nil {
		return false
	}
	opts := peer.CryptOptions
	return opts&cryptOptionRequested != 0 || opts&cryptOptionRequired != 0
}

func obfuscationDialAddr(base *net.TCPAddr, _ Settings) *net.TCPAddr {
	if base == nil {
		return nil
	}
	// eMule 约定：对端混淆端口为其 TCP 端口 + 3（与本地 ListenPort 无关）。
	port := base.Port + 3
	if port <= 0 || port == base.Port {
		return nil
	}
	cp := cloneTCPAddr(base)
	cp.Port = port
	return cp
}

func peerProvidesUserHash(peer *Peer) (protocol.Hash, bool) {
	if peer == nil {
		return protocol.Invalid, false
	}
	if !peer.UserHash.Equal(protocol.Invalid) {
		return peer.UserHash, true
	}
	if peer.CryptOptions&cryptOptionUserHash != 0 {
		return protocol.Invalid, false
	}
	return protocol.Invalid, false
}

func cryptOptionsForLocal(settings Settings) uint8 {
	var opts uint8
	opts |= cryptOptionSupported
	if settings.EnableCryptLayer {
		opts |= cryptOptionRequested
	}
	if settings.CryptLayerRequired {
		opts |= cryptOptionRequired
	}
	if !settings.UserAgent.Equal(protocol.Invalid) {
		opts |= cryptOptionUserHash
	}
	return opts
}

// encodeClientClientHandshakeRequest exposes handshake encoding for tests.
func encodeClientClientHandshakeRequest(peerHash protocol.Hash, randomKeyPart uint32) ([]byte, []byte, []byte, error) {
	h := newOutgoingClientHandshake(peerHash)
	h.randomKeyPart = randomKeyPart
	return h.buildOutgoingRequest()
}

// decodeClientClientHandshakeResponse verifies an initiator-side response.
func decodeClientClientHandshakeResponse(sendKey, recvKey []byte, enc []byte) error {
	if len(enc) < 6 {
		return fmt.Errorf("response too short")
	}
	recvHandshake, err := newRC4StreamCipher(recvKey, 0)
	if err != nil {
		return err
	}
	plain := make([]byte, len(enc))
	copy(plain, enc)
	recvHandshake.cipher.XORKeyStream(plain, plain)
	if binary.LittleEndian.Uint32(plain[:4]) != magicValueSync {
		return errObfuscationHandshake
	}
	if plain[4] != encryptionMethodObfuscation {
		return errObfuscationHandshake
	}
	padLen := int(plain[5])
	if len(plain) < 6+padLen {
		return fmt.Errorf("padding truncated")
	}
	_ = sendKey
	return nil
}
