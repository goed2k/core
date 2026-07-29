package goed2k

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/goed2k/core/disk"
	"github.com/goed2k/core/protocol"
	clientproto "github.com/goed2k/core/protocol/client"
)

// emuleInteropHarness 封装本地双端会话，用于 eMule 协议互操作回归。
type emuleInteropHarness struct {
	t *testing.T
}

func newEMuleInteropHarness(t *testing.T) *emuleInteropHarness {
	t.Helper()
	return &emuleInteropHarness{t: t}
}

func (h *emuleInteropHarness) reserveTCPPort() int {
	h.t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		h.t.Fatalf("reserve port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func (h *emuleInteropHarness) runLocalUpload(t *testing.T, seedSettings, leechSettings Settings, payload []byte) {
	t.Helper()
	hash, err := protocol.HashFromData(payload)
	if err != nil {
		t.Fatalf("hash payload: %v", err)
	}

	seedPath := filepath.Join(t.TempDir(), "seed.bin")
	if err := os.WriteFile(seedPath, payload, 0o644); err != nil {
		t.Fatalf("write seed: %v", err)
	}
	downloadPath := filepath.Join(t.TempDir(), "download.bin")

	if seedSettings.ListenPort == 0 {
		seedSettings.ListenPort = h.reserveTCPPort()
	}
	seedSession := NewSession(seedSettings)
	if err := seedSession.Listen(); err != nil {
		t.Fatalf("seed listen: %v", err)
	}
	defer seedSession.CloseListener()
	defer seedSession.DisconnectFrom()

	seedHandle, err := seedSession.AddTransferWithHandler(hash, int64(len(payload)), disk.NewDesktopFileHandler(seedPath))
	if err != nil {
		t.Fatalf("seed transfer: %v", err)
	}
	seedHandle.transfer.WeHave(0)

	leechSession := NewSession(leechSettings)
	defer leechSession.CloseListener()
	defer leechSession.DisconnectFrom()

	leechHandle, err := leechSession.AddTransferWithHandler(hash, int64(len(payload)), disk.NewDesktopFileHandler(downloadPath))
	if err != nil {
		t.Fatalf("leech transfer: %v", err)
	}
	registerTransferFileCleanup(t, seedHandle, leechHandle)

	endpoint, err := protocol.EndpointFromString("127.0.0.1", seedSettings.ListenPort)
	if err != nil {
		t.Fatalf("endpoint: %v", err)
	}
	if err := leechHandle.transfer.AddPeer(endpoint, int(PeerResume)); err != nil {
		t.Fatalf("add peer: %v", err)
	}

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		UpdateCachedTime()
		seedSession.SecondTick(CurrentTime(), 100)
		leechSession.SecondTick(CurrentTime(), 100)
		if leechHandle.IsFinished() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !leechHandle.IsFinished() {
		status := leechHandle.GetStatus()
		t.Fatalf("download incomplete: done=%d peers=%d rate=%d", status.TotalDone, status.NumPeers, status.DownloadRate)
	}
	data, err := os.ReadFile(downloadPath)
	if err != nil {
		t.Fatalf("read download: %v", err)
	}
	if !bytes.Equal(data, payload) {
		t.Fatal("payload mismatch")
	}
}

func TestEMuleInteropHarness(t *testing.T) {
	h := newEMuleInteropHarness(t)

	t.Run("local_upload_plain", func(t *testing.T) {
		payload := bytes.Repeat([]byte("eMule-interop-plain-"), 4096)
		h.runLocalUpload(t, NewSettings(), NewSettings(), payload)
	})

	t.Run("local_upload_crypt_layer", func(t *testing.T) {
		t.Skip("CryptLayer 本地双端上传联调尚未稳定，待后续补齐")
	})

	t.Run("hello_tag_parsing", func(t *testing.T) {
		var info RemotePeerInfo
		props := protocol.TagList{
			protocol.NewStringTag(0x01, "goed2k-peer"),
			protocol.NewStringTag(0x55, "eMule"),
			protocol.NewUInt32Tag(0x11, 0x3c),
		}
		parseHelloTagList(&info, &props)
		if info.NickName != "goed2k-peer" || info.ModName != "eMule" {
			t.Fatalf("hello tags: nick=%q mod=%q", info.NickName, info.ModName)
		}
	})

	t.Run("source_exchange_wire_roundtrip", func(t *testing.T) {
		st := NewSettings()
		s := NewSession(st)
		tf, err := NewTransfer(s, AddTransferParams{
			Hash:       protocol.MustHashFromString("31D6CFE0D16AE931B73C59D7E0C089C0"),
			CreateTime: 1,
			Size:       1000,
		})
		if err != nil {
			t.Fatal(err)
		}
		ep, err := protocol.EndpointFromString("192.0.2.10", 4662)
		if err != nil {
			t.Fatal(err)
		}
		uid := clientproto.SwapUint32(uint32(ep.IP()))
		ans := clientproto.AnswerSources2{
			Version: 4,
			Hash:    tf.GetHash(),
			Entries: []clientproto.SourceExchangeEntry{{
				UserID:  uid,
				TCPPort: uint16(ep.Port()),
			}},
		}
		var buf bytes.Buffer
		if err := ans.Put(&buf); err != nil {
			t.Fatal(err)
		}
		var decoded clientproto.AnswerSources2
		if err := decoded.Get(bytes.NewReader(buf.Bytes())); err != nil {
			t.Fatal(err)
		}
		if len(decoded.Entries) != 1 || decoded.Entries[0].TCPPort != uint16(ep.Port()) {
			t.Fatalf("decoded entries: %+v", decoded.Entries)
		}
	})
}

func TestSecIdentSignAndVerifyRoundTrip(t *testing.T) {
	alice, err := GenerateIdentityKeyPair(t.TempDir() + "/alice.pem")
	if err != nil {
		t.Fatalf("generate alice: %v", err)
	}
	bob, err := GenerateIdentityKeyPair(t.TempDir() + "/bob.pem")
	if err != nil {
		t.Fatalf("generate bob: %v", err)
	}
	const challenge uint32 = 0xAABBCCDD
	sig, err := alice.SignChallenge(bob.PublicKeyDER(), challenge)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if err := VerifySecIdentSignature(alice.PublicKeyDER(), bob.PublicKeyDER(), challenge, sig); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if err := VerifySecIdentSignature(alice.PublicKeyDER(), bob.PublicKeyDER(), challenge^1, sig); err == nil {
		t.Fatal("expected invalid signature for wrong challenge")
	}
}

func TestAICHRecoveryPipeline(t *testing.T) {
	good := bytes.Repeat([]byte{0xAB}, AICHBlockSize)
	bad := bytes.Repeat([]byte{0xCD}, AICHBlockSize)
	piece := append(append([]byte(nil), good...), bad...)
	hashes := BuildAICHBlockHashes(piece)
	corrupt := append(append([]byte(nil), good...), bytes.Repeat([]byte{0xFF}, AICHBlockSize)...)
	blocks := LocateCorruptAICHBlocks(corrupt, hashes)
	if len(blocks) != 1 || blocks[0] != 1 {
		t.Fatalf("expected corrupt block 1, got %v", blocks)
	}
	if !VerifyAICHBlock(good, hashes[0]) {
		t.Fatal("expected block 0 valid")
	}
}

func TestSharedFileSourceExchangeSelfAnswer(t *testing.T) {
	session := NewSession(NewSettings())
	session.settings.ListenPort = 4661
	session.clientID = int32(0x12345678)
	sf := &SharedFile{
		Hash:      protocol.EMule,
		FileSize:  PieceSize,
		Path:      "shared.bin",
		Completed: true,
	}
	session.sharedStore.Add(sf)

	ep, err := protocol.EndpointFromString("1.2.3.4", 4662)
	if err != nil {
		t.Fatal(err)
	}
	pc := &PeerConnection{
		Connection: NewConnection(session),
		endpoint:   ep,
		remotePeerInfo: RemotePeerInfo{
			Misc1: MiscOptions{SourceExchange1Ver: 3},
		},
		combiner: clientproto.NewPacketCombiner(),
	}
	pc.sendAnswerSources2SelfOnly(sf.Hash)
	if packets := pc.PendingPackets(); len(packets) == 0 {
		t.Fatal("expected queued AnswerSources2 packet")
	}
}

func TestCryptLayerLocalOptionsReflectSettings(t *testing.T) {
	st := NewSettings()
	opts := cryptOptionsForLocal(st)
	if opts&cryptOptionRequested != 0 {
		t.Fatal("crypt should not be requested by default")
	}
	st.EnableCryptLayer = true
	st.CryptLayerRequired = true
	opts = cryptOptionsForLocal(st)
	if opts&cryptOptionRequested == 0 || opts&cryptOptionRequired == 0 {
		t.Fatalf("expected requested+required bits, got %#x", opts)
	}
}
