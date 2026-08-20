package goed2k

import (
	"bytes"
	"compress/zlib"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"net"
	"path/filepath"
	"strings"
	"sync"

	"github.com/goed2k/core/data"
	"github.com/goed2k/core/internal/logx"
	"github.com/goed2k/core/protocol"
	clientproto "github.com/goed2k/core/protocol/client"
)

func debugPeerf(format string, args ...any) {
	logx.Debug(formatMessage(format, args...))
}

const MaxOutgoingBufferSize = 102*2 + 8

type PeerSpeed int

const (
	PeerSpeedSlow PeerSpeed = iota
	PeerSpeedMedium
	PeerSpeedFast
)

type UploadState int

const (
	UploadStateNone UploadState = iota
	UploadStateOnQueue
	UploadStateUploading
	UploadStateConnecting
)

type MiscOptions struct {
	AICHVersion         int
	UnicodeSupport      int
	UDPVer              int
	DataCompVer         int
	SupportSecIdent     int
	SourceExchange1Ver  int
	ExtendedRequestsVer int
	AcceptCommentVer    int
	NoViewSharedFiles   int
	MultiPacket         int
	SupportsPreview     int
}

const (
	aichVersionOffset       = 29
	unicodeSupportOffset    = 28
	udpVersionOffset        = 24
	dataCompressionOffset   = 20
	secureIdentOffset       = 16
	sourceExchange1Offset   = 12
	extendedRequestsOffset  = 8
	acceptCommentOffset     = 4
	noViewSharedFilesOffset = 2
	multiPacketOffset       = 1
	supportsPreviewOffset   = 0
)

func (m MiscOptions) IntValue() int {
	return (m.AICHVersion << aichVersionOffset) |
		(m.UnicodeSupport << unicodeSupportOffset) |
		(m.UDPVer << udpVersionOffset) |
		(m.DataCompVer << dataCompressionOffset) |
		(m.SupportSecIdent << secureIdentOffset) |
		(m.SourceExchange1Ver << sourceExchange1Offset) |
		(m.ExtendedRequestsVer << extendedRequestsOffset) |
		(m.AcceptCommentVer << acceptCommentOffset) |
		(m.NoViewSharedFiles << noViewSharedFilesOffset) |
		(m.MultiPacket << multiPacketOffset) |
		(m.SupportsPreview << supportsPreviewOffset)
}

func (m *MiscOptions) Assign(value int) {
	m.AICHVersion = (value >> aichVersionOffset) & 0x07
	m.UnicodeSupport = (value >> unicodeSupportOffset) & 0x01
	m.UDPVer = (value >> udpVersionOffset) & 0x0f
	m.DataCompVer = (value >> dataCompressionOffset) & 0x0f
	m.SupportSecIdent = (value >> secureIdentOffset) & 0x0f
	m.SourceExchange1Ver = (value >> sourceExchange1Offset) & 0x0f
	m.ExtendedRequestsVer = (value >> extendedRequestsOffset) & 0x0f
	m.AcceptCommentVer = (value >> acceptCommentOffset) & 0x0f
	m.NoViewSharedFiles = (value >> noViewSharedFilesOffset) & 0x01
	m.MultiPacket = (value >> multiPacketOffset) & 0x01
	m.SupportsPreview = (value >> supportsPreviewOffset) & 0x01
}

type MiscOptions2 struct {
	Value int
}

const (
	largeFileOffset = 4
	multipOffset    = 5
	srcExtOffset    = 10
	captchaOffset   = 11
)

func (m MiscOptions2) SupportCaptcha() bool        { return ((m.Value >> captchaOffset) & 0x01) == 1 }
func (m MiscOptions2) SupportSourceExt2() bool     { return ((m.Value >> srcExtOffset) & 0x01) == 1 }
func (m MiscOptions2) SupportExtMultipacket() bool { return ((m.Value >> multipOffset) & 0x01) == 1 }
func (m MiscOptions2) SupportLargeFiles() bool     { return ((m.Value >> largeFileOffset) & 0x01) == 1 }
func (m *MiscOptions2) SetCaptcha()                { m.Value |= 1 << captchaOffset }
func (m *MiscOptions2) SetSourceExt2()             { m.Value |= 1 << srcExtOffset }
func (m *MiscOptions2) SetExtMultipacket()         { m.Value |= 1 << multipOffset }
func (m *MiscOptions2) SetLargeFiles()             { m.Value |= 1 << largeFileOffset }
func (m *MiscOptions2) Assign(value int)           { m.Value = value }

type RemotePeerInfo struct {
	Point              protocol.Endpoint
	NickName           string
	ModName            string
	Version            int
	ModVersion         string
	ModNumber          int
	Misc1              MiscOptions
	Misc2              MiscOptions2
	SourceExchange2Ver byte
	SecIdentVersion    int
	SecIdentKeyFP      uint32
	UDPPort            uint16
}

type PendingBlock struct {
	Block      data.PieceBlock
	DataSize   int64
	CreateTime int64
	Received   int64
	Buffer     []byte
}

type RequestedUploadBlock struct {
	Begin       int64
	End         int64
	Transferred int64
}

func NewPendingBlock(block data.PieceBlock, totalSize int64) PendingBlock {
	return PendingBlock{
		Block:      block,
		DataSize:   int64(block.Size(totalSize)),
		CreateTime: CurrentTime(),
	}
}

type PeerConnection struct {
	Connection
	remotePeerInfo     RemotePeerInfo
	remoteHash         protocol.Hash
	transfer           *Transfer
	callbackClientID   int32
	remotePieces       protocol.BitField
	speed              PeerSpeed
	peerInfo           *Peer
	failed             bool
	transferringData   bool
	recvReq            data.PeerRequest
	recvReqCompressed  bool
	recvPos            int
	endpoint           protocol.Endpoint
	combiner           protocol.PacketCombiner
	downloadQueue      []PendingBlock
	remoteQueueRank    uint16
	uploadState        UploadState
	uploadQueueRank    uint16
	uploadWaitStart    int64
	uploadStartTime    int64
	uploadSessionBase  int64
	lastUploadRequest  int64
	uploadBlocks       []RequestedUploadBlock
	uploadDone         []RequestedUploadBlock
	uploadAddNext      bool
	friendSlot         bool
	uploadResource     UploadableResource
	sourceExchangeSent bool
	pendingAICHPiece   int
	aichRootRequested  bool
	identityVerified   bool
	remotePubKey       []byte
	remoteChallenge    uint32
	ourChallenge       uint32
	remoteSecIdentVer  int
	remoteKeyFP        uint32
	helloInfoFlags     byte
	remoteFileRating   byte
	remoteFileComment  string
	helloOnce          sync.Once
	obfHelloQueued     bool
}

func NewPeerConnection(session *Session, point protocol.Endpoint, transfer *Transfer, peerInfo *Peer) *PeerConnection {
	return &PeerConnection{
		Connection:       NewConnection(session),
		transfer:         transfer,
		speed:            PeerSpeedSlow,
		peerInfo:         peerInfo,
		endpoint:         point,
		remotePeerInfo:   RemotePeerInfo{},
		combiner:         clientproto.NewPacketCombiner(),
		downloadQueue:    make([]PendingBlock, 0),
		uploadBlocks:     make([]RequestedUploadBlock, 0),
		uploadDone:       make([]RequestedUploadBlock, 0),
		pendingAICHPiece: -1,
	}
}

func NewIncomingPeerConnection(session *Session, conn net.Conn, forceObfuscated bool) *PeerConnection {
	pc := &PeerConnection{
		Connection:       NewConnection(session),
		speed:            PeerSpeedSlow,
		remotePeerInfo:   RemotePeerInfo{},
		combiner:         clientproto.NewPacketCombiner(),
		downloadQueue:    make([]PendingBlock, 0),
		uploadBlocks:     make([]RequestedUploadBlock, 0),
		uploadDone:       make([]RequestedUploadBlock, 0),
		pendingAICHPiece: -1,
	}
	if forceObfuscated {
		pc.socket = WrapIncomingObfuscatedConn(conn, session.GetUserAgent(), true, session.settings.CryptLayerRequired)
	} else if session.settings.CryptLayerRequired {
		pc.socket = WrapIncomingObfuscatedConn(conn, session.GetUserAgent(), false, true)
	} else {
		pc.socket = conn
	}
	if tcpAddr, ok := conn.RemoteAddr().(*net.TCPAddr); ok {
		pc.endpoint = protocol.EndpointFromInet(tcpAddr)
	}
	return pc
}

func (p *PeerConnection) HasEndpoint() bool {
	if p.peerInfo != nil && p.peerInfo.DialAddr != nil {
		return true
	}
	return p.endpoint.Defined()
}

func (p *PeerConnection) Connect() error {
	var addr *net.TCPAddr
	var err error
	if p.peerInfo != nil {
		addr, err = p.peerInfo.peerDialTCPAddr()
	} else {
		addr, err = p.endpoint.ToTCPAddr()
	}
	if err != nil {
		return err
	}
	settings := p.session.settings
	useObf := peerWantsObfuscation(p.peerInfo, settings)
	if useObf {
		if peerHash, ok := peerProvidesUserHash(p.peerInfo); ok {
			if obfAddr := obfuscationDialAddr(addr, settings); obfAddr != nil {
				addr = obfAddr
			}
			conn, err := net.DialTCP("tcp", nil, addr)
			if err != nil {
				if settings.CryptLayerRequired {
					return err
				}
			} else {
				obfConn, obfErr := NewOutgoingClientObfuscatedConn(conn, peerHash)
				if obfErr == nil {
					p.socket = obfConn
					return nil
				}
				_ = conn.Close()
				if settings.CryptLayerRequired {
					return obfErr
				}
			}
		} else if settings.CryptLayerRequired {
			return errObfuscationRequired
		}
	}
	if err := p.Connection.Connect(addr); err != nil {
		return err
	}
	p.OnConnect()
	return nil
}

