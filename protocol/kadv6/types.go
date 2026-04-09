package kadv6

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"

	"github.com/monkeyWie/goed2k/protocol"
)

const (
	// ProtocolHeader marks KADV6 UDP packets.
	ProtocolHeader byte = 0xE6
	// KademliaVersion is the first published KADV6 protocol version.
	KademliaVersion byte = 0x01

	// Tag types use a simpler typed tag encoding than legacy Kad.
	TagTypeString byte = 0x02
	TagTypeUint32 byte = 0x03
	TagTypeUint16 byte = 0x08
	TagTypeUint8  byte = 0x09
	TagTypeUint64 byte = 0x0B
	TagTypeBytes  byte = 0x0C

	// Source-related tag identifiers.
	TagName          byte = 0x01
	TagFileSize      byte = 0xD3
	TagAddrFamily    byte = 0xEE
	TagSourceIP6     byte = 0xF0
	TagSourceUDPPort byte = 0xFC
	TagSourcePort    byte = 0xFD
	TagSourceType    byte = 0xFF

	// Supported address family in KADV6.
	AddrFamilyIPv6 byte = 6
)

var nodesDatMagic = [4]byte{'K', 'D', '6', 'N'}

// ID is the KADV6 node identifier.
type ID struct {
	protocol.Hash
}

// NewID wraps a protocol hash as a KADV6 node id.
func NewID(hash protocol.Hash) ID {
	return ID{Hash: hash}
}

// Get decodes the node id from a reader.
func (i *ID) Get(src *bytes.Reader) error {
	raw, err := protocol.ReadBytes(src, 16)
	if err != nil {
		return err
	}
	hash, err := protocol.HashFromBytes(raw)
	if err != nil {
		return err
	}
	i.Hash = hash
	return nil
}

// Put encodes the node id into a buffer.
func (i ID) Put(dst *bytes.Buffer) error {
	_, err := dst.Write(i.Hash.Bytes())
	return err
}

// EndpointV6 stores a native IPv6 endpoint for KADV6 contacts.
type EndpointV6 struct {
	IP      [16]byte
	UDPPort uint16
	TCPPort uint16
}

// EndpointFromIP builds an endpoint from a native IPv6 address.
func EndpointFromIP(ip net.IP, udpPort, tcpPort uint16) (EndpointV6, error) {
	if !isNativeIPv6(ip) {
		return EndpointV6{}, errors.New("kadv6 endpoint requires a native ipv6 address")
	}
	if udpPort == 0 {
		return EndpointV6{}, errors.New("kadv6 endpoint requires a non-zero udp port")
	}
	var value EndpointV6
	copy(value.IP[:], ip.To16())
	value.UDPPort = udpPort
	value.TCPPort = tcpPort
	return value, nil
}

// EndpointFromUDPAddr builds an endpoint from a UDP address plus advertised TCP port.
func EndpointFromUDPAddr(addr *net.UDPAddr, tcpPort uint16) (EndpointV6, error) {
	if addr == nil {
		return EndpointV6{}, errors.New("kadv6 endpoint requires an address")
	}
	return EndpointFromIP(addr.IP, uint16(addr.Port), tcpPort)
}

// Validate checks whether the endpoint can be routed inside KADV6.
func (e EndpointV6) Validate() error {
	ip := e.IPNet()
	if !isNativeIPv6(ip) {
		return errors.New("kadv6 endpoint requires a native ipv6 address")
	}
	if e.UDPPort == 0 {
		return errors.New("kadv6 endpoint requires a non-zero udp port")
	}
	return nil
}

// IPNet returns a copy of the stored IPv6 address as net.IP.
func (e EndpointV6) IPNet() net.IP {
	ip := make(net.IP, net.IPv6len)
	copy(ip, e.IP[:])
	return ip
}

// UDPAddr converts the endpoint to a UDP address.
func (e EndpointV6) UDPAddr() *net.UDPAddr {
	return &net.UDPAddr{IP: e.IPNet(), Port: int(e.UDPPort)}
}

