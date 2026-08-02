package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	PartFileVersion          byte = 0xE0
	PartFileSplittedVersion  byte = 0xE1
	PartFileLargeFileVersion byte = 0xE2

	FTTransferred  byte = 0x08
	FTGapStart     byte = 0x09
	FTGapEnd       byte = 0x0A
	FTPartFilename byte = 0x12
	FTStatus       byte = 0x14
	FTDLPriority   byte = 0x18

	emuleTagTypeHash      byte = 0x01
	emuleTagTypeString    byte = 0x02
	emuleTagTypeUint32    byte = 0x03
	emuleTagTypeBool      byte = 0x05
	emuleTagTypeBoolArray byte = 0x06
	emuleTagTypeBlob      byte = 0x07
	emuleTagTypeUint16    byte = 0x08
	emuleTagTypeUint8     byte = 0x09
	emuleTagTypeUint64    byte = 0x0B
	emuleTagTypeStr1      byte = 0x11
)

// EmuleMetTag 表示 eMule/aMule .part.met 中的元数据标签。
type EmuleMetTag struct {
	Type     byte
	NameID   byte
	Name     string
	Hash     Hash
	String   string
	UInt32   uint32
	UInt64   uint64
	Bool     bool
	BoolBits []bool
	Blob     []byte
}

// PartMetFile 为 eMule 二进制 .part.met 解析结果。
type PartMetFile struct {
	Version     byte
	Modified    uint32
	Hash        Hash
	PieceHashes []Hash
	Tags        []EmuleMetTag
}

func readEmuleMetTag(src *bytes.Reader) (EmuleMetTag, error) {
	var tag EmuleMetTag
	typ, err := src.ReadByte()
	if err != nil {
		return tag, err
	}
	tag.Type = typ
	nameLen, err := ReadUInt16(src)
	if err != nil {
		return tag, err
	}
	switch nameLen {
	case 0:
		return tag, errors.New("emule met tag: empty name")
	case 1:
		id, err := src.ReadByte()
		if err != nil {
			return tag, err
		}
		tag.NameID = id
	default:
		raw, err := ReadBytes(src, int(nameLen))
		if err != nil {
			return tag, err
		}
		if len(raw) == 1 {
			tag.NameID = raw[0]
		} else {
			tag.Name = string(raw)
		}
	}
	switch tag.Type {
	case emuleTagTypeHash:
		h, err := ReadHash(src)
		if err != nil {
			return tag, err
		}
		tag.Hash = h
	case emuleTagTypeString:
		size, err := ReadUInt16(src)
		if err != nil {
			return tag, err
		}
		raw, err := ReadBytes(src, int(size))
		if err != nil {
			return tag, err
		}
		tag.String = string(raw)
	case emuleTagTypeUint32:
		v, err := ReadUInt32(src)
		if err != nil {
			return tag, err
		}
		tag.UInt32 = v
		tag.UInt64 = uint64(v)
	case emuleTagTypeUint16:
		v, err := ReadUInt16(src)
		if err != nil {
			return tag, err
		}
		tag.UInt32 = uint32(v)
		tag.UInt64 = uint64(v)
	case emuleTagTypeUint8:
		v, err := src.ReadByte()
		if err != nil {
			return tag, err
		}
		tag.UInt32 = uint32(v)
		tag.UInt64 = uint64(v)
	case emuleTagTypeUint64:
		v, err := ReadUInt64(src)
		if err != nil {
			return tag, err
		}
		tag.UInt64 = v
		tag.UInt32 = uint32(v)
	case emuleTagTypeBool:
		v, err := src.ReadByte()
		if err != nil {
			return tag, err
		}
		tag.Bool = v != 0
	case emuleTagTypeBoolArray:
		bitCount, err := ReadUInt16(src)
		if err != nil {
			return tag, err
		}
		byteCount := int(bitCount)/8 + 1
		raw, err := ReadBytes(src, byteCount)
		if err != nil {
			return tag, err
		}
		tag.BoolBits = make([]bool, bitCount)
		for i := 0; i < int(bitCount); i++ {
			tag.BoolBits[i] = raw[i/8]&(1<<(uint(i)%8)) != 0
		}
	case emuleTagTypeBlob:
		size, err := ReadUInt32(src)
		if err != nil {
			return tag, err
		}
		tag.Blob, err = ReadBytes(src, int(size))
		if err != nil {
			return tag, err
		}
	default:
		if tag.Type >= emuleTagTypeStr1 && tag.Type <= emuleTagTypeStr1+15 {
			size := int(tag.Type-emuleTagTypeStr1) + 1
			raw, err := ReadBytes(src, size)
			if err != nil {
				return tag, err
			}
			tag.String = string(raw)
		} else {
			return tag, fmt.Errorf("emule met tag: unsupported type 0x%02x", tag.Type)
		}
	}
	return tag, nil
}