func (p *PeerConnection) OnDisconnect(ec BaseErrorCode) {
	debugPeerf("peer %s disconnect code=%d", p.endpoint.String(), ec.Code())
	if ec.Code() != NoError.Code() && ec.Code() != TransferPaused.Code() && ec.Code() != QueueRanking.Code() {
		p.failed = true
	}
	if q := p.session.UploadQueue(); q != nil {
		q.RemoveFromUploadQueue(p)
	}
	if p.transfer != nil {
		transfer := p.transfer
		p.AbortAllRequests()
		transfer.AddStats(p.Statistics())
		transfer.RemovePeerConnection(p)
		p.transfer = nil
	}
	p.uploadResource = nil
	p.session.CloseConnection(p)
}

func (p *PeerConnection) SecondTick(tickIntervalMS int64) {
	if p.IsDisconnecting() {
		return
	}
	p.Connection.SecondTick(tickIntervalMS)
	now := CurrentTime()
	if now-p.lastReceive > int64(p.session.settings.PeerConnectionTimeout)*1000 {
		p.Close(ConnectionTimeout)
		return
	}
	if p.hasStalledDownloadRequest(now) {
		p.Close(ConnectionTimeout)
	}
}

func (p *PeerConnection) Endpoint() protocol.Endpoint {
	return p.endpoint
}

func (p *PeerConnection) UploadState() UploadState {
	return p.uploadState
}

func (p *PeerConnection) SetUploadState(state UploadState) {
	p.uploadState = state
	if state != UploadStateUploading {
		p.uploadStartTime = 0
	}
}

func (p *PeerConnection) UploadQueueRank() uint16 {
	return p.uploadQueueRank
}

func (p *PeerConnection) SetUploadQueueRank(rank uint16) {
	p.uploadQueueRank = rank
}

func (p *PeerConnection) UploadWaitStart() int64 {
	return p.uploadWaitStart
}

func (p *PeerConnection) SetUploadWaitStart(ts int64) {
	p.uploadWaitStart = ts
}

func (p *PeerConnection) ClearUploadWaitStart() {
	p.uploadWaitStart = 0
}

func (p *PeerConnection) SetUploadStartTime(ts int64) {
	p.uploadStartTime = ts
}

func (p *PeerConnection) UploadStartDelay() int64 {
	if p.uploadStartTime == 0 {
		return 0
	}
	return CurrentTime() - p.uploadStartTime
}

func (p *PeerConnection) ResetUploadSession() {
	p.uploadSessionBase = p.Statistics().TotalUpload()
}

func (p *PeerConnection) UploadSession() int64 {
	total := p.Statistics().TotalUpload()
	if total < p.uploadSessionBase {
		return 0
	}
	return total - p.uploadSessionBase
}

func (p *PeerConnection) UploadAddNextConnect() bool {
	return p.uploadAddNext
}

func (p *PeerConnection) SetUploadAddNextConnect(v bool) {
	p.uploadAddNext = v
}

func (p *PeerConnection) FriendSlot() bool {
	return p.friendSlot
}

func (p *PeerConnection) SetFriendSlot(v bool) {
	p.friendSlot = v
}

func (p *PeerConnection) IsUploadConnected() bool {
	return p != nil && p.socket != nil && !p.IsDisconnecting()
}

func (p *PeerConnection) IsUploadLowID() bool {
	if p == nil {
		return false
	}
	if p.peerInfo != nil {
		return !p.peerInfo.Connectable
	}
	return false
}

func (p *PeerConnection) PrepareHelloAnswer() clientproto.HelloAnswer {
	const clientSoftwareAMule = 3
	mo := MiscOptions{
		AICHVersion:        1,
		UnicodeSupport:     1,
		UDPVer:             clientUDPReaskVersion,
		DataCompVer:        p.session.GetCompressionVersion(),
		SourceExchange1Ver: 1,
		NoViewSharedFiles:  1,
	}
	// Hello 只宣告已有完整处理路径的能力。
	var mo2 MiscOptions2
	mo2.SetLargeFiles()
	mo2.SetSourceExt2()
	if p.session.settings.EnableSecIdent && p.session.Identity().Available() {
		mo.SupportSecIdent = 1
	}
	return clientproto.HelloAnswer{
		Hash:  p.session.GetUserAgent(),
		Point: protocol.NewEndpoint(p.session.GetClientID(), p.session.GetListenPort()),
		Properties: protocol.TagList{
			protocol.NewStringTag(0x01, p.session.GetClientName()),
			protocol.NewStringTag(0x55, p.session.GetModName()),
			protocol.NewUInt32Tag(0x11, uint32(p.session.GetAppVersion())),
			protocol.NewUInt32Tag(helloTagUDPPorts, p.session.helloUDPPortsValue()),
			protocol.NewUInt32Tag(0xFB, uint32((clientSoftwareAMule<<24)|((p.session.GetModMajorVersion()&0x7f)<<17)|((p.session.GetModMinorVersion()&0x7f)<<10)|((p.session.GetModBuildVersion()&0x7f)<<7))),
			protocol.NewUInt32Tag(0xFA, uint32(mo.IntValue())),
			protocol.NewUInt32Tag(0xFE, uint32(mo2.Value)),
			protocol.NewUInt32Tag(helloTagSourceExchange2Ver, uint32(clientproto.SourceExchangeIPv6Version)),
		},
		ServerPoint: protocol.Endpoint{},
	}
}

func (p *PeerConnection) PrepareHello() clientproto.Hello {
	return clientproto.Hello{
		HashLength:  16,
		HelloAnswer: p.PrepareHelloAnswer(),
	}
}

func (p *PeerConnection) OnConnect() {
	p.helloOnce.Do(func() {
		packet := p.PrepareHello()
		if raw, err := p.combiner.Pack("client.Hello", &packet); err == nil {
			p.QueuePacket(raw)
		}
	})
}

func (p *PeerConnection) buildExtendedHandshake() clientproto.ExtendedHandshake {
	props := protocol.TagList{
		protocol.NewUInt32Tag(0x20, 0),
		protocol.NewUInt32Tag(0x21, 0),
		protocol.NewUInt32Tag(0x22, 0),
		protocol.NewUInt32Tag(0x23, 0),
		protocol.NewUInt32Tag(0x24, 0),
		protocol.NewUInt32Tag(0x25, 0),
		protocol.NewUInt32Tag(0x26, 0x03),
		protocol.NewUInt32Tag(0x27, 0),
		protocol.NewUInt32Tag(0x55, uint32(p.session.settings.Version)),
	}
	if p.session.settings.EnableSecIdent {
		if id := p.session.Identity(); id != nil && id.Available() {
			props = append(props,
				protocol.NewUInt32Tag(extTagSecIdentVersion, uint32(id.Version)),
				protocol.NewUInt32Tag(extTagSecIdentKeyFP, id.Fingerprint()),
			)
		}
	}
	return clientproto.ExtendedHandshake{
		Version:         0x10,
		ProtocolVersion: 0x01,
		Properties:      props,
	}
}

func (p *PeerConnection) SendExtHello() {
	packet := clientproto.ExtHello{ExtendedHandshake: p.buildExtendedHandshake()}
	if raw, err := p.combiner.Pack("client.ExtHello", &packet); err == nil {
		p.QueuePacket(raw)
	}
}

func (p *PeerConnection) SendExtHelloAnswer() {
	packet := clientproto.ExtHelloAnswer{ExtendedHandshake: p.buildExtendedHandshake()}
	if raw, err := p.combiner.Pack("client.ExtHelloAnswer", &packet); err == nil {
		p.QueuePacket(raw)
	}
}

func (p *PeerConnection) SendFileRequest(hash protocol.Hash) {
	debugPeerf("peer %s -> FileRequest", p.endpoint.String())
	packet := clientproto.FileRequest{Hash: hash}
	if raw, err := p.combiner.Pack("client.FileRequest", &packet); err == nil {
		p.QueuePacket(raw)
	}
}

func (p *PeerConnection) SendFileAnswer(res UploadableResource) {
	if res == nil {
		return
	}
	packet := clientproto.FileAnswer{
		Hash: res.GetHash(),
		Name: protocol.ByteContainer16FromString(filepath.Base(res.FileLabel())),
	}
	if raw, err := p.combiner.Pack("client.FileAnswer", &packet); err == nil {
		p.QueuePacket(raw)
	}
}

func (p *PeerConnection) SendFileStatusRequest(hash protocol.Hash) {
	debugPeerf("peer %s -> FileStatusRequest", p.endpoint.String())
	packet := clientproto.FileStatusRequest{Hash: hash}
	if raw, err := p.combiner.Pack("client.FileStatusRequest", &packet); err == nil {
		p.QueuePacket(raw)
	}
}

