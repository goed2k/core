package client

import (
	"bytes"
	"compress/zlib"
	"io"

	"github.com/goed2k/core/protocol"
)

// PackMultiPacketExt2 将多条已编码的 eMule TCP 帧 zlib 压缩后以 OP_MULTIPACKET_EXT2 (0xA9) 发出。
func PackMultiPacketExt2(frames [][]byte) ([]byte, error) {
	if len(frames) == 0 {
		return nil, nil
	}
	if len(frames) == 1 {
		return frames[0], nil
	}
	var plain bytes.Buffer
	for _, f := range frames {
		if _, err := plain.Write(f); err != nil {
			return nil, err
		}
	}
	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	if _, err := w.Write(plain.Bytes()); err != nil {
		_ = w.Close()
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	body := compressed.Bytes()
	header := protocol.PacketHeader{
		Protocol: protocol.PackedProt,
		Size:     int32(len(body) + 1),
		Packet:   opMultiPacketExt2,
	}
	out := bytes.NewBuffer(make([]byte, 0, header.BytesCount()+len(body)))
	if err := header.Put(out); err != nil {
		return nil, err
	}
	if _, err := out.Write(body); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// UnpackMultiPacketExt2 解压 MultiPacket 载荷，返回内部原始 TCP 帧列表。
func UnpackMultiPacketExt2(payload []byte) ([][]byte, error) {
	reader, err := zlib.NewReader(bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	raw, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	var frames [][]byte
	for len(raw) >= protocol.PacketHeaderSize {
		sizeField := int32(raw[1]) | int32(raw[2])<<8 | int32(raw[3])<<16 | int32(raw[4])<<24
		frameLen := 5 + int(sizeField)
		if frameLen > len(raw) || frameLen < protocol.PacketHeaderSize {
			break
		}
		frames = append(frames, append([]byte(nil), raw[:frameLen]...))
		raw = raw[frameLen:]
	}
	if len(frames) == 0 {
		return nil, nil
	}
	return frames, nil
}