func writeEmuleMetTag(dst *bytes.Buffer, tag EmuleMetTag) error {
	if err := dst.WriteByte(tag.Type); err != nil {
		return err
	}
	if tag.Name != "" {
		if err := WriteUInt16(dst, uint16(len(tag.Name))); err != nil {
			return err
		}
		if _, err := dst.WriteString(tag.Name); err != nil {
			return err
		}
	} else {
		if err := WriteUInt16(dst, 1); err != nil {
			return err
		}
		if err := dst.WriteByte(tag.NameID); err != nil {
			return err
		}
	}
	switch tag.Type {
	case emuleTagTypeHash:
		return WriteHash(dst, tag.Hash)
	case emuleTagTypeString:
		if err := WriteUInt16(dst, uint16(len(tag.String))); err != nil {
			return err
		}
		_, err := dst.WriteString(tag.String)
		return err
	case emuleTagTypeUint32:
		return WriteUInt32(dst, tag.UInt32)
	case emuleTagTypeUint16:
		return WriteUInt16(dst, uint16(tag.UInt64))
	case emuleTagTypeUint8:
		return dst.WriteByte(byte(tag.UInt64))
	case emuleTagTypeUint64:
		return WriteUInt64(dst, tag.UInt64)
	case emuleTagTypeBool:
		if tag.Bool {
			return dst.WriteByte(1)
		}
		return dst.WriteByte(0)
	default:
		if tag.Type >= emuleTagTypeStr1 && tag.Type <= emuleTagTypeStr1+15 {
			_, err := dst.WriteString(tag.String)
			return err
		}
		return fmt.Errorf("emule met tag: unsupported write type 0x%02x", tag.Type)
	}
}

// ParseEmulePartMet 解析 eMule/aMule 二进制 .part.met。
func ParseEmulePartMet(raw []byte) (*PartMetFile, error) {
	if len(raw) < 23 {
		return nil, errors.New("part.met: file too short")
	}
	r := bytes.NewReader(raw)
	version, err := r.ReadByte()
	if err != nil {
		return nil, err
	}
	switch version {
	case PartFileVersion, PartFileSplittedVersion, PartFileLargeFileVersion:
	default:
		return nil, fmt.Errorf("part.met: unsupported version 0x%02x", version)
	}
	out := &PartMetFile{Version: version}
	if err := binary.Read(r, binary.LittleEndian, &out.Modified); err != nil {
		return nil, err
	}
	h, err := ReadHash(r)
	if err != nil {
		return nil, err
	}
	out.Hash = h
	count, err := ReadUInt16(r)
	if err != nil {
		return nil, err
	}
	if count > 20000 {
		return nil, fmt.Errorf("part.met: excessive piece hash count %d", count)
	}
	out.PieceHashes = make([]Hash, count)
	for i := 0; i < int(count); i++ {
		ph, err := ReadHash(r)
		if err != nil {
			return nil, err
		}
		out.PieceHashes[i] = ph
	}
	tagCount, err := ReadUInt32(r)
	if err != nil {
		return nil, err
	}
	if tagCount > 10000 {
		return nil, fmt.Errorf("part.met: excessive tag count %d", tagCount)
	}
	rest := bytes.NewReader(raw[len(raw)-r.Len():])
	for i := uint32(0); i < tagCount; i++ {
		tag, err := readEmuleMetTag(rest)
		if err != nil {
			return nil, fmt.Errorf("part.met tag %d: %w", i, err)
		}
		out.Tags = append(out.Tags, tag)
	}
	if rest.Len() != 0 {
		return nil, fmt.Errorf("part.met: %d trailing bytes", rest.Len())
	}
	return out, nil
}