func (p *PeerConnection) SendFileStatusAnswer(res UploadableResource) {
	if res == nil {
		return
	}
	packet := clientproto.FileStatusAnswer{
		Hash:     res.GetHash(),
		BitField: res.AvailablePieces(),
	}
	if raw, err := p.combiner.Pack("client.FileStatusAnswer", &packet); err == nil {
		p.QueuePacket(raw)
	}
}

func (p *PeerConnection) SendHashSetRequest(hash protocol.Hash) {
	debugPeerf("peer %s -> HashSetRequest", p.endpoint.String())
	packet := clientproto.HashSetRequest{Hash: hash}
	if raw, err := p.combiner.Pack("client.HashSetRequest", &packet); err == nil {
		p.QueuePacket(raw)
	}
}

func (p *PeerConnection) SendHashSetAnswer(res UploadableResource) {
	if res == nil {
		return
	}
	packet := clientproto.HashSetAnswer{
		Hash:  res.GetHash(),
		Parts: res.UploadHashSet(),
	}
	if len(packet.Parts) == 0 {
		return
	}
	if raw, err := p.combiner.Pack("client.HashSetAnswer", &packet); err == nil {
		p.QueuePacket(raw)
	}
}

func (p *PeerConnection) SendAICHRequest(hash protocol.Hash, requested []protocol.AICHHash) {
	if len(requested) == 0 {
		return
	}
	debugPeerf("peer %s -> AICHRequest hashes=%d", p.endpoint.String(), len(requested))
	packet := clientproto.AICHRequest{Hash: hash, Hashes: requested}
	if raw, err := p.combiner.Pack("client.AICHRequest", &packet); err == nil {
		p.QueuePacket(raw)
	}
}

func (p *PeerConnection) SendAICHAnswer(res UploadableResource, requested []protocol.AICHHash) {
	if res == nil || len(requested) == 0 {
		return
	}
	answered := res.UploadAICHHashes(requested)
	if len(answered) == 0 {
		return
	}
	packet := clientproto.AICHAnswer{Hash: res.GetHash(), Hashes: answered}
	if raw, err := p.combiner.Pack("client.AICHAnswer", &packet); err == nil {
		p.QueuePacket(raw)
	}
}

func (p *PeerConnection) SendAICHFileHashAnswer(res UploadableResource) {
	if res == nil {
		return
	}
	root, ok := res.AICHRootHash()
	if !ok {
		return
	}
	packet := clientproto.AICHFileHashAnswer{Hash: res.GetHash(), RootHash: root}
	if raw, err := p.combiner.Pack("client.AICHFileHashAnswer", &packet); err == nil {
		p.QueuePacket(raw)
	}
}

func (p *PeerConnection) SendAICHFileHashRequest(hash protocol.Hash) {
	if p == nil || hash.IsZero() {
		return
	}
	debugPeerf("peer %s -> AICHFileHashRequest", p.endpoint.String())
	packet := clientproto.AICHFileHashRequest{Hash: hash}
	if raw, err := p.combiner.Pack("client.AICHFileHashRequest", &packet); err == nil {
		p.QueuePacket(raw)
	}
}

// maybeRequestAICHRoot 在对端宣告 AICH 且本任务尚无根哈希时发送 OP_AICHFILEHASHREQ。
// 每个连接最多请求一次，避免对不回答的来源重复刷包。
func (p *PeerConnection) maybeRequestAICHRoot() {
	if p == nil || p.transfer == nil || p.IsDisconnecting() {
		return
	}
	if p.aichRootRequested {
		return
	}
	if p.remotePeerInfo.Misc1.AICHVersion == 0 {
		return
	}
	if _, ok := p.transfer.AICHRootHash(); ok {
		return
	}
	hash := p.transfer.GetHash()
	if hash.IsZero() {
		return
	}
	p.aichRootRequested = true
	p.SendAICHFileHashRequest(hash)
}

func (p *PeerConnection) SendAICHRequestForPiece(t *Transfer, pieceIndex int) {
	if p == nil || t == nil || pieceIndex < 0 {
		return
	}
	if _, ok := t.AICHRootHash(); !ok {
		return
	}
	p.pendingAICHPiece = pieceIndex
	blockCount := AICHBlockCount(t.pieceSize(pieceIndex))
	requested := make([]protocol.AICHHash, blockCount)
	for i := range requested {
		requested[i] = AICHRequestMarker(pieceIndex, i)
	}
	p.SendAICHRequest(t.GetHash(), requested)
}

func (p *PeerConnection) SendStartUpload(hash protocol.Hash) {
	debugPeerf("peer %s -> StartUpload", p.endpoint.String())
	packet := clientproto.StartUpload{Hash: hash}
	if raw, err := p.combiner.Pack("client.StartUpload", &packet); err == nil {
		p.QueuePacket(raw)
	}
}

func (p *PeerConnection) SendAcceptUpload() {
	packet := clientproto.AcceptUpload{}
	if raw, err := p.combiner.Pack("client.AcceptUpload", &packet); err == nil {
		p.QueuePacket(raw)
	}
}

func (p *PeerConnection) SendQueueRanking(rank uint16) {
	packet := clientproto.QueueRanking{Rank: rank}
	if raw, err := p.combiner.Pack("client.QueueRanking", &packet); err == nil {
		p.QueuePacket(raw)
	}
}

func (p *PeerConnection) HandleQueueRanking(value *clientproto.QueueRanking) {
	if value == nil {
		return
	}
	p.remoteQueueRank = value.Rank
	debugPeerf("peer %s <- QueueRanking rank=%d", p.endpoint.String(), value.Rank)
	p.Close(QueueRanking)
}

func (p *PeerConnection) clearRemoteQueueState() {
	if p == nil || p.peerInfo == nil {
		return
	}
	if p.transfer == nil {
		p.peerInfo.clearRemoteQueue()
		return
	}
	if p.transfer.session != nil {
		p.transfer.session.mu.Lock()
		defer p.transfer.session.mu.Unlock()
	}
	p.transfer.policy.ClearRemoteQueue(p)
}

func (p *PeerConnection) HandleAcceptUpload() {
	debugPeerf("peer %s <- AcceptUpload", p.endpoint.String())
	p.clearRemoteQueueState()
	p.maybeSendPreviewRequest()
	p.RequestBlocks()
}

func (p *PeerConnection) SendOutOfParts() {
	packet := clientproto.OutOfParts{}
	if raw, err := p.combiner.Pack("client.OutOfParts", &packet); err == nil {
		p.QueuePacket(raw)
	}
}

func (p *PeerConnection) SendCancelTransfer() {
	packet := clientproto.CancelTransfer{}
	if raw, err := p.combiner.Pack("client.CancelTransfer", &packet); err == nil {
		p.QueuePacket(raw)
	}
}

func (p *PeerConnection) SendRequestParts32(packet *clientproto.RequestParts32) {
	if packet == nil {
		return
	}
	if raw, err := p.combiner.Pack("client.RequestParts32", packet); err == nil {
		debugPeerf("peer %s request32 raw=% X", p.endpoint.String(), raw)
		p.QueuePacket(raw)
	}
}

func (p *PeerConnection) SendRequestParts64(packet *clientproto.RequestParts64) {
	if packet == nil {
		return
	}
	if raw, err := p.combiner.Pack("client.RequestParts64", packet); err == nil {
		debugPeerf("peer %s request64 raw=% X", p.endpoint.String(), raw)
		p.QueuePacket(raw)
	}
}

func (p *PeerConnection) SendPart(begin, end int64, payload []byte) error {
	src := p.ActiveUploadSource()
	if src == nil || begin < 0 || end <= begin || len(payload) != int(end-begin) {
		return NewError(IllegalArgument)
	}
	if src.Size() <= math.MaxUint32 {
		packet := clientproto.SendingPart32{
			Hash:        src.GetHash(),
			BeginOffset: uint32(begin),
			EndOffset:   uint32(end),
		}
		raw, protoSize, err := p.combiner.PackPayload("client.SendingPart32", &packet, payload)
		if err != nil {
			return err
		}
		p.QueuePacketWithStats(raw, int64(protoSize), int64(len(payload)))
		p.session.Credits().AddUploaded(p.remoteHash, int64(len(payload)), p.identityVerified)
		return nil
	}
	packet := clientproto.SendingPart64{
		Hash:        src.GetHash(),
		BeginOffset: uint64(begin),
		EndOffset:   uint64(end),
	}
	raw, protoSize, err := p.combiner.PackPayload("client.SendingPart64", &packet, payload)
	if err != nil {
		return err
	}
	p.QueuePacketWithStats(raw, int64(protoSize), int64(len(payload)))
	p.session.Credits().AddUploaded(p.remoteHash, int64(len(payload)), p.identityVerified)
	return nil
}

func (p *PeerConnection) canCompressToPeer() bool {
	if p == nil || p.session == nil {
		return false
	}
	version := p.session.GetCompressionVersion()
	if version <= 0 {
		return false
	}
	return p.remotePeerInfo.Misc1.DataCompVer == version
}

func tryCompressPayload(payload []byte) ([]byte, bool) {
	if len(payload) == 0 {
		return nil, false
	}
	var buf bytes.Buffer
	writer, err := zlib.NewWriterLevel(&buf, zlib.BestCompression)
	if err != nil {
		return nil, false
	}
	if _, err := writer.Write(payload); err != nil {
		return nil, false
	}
	if err := writer.Close(); err != nil {
		return nil, false
	}
	compressed := buf.Bytes()
	if len(compressed) >= len(payload) {
		return nil, false
	}
	return compressed, true
}