// TCPAddr converts the endpoint to a TCP address.
func (e EndpointV6) TCPAddr() *net.TCPAddr {
	return &net.TCPAddr{IP: e.IPNet(), Port: int(e.TCPPort)}
}

// String renders the endpoint in bracketed IPv6 form.
func (e EndpointV6) String() string {
	return fmt.Sprintf("[%s]:%d", e.IPNet().String(), e.UDPPort)
}

// Get decodes the endpoint from a reader.
func (e *EndpointV6) Get(src *bytes.Reader) error {
	raw, err := protocol.ReadBytes(src, net.IPv6len)
	if err != nil {
		return err
	}
	copy(e.IP[:], raw)
	if err := binary.Read(src, binary.LittleEndian, &e.UDPPort); err != nil {
		return err
	}
	if err := binary.Read(src, binary.LittleEndian, &e.TCPPort); err != nil {
		return err
	}
	return e.Validate()
}

// Put encodes the endpoint into a buffer.
func (e EndpointV6) Put(dst *bytes.Buffer) error {
	if err := e.Validate(); err != nil {
		return err
	}
	if _, err := dst.Write(e.IP[:]); err != nil {
		return err
	}
	if err := binary.Write(dst, binary.LittleEndian, e.UDPPort); err != nil {
		return err
	}
	return binary.Write(dst, binary.LittleEndian, e.TCPPort)
}

// EntryV6 is a KADV6 routing entry.
type EntryV6 struct {
	ID       ID
	Endpoint EndpointV6
	Version  byte
	Verified bool
}

// Get decodes an entry from a reader.
func (e *EntryV6) Get(src *bytes.Reader) error {
	if err := e.ID.Get(src); err != nil {
		return err
	}
	if err := e.Endpoint.Get(src); err != nil {
		return err
	}
	version, err := src.ReadByte()
	if err != nil {
		return err
	}
	flags, err := src.ReadByte()
	if err != nil {
		return err
	}
	e.Version = version
	e.Verified = flags&0x01 != 0
	return nil
}

// Put encodes an entry into a buffer.
func (e EntryV6) Put(dst *bytes.Buffer) error {
	if err := e.ID.Put(dst); err != nil {
		return err
	}
	if err := e.Endpoint.Put(dst); err != nil {
		return err
	}
	if err := dst.WriteByte(e.Version); err != nil {
		return err
	}
	flags := byte(0)
	if e.Verified {
		flags |= 0x01
	}
	return dst.WriteByte(flags)
}

// Tag stores one typed metadata value.
type Tag struct {
	Type   byte
	ID     byte
	UInt64 uint64
	String string
	Bytes  []byte
}

// Get decodes a tag from a reader.
func (t *Tag) Get(src *bytes.Reader) error {
	tagType, err := src.ReadByte()
	if err != nil {
		return err
	}
	tagID, err := src.ReadByte()
	if err != nil {
		return err
	}
	t.Type = tagType
	t.ID = tagID
	t.UInt64 = 0
	t.String = ""
	t.Bytes = nil
	switch t.Type {
	case TagTypeUint8:
		value, err := src.ReadByte()
		if err != nil {
			return err
		}
		t.UInt64 = uint64(value)
	case TagTypeUint16:
		value, err := protocol.ReadUInt16(src)
		if err != nil {
			return err
		}
		t.UInt64 = uint64(value)
	case TagTypeUint32:
		value, err := protocol.ReadUInt32(src)
		if err != nil {
			return err
		}
		t.UInt64 = uint64(value)
	case TagTypeUint64:
		value, err := protocol.ReadUInt64(src)
		if err != nil {
			return err
		}
		t.UInt64 = value
	case TagTypeString:
		size, err := protocol.ReadUInt16(src)
		if err != nil {
			return err
		}
		value, err := protocol.ReadBytes(src, int(size))
		if err != nil {
			return err
		}
		t.String = string(value)
	case TagTypeBytes:
		size, err := protocol.ReadUInt16(src)
		if err != nil {
			return err
		}
		value, err := protocol.ReadBytes(src, int(size))
		if err != nil {
			return err
		}
		t.Bytes = bytes.Clone(value)
	default:
		return errors.New("unsupported kadv6 tag type")
	}
	return nil
}

