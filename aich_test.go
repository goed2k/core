package goed2k

import (
	"bytes"
	"testing"

	"github.com/goed2k/core/protocol"
	clientproto "github.com/goed2k/core/protocol/client"
)

func TestComputeAICHHashKnownVector(t *testing.T) {
	t.Parallel()
	// SHA-1("abc") = a9993e36 4706816a ba3e2571 7850c26c 9cd0d89d
	data := []byte("abc")
	got := ComputeAICHHash(data)
	want, err := protocol.AICHHashFromString("A9993E364706816ABA3E25717850C26C9CD0D89D")
	if err != nil {
		t.Fatalf("parse expected hash: %v", err)
	}
	if !got.Equal(want) {
		t.Fatalf("ComputeAICHHash() = %s, want %s", got.String(), want.String())
	}
}

func TestVerifyAICHBlock(t *testing.T) {
	t.Parallel()
	data := bytes.Repeat([]byte{0x5A}, AICHBlockSize)
	hash := ComputeAICHHash(data)
	if !VerifyAICHBlock(data, hash) {
		t.Fatal("expected valid AICH block")
	}
	corrupt := append([]byte(nil), data...)
	corrupt[0] ^= 0xFF
	if VerifyAICHBlock(corrupt, hash) {
		t.Fatal("expected corrupt AICH block to fail verification")
	}
}

func TestBuildAICHBlockHashesAndTree(t *testing.T) {
	t.Parallel()
	blockA := bytes.Repeat([]byte{'A'}, AICHBlockSize)
	blockB := bytes.Repeat([]byte{'B'}, AICHBlockSize/2)
	leaves := BuildAICHBlockHashes(append(blockA, blockB...))
	if len(leaves) != 2 {
		t.Fatalf("expected 2 block hashes, got %d", len(leaves))
	}
	root := BuildAICHTreeRoot(leaves)
	combined := protocol.CombineAICHHashes(leaves[0], leaves[1])
	if !root.Equal(combined) {
		t.Fatalf("tree root %s != combined %s", root.String(), combined.String())
	}
}

func TestBuildAICHRootFromDataSmallFile(t *testing.T) {
	t.Parallel()
	data := []byte("small-file-payload")
	root := BuildAICHRootFromData(data)
	want := ComputeAICHHash(data)
	if !root.Equal(want) {
		t.Fatalf("small file root %s != block hash %s", root.String(), want.String())
	}
}

func TestLocateCorruptAICHBlocks(t *testing.T) {
	t.Parallel()
	good := bytes.Repeat([]byte{1}, AICHBlockSize)
	bad := bytes.Repeat([]byte{2}, AICHBlockSize)
	pieceData := append(append([]byte(nil), good...), bad...)
	hashes := BuildAICHBlockHashes(pieceData)
	corrupt := append(append([]byte(nil), good...), bytes.Repeat([]byte{9}, AICHBlockSize)...)
	got := LocateCorruptAICHBlocks(corrupt, hashes)
	if len(got) != 1 || got[0] != 1 {
		t.Fatalf("expected corrupt block index 1, got %v", got)
	}
}

func TestParseEMuleLinkWithAICHRoot(t *testing.T) {
	t.Parallel()
	root, err := protocol.AICHHashFromString("A9993E364706816ABA3E25717850C26C9CD0D89D")
	if err != nil {
		t.Fatalf("parse root: %v", err)
	}
	link, err := ParseEMuleLink("ed2k://|file|demo.bin|1024|31D6CFE0D16AE931B73C59D7E0C089C0|h=" + root.Base32() + "|/")
	if err != nil {
		t.Fatalf("parse link: %v", err)
	}
	if !link.AICHRootHash.Equal(root) {
		t.Fatalf("AICH root %s != %s", link.AICHRootHash.String(), root.String())
	}
}

func TestAICHProtocolRoundtrip(t *testing.T) {
	t.Parallel()
	root := ComputeAICHHash([]byte("payload"))
	req := clientproto.AICHRequest{
		Hash:   protocol.EMule,
		Hashes: []protocol.AICHHash{root},
	}
	var reqBuf bytes.Buffer
	if err := req.Put(&reqBuf); err != nil {
		t.Fatalf("put request: %v", err)
	}
	var decoded clientproto.AICHRequest
	if err := decoded.Get(bytes.NewReader(reqBuf.Bytes())); err != nil {
		t.Fatalf("get request: %v", err)
	}
	if len(decoded.Hashes) != 1 || !decoded.Hashes[0].Equal(root) {
		t.Fatalf("decoded request hashes mismatch: %+v", decoded.Hashes)
	}

	ans := clientproto.AICHAnswer{
		Hash:   protocol.EMule,
		Hashes: []protocol.AICHHash{root},
	}
	var ansBuf bytes.Buffer
	if err := ans.Put(&ansBuf); err != nil {
		t.Fatalf("put answer: %v", err)
	}
	var answer clientproto.AICHAnswer
	if err := answer.Get(bytes.NewReader(ansBuf.Bytes())); err != nil {
		t.Fatalf("get answer: %v", err)
	}
	if len(answer.Hashes) != 1 || !answer.Hashes[0].Equal(root) {
		t.Fatalf("decoded answer hashes mismatch: %+v", answer.Hashes)
	}
}

func TestAICHRequestMarkerRoundtrip(t *testing.T) {
	t.Parallel()
	markers := make([]protocol.AICHHash, 3)
	for i := range markers {
		markers[i] = AICHRequestMarker(2, i)
	}
	piece, count, ok := isAICHMarkerRequest(markers)
	if !ok || piece != 2 || count != 3 {
		t.Fatalf("marker decode failed: piece=%d count=%d ok=%v", piece, count, ok)
	}
}

func TestHelloDeclaresAICHVersion(t *testing.T) {
	t.Parallel()
	session := NewSession(NewSettings())
	pc := NewPeerConnection(session, protocol.Endpoint{}, nil, nil)
	hello := pc.PrepareHelloAnswer()
	var misc MiscOptions
	for _, tag := range hello.Properties {
		if tag.ID == helloTagMisc1 && tag.Type == protocol.TagTypeUint32 {
			misc.Assign(int(tag.UInt32))
		}
	}
	if misc.AICHVersion == 0 {
		t.Fatal("expected Hello to advertise AICH support")
	}
}