func (p *PeerConnection) SendCompressedPart(begin, compressedTotalLen int64, payload []byte) error {
	src := p.ActiveUploadSource()
	if src == nil || begin < 0 || compressedTotalLen <= 0 || len(payload) == 0 {
		return NewError(IllegalArgument)
	}
	if compressedTotalLen > math.MaxUint32 {
		return NewError(IllegalArgument)
	}
	if src.Size() <= math.MaxUint32 {
		packet := clientproto.CompressedPart32{
			Hash:             src.GetHash(),
			BeginOffset:      uint32(begin),
			CompressedLength: uint32(compressedTotalLen),
		}
		raw, protoSize, err := p.combiner.PackPayload("client.CompressedPart32", &packet, payload)
		if err != nil {
			return err
		}
		p.QueuePacketWithStats(raw, int64(protoSize), int64(len(payload)))
		p.session.Credits().AddUploaded(p.remoteHash, int64(len(payload)), p.identityVerified)
		return nil
	}
	packet := clientproto.CompressedPart64{
		Hash:             src.GetHash(),
		BeginOffset:      uint64(begin),
		CompressedLength: uint32(compressedTotalLen),
	}
	raw, protoSize, err := p.combiner.PackPayload("client.CompressedPart64", &packet, payload)
	if err != nil {
		return err
	}
	p.QueuePacketWithStats(raw, int64(protoSize), int64(len(payload)))
	p.session.Credits().AddUploaded(p.remoteHash, int64(len(payload)), p.identityVerified)
	return nil
}

func (p *PeerConnection) sendCompressedPayload(begin int64, compressed []byte) error {
	togo := len(compressed)
	totalCompressed := int64(togo)
	packetSize := togo
	if togo > 10240 {
		packetSize = togo / (togo / 10240)
	}
	offset := 0
	for togo > 0 {
		if togo < packetSize*2 {
			packetSize = togo
		}
		chunk := compressed[offset : offset+packetSize]
		if err := p.SendCompressedPart(begin, totalCompressed, chunk); err != nil {
			return err
		}
		offset += packetSize
		togo -= packetSize
	}
	return nil
}

func (p *PeerConnection) AddUploadRequest(req data.PeerRequest) {
	if p.uploadState != UploadStateUploading {
		return
	}
	block := RequestedUploadBlock{
		Begin: req.Range().Left,
		End:   req.Range().Right,
	}
	for _, item := range p.uploadDone {
		if item.Begin == block.Begin && item.End == block.End {
			return
		}
	}
	for _, item := range p.uploadBlocks {
		if item.Begin == block.Begin && item.End == block.End {
			return
		}
	}
	p.uploadBlocks = append(p.uploadBlocks, block)
}

func (p *PeerConnection) ClearUploadBlockRequests() {
	p.uploadBlocks = p.uploadBlocks[:0]
	p.uploadDone = p.uploadDone[:0]
}

func (p *PeerConnection) SendBlockData() {
	if p.uploadState != UploadStateUploading {
		return
	}
	if q := p.session.UploadQueue(); q != nil && q.CheckForTimeOver(p) {
		q.RemoveFromUploadQueue(p)
		p.SendOutOfPartReqsAndAddToWaitingQueue()
		return
	}
	src := p.ActiveUploadSource()
	if len(p.uploadBlocks) == 0 || src == nil {
		return
	}
	current := p.uploadBlocks[0]
	p.uploadBlocks = p.uploadBlocks[1:]
	if !src.CanUploadRange(current.Begin, current.End) {
		p.SendOutOfPartReqsAndAddToWaitingQueue()
		return
	}
	payload, err := src.ReadRange(current.Begin, current.End)
	if err != nil {
		if q := p.session.UploadQueue(); q != nil {
			q.RemoveFromUploadQueue(p)
		}
		return
	}
	togo := len(payload)
	if togo == 0 {
		return
	}
	if p.canCompressToPeer() {
		if compressed, ok := tryCompressPayload(payload); ok {
			if err := p.sendCompressedPayload(current.Begin, compressed); err != nil {
				if q := p.session.UploadQueue(); q != nil {
					q.RemoveFromUploadQueue(p)
				}
				return
			}
			current.Transferred = current.End - current.Begin
			p.uploadDone = append(p.uploadDone, current)
			return
		}
	}
	packetSize := togo
	if togo > 10240 {
		packetSize = togo / (togo / 10240)
	}
	offset := current.Begin
	for togo > 0 {
		if togo < packetSize*2 {
			packetSize = togo
		}
		chunk := payload[len(payload)-togo : len(payload)-togo+packetSize]
		if err := p.SendPart(offset, offset+int64(packetSize), chunk); err != nil {
			if q := p.session.UploadQueue(); q != nil {
				q.RemoveFromUploadQueue(p)
			}
			return
		}
		offset += int64(packetSize)
		togo -= packetSize
	}
	current.Transferred = current.End - current.Begin
	p.uploadDone = append(p.uploadDone, current)
}

func (p *PeerConnection) SendOutOfPartReqsAndAddToWaitingQueue() {
	p.SendOutOfParts()
	p.ClearUploadBlockRequests()
	if q := p.session.UploadQueue(); q != nil {
		q.AddClientToQueue(p)
	}
}

func (p *PeerConnection) HandleHelloAnswer(value *clientproto.HelloAnswer) {
	if value == nil {
		return
	}
	p.remoteHash = value.Hash
	p.friendSlot = p.session.IsFriendSlot(value.Hash)
	if value.Point.Defined() {
		p.endpoint.AssignEndpoint(value.Point)
	}
	parseHelloTagList(&p.remotePeerInfo, &value.Properties)
	p.persistRemoteUDPPort()
	debugPeerf("peer %s <- HelloAnswer", p.endpoint.String())
	p.markHelloInfoDone()
	p.SendExtHello()
	if p.transfer != nil {
		p.SendFileRequest(p.transfer.GetHash())
	}
}

func (p *PeerConnection) HandleExtHello(value *clientproto.ExtHello) {
	if value == nil {
		return
	}
	debugPeerf("peer %s <- ExtHello", p.endpoint.String())
	p.applyExtHelloTags(&value.Properties)
	p.SendExtHelloAnswer()
	p.markExtHelloInfoDone()
}

func (p *PeerConnection) HandleExtHelloAnswer(value *clientproto.ExtHelloAnswer) {
	if value == nil {
		return
	}
	debugPeerf("peer %s <- ExtHelloAnswer", p.endpoint.String())
	p.applyExtHelloTags(&value.Properties)
	p.markExtHelloInfoDone()
}

func (p *PeerConnection) HandleClientHello(value *clientproto.Hello) {
	if value == nil {
		return
	}
	p.remoteHash = value.Hash
	p.friendSlot = p.session.IsFriendSlot(value.Hash)
	if value.Point.Defined() {
		p.endpoint.AssignEndpoint(value.Point)
	}
	parseHelloTagList(&p.remotePeerInfo, &value.Properties)
	p.persistRemoteUDPPort()
	debugPeerf("peer %s <- Hello", p.endpoint.String())
	if value.Point.Defined() && IsLowID(value.Point.IP()) {
		p.session.tryAttachCallbackPeer(p, value.Point.IP())
	}
	answer := p.PrepareHelloAnswer()
	if raw, err := p.combiner.Pack("client.HelloAnswer", &answer); err == nil {
		p.QueuePacket(raw)
	}
	p.markHelloInfoDone()
	p.SendExtHello()
}

func (p *PeerConnection) persistRemoteUDPPort() {
	if p == nil || p.remotePeerInfo.UDPPort == 0 || p.peerInfo == nil {
		return
	}
	p.peerInfo.UDPPort = p.remotePeerInfo.UDPPort
}

func (p *PeerConnection) ActiveUploadSource() UploadableResource {
	if p.transfer != nil {
		return p.transfer
	}
	return p.uploadResource
}

func (p *PeerConnection) SetUploadResource(res UploadableResource) {
	p.uploadResource = res
}

func (p *PeerConnection) attachUploadByHash(hash protocol.Hash) UploadableResource {
	if p.transfer != nil && p.transfer.GetHash().Equal(hash) && p.transfer.CanUpload() {
		return p.transfer
	}
	if t := p.session.LookupTransfer(hash); t != nil && t.CanUpload() {
		if err := t.AttachIncomingPeer(p); err != nil {
			return nil
		}
		return t
	}
	if sf := p.session.SharedStore().Get(hash); sf != nil && sf.CanUpload() {
		if err := p.session.attachIncomingSharedUpload(p, sf); err != nil {
			return nil
		}
		return sf
	}
	return nil
}

func (p *PeerConnection) HandleClientFileRequest(value *clientproto.FileRequest) {
	if value == nil {
		return
	}
	res := p.attachUploadByHash(value.Hash)
	if res == nil {
		p.Close(NoTransfer)
		return
	}
	p.SendFileAnswer(res)
}

func (p *PeerConnection) HandleClientFileStatusRequest(value *clientproto.FileStatusRequest) {
	if value == nil {
		return
	}
	res := p.attachUploadByHash(value.Hash)
	if res == nil {
		packet := clientproto.NoFileStatus{}
		if raw, err := p.combiner.Pack("client.NoFileStatus", &packet); err == nil {
			p.QueuePacket(raw)
		}
		return
	}
	p.SendFileStatusAnswer(res)
}