// LoadEmulePartMet 从路径加载 eMule 二进制 .part.met。
func LoadEmulePartMet(path string) (*PartMetFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseEmulePartMet(raw)
}

// PartMetGap 表示缺失字节区间 [Start, End)（End 为开区间，与 eMule gap end tag 一致）。
type PartMetGap struct {
	Start uint64
	End   uint64
}

// GapsFromEmulePartMet 从 gap 标签提取缺失区间。
func GapsFromEmulePartMet(met *PartMetFile) ([]PartMetGap, error) {
	if met == nil {
		return nil, errors.New("nil part.met")
	}
	type gapPair struct {
		start uint64
		end   uint64
	}
	pairs := make(map[int]*gapPair)
	for _, tag := range met.Tags {
		if tag.Name == "" || len(tag.Name) < 2 {
			continue
		}
		if tag.Name[0] != FTGapStart && tag.Name[0] != FTGapEnd {
			continue
		}
		if tag.Type != emuleTagTypeUint32 && tag.Type != emuleTagTypeUint64 {
			continue
		}
		idx, err := strconv.Atoi(tag.Name[1:])
		if err != nil {
			continue
		}
		p, ok := pairs[idx]
		if !ok {
			p = &gapPair{start: ^uint64(0), end: ^uint64(0)}
			pairs[idx] = p
		}
		val := tag.UInt64
		if tag.Name[0] == FTGapStart {
			p.start = val
		} else {
			if val == 0 {
				continue
			}
			p.end = val
		}
	}
	out := make([]PartMetGap, 0, len(pairs))
	for _, p := range pairs {
		if p.start == ^uint64(0) || p.end == ^uint64(0) || p.end <= p.start {
			continue
		}
		out = append(out, PartMetGap{Start: p.start, End: p.end})
	}
	return out, nil
}

// FileSizeFromEmulePartMet 从标签读取文件大小。
func FileSizeFromEmulePartMet(met *PartMetFile) (int64, error) {
	if met == nil {
		return 0, errors.New("nil part.met")
	}
	var size uint64
	for _, tag := range met.Tags {
		switch tag.NameID {
		case FTFileSize:
			if tag.Type == emuleTagTypeUint64 {
				size = tag.UInt64
			} else {
				size = uint64(tag.UInt32)
			}
		case FTFileSizeHi:
			if tag.Type == emuleTagTypeUint32 {
				size |= uint64(tag.UInt32) << 32
			}
		}
	}
	if size == 0 {
		return 0, errors.New("part.met: file size tag missing")
	}
	return int64(size), nil
}

// FilenameFromEmulePartMet 返回文件名标签。
func FilenameFromEmulePartMet(met *PartMetFile) string {
	if met == nil {
		return ""
	}
	for _, tag := range met.Tags {
		if tag.NameID == FTFilename && tag.String != "" {
			return tag.String
		}
	}
	return ""
}

