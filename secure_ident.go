package goed2k

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"

	"github.com/goed2k/core/protocol"
)

const (
	SecIdentWireVersion = 1

	SecIdentStateUnavailable      = 0
	SecIdentStateSignatureNeeded  = 1
	SecIdentStateKeyAndSigNeeded  = 2

	maxSecIdentPubKeyLen  = 512
	maxSecIdentSigLen     = 512
	minSecIdentPubKeyLen  = 10
	identityKeyBits       = 2048
	identityPEMBlockType  = "RSA PRIVATE KEY"
)

var (
	errSecIdentUnavailable = errors.New("secure ident unavailable")
	errSecIdentBadKey      = errors.New("invalid secure ident public key")
	errSecIdentBadSig      = errors.New("invalid secure ident signature")
)

// IdentityState holds the local RSA identity used for SecIdent.
type IdentityState struct {
	Version      int
	keyPath      string
	privateKey   *rsa.PrivateKey
	publicKeyDER []byte
	fingerprint  uint32
	userHash     protocol.Hash
}

func NewIdentityState() *IdentityState {
	return &IdentityState{Version: SecIdentWireVersion}
}

func (id *IdentityState) Available() bool {
	return id != nil && id.privateKey != nil && len(id.publicKeyDER) > 0
}

func (id *IdentityState) PublicKeyDER() []byte {
	if id == nil {
		return nil
	}
	out := make([]byte, len(id.publicKeyDER))
	copy(out, id.publicKeyDER)
	return out
}

func (id *IdentityState) Fingerprint() uint32 {
	if id == nil {
		return 0
	}
	return id.fingerprint
}

func (id *IdentityState) UserHash() protocol.Hash {
	if id == nil {
		return protocol.Invalid
	}
	return id.userHash
}

func (id *IdentityState) KeyPath() string {
	if id == nil {
		return ""
	}
	return id.keyPath
}

// GenerateIdentityKeyPair creates a new RSA 2048 key pair and writes the private key PEM (0600).
func GenerateIdentityKeyPair(path string) (*IdentityState, error) {
	if path == "" {
		return nil, errors.New("identity key path is empty")
	}
	key, err := rsa.GenerateKey(rand.Reader, identityKeyBits)
	if err != nil {
		return nil, err
	}
	if err := writeIdentityPrivateKeyPEM(path, key); err != nil {
		return nil, err
	}
	return loadIdentityFromKey(path, key)
}

// LoadIdentityState loads or creates the identity at path.
func LoadIdentityState(path string) (*IdentityState, error) {
	if path == "" {
		return nil, errors.New("identity key path is empty")
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return GenerateIdentityKeyPair(path)
	}
	return loadIdentityFromFile(path)
}

func loadIdentityFromFile(path string) (*IdentityState, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != identityPEMBlockType {
		return nil, errors.New("invalid identity PEM")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	return loadIdentityFromKey(path, key)
}

func loadIdentityFromKey(path string, key *rsa.PrivateKey) (*IdentityState, error) {
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return nil, err
	}
	userHash, err := UserHashFromPublicKey(pubDER)
	if err != nil {
		return nil, err
	}
	return &IdentityState{
		Version:      SecIdentWireVersion,
		keyPath:      path,
		privateKey:   key,
		publicKeyDER: pubDER,
		fingerprint:  PublicKeyFingerprint(pubDER),
		userHash:     userHash,
	}, nil
}

func writeIdentityPrivateKeyPEM(path string, key *rsa.PrivateKey) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	der := x509.MarshalPKCS1PrivateKey(key)
	block := &pem.Block{Type: identityPEMBlockType, Bytes: der}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := pem.Encode(f, block); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// PublicKeyFingerprint returns the first four bytes of SHA-1(pubDER) as little-endian uint32.
func PublicKeyFingerprint(pubDER []byte) uint32 {
	if len(pubDER) == 0 {
		return 0
	}
	sum := sha1.Sum(pubDER)
	return binary.LittleEndian.Uint32(sum[:4])
}

// UserHashFromPublicKey derives a 16-byte user hash from the public key (MD4 + eMule markers).
func UserHashFromPublicKey(pubDER []byte) (protocol.Hash, error) {
	hash, err := protocol.HashFromData(pubDER)
	if err != nil {
		return protocol.Invalid, err
	}
	hash.Set(5, 14)
	hash.Set(14, 111)
	return hash, nil
}

// LinkUserHash associates the identity with an existing protocol.Hash instead of deriving one.
func (id *IdentityState) LinkUserHash(hash protocol.Hash) {
	if id == nil || hash.Equal(protocol.Invalid) {
		return
	}
	id.userHash = hash
}

func buildSecIdentMessage(remotePubKey []byte, challenge uint32) []byte {
	msg := make([]byte, len(remotePubKey)+4)
	copy(msg, remotePubKey)
	binary.LittleEndian.PutUint32(msg[len(remotePubKey):], challenge)
	return msg
}

// SignChallenge signs remotePubKey||challenge with SHA-1 + PKCS1v15 (eMule SecIdent v1).
func (id *IdentityState) SignChallenge(remotePubKey []byte, challenge uint32) ([]byte, error) {
	if !id.Available() {
		return nil, errSecIdentUnavailable
	}
	if len(remotePubKey) < minSecIdentPubKeyLen || len(remotePubKey) > maxSecIdentPubKeyLen {
		return nil, errSecIdentBadKey
	}
	if challenge == 0 {
		return nil, errors.New("secure ident challenge is zero")
	}
	msg := buildSecIdentMessage(remotePubKey, challenge)
	digest := sha1.Sum(msg)
	sig, err := rsa.SignPKCS1v15(rand.Reader, id.privateKey, crypto.SHA1, digest[:])
	if err != nil {
		return nil, err
	}
	if len(sig) > maxSecIdentSigLen {
		return nil, errors.New("secure ident signature too large")
	}
	return sig, nil
}

// VerifySecIdentSignature verifies sender's signature over localPubKey||challenge.
func VerifySecIdentSignature(senderPubKey []byte, localPubKey []byte, challenge uint32, signature []byte) error {
	if len(senderPubKey) < minSecIdentPubKeyLen || len(senderPubKey) > maxSecIdentPubKeyLen {
		return errSecIdentBadKey
	}
	if len(localPubKey) < minSecIdentPubKeyLen || len(localPubKey) > maxSecIdentPubKeyLen {
		return errSecIdentBadKey
	}
	if challenge == 0 || len(signature) == 0 || len(signature) > maxSecIdentSigLen {
		return errSecIdentBadSig
	}
	pubAny, err := x509.ParsePKIXPublicKey(senderPubKey)
	if err != nil {
		return err
	}
	pub, ok := pubAny.(*rsa.PublicKey)
	if !ok {
		return errSecIdentBadKey
	}
	msg := buildSecIdentMessage(localPubKey, challenge)
	digest := sha1.Sum(msg)
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA1, digest[:], signature); err != nil {
		return errSecIdentBadSig
	}
	return nil
}