func (p *PeerConnection) HandleClientHashSetRequest(value *clientproto.HashSetRequest) {
	if value == nil {
		return
	}
	res := p.attachUploadByHash(value.Hash)
	if res == nil {
		p.Close(NoTransfer)
		return
	}
	if len(res.UploadHashSet()) == 0 {
		p.Close(WrongHashSet)
		return
	}
	p.SendHashSetAnswer(res)
}

func (p *PeerConnection) HandleClientAICHRequest(value *clientproto.AICHRequest) {
	if value == nil {
		return
	}
	res := p.attachUploadByHash(value.Hash)
	if res == nil {
		return
	}
	p.SendAICHAnswer(res, value.Hashes)
}

func (p *PeerConnection) HandleClientAICHFileHashRequest(value *clientproto.AICHFileHashRequest) {
	if value == nil {
		return
	}
	res := p.attachUploadByHash(value.Hash)
	if res == nil {
		return
	}
	p.SendAICHFileHashAnswer(res)
}

func (p *PeerConnection) HandleClientStartUpload(value *clientproto.StartUpload) {
	if value == nil {
		return
	}
	res := p.attachUploadByHash(value.Hash)
	if res == nil || !res.CanUpload() {
		p.SendQueueRanking(1)
		return
	}
	p.session.UploadQueue().AddClientToQueue(p)
}

func (p *PeerConnection) HandleClientCancelTransfer() {
	if q := p.session.UploadQueue(); q != nil {
		q.RemoveFromUploadQueue(p)
	}
}

func (p *PeerConnection) handleUploadRange(begin, end int64) error {
	src := p.ActiveUploadSource()
	if src == nil || !src.CanUploadRange(begin, end) {
		p.SendOutOfParts()
		return nil
	}
	payload, err := src.ReadRange(begin, end)
	if err != nil {
		return err
	}
	return p.SendPart(begin, end, payload)
}

func (p *PeerConnection) HandleClientRequestParts32(value *clientproto.RequestParts32) error {
	if value == nil {
		return nil
	}
	res := p.attachUploadByHash(value.Hash)
	if res == nil {
		p.SendOutOfParts()
		return nil
	}
	for i := 0; i < value.CurrentFree; i++ {
		begin := int64(value.BeginOffset[i])
		end := int64(value.EndOffset[i])
		if end <= begin {
			continue
		}
		reqs, err := data.MakePeerRequests(begin, end, res.Size())
		if err != nil {
			return err
		}
		for _, req := range reqs {
			p.AddUploadRequest(req)
		}
	}
	return nil
}

func (p *PeerConnection) HandleClientRequestParts64(value *clientproto.RequestParts64) error {
	if value == nil {
		return nil
	}
	res := p.attachUploadByHash(value.Hash)
	if res == nil {
		p.SendOutOfParts()
		return nil
	}
	for i := 0; i < value.CurrentFree; i++ {
		begin := int64(value.BeginOffset[i])
		end := int64(value.EndOffset[i])
		if end <= begin {
			continue
		}
		reqs, err := data.MakePeerRequests(begin, end, res.Size())
		if err != nil {
			return err
		}
		for _, req := range reqs {
			p.AddUploadRequest(req)
		}
	}
	return nil
}

func (p *PeerConnection) HandleAICHAnswer(value *clientproto.AICHAnswer) {
	if value == nil || p.transfer == nil || !value.Hash.Equal(p.transfer.GetHash()) {
		return
	}
	if len(value.Hashes) == 0 {
		return
	}
	pieceIndex := p.pendingAICHPiece
	p.pendingAICHPiece = -1
	if pieceIndex < 0 {
		return
	}
	p.transfer.StoreAICHPieceBlocks(pieceIndex, value.Hashes)
	p.transfer.tryAICHRecoverPiece(pieceIndex)
	p.transfer.retryPendingAICHRecoveries()
}

func (p *PeerConnection) HandleAICHFileHashAnswer(value *clientproto.AICHFileHashAnswer) {
	if value == nil || p.transfer == nil || !value.Hash.Equal(p.transfer.GetHash()) {
		return
	}
	if value.RootHash.IsZero() {
		return
	}
	if existing, ok := p.transfer.AICHRootHash(); ok {
		if !existing.Equal(value.RootHash) {
			debugPeerf("peer %s AICH root conflict have=%s got=%s", p.endpoint.String(), existing.String(), value.RootHash.String())
		}
		return
	}
	p.transfer.SetAICHRootHash(value.RootHash)
	if _, ok := p.transfer.AICHRootHash(); ok {
		p.transfer.retryPendingAICHRecoveries()
	}
}

func (p *PeerConnection) HandleFileAnswer(value *clientproto.FileAnswer) {
	debugPeerf("peer %s <- FileAnswer", p.endpoint.String())
	if p.transfer != nil && value.Hash.Equal(p.transfer.GetHash()) {
		p.SendFileStatusRequest(p.transfer.GetHash())
	} else {
		p.Close(NoTransfer)
	}
}

func (p *PeerConnection) HandleFileStatusAnswer(value *clientproto.FileStatusAnswer) {
	debugPeerf("peer %s <- FileStatusAnswer pieces=%d have=%d first0=%t first1=%t first2=%t",
		p.endpoint.String(),
		p.remotePieces.Len(),
		p.remotePieces.Count(),
		p.remotePieces.GetBit(0),
		p.remotePieces.GetBit(1),
		p.remotePieces.GetBit(2))
	if p.transfer != nil && value != nil && p.transfer.GetHash().Equal(value.Hash) {
		p.maybeRequestAICHRoot()
	}
	if p.transfer != nil && p.transfer.Size() > 9728000 {
		p.SendHashSetRequest(p.transfer.GetHash())
	} else if p.transfer != nil {
		if p.transfer.GetHash().Equal(value.Hash) {
			p.transfer.SetHashSet(value.Hash, []protocol.Hash{value.Hash})
			p.maybeSendSourceExchange()
			p.SendStartUpload(p.transfer.GetHash())
		} else {
			p.Close(HashMismatch)
		}
	}
}

func (p *PeerConnection) ProcessIncoming() error {
	if err := p.expandIncomingMultiPackets(); err != nil {
		return err
	}
	headers, bodies, err := p.ReadFramesWithCombiner(&p.combiner)
	if err != nil {
		return err
	}
	for i, header := range headers {
		packet, err := p.combiner.Unpack(header, bodies[i])
		if err != nil {
			debugPeerf("peer %s unpack error header=%s body=%d err=%v", p.endpoint.String(), header.String(), len(bodies[i]), err)
			return err
		}
		switch value := packet.(type) {
		case *clientproto.HelloAnswer:
			p.HandleHelloAnswer(value)
		case *clientproto.Hello:
			p.HandleClientHello(value)
		case *clientproto.ExtHello:
			p.HandleExtHello(value)
		case *clientproto.ExtHelloAnswer:
			p.HandleExtHelloAnswer(value)
		case *clientproto.PublicKey:
			p.HandlePublicKey(value)
		case *clientproto.Signature:
			p.HandleSignature(value)
		case *clientproto.SecIdentState:
			p.HandleSecIdentState(value)
		case *clientproto.FileRequest:
			p.HandleClientFileRequest(value)
		case *clientproto.FileAnswer:
			p.HandleFileAnswer(value)
		case *clientproto.FileStatusRequest:
			p.HandleClientFileStatusRequest(value)
		case *clientproto.FileStatusAnswer:
			p.remotePieces = value.BitField
			p.HandleFileStatusAnswer(value)
		case *clientproto.HashSetRequest:
			p.HandleClientHashSetRequest(value)
		case *clientproto.AICHRequest:
			p.HandleClientAICHRequest(value)
		case *clientproto.AICHFileHashRequest:
			p.HandleClientAICHFileHashRequest(value)
		case *clientproto.HashSetAnswer:
			debugPeerf("peer %s <- HashSetAnswer parts=%d", p.endpoint.String(), len(value.Parts))
			if p.transfer != nil {
				if p.transfer.GetHash().Equal(value.Hash) &&
					p.transfer.GetHash().Equal(protocol.HashFromHashSet(value.Parts)) &&
					p.transfer.picker.GetPieceCount() == len(value.Parts) {
					p.transfer.SetHashSet(value.Hash, value.Parts)
					p.maybeRequestAICHRoot()
					p.maybeSendSourceExchange()
					p.SendStartUpload(p.transfer.GetHash())
				} else {
					p.Close(WrongHashSet)
				}
			}
		case *clientproto.AcceptUpload:
			p.HandleAcceptUpload()
		case *clientproto.StartUpload:
			p.HandleClientStartUpload(value)
		case *clientproto.RequestParts32:
			if err := p.HandleClientRequestParts32(value); err != nil {
				return err
			}
		case *clientproto.RequestParts64:
			if err := p.HandleClientRequestParts64(value); err != nil {
				return err
			}
		case *clientproto.CancelTransfer:
			p.HandleClientCancelTransfer()
		case *clientproto.QueueRanking:
			p.HandleQueueRanking(value)
		case *clientproto.FileComment:
			p.HandleFileComment(value)
		case *clientproto.SendingPart32:
			debugPeerf("peer %s <- SendingPart32 %d..%d", p.endpoint.String(), value.BeginOffset, value.EndOffset)
			if req, err := data.MakePeerRequest(int64(value.BeginOffset), int64(value.EndOffset)); err == nil {
				p.ReceiveData(req, false)
			}
		case *clientproto.SendingPart64:
			debugPeerf("peer %s <- SendingPart64 %d..%d", p.endpoint.String(), value.BeginOffset, value.EndOffset)
			if req, err := data.MakePeerRequest(int64(value.BeginOffset), int64(value.EndOffset)); err == nil {
				p.ReceiveData(req, false)
			}
		case *clientproto.CompressedPart32:
			debugPeerf("peer %s <- CompressedPart32 %d len=%d", p.endpoint.String(), value.BeginOffset, value.CompressedLength)
			p.ReceiveCompressedData(header, int64(value.BeginOffset), int64(value.CompressedLength), value.BytesCount())
		case *clientproto.CompressedPart64:
			debugPeerf("peer %s <- CompressedPart64 %d len=%d", p.endpoint.String(), value.BeginOffset, value.CompressedLength)
			p.ReceiveCompressedData(header, int64(value.BeginOffset), int64(value.CompressedLength), value.BytesCount())
		case *clientproto.NoFileStatus:
			p.Close(NoTransfer)
		case *clientproto.OutOfParts:
			p.Close(OutOfParts)
		case *clientproto.RequestSources2:
			p.HandleRequestSources2(value)
		case *clientproto.AnswerSources2:
			p.HandleAnswerSources2(value)
		case *clientproto.RequestSources:
			p.HandleRequestSources(value)
		case *clientproto.AnswerSources:
			p.HandleAnswerSources(value)
		case *clientproto.RequestPreview:
			p.HandleClientRequestPreview(value)
		case *clientproto.PreviewAnswer:
			p.HandlePreviewAnswer(value)
		case *clientproto.AICHAnswer:
			p.HandleAICHAnswer(value)
		case *clientproto.AICHFileHashAnswer:
			p.HandleAICHFileHashAnswer(value)
		}
	}
	return nil
}

