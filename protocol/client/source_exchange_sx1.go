package client

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/goed2k/core/protocol"
)

// RequestSources（SX1）：OP_EMULEPROT OP_REQUESTSOURCES (0x81)，载荷为 16 字节文件 hash。
type RequestSources struct {
	Hash protocol.Hash
}

func (r *RequestSources) Get(src *bytes.Reader) error {
	h, err := protocol.ReadHash(src)
	if err != nil {
		return err
	}
	r.Hash = h
	return nil
}

func (r RequestSources) Put(dst *bytes.Buffer) error {
	return protocol.WriteHash(dst, r.Hash)
}

func (r RequestSources) BytesCount() int { return 16 }

// AnswerSources（SX1）：OP_EMULEPROT OP_ANSWERSOURCES (0x82)。
// 顺序：16 字节 hash + uint16 数量 + 每条目 12 字节（UserID、TCPPort、ServerIP、ServerPort）。
type AnswerSources struct {
	Hash    protocol.Hash
	Entries []SourceExchangeEntry
}

func (a *AnswerSources) Get(src *bytes.Reader) error {
	h, err := protocol.ReadHash(src)
	if err != nil {
		return err
	}
	a.Hash = h
	n, err := protocol.ReadUInt16(src)
	if err != nil {
		return err
	}
	if n > 500 {
		return fmt.Errorf("answer sources: excessive count %d", n)
	}
	rest := src.Len()
	if rest != int(n)*12 {
		return fmt.Errorf("answer sources: size mismatch count=%d rest=%d", n, rest)
	}
	a.Entries = make([]SourceExchangeEntry, 0, n)
	for i := 0; i < int(n); i++ {
		var e SourceExchangeEntry
		if err := binary.Read(src, binary.LittleEndian, &e.UserID); err != nil {
			return err
		}
		if err := binary.Read(src, binary.LittleEndian, &e.TCPPort); err != nil {
			return err
		}
		if err := binary.Read(src, binary.LittleEndian, &e.ServerIP); err != nil {
			return err
		}
		if err := binary.Read(src, binary.LittleEndian, &e.ServerPort); err != nil {
			return err
		}
		a.Entries = append(a.Entries, e)
	}
	return nil
}

func (a AnswerSources) Put(dst *bytes.Buffer) error {
	if err := protocol.WriteHash(dst, a.Hash); err != nil {
		return err
	}
	if err := protocol.WriteUInt16(dst, uint16(len(a.Entries))); err != nil {
		return err
	}
	for i := range a.Entries {
		e := &a.Entries[i]
		if err := binary.Write(dst, binary.LittleEndian, e.UserID); err != nil {
			return err
		}
		if err := binary.Write(dst, binary.LittleEndian, e.TCPPort); err != nil {
			return err
		}
		if err := binary.Write(dst, binary.LittleEndian, e.ServerIP); err != nil {
			return err
		}
		if err := binary.Write(dst, binary.LittleEndian, e.ServerPort); err != nil {
			return err
		}
	}
	return nil
}

func (a AnswerSources) BytesCount() int { return 0 }