// Put encodes a tag into a buffer.
func (t Tag) Put(dst *bytes.Buffer) error {
	if err := dst.WriteByte(t.Type); err != nil {
		return err
	}
	if err := dst.WriteByte(t.ID); err != nil {
		return err
	}
	switch t.Type {
	case TagTypeUint8:
		return dst.WriteByte(byte(t.UInt64))
	case TagTypeUint16:
		return protocol.WriteUInt16(dst, uint16(t.UInt64))
	case TagTypeUint32:
		return protocol.WriteUInt32(dst, uint32(t.UInt64))
	case TagTypeUint64:
		return protocol.WriteUInt64(dst, t.UInt64)
	case TagTypeString:
		if err := protocol.WriteUInt16(dst, uint16(len(t.String))); err != nil {
			return err
		}
		_, err := dst.WriteString(t.String)
		return err
	case TagTypeBytes:
		if err := protocol.WriteUInt16(dst, uint16(len(t.Bytes))); err != nil {
			return err
		}
		_, err := dst.Write(t.Bytes)
		return err
	default:
		return errors.New("unsupported kadv6 tag type")
	}
}

// SearchEntry stores source, keyword, or note metadata.
type SearchEntry struct {
	ID   ID
	Tags []Tag
}

// Get decodes a search entry from a reader.
func (s *SearchEntry) Get(src *bytes.Reader) error {
	if err := s.ID.Get(src); err != nil {
		return err
	}
	count, err := src.ReadByte()
	if err != nil {
		return err
	}
	s.Tags = make([]Tag, int(count))
	for idx := range s.Tags {
		if err := s.Tags[idx].Get(src); err != nil {
			return err
		}
	}
	return nil
}

// Put encodes a search entry into a buffer.
func (s SearchEntry) Put(dst *bytes.Buffer) error {
	if err := s.ID.Put(dst); err != nil {
		return err
	}
	if err := dst.WriteByte(byte(len(s.Tags))); err != nil {
		return err
	}
	for _, tag := range s.Tags {
		if err := tag.Put(dst); err != nil {
			return err
		}
	}
	return nil
}

// UIntTag returns the first matching unsigned integer tag.
func (s SearchEntry) UIntTag(id byte) (uint64, bool) {
	for _, tag := range s.Tags {
		if tag.ID == id {
			return tag.UInt64, true
		}
	}
	return 0, false
}

// StringTag returns the first matching string tag.
func (s SearchEntry) StringTag(id byte) (string, bool) {
	for _, tag := range s.Tags {
		if tag.ID == id {
			return tag.String, true
		}
	}
	return "", false
}

// BytesTag returns the first matching raw-bytes tag.
func (s SearchEntry) BytesTag(id byte) ([]byte, bool) {
	for _, tag := range s.Tags {
		if tag.ID == id {
			return bytes.Clone(tag.Bytes), true
		}
	}
	return nil, false
}

// SourceAddr extracts a direct IPv6 TCP source from the search entry.
func (s SearchEntry) SourceAddr() (*net.TCPAddr, bool) {
	family, hasFamily := s.UIntTag(TagAddrFamily)
	ip, hasIP := s.BytesTag(TagSourceIP6)
	port, hasPort := s.UIntTag(TagSourcePort)
	sourceType, hasType := s.UIntTag(TagSourceType)
	if !hasFamily || !hasIP || !hasPort {
		return nil, false
	}
	if family != uint64(AddrFamilyIPv6) || len(ip) != net.IPv6len {
		return nil, false
	}
	if hasType && sourceType != 0 && sourceType != 1 && sourceType != 4 {
		return nil, false
	}
	if !isNativeIPv6(net.IP(ip)) || port == 0 {
		return nil, false
	}
	return &net.TCPAddr{IP: bytes.Clone(ip), Port: int(port)}, true
}

