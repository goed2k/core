package client

import (
	"bytes"
	"errors"

	"github.com/goed2k/core/protocol"
)

const MaxPreviewBytes = 256 * 1024

// RequestPreview：OP_EMULEPROT OP_REQUESTPREVIEW (0x90)。
// 载荷：16 字节 hash + uint16 预览分片索引（eMule 按分片请求预览数据）。
type RequestPreview struct {
	Hash       protocol.Hash
	PieceIndex uint16
}

func (r *RequestPreview) Get(src *bytes.Reader) error {
	h, err := protocol.ReadHash(src)
	if err != nil {
		return err
	}
	r.Hash = h
	idx, err := protocol.ReadUInt16(src)
	if err != nil {
		return err
	}
	r.PieceIndex = idx
	return nil
}

func (r RequestPreview) Put(dst *bytes.Buffer) error {
	if err := protocol.WriteHash(dst, r.Hash); err != nil {
		return err
	}
	return protocol.WriteUInt16(dst, r.PieceIndex)
}

func (r RequestPreview) BytesCount() int { return 16 + 2 }

// PreviewAnswer：OP_EMULEPROT OP_PREVIEWANSWER (0x91)。
// 载荷：16 字节 hash + uint16 分片索引 + uint32 数据长度 + 数据。
type PreviewAnswer struct {
	Hash       protocol.Hash
	PieceIndex uint16
	Data       []byte
}

func (p *PreviewAnswer) Get(src *bytes.Reader) error {
	h, err := protocol.ReadHash(src)
	if err != nil {
		return err
	}
	p.Hash = h
	idx, err := protocol.ReadUInt16(src)
	if err != nil {
		return err
	}
	p.PieceIndex = idx
	length, err := protocol.ReadUInt32(src)
	if err != nil {
		return err
	}
	if length > MaxPreviewBytes {
		return errors.New("preview answer: payload too large")
	}
	data, err := protocol.ReadBytes(src, int(length))
	if err != nil {
		return err
	}
	p.Data = data
	return nil
}

func (p PreviewAnswer) Put(dst *bytes.Buffer) error {
	if err := protocol.WriteHash(dst, p.Hash); err != nil {
		return err
	}
	if err := protocol.WriteUInt16(dst, p.PieceIndex); err != nil {
		return err
	}
	if err := protocol.WriteUInt32(dst, uint32(len(p.Data))); err != nil {
		return err
	}
	_, err := dst.Write(p.Data)
	return err
}

func (p PreviewAnswer) BytesCount() int {
	return 16 + 2 + 4 + len(p.Data)
}