func (p *PeerConnection) SetPeer(peer *Peer) {
	p.peerInfo = peer
}

func (p *PeerConnection) SetTransfer(transfer *Transfer) {
	p.transfer = transfer
	p.uploadResource = nil
}

func (p *PeerConnection) GetInfo() PeerInfo {
	if p == nil {
		return PeerInfo{}
	}
	stats := p.Statistics()
	info := PeerInfo{
		DownloadSpeed:        int(stats.DownloadRate()),
		PayloadDownloadSpeed: int(stats.DownloadPayloadRate()),
		UploadSpeed:          int(stats.UploadRate()),
		PayloadUploadSpeed:   int(stats.UploadPayloadRate()),
		RemotePieces:         p.remotePieces,
		Endpoint:             p.endpoint,
		SourceFlag:           0,
	}
	info.UserHash = p.remoteHash
	info.NickName = p.remotePeerInfo.NickName
	info.Connected = p.socket != nil && !p.IsDisconnecting()
	if p.session != nil {
		info.TotalUploaded, info.TotalDownloaded = p.session.Credits().TotalsForPeer(p.remoteHash)
	}
	if p.peerInfo != nil {
		info.FailCount = p.peerInfo.FailCount
		info.SourceFlag = p.peerInfo.SourceFlag
	}
	info.ModName = p.remotePeerInfo.ModName
	info.Version = p.remotePeerInfo.Version
	info.ModVersion = p.remotePeerInfo.ModNumber
	info.StrModVersion = p.remotePeerInfo.ModVersion
	info.HelloMisc1 = p.remotePeerInfo.Misc1.IntValue()
	info.HelloMisc2 = p.remotePeerInfo.Misc2.Value
	return info
}

func (p *PeerConnection) GetPeer() *Peer {
	return p.peerInfo
}

func (p *PeerConnection) Speed() PeerSpeed {
	if p.transfer != nil {
		downloadRate := p.Statistics().DownloadPayloadRate()
		transferDownloadRate := p.transfer.stat.DownloadPayloadRate()
		if downloadRate > 512 && downloadRate > transferDownloadRate/16 {
			p.speed = PeerSpeedFast
		} else if downloadRate > 4096 && downloadRate > transferDownloadRate/64 {
			p.speed = PeerSpeedMedium
		} else if downloadRate < transferDownloadRate/15 && p.speed == PeerSpeedFast {
			p.speed = PeerSpeedMedium
		} else {
			p.speed = PeerSpeedSlow
		}
	}
	return p.speed
}

func (p *PeerConnection) IsRequesting(block data.PieceBlock) bool {
	return p.GetDownloading(block) != nil
}

func (p *PeerConnection) GetDownloading(block data.PieceBlock) *PendingBlock {
	for i := range p.downloadQueue {
		if p.downloadQueue[i].Block.Compare(block) == 0 {
			return &p.downloadQueue[i]
		}
	}
	return nil
}

func (p *PeerConnection) RequestBlocks() {
	if p.transfer == nil || p.transferringData || len(p.downloadQueue) != 0 {
		return
	}
	blocks := make([]data.PieceBlock, 0)
	p.transfer.picker.PickPiecesWithAvailability(&blocks, RequestQueueSize, p.GetPeer(), p.Speed(), &p.remotePieces)
	use32 := p.transfer.Size() <= math.MaxUint32
	req32 := &clientproto.RequestParts32{Hash: p.transfer.GetHash()}
	req64 := &clientproto.RequestParts64{Hash: p.transfer.GetHash()}
	for len(blocks) > 0 && len(p.downloadQueue) < RequestQueueSize {
		block := blocks[0]
		blocks = blocks[1:]
		p.downloadQueue = append(p.downloadQueue, NewPendingBlock(block, p.transfer.Size()))
		if use32 {
			req32.AppendRange(block.Range(p.transfer.Size()))
		} else {
			req64.AppendRange(block.Range(p.transfer.Size()))
		}
	}
	if use32 && req32.CurrentFree > 0 {
		debugPeerf("peer %s -> RequestParts32 blocks=%d ranges=[%d..%d][%d..%d][%d..%d]",
			p.endpoint.String(),
			req32.CurrentFree,
			req32.BeginOffset[0], req32.EndOffset[0],
			req32.BeginOffset[1], req32.EndOffset[1],
			req32.BeginOffset[2], req32.EndOffset[2])
		p.SendRequestParts32(req32)
	} else if !use32 && req64.CurrentFree > 0 {
		debugPeerf("peer %s -> RequestParts64 blocks=%d ranges=[%d..%d][%d..%d][%d..%d]",
			p.endpoint.String(),
			req64.CurrentFree,
			req64.BeginOffset[0], req64.EndOffset[0],
			req64.BeginOffset[1], req64.EndOffset[1],
			req64.BeginOffset[2], req64.EndOffset[2])
		p.SendRequestParts64(req64)
	} else {
		debugPeerf("peer %s no blocks to request, closing", p.endpoint.String())
		p.Close(NoError)
	}
}

func (p *PeerConnection) AbortAllRequests() {
	if p.transfer != nil {
		for _, pb := range p.downloadQueue {
			p.transfer.picker.AbortDownload(pb.Block, p.GetPeer())
		}
	}
	p.downloadQueue = nil
}

func (p *PeerConnection) hasStalledDownloadRequest(now int64) bool {
	if p.transfer == nil || p.transferringData || len(p.downloadQueue) == 0 {
		return false
	}
	timeout := int64(p.session.settings.PeerConnectionTimeout) * 500
	if timeout < Seconds(10) {
		timeout = Seconds(10)
	}
	if p.MillisecondsSinceLastReceive() < timeout/2 {
		return false
	}
	for _, pb := range p.downloadQueue {
		if now-pb.CreateTime >= timeout {
			return true
		}
	}
	return false
}

func (p *PeerConnection) ReceiveCompressedData(header protocol.PacketHeader, offset, compressedLength int64, payloadSize int) {
	block := data.MakePieceBlock(offset)
	pb := p.GetDownloading(block)
	if pb != nil && len(pb.Buffer) == 0 {
		pb.DataSize = compressedLength
	}
	beginOffset := offset
	if pb != nil {
		beginOffset = int64(pb.Block.Range(p.transfer.Size()).Left) + pb.Received
	}
	endOffset := beginOffset + int64(header.SizePacket()) - int64(payloadSize)
	if req, err := data.MakePeerRequest(beginOffset, endOffset); err == nil {
		p.ReceiveData(req, true)
	}
}

func (p *PeerConnection) ReceiveData(req data.PeerRequest, compressed bool) {
	p.clearRemoteQueueState()
	p.transferringData = true
	p.recvReq = req
	p.recvPos = 0
	p.recvReqCompressed = compressed
	p.ReceivePendingData()
}

func (p *PeerConnection) ReceivePendingData() {
	if !p.transferringData || p.transfer == nil {
		return
	}
	block := data.NewPieceBlock(p.recvReq.Piece, int(p.recvReq.Start/BlockSize))
	pb := p.GetDownloading(block)
	if pb == nil {
		p.skipPendingData()
		return
	}
	if pb.Buffer == nil {
		pb.Buffer = make([]byte, block.Size(p.transfer.Size()))
	}
	remaining := int(p.recvReq.Length) - p.recvPos
	if remaining <= 0 {
		p.transferringData = false
		return
	}
	payload := p.ConsumeIncoming(remaining)
	if len(payload) == 0 {
		return
	}
	p.session.Credits().AddDownloaded(p.remoteHash, int64(len(payload)), p.identityVerified)
	offset := int(p.recvReq.InBlockOffset()) + p.recvPos
	copy(pb.Buffer[offset:], payload)
	pb.Received += int64(len(payload))
	p.recvPos += len(payload)
	if p.recvPos < int(p.recvReq.Length) {
		return
	}
	if p.CompleteBlock(pb) {
		block := pb.Block
		buffer := pb.Buffer
		wasFinished := p.transfer.picker.IsPieceFinished(p.recvReq.Piece)
		wasDownloading := p.transfer.picker.MarkAsWriting(block)
		p.removePending(block)
		if wasDownloading {
			p.asyncWrite(block, buffer, p.transfer)
			if p.transfer.picker.IsPieceFinished(p.recvReq.Piece) && !wasFinished {
				p.transfer.QueuePieceHash(block.PieceIndex)
			}
		}
		p.transferringData = false
		p.continueBufferedIncoming()
		p.RequestBlocks()
		return
	}
	p.transferringData = false
	p.continueBufferedIncoming()
}

