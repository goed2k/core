package client

import (
	"bytes"

	"github.com/goed2k/core/protocol"
)

type AICHRequest struct {
	Hash   protocol.Hash
	Hashes []protocol.AICHHash
}

func (a *AICHRequest) Get(src *bytes.Reader) error {
	hash, err := protocol.ReadHash(src)
	if err != nil {
		return err
	}
	a.Hash = hash
	count, err := protocol.ReadUInt16(src)
	if err != nil {
		return err
	}
	a.Hashes = make([]protocol.AICHHash, int(count))
	for i := range a.Hashes {
		h, err := protocol.ReadAICHHash(src)
		if err != nil {
			return err
		}
		a.Hashes[i] = h
	}
	return nil
}

func (a AICHRequest) Put(dst *bytes.Buffer) error {
	if err := protocol.WriteHash(dst, a.Hash); err != nil {
		return err
	}
	if err := protocol.WriteUInt16(dst, uint16(len(a.Hashes))); err != nil {
		return err
	}
	for _, h := range a.Hashes {
		if err := protocol.WriteAICHHash(dst, h); err != nil {
			return err
		}
	}
	return nil
}

func (a AICHRequest) BytesCount() int {
	return 16 + 2 + len(a.Hashes)*protocol.AICHHashSize
}

type AICHAnswer struct {
	Hash   protocol.Hash
	Hashes []protocol.AICHHash
	Data   []byte
}

func (a *AICHAnswer) Get(src *bytes.Reader) error {
	hash, err := protocol.ReadHash(src)
	if err != nil {
		return err
	}
	a.Hash = hash
	count, err := protocol.ReadUInt16(src)
	if err != nil {
		return err
	}
	a.Hashes = make([]protocol.AICHHash, int(count))
	for i := range a.Hashes {
		h, err := protocol.ReadAICHHash(src)
		if err != nil {
			return err
		}
		a.Hashes[i] = h
	}
	rest, err := protocol.ReadBytes(src, src.Len())
	if err != nil {
		return err
	}
	a.Data = rest
	return nil
}

func (a AICHAnswer) Put(dst *bytes.Buffer) error {
	if err := protocol.WriteHash(dst, a.Hash); err != nil {
		return err
	}
	if err := protocol.WriteUInt16(dst, uint16(len(a.Hashes))); err != nil {
		return err
	}
	for _, h := range a.Hashes {
		if err := protocol.WriteAICHHash(dst, h); err != nil {
			return err
		}
	}
	if len(a.Data) > 0 {
		_, err := dst.Write(a.Data)
		return err
	}
	return nil
}

func (a AICHAnswer) BytesCount() int {
	return 16 + 2 + len(a.Hashes)*protocol.AICHHashSize + len(a.Data)
}

// AICHFileHashRequest asks for the file-level AICH root hash (deprecated in eMule).
type AICHFileHashRequest struct {
	Hash protocol.Hash
}

func (a *AICHFileHashRequest) Get(src *bytes.Reader) error {
	hash, err := protocol.ReadHash(src)
	if err != nil {
		return err
	}
	a.Hash = hash
	return nil
}

func (a AICHFileHashRequest) Put(dst *bytes.Buffer) error {
	return protocol.WriteHash(dst, a.Hash)
}

func (a AICHFileHashRequest) BytesCount() int {
	return 16
}

// AICHFileHashAnswer returns the file-level AICH root hash (deprecated in eMule).
type AICHFileHashAnswer struct {
	Hash     protocol.Hash
	RootHash protocol.AICHHash
}

func (a *AICHFileHashAnswer) Get(src *bytes.Reader) error {
	hash, err := protocol.ReadHash(src)
	if err != nil {
		return err
	}
	a.Hash = hash
	root, err := protocol.ReadAICHHash(src)
	if err != nil {
		return err
	}
	a.RootHash = root
	return nil
}

func (a AICHFileHashAnswer) Put(dst *bytes.Buffer) error {
	if err := protocol.WriteHash(dst, a.Hash); err != nil {
		return err
	}
	return protocol.WriteAICHHash(dst, a.RootHash)
}

func (a AICHFileHashAnswer) BytesCount() int {
	return 16 + protocol.AICHHashSize
}