// NodesDat stores bootstrap contacts for KADV6.
type NodesDat struct {
	Version          uint32
	BootstrapEdition uint32
	Contacts         []EntryV6
}

// Unpack decodes the binary nodes6.dat payload.
func (n *NodesDat) Unpack(payload []byte) error {
	reader := bytes.NewReader(payload)
	var magic [4]byte
	if _, err := reader.Read(magic[:]); err != nil {
		return err
	}
	if magic != nodesDatMagic {
		return errors.New("unsupported kadv6 nodes.dat magic")
	}
	version, err := protocol.ReadUInt32(reader)
	if err != nil {
		return err
	}
	bootstrapEdition, err := protocol.ReadUInt32(reader)
	if err != nil {
		return err
	}
	count, err := protocol.ReadUInt32(reader)
	if err != nil {
		return err
	}
	const minEntrySize = 16 + 16 + 2 + 2 + 1 + 1
	if uint64(count) > uint64(reader.Len()/minEntrySize) {
		return errors.New("invalid kadv6 nodes.dat contact count")
	}
	n.Version = version
	n.BootstrapEdition = bootstrapEdition
	n.Contacts = make([]EntryV6, int(count))
	for idx := range n.Contacts {
		if err := n.Contacts[idx].Get(reader); err != nil {
			return err
		}
	}
	return nil
}

// Pack encodes the nodes6.dat payload.
func (n NodesDat) Pack() ([]byte, error) {
	var payload bytes.Buffer
	if _, err := payload.Write(nodesDatMagic[:]); err != nil {
		return nil, err
	}
	version := n.Version
	if version == 0 {
		version = 1
	}
	if err := protocol.WriteUInt32(&payload, version); err != nil {
		return nil, err
	}
	if err := protocol.WriteUInt32(&payload, n.BootstrapEdition); err != nil {
		return nil, err
	}
	if err := protocol.WriteUInt32(&payload, uint32(len(n.Contacts))); err != nil {
		return nil, err
	}
	for _, entry := range n.Contacts {
		if err := entry.Put(&payload); err != nil {
			return nil, err
		}
	}
	return payload.Bytes(), nil
}

// ParseNodesDat decodes a KADV6 nodes file from raw bytes.
func ParseNodesDat(raw []byte) (*NodesDat, error) {
	nodes := &NodesDat{}
	if err := nodes.Unpack(raw); err != nil {
		return nil, err
	}
	return nodes, nil
}

// LoadNodesDat loads and parses a KADV6 nodes file from disk.
func LoadNodesDat(path string) (*NodesDat, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseNodesDat(raw)
}

// DecodePacket extracts the opcode and payload from a packet.
func DecodePacket(packet []byte) (byte, []byte, error) {
	if len(packet) < 2 {
		return 0, nil, errors.New("kadv6 packet too short")
	}
	if packet[0] != ProtocolHeader {
		return 0, nil, errors.New("unsupported kadv6 protocol header")
	}
	return packet[1], packet[2:], nil
}

// DistanceCompare compares XOR distances to the target.
func DistanceCompare(a, b, target ID) int {
	for i := 0; i < 16; i++ {
		da := a.Hash.At(i) ^ target.Hash.At(i)
		db := b.Hash.At(i) ^ target.Hash.At(i)
		if da < db {
			return -1
		}
		if da > db {
			return 1
		}
	}
	return 0
}

func isNativeIPv6(ip net.IP) bool {
	if ip == nil || ip.To4() != nil {
		return false
	}
	ip16 := ip.To16()
	return len(ip16) == net.IPv6len && !ip16.Equal(net.IPv6zero)
}
