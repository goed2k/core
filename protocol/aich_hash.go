package protocol

import (
	"bytes"
	"crypto/sha1"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"strings"
)

const AICHHashSize = sha1.Size

// AICHHash is a 20-byte SHA-1 digest used by eMule AICH (block / tree hashes).
type AICHHash [AICHHashSize]byte

var InvalidAICH AICHHash

func AICHHashFromBytes(raw []byte) (AICHHash, error) {
	if len(raw) != AICHHashSize {
		return InvalidAICH, errors.New("illegal aich hash length")
	}
	var h AICHHash
	copy(h[:], raw)
	return h, nil
}

func AICHHashFromSHA1(data []byte) AICHHash {
	return AICHHash(sha1.Sum(data))
}

func AICHHashFromString(value string) (AICHHash, error) {
	value = strings.TrimSpace(value)
	if len(value) == AICHHashSize*2 {
		raw, err := hex.DecodeString(value)
		if err != nil {
			return InvalidAICH, err
		}
		return AICHHashFromBytes(raw)
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding)
	raw, err := enc.DecodeString(strings.ToUpper(value))
	if err != nil {
		return InvalidAICH, err
	}
	return AICHHashFromBytes(raw)
}

func (h AICHHash) Equal(other AICHHash) bool {
	return h == other
}

func (h AICHHash) IsZero() bool {
	return h == InvalidAICH
}

func (h AICHHash) Bytes() []byte {
	out := make([]byte, AICHHashSize)
	copy(out, h[:])
	return out
}

func (h AICHHash) String() string {
	return strings.ToUpper(hex.EncodeToString(h[:]))
}

func (h AICHHash) Base32() string {
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(h[:])
}

func ReadAICHHash(src *bytes.Reader) (AICHHash, error) {
	raw, err := readBytes(src, AICHHashSize)
	if err != nil {
		return InvalidAICH, err
	}
	return AICHHashFromBytes(raw)
}

func WriteAICHHash(dst *bytes.Buffer, hash AICHHash) error {
	_, err := dst.Write(hash[:])
	return err
}

func CombineAICHHashes(left, right AICHHash) AICHHash {
	buf := make([]byte, 0, AICHHashSize*2)
	buf = append(buf, left[:]...)
	buf = append(buf, right[:]...)
	return AICHHashFromSHA1(buf)
}
