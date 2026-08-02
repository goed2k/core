package server

import (
	"bytes"
	"fmt"

	"github.com/goed2k/core/protocol"
)

const CryptOptionObfuUserHash = 0x80

// ObfuFileSource 表示 OP_FOUNDSOURCES_OBFU (0x44) 中的单个来源。
type ObfuFileSource struct {
	Endpoint     protocol.Endpoint
	CryptOptions uint8
	UserHash     protocol.Hash
}

// FoundFileSourcesObfu：server -> client，混淆来源列表。
// 布局：16 字节 hash + 1 字节数量 + 每条目（4 字节 ID + 2 字节端口 + 1 字节混淆设置 [+ 16 字节 UserHash 若 settings&0x80]）。
type FoundFileSourcesObfu struct {
	Hash    protocol.Hash
	Sources []ObfuFileSource
}

func (f *FoundFileSourcesObfu) Get(src *bytes.Reader) error {
	hash, err := protocol.ReadHash(src)
	if err != nil {
		return err
	}
	f.Hash = hash
	countByte, err := src.ReadByte()
	if err != nil {
		return err
	}
	count := int(countByte)
	if count > 500 {
		return fmt.Errorf("found sources obfu: excessive count %d", count)
	}
	f.Sources = make([]ObfuFileSource, 0, count)
	for i := 0; i < count; i++ {
		ep, err := protocol.ReadEndpoint(src)
		if err != nil {
			return err
		}
		settings, err := src.ReadByte()
		if err != nil {
			return err
		}
		entry := ObfuFileSource{
			Endpoint:     ep,
			CryptOptions: settings,
		}
		if settings&CryptOptionObfuUserHash != 0 {
			uh, err := protocol.ReadHash(src)
			if err != nil {
				return err
			}
			entry.UserHash = uh
		}
		f.Sources = append(f.Sources, entry)
	}
	return nil
}

func (f FoundFileSourcesObfu) Put(dst *bytes.Buffer) error {
	if err := protocol.WriteHash(dst, f.Hash); err != nil {
		return err
	}
	if err := dst.WriteByte(byte(len(f.Sources))); err != nil {
		return err
	}
	for _, s := range f.Sources {
		if err := protocol.WriteEndpoint(dst, s.Endpoint); err != nil {
			return err
		}
		if err := dst.WriteByte(s.CryptOptions); err != nil {
			return err
		}
		if s.CryptOptions&CryptOptionObfuUserHash != 0 {
			if err := protocol.WriteHash(dst, s.UserHash); err != nil {
				return err
			}
		}
	}
	return nil
}

func (f FoundFileSourcesObfu) BytesCount() int { return 0 }

// GetFileSourcesObfu 与 GetFileSources 载荷相同，仅操作码为 OP_GETSOURCES_OBFU (0x23)。
type GetFileSourcesObfu GetFileSources

func (g *GetFileSourcesObfu) Get(src *bytes.Reader) error {
	return (*GetFileSources)(g).Get(src)
}

func (g GetFileSourcesObfu) Put(dst *bytes.Buffer) error {
	return (GetFileSources)(g).Put(dst)
}

func (g GetFileSourcesObfu) BytesCount() int {
	return (GetFileSources)(g).BytesCount()
}
