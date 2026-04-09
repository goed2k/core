package client

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/monkeyWie/goed2k/protocol"
)

// SourceExchange2 报文布局参照 aMule：
// - 请求：amule-project/amule src/DownloadClient.cpp（独立 EMule 包 OP_REQUESTSOURCES2）与 src/PartFile.cpp CreateSrcInfoPacket 发送侧一致。
// - 应答：amule-project/amule src/ClientTCPSocket.cpp case OP_ANSWERSOURCES2（先读版本字节与 hash，再交给 PartFile::AddClientSources）与 src/PartFile.cpp CreateSrcInfoPacket 写入侧。
// 整型均为 little-endian（CMemFile WriteUInt16/WriteUInt32）。

// SourceExchange2Version 为当前实现的 SX2 载荷版本（与 aMule SOURCEEXCHANGE2_VERSION 一致）。
const SourceExchange2Version byte = 4

// RequestSources2：OP_EMULEPROT OP_REQUESTSOURCES2 载荷。
// 顺序：uint8 版本（SourceExchange2Version）+ uint16 保留(0) + 16 字节文件 hash。
type RequestSources2 struct {
	Version  byte
	Reserved uint16
	Hash     protocol.Hash
}

func (r *RequestSources2) Get(src *bytes.Reader) error {
	v, err := src.ReadByte()
	if err != nil {
		return err
	}
	r.Version = v
	res, err := protocol.ReadUInt16(src)
	if err != nil {
		return err
	}
	r.Reserved = res
	h, err := protocol.ReadHash(src)
	if err != nil {
		return err
	}
	r.Hash = h
	return nil
}

func (r RequestSources2) Put(dst *bytes.Buffer) error {
	if err := dst.WriteByte(r.Version); err != nil {
		return err
	}
	if err := protocol.WriteUInt16(dst, r.Reserved); err != nil {
		return err
	}
	return protocol.WriteHash(dst, r.Hash)
}

func (r RequestSources2) BytesCount() int { return 0 }

// SourceExchangeEntry 表示 AnswerSources2 中单个来源（SX2 v4：含 UserHash 与 CryptOptions）。
type SourceExchangeEntry struct {
	UserID       uint32
	TCPPort      uint16
	ServerIP     uint32
	ServerPort   uint16
	UserHash     protocol.Hash
	CryptOptions uint8
}

// AnswerSources2：OP_EMULEPROT OP_ANSWERSOURCES2 载荷。
// 顺序：uint8 版本 + 16 字节 hash + uint16 来源数量 + 每条目（版本依赖，见 Get）。
type AnswerSources2 struct {
	Version byte
	Hash    protocol.Hash
	Entries []SourceExchangeEntry
}

func entrySizeForVersion(ver byte) (int, error) {
	switch ver {
	case 1:
		return 4 + 2 + 4 + 2, nil
	case 2, 3:
		return 4 + 2 + 4 + 2 + 16, nil
	case 4:
		return 4 + 2 + 4 + 2 + 16 + 1, nil
	default:
		return 0, fmt.Errorf("answer sources2: unsupported sx entry version %d", ver)
	}
}

func (a *AnswerSources2) Get(src *bytes.Reader) error {
	v, err := src.ReadByte()
	if err != nil {
		return err
	}
	a.Version = v
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
		return fmt.Errorf("answer sources2: excessive count %d", n)
	}
	esize, err := entrySizeForVersion(a.Version)
	if err != nil {
		return err
	}
	rest := src.Len()
	if rest != int(n)*esize {
		return fmt.Errorf("answer sources2: size mismatch count=%d ver=%d rest=%d need=%d", n, a.Version, rest, int(n)*esize)
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
		if a.Version > 1 {
			uh, err := protocol.ReadHash(src)
			if err != nil {
				return err
			}
			e.UserHash = uh
		}
		if a.Version >= 4 {
			c, err := src.ReadByte()
			if err != nil {
				return err
			}
			e.CryptOptions = c
		}
		a.Entries = append(a.Entries, e)
	}
	return nil
}

func (a AnswerSources2) Put(dst *bytes.Buffer) error {
	if err := dst.WriteByte(a.Version); err != nil {
		return err
	}
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
		if a.Version > 1 {
			if err := protocol.WriteHash(dst, e.UserHash); err != nil {
				return err
			}
		}
		if a.Version >= 4 {
			if err := dst.WriteByte(e.CryptOptions); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a AnswerSources2) BytesCount() int { return 0 }

// SwapUint32 与 aMule wxUINT32_SWAP_ALWAYS 一致，用于 UserID 混合编码与旧版 SX1 兼容。
func SwapUint32(x uint32) uint32 {
	return x>>24 | (x>>8)&0xff00 | (x<<8)&0xff0000 | x<<24
}
