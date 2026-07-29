package client

import (
	"bytes"
	"errors"

	"github.com/goed2k/core/protocol"
)

const (
	SecIdentMaxPubKeyLen = 512
	SecIdentMaxSigLen   = 512
)

type PublicKey struct {
	Key []byte
}

func (p *PublicKey) Get(src *bytes.Reader) error {
	length, err := protocol.ReadUInt16(src)
	if err != nil {
		return err
	}
	if length < 1 || int(length) > SecIdentMaxPubKeyLen {
		return errors.New("invalid secure ident public key length")
	}
	key, err := protocol.ReadBytes(src, int(length))
	if err != nil {
		return err
	}
	p.Key = key
	return nil
}

func (p PublicKey) Put(dst *bytes.Buffer) error {
	if len(p.Key) == 0 || len(p.Key) > SecIdentMaxPubKeyLen {
		return errors.New("invalid secure ident public key length")
	}
	if err := protocol.WriteUInt16(dst, uint16(len(p.Key))); err != nil {
		return err
	}
	_, err := dst.Write(p.Key)
	return err
}

func (p PublicKey) BytesCount() int {
	return 2 + len(p.Key)
}

type Signature struct {
	Signature []byte
}

func (s *Signature) Get(src *bytes.Reader) error {
	length, err := protocol.ReadUInt16(src)
	if err != nil {
		return err
	}
	if length < 1 || int(length) > SecIdentMaxSigLen {
		return errors.New("invalid secure ident signature length")
	}
	sig, err := protocol.ReadBytes(src, int(length))
	if err != nil {
		return err
	}
	s.Signature = sig
	return nil
}

func (s Signature) Put(dst *bytes.Buffer) error {
	if len(s.Signature) == 0 || len(s.Signature) > SecIdentMaxSigLen {
		return errors.New("invalid secure ident signature length")
	}
	if err := protocol.WriteUInt16(dst, uint16(len(s.Signature))); err != nil {
		return err
	}
	_, err := dst.Write(s.Signature)
	return err
}

func (s Signature) BytesCount() int {
	return 2 + len(s.Signature)
}

type SecIdentState struct {
	State     byte
	Challenge uint32
}

func (s *SecIdentState) Get(src *bytes.Reader) error {
	state, err := src.ReadByte()
	if err != nil {
		return err
	}
	challenge, err := protocol.ReadUInt32(src)
	if err != nil {
		return err
	}
	s.State = state
	s.Challenge = challenge
	return nil
}

func (s SecIdentState) Put(dst *bytes.Buffer) error {
	if err := dst.WriteByte(s.State); err != nil {
		return err
	}
	return protocol.WriteUInt32(dst, s.Challenge)
}

func (s SecIdentState) BytesCount() int {
	return 5
}
