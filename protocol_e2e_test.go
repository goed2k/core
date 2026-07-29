package goed2k

import (
	"bytes"
	"testing"

	"github.com/goed2k/core/protocol"
	clientproto "github.com/goed2k/core/protocol/client"
)

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