// BuildEmulePartMet 构造 eMule 二进制 .part.met。
func BuildEmulePartMet(opts EmulePartMetOptions) ([]byte, error) {
	if opts.Hash.IsZero() {
		return nil, errors.New("part.met: hash required")
	}
	if opts.FileSize <= 0 {
		return nil, errors.New("part.met: file size required")
	}
	version := PartFileVersion
	if opts.FileSize >= int64(^uint32(0)) {
		version = PartFileLargeFileVersion
	}
	modified := opts.Modified
	if modified == 0 {
		modified = uint32(time.Now().Unix())
	}
	tags := make([]EmuleMetTag, 0, 8+len(opts.Gaps)*2)
	tags = append(tags, EmuleMetTag{
		Type: emuleTagTypeString, NameID: FTFilename, String: opts.Filename,
	})
	if version == PartFileLargeFileVersion {
		tags = append(tags,
			EmuleMetTag{Type: emuleTagTypeUint64, NameID: FTFileSize, UInt64: uint64(opts.FileSize)},
			EmuleMetTag{Type: emuleTagTypeUint64, NameID: FTTransferred, UInt64: opts.Transferred},
		)
	} else {
		tags = append(tags,
			EmuleMetTag{Type: emuleTagTypeUint32, NameID: FTFileSize, UInt32: uint32(opts.FileSize)},
			EmuleMetTag{Type: emuleTagTypeUint32, NameID: FTTransferred, UInt32: uint32(opts.Transferred)},
		)
	}
	tags = append(tags, EmuleMetTag{Type: emuleTagTypeUint8, NameID: FTStatus, UInt64: 0})
	for i, gap := range opts.Gaps {
		idx := strconv.Itoa(i)
		gapType := emuleTagTypeUint32
		if version == PartFileLargeFileVersion {
			gapType = emuleTagTypeUint64
		}
		tags = append(tags,
			EmuleMetTag{
				Type: gapType, Name: string([]byte{FTGapStart}) + idx,
				UInt32: uint32(gap.Start), UInt64: gap.Start,
			},
			EmuleMetTag{
				Type: gapType, Name: string([]byte{FTGapEnd}) + idx,
				UInt32: uint32(gap.End), UInt64: gap.End,
			},
		)
	}
	var buf bytes.Buffer
	if err := buf.WriteByte(version); err != nil {
		return nil, err
	}
	if err := binary.Write(&buf, binary.LittleEndian, modified); err != nil {
		return nil, err
	}
	if err := WriteHash(&buf, opts.Hash); err != nil {
		return nil, err
	}
	if err := WriteUInt16(&buf, uint16(len(opts.PieceHashes))); err != nil {
		return nil, err
	}
	for _, ph := range opts.PieceHashes {
		if err := WriteHash(&buf, ph); err != nil {
			return nil, err
		}
	}
	if err := WriteUInt32(&buf, uint32(len(tags))); err != nil {
		return nil, err
	}
	for _, tag := range tags {
		if err := writeEmuleMetTag(&buf, tag); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

// EmulePartMetOptions 为导出 eMule .part.met 所需参数。
type EmulePartMetOptions struct {
	Hash        Hash
	FileSize    int64
	Filename    string
	Transferred uint64
	Modified    uint32
	PieceHashes []Hash
	Gaps        []PartMetGap
}

// WriteEmulePartMet 原子写入 eMule 二进制 .part.met。
func WriteEmulePartMet(path string, opts EmulePartMetOptions) error {
	raw, err := BuildEmulePartMet(opts)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// IsEmulePartMetBytes 判断字节是否为 eMule 二进制 .part.met。
func IsEmulePartMetBytes(raw []byte) bool {
	if len(raw) < 1 {
		return false
	}
	switch raw[0] {
	case PartFileVersion, PartFileSplittedVersion, PartFileLargeFileVersion:
		return true
	default:
		return false
	}
}

// IsGoed2kPartMetJSON 判断字节是否为 goed2k JSON .part.met。
func IsGoed2kPartMetJSON(raw []byte) bool {
	trim := strings.TrimSpace(string(raw))
	return strings.HasPrefix(trim, "{")
}

// ReadAllPartMet 读取整个 .part.met 文件内容。
func ReadAllPartMet(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// SkipEmulePartMetHeader 返回标签区起始偏移（用于调试）。
func SkipEmulePartMetHeader(r io.Reader) (int, error) {
	var header [1]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, err
	}
	return 1, nil
}