func (p *PeerConnection) skipPendingData() {
	remaining := int(p.recvReq.Length) - p.recvPos
	if remaining <= 0 {
		p.transferringData = false
		p.continueBufferedIncoming()
		p.RequestBlocks()
		return
	}
	chunk := p.ConsumeIncoming(remaining)
	p.recvPos += len(chunk)
	if p.recvPos >= int(p.recvReq.Length) {
		p.transferringData = false
		p.continueBufferedIncoming()
		p.RequestBlocks()
	}
}

func (p *PeerConnection) continueBufferedIncoming() {
	for !p.transferringData && len(p.incoming) != 0 {
		before := p.IncomingBytes()
		if err := p.ProcessIncoming(); err != nil {
			p.Close(IOException)
			return
		}
		after := p.IncomingBytes()
		if len(p.incoming) == 0 || after >= before {
			return
		}
	}
}

func (p *PeerConnection) CompleteBlock(pb *PendingBlock) bool {
	if pb == nil {
		return false
	}
	if pb.Received < pb.DataSize {
		return false
	}
	if p.recvReqCompressed {
		reader, err := zlib.NewReader(bytes.NewReader(pb.Buffer[:pb.Received]))
		if err == nil {
			defer reader.Close()
			raw, err := io.ReadAll(reader)
			if err == nil {
				pb.Buffer = raw
			}
		}
	}
	return true
}

func (p *PeerConnection) UploadScore() uint32 {
	if p == nil || p.uploadState == UploadStateUploading {
		return 0
	}
	if p.FriendSlot() && !p.IsUploadLowID() {
		return 0x0FFFFFFF
	}
	waitStart := p.UploadWaitStart()
	if waitStart == 0 {
		return 0
	}
	base := float64(CurrentTime()-waitStart) / 1000.0
	base *= p.session.Credits().ScoreRatio(p.remoteHash)
	if src := p.ActiveUploadSource(); src != nil {
		base *= src.UploadPriority().ScoreFactor()
	}
	if base < 0 {
		return 0
	}
	return uint32(base)
}

func (p *PeerConnection) removePending(block data.PieceBlock) {
	dst := p.downloadQueue[:0]
	for _, pb := range p.downloadQueue {
		if pb.Block.Compare(block) != 0 {
			dst = append(dst, pb)
		}
	}
	p.downloadQueue = dst
}

func (p *PeerConnection) asyncWrite(block data.PieceBlock, buffer []byte, transfer *Transfer) {
	if transfer != nil {
		transfer.throttleDownloadWrite(len(buffer))
	}
	transfer.picker.MarkAsWriting(block)
	p.session.SubmitDiskTask(NewAsyncWrite(block, buffer, transfer))
}

const (
	helloTagNickName           = 0x01
	helloTagVersion            = 0x11
	helloTagModName            = 0x55
	helloTagModVer             = 0xFB
	helloTagUDPPorts           = 0xF9
	helloTagMisc1              = 0xFA
	helloTagMisc2              = 0xFE
	helloTagSourceExchange2Ver = 0x3B
)

func helloStringFromTag(t protocol.SimpleTag) string {
	switch t.Type {
	case protocol.TagTypeString:
		return t.String
	default:
		if t.Type >= protocol.TagTypeStr1 && t.Type <= protocol.TagTypeStr1+15 {
			return t.String
		}
	}
	return ""
}

func decodeHelloModComposite(v uint32) string {
	major := int((v >> 17) & 0x7f)
	// 低 17 位内 minor（10..16）与 build（7..13）在编码上重叠，需先去掉 minor 再取 build。
	r := v & 0x1ffff
	minor := int((r >> 10) & 0x7f)
	build := int(((r - (uint32(minor) << 10)) >> 7) & 0x7f)
	if major == 0 && minor == 0 && build == 0 {
		return ""
	}
	return fmt.Sprintf("%d.%d.%d", major, minor, build)
}

// parseHelloTagList 从 Hello/HelloAnswer 的 Properties 解析昵称、客户端标识与 Misc 选项（与 eMule 标签约定一致）。
func parseHelloTagList(dst *RemotePeerInfo, props *protocol.TagList) {
	if dst == nil || props == nil {
		return
	}
	for _, t := range *props {
		switch t.ID {
		case helloTagNickName:
			if s := helloStringFromTag(t); s != "" {
				dst.NickName = s
			}
		case helloTagModName:
			if s := helloStringFromTag(t); s != "" {
				dst.ModName = s
			}
		case helloTagVersion:
			if t.Type == protocol.TagTypeUint32 {
				dst.Version = int(t.UInt32)
			}
		case helloTagModVer:
			if t.Type == protocol.TagTypeUint32 {
				dst.ModNumber = int(t.UInt32)
				dst.ModVersion = decodeHelloModComposite(t.UInt32)
			}
		case helloTagUDPPorts:
			if t.Type == protocol.TagTypeUint32 {
				dst.UDPPort = uint16(t.UInt32)
			}
		case helloTagMisc1:
			if t.Type == protocol.TagTypeUint32 {
				dst.Misc1.Assign(int(t.UInt32))
			}
		case helloTagMisc2:
			if t.Type == protocol.TagTypeUint32 {
				dst.Misc2.Assign(int(t.UInt32))
			}
		case helloTagSourceExchange2Ver:
			if t.Type == protocol.TagTypeUint32 && t.UInt32 > 0 {
				dst.SourceExchange2Ver = byte(t.UInt32)
			}
		}
	}
}

func (p *PeerConnection) maybeSendRequestSources2() {
	p.maybeSendSourceExchange()
}

func (p *PeerConnection) SendRequestSources2(hash protocol.Hash) error {
	pkt := clientproto.RequestSources2{
		Version:  clientproto.SourceExchange2Version,
		Reserved: 0,
		Hash:     hash,
	}
	raw, err := p.combiner.Pack("client.RequestSources2", &pkt)
	if err != nil {
		return err
	}
	debugPeerf("peer %s -> RequestSources2", p.endpoint.String())
	p.QueuePacket(raw)
	return nil
}

func (p *PeerConnection) HandleFileComment(value *clientproto.FileComment) {
	if value == nil || p.remotePeerInfo.Misc1.AcceptCommentVer == 0 {
		return
	}
	comment := string(value.Comment)
	if len(comment) > clientproto.MaxFileCommentLen {
		comment = comment[:clientproto.MaxFileCommentLen]
	}
	p.remoteFileRating = value.Rating
	p.remoteFileComment = comment
}

func (p *PeerConnection) HandleRequestSources2(req *clientproto.RequestSources2) {
	if req == nil {
		return
	}
	res := p.attachUploadByHash(req.Hash)
	if tf, ok := res.(*Transfer); ok && tf != nil {
		peers := tf.policy.PeersForSourceExchange(p.endpoint, SourceExchangePeerLimit)
		if len(peers) == 0 {
			return
		}
		entries := p.buildSourceExchangeEntries(peers)
		p.sendAnswerSources2(req.Hash, entries)
		return
	}
	if sf, ok := res.(*SharedFile); ok && sf != nil {
		_ = sf
		p.sendAnswerSources2SelfOnly(req.Hash)
	}
}

func (p *PeerConnection) sendAnswerSources2SelfOnly(hash protocol.Hash) {
	ep := p.session.kadPublishEndpoint()
	if !ep.Defined() {
		return
	}
	sx1 := p.remotePeerInfo.Misc1.SourceExchange1Ver
	uid := uint32(ep.IP())
	if sx1 <= 2 {
		uid = clientproto.SwapUint32(uid)
	}
	entries := []clientproto.SourceExchangeEntry{{
		UserID:       uid,
		TCPPort:      uint16(ep.Port()),
		CryptOptions: cryptOptionsForLocal(p.session.settings),
	}}
	p.sendAnswerSources2(hash, entries)
}

func (p *PeerConnection) sourceExchange2VersionForPeer() byte {
	if p.remotePeerInfo.SourceExchange2Ver >= clientproto.SourceExchangeIPv6Version {
		return clientproto.SourceExchangeIPv6Version
	}
	return clientproto.SourceExchange2Version
}

func (p *PeerConnection) sendAnswerSources2(hash protocol.Hash, entries []clientproto.SourceExchangeEntry) {
	if len(entries) == 0 {
		return
	}
	ans := clientproto.AnswerSources2{
		Version: p.sourceExchange2VersionForPeer(),
		Hash:    hash,
		Entries: entries,
	}
	raw, err := p.combiner.Pack("client.AnswerSources2", &ans)
	if err != nil {
		return
	}
	debugPeerf("peer %s -> AnswerSources2 entries=%d", p.endpoint.String(), len(entries))
	p.QueuePacket(raw)
}

