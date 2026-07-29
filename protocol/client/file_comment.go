package client

import (
	"bytes"

	"github.com/goed2k/core/protocol"
)

const MaxFileCommentLen = 128

// FileComment carries a peer file rating and comment (OP_FILEDESC / 0x61).
type FileComment struct {
	Rating  byte
	Comment []byte
}

func (f *FileComment) Get(src *bytes.Reader) error {
	rating, err := src.ReadByte()
	if err != nil {
		return err
	}
	f.Rating = rating
	length, err := protocol.ReadUInt32(src)
	if err != nil {
		return err
	}
	if length > MaxFileCommentLen*4 {
		length = MaxFileCommentLen * 4
	}
	if length == 0 {
		f.Comment = nil
		return nil
	}
	value, err := protocol.ReadBytes(src, int(length))
	if err != nil {
		return err
	}
	f.Comment = value
	return nil
}

func (f FileComment) Put(dst *bytes.Buffer) error {
	if err := dst.WriteByte(f.Rating); err != nil {
		return err
	}
	comment := f.Comment
	if len(comment) > MaxFileCommentLen {
		comment = comment[:MaxFileCommentLen]
	}
	if err := protocol.WriteUInt32(dst, uint32(len(comment))); err != nil {
		return err
	}
	if len(comment) == 0 {
		return nil
	}
	_, err := dst.Write(comment)
	return err
}

func (f FileComment) BytesCount() int {
	commentLen := len(f.Comment)
	if commentLen > MaxFileCommentLen {
		commentLen = MaxFileCommentLen
	}
	return 1 + 4 + commentLen
}
