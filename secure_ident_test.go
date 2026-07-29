package goed2k

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/goed2k/core/protocol"
	clientproto "github.com/goed2k/core/protocol/client"
)

func TestGenerateIdentityKeyPairAndLoad(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "secident.pem")
	id, err := GenerateIdentityKeyPair(path)
	if err != nil {
		t.Fatalf("GenerateIdentityKeyPair: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat key: %v", err)
	}
	if info.Mode().Perm() != 0o600 && runtime.GOOS != "windows" {
		t.Fatalf("expected key mode 0600, got %o", info.Mode().Perm())
	}
	if !id.Available() {
		t.Fatal("identity should be available")
	}
	if id.Fingerprint() == 0 {
		t.Fatal("expected non-zero fingerprint")
	}
	if id.UserHash().Equal(protocol.Invalid) {
		t.Fatal("expected derived user hash")
	}

	loaded, err := LoadIdentityState(path)
	if err != nil {
		t.Fatalf("LoadIdentityState: %v", err)
	}
	if loaded.Fingerprint() != id.Fingerprint() {
		t.Fatalf("fingerprint mismatch after reload")
	}
}

func TestSecIdentSignVerifyRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	alice, err := GenerateIdentityKeyPair(filepath.Join(dir, "alice.pem"))
	if err != nil {
		t.Fatalf("alice key: %v", err)
	}
	bob, err := GenerateIdentityKeyPair(filepath.Join(dir, "bob.pem"))
	if err != nil {
		t.Fatalf("bob key: %v", err)
	}
	var challengeBuf [4]byte
	if _, err := rand.Read(challengeBuf[:]); err != nil {
		t.Fatalf("rand: %v", err)
	}
	challenge := binary.LittleEndian.Uint32(challengeBuf[:])
	if challenge == 0 {
		challenge = 42
	}

	sig, err := alice.SignChallenge(bob.PublicKeyDER(), challenge)
	if err != nil {
		t.Fatalf("SignChallenge: %v", err)
	}
	if err := VerifySecIdentSignature(alice.PublicKeyDER(), bob.PublicKeyDER(), challenge, sig); err != nil {
		t.Fatalf("VerifySecIdentSignature valid sig: %v", err)
	}
	if err := VerifySecIdentSignature(alice.PublicKeyDER(), bob.PublicKeyDER(), challenge^1, sig); err == nil {
		t.Fatal("expected verification to fail for wrong challenge")
	}
}

func TestSecIdentProtocolRoundtrip(t *testing.T) {
	t.Parallel()
	state := clientproto.SecIdentState{State: SecIdentStateKeyAndSigNeeded, Challenge: 0xAABBCCDD}
	var stateBuf bytes.Buffer
	if err := state.Put(&stateBuf); err != nil {
		t.Fatalf("put SecIdentState: %v", err)
	}
	var decodedState clientproto.SecIdentState
	if err := decodedState.Get(bytes.NewReader(stateBuf.Bytes())); err != nil {
		t.Fatalf("get SecIdentState: %v", err)
	}
	if decodedState.State != state.State || decodedState.Challenge != state.Challenge {
		t.Fatalf("decoded %+v != %+v", decodedState, state)
	}

	id, err := GenerateIdentityKeyPair(filepath.Join(t.TempDir(), "key.pem"))
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	pub := clientproto.PublicKey{Key: id.PublicKeyDER()}
	var pubBuf bytes.Buffer
	if err := pub.Put(&pubBuf); err != nil {
		t.Fatalf("put PublicKey: %v", err)
	}
	var pubDecoded clientproto.PublicKey
	if err := pubDecoded.Get(bytes.NewReader(pubBuf.Bytes())); err != nil {
		t.Fatalf("get PublicKey: %v", err)
	}
	if len(pubDecoded.Key) != len(pub.Key) {
		t.Fatalf("public key roundtrip failed")
	}

	sigBytes, err := id.SignChallenge(pub.Key, state.Challenge)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	sig := clientproto.Signature{Signature: sigBytes}
	var sigBuf bytes.Buffer
	if err := sig.Put(&sigBuf); err != nil {
		t.Fatalf("put Signature: %v", err)
	}
	var sigDecoded clientproto.Signature
	if err := sigDecoded.Get(bytes.NewReader(sigBuf.Bytes())); err != nil {
		t.Fatalf("get Signature: %v", err)
	}
	if len(sigDecoded.Signature) != len(sig.Signature) {
		t.Fatalf("signature roundtrip failed")
	}
}

func TestUserHashFromPublicKeyMarkers(t *testing.T) {
	t.Parallel()
	id, err := GenerateIdentityKeyPair(filepath.Join(t.TempDir(), "key.pem"))
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	hash := id.UserHash()
	if hash.At(5) != 14 || hash.At(14) != 111 {
		t.Fatalf("expected eMule user hash markers, got %02X %02X", hash.At(5), hash.At(14))
	}
}