func (p *PeerConnection) maybeSendFileComment() {
	if p.transfer == nil {
		return
	}
	comment := strings.TrimSpace(p.transfer.FileComment())
	if comment == "" {
		return
	}
	packet := clientproto.FileComment{Comment: []byte(comment)}
	raw, err := p.combiner.Pack("client.FileComment", &packet)
	if err != nil {
		return
	}
	p.QueuePacket(raw)
}

func (p *PeerConnection) buildSourceExchangeEntries(peers []Peer) []clientproto.SourceExchangeEntry {
	sx1 := p.remotePeerInfo.Misc1.SourceExchange1Ver
	sx2 := p.sourceExchange2VersionForPeer()
	crypt := cryptOptionsForLocal(p.session.settings)
	out := make([]clientproto.SourceExchangeEntry, 0, len(peers))
	for _, pe := range peers {
		entry, ok := pe.ToSourceExchangeEntry(sx1, sx2, crypt)
		if !ok {
			continue
		}
		out = append(out, entry)
	}
	return out
}

func (p *PeerConnection) HandleAnswerSources2(ans *clientproto.AnswerSources2) {
	if ans == nil || p.transfer == nil {
		return
	}
	if !ans.Hash.Equal(p.transfer.GetHash()) {
		return
	}
	peers := make([]Peer, 0, len(ans.Entries))
	for _, e := range ans.Entries {
		peer, ok := peerFromSourceExchangeEntry(e, ans.Version)
		if !ok {
			continue
		}
		if peer.Endpoint.Defined() && !p.isAcceptableSourceExchangeEndpoint(peer.Endpoint) {
			continue
		}
		if peer.DialAddr != nil && isFilteredPeerTCPAddr(peer.DialAddr) {
			continue
		}
		peers = append(peers, peer)
	}
	if n := p.transfer.policy.MergeSourceExchangePeers(peers); n > 0 {
		debugPeerf("peer %s <- AnswerSources2 merged=%d", p.endpoint.String(), n)
	}
}

func peerFromSourceExchangeEntry(e clientproto.SourceExchangeEntry, packetVer byte) (Peer, bool) {
	peer := Peer{
		Connectable:  true,
		SourceFlag:   int(PeerSourceExchange),
		UserHash:     e.UserHash,
		CryptOptions: e.CryptOptions,
	}
	if e.UserID != 0 {
		ep := endpointFromSourceExchangeEntry(e.UserID, e.TCPPort, packetVer)
		if !ep.Defined() {
			return Peer{}, false
		}
		peer.Endpoint = ep
	}
	if packetVer >= clientproto.SourceExchangeIPv6Version {
		if ip := e.EntryIPv6Addr(); ip != nil && !ip.IsUnspecified() {
			peer.DialAddr = &net.TCPAddr{IP: ip, Port: int(e.TCPPort)}
		}
	}
	if !peer.Endpoint.Defined() && peer.DialAddr == nil {
		return Peer{}, false
	}
	return peer, true
}

func endpointFromSourceExchangeEntry(dwID uint32, port uint16, packetVer byte) protocol.Endpoint {
	var ipU32 uint32
	if packetVer >= 3 {
		ipU32 = clientproto.SwapUint32(dwID)
	} else {
		ipU32 = dwID
	}
	return protocol.NewEndpoint(int32(ipU32), int(port))
}

func (p *PeerConnection) isAcceptableSourceExchangeEndpoint(ep protocol.Endpoint) bool {
	if !ep.Defined() {
		return false
	}
	if IsLocalAddress(ep.IP()) {
		return false
	}
	self := protocol.NewEndpoint(p.session.GetClientID(), p.session.GetListenPort())
	if ep.Equal(self) {
		return false
	}
	if ep.Equal(p.endpoint) {
		return false
	}
	return true
}

const (
	helloInfoStandard = 1 << 0
	helloInfoExtended = 1 << 1

	extTagSecIdentVersion = 0x28
	extTagSecIdentKeyFP   = 0x29
)

func (p *PeerConnection) IdentityVerified() bool {
	return p != nil && p.identityVerified
}

func (p *PeerConnection) applyExtHelloTags(props *protocol.TagList) {
	if props == nil {
		return
	}
	for _, t := range *props {
		switch t.ID {
		case extTagSecIdentVersion:
			if t.Type == protocol.TagTypeUint32 {
				p.remoteSecIdentVer = int(t.UInt32)
			}
		case extTagSecIdentKeyFP:
			if t.Type == protocol.TagTypeUint32 {
				p.remoteKeyFP = t.UInt32
			}
		}
	}
}

func (p *PeerConnection) markHelloInfoDone() {
	p.helloInfoFlags |= helloInfoStandard
	p.maybeSendFileComment()
	p.maybeStartSecIdent()
}

func (p *PeerConnection) markExtHelloInfoDone() {
	p.helloInfoFlags |= helloInfoExtended
	p.maybeStartSecIdent()
}

func (p *PeerConnection) secIdentEnabled() bool {
	if p == nil || p.session == nil {
		return false
	}
	if !p.session.settings.EnableSecIdent {
		return false
	}
	if p.remotePeerInfo.Misc1.SupportSecIdent == 0 {
		return false
	}
	id := p.session.Identity()
	return id != nil && id.Available()
}

func (p *PeerConnection) maybeStartSecIdent() {
	if p.helloInfoFlags&helloInfoStandard == 0 || p.helloInfoFlags&helloInfoExtended == 0 {
		return
	}
	if !p.secIdentEnabled() {
		return
	}
	p.SendSecIdentState()
}

func (p *PeerConnection) randomChallenge() uint32 {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return 1
	}
	ch := binary.LittleEndian.Uint32(buf[:])
	if ch == 0 {
		return 1
	}
	return ch
}

func (p *PeerConnection) SendSecIdentState() {
	if !p.secIdentEnabled() {
		return
	}
	state := byte(SecIdentStateSignatureNeeded)
	if len(p.remotePubKey) == 0 {
		state = SecIdentStateKeyAndSigNeeded
	}
	p.ourChallenge = p.randomChallenge()
	packet := clientproto.SecIdentState{State: state, Challenge: p.ourChallenge}
	if raw, err := p.combiner.Pack("client.SecIdentState", &packet); err == nil {
		debugPeerf("peer %s -> SecIdentState state=%d", p.endpoint.String(), state)
		p.QueuePacket(raw)
	}
}

func (p *PeerConnection) SendPublicKey() {
	id := p.session.Identity()
	if id == nil || !id.Available() {
		return
	}
	packet := clientproto.PublicKey{Key: id.PublicKeyDER()}
	if raw, err := p.combiner.Pack("client.PublicKey", &packet); err == nil {
		debugPeerf("peer %s -> PublicKey len=%d", p.endpoint.String(), len(packet.Key))
		p.QueuePacket(raw)
	}
}

func (p *PeerConnection) SendSignature() {
	id := p.session.Identity()
	if id == nil || !id.Available() || len(p.remotePubKey) == 0 || p.remoteChallenge == 0 {
		return
	}
	sig, err := id.SignChallenge(p.remotePubKey, p.remoteChallenge)
	if err != nil {
		debugPeerf("peer %s sign challenge failed: %v", p.endpoint.String(), err)
		return
	}
	packet := clientproto.Signature{Signature: sig}
	if raw, err := p.combiner.Pack("client.Signature", &packet); err == nil {
		debugPeerf("peer %s -> Signature len=%d", p.endpoint.String(), len(sig))
		p.QueuePacket(raw)
	}
}

func (p *PeerConnection) HandleSecIdentState(value *clientproto.SecIdentState) {
	if value == nil || !p.secIdentEnabled() {
		return
	}
	debugPeerf("peer %s <- SecIdentState state=%d", p.endpoint.String(), value.State)
	p.remoteChallenge = value.Challenge
	switch value.State {
	case SecIdentStateSignatureNeeded:
		if len(p.remotePubKey) == 0 {
			p.SendPublicKey()
		}
		p.SendSignature()
	case SecIdentStateKeyAndSigNeeded:
		p.SendPublicKey()
	}
}

func (p *PeerConnection) HandlePublicKey(value *clientproto.PublicKey) {
	if value == nil || len(value.Key) == 0 {
		return
	}
	if len(p.remotePubKey) == 0 {
		p.remotePubKey = append([]byte(nil), value.Key...)
		if p.remoteKeyFP != 0 && PublicKeyFingerprint(value.Key) != p.remoteKeyFP {
			debugPeerf("peer %s public key fingerprint mismatch", p.endpoint.String())
		}
	}
	p.SendSignature()
}

func (p *PeerConnection) HandleSignature(value *clientproto.Signature) {
	if value == nil || len(value.Signature) == 0 || !p.secIdentEnabled() {
		return
	}
	id := p.session.Identity()
	if id == nil || !id.Available() || p.ourChallenge == 0 {
		return
	}
	if err := VerifySecIdentSignature(p.remotePubKey, id.PublicKeyDER(), p.ourChallenge, value.Signature); err != nil {
		debugPeerf("peer %s secure ident verify failed: %v", p.endpoint.String(), err)
		if p.session.settings.SecIdentRequired {
			p.Close(IllegalArgument)
		}
		return
	}
	p.identityVerified = true
	debugPeerf("peer %s secure ident verified", p.endpoint.String())
}
