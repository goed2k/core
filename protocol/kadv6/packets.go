package kadv6

import (
	"bytes"

	"github.com/monkeyWie/goed2k/protocol"
)

const (
	BootstrapReqOp     byte = 0x01
	BootstrapResOp     byte = 0x09
	HelloReqOp         byte = 0x11
	HelloResOp         byte = 0x19
	FindNodeReqOp      byte = 0x21
	FindNodeResOp      byte = 0x29
	SearchKeysReqOp    byte = 0x33
	SearchSrcReqOp     byte = 0x34
	SearchNotesReqOp   byte = 0x35
	SearchResOp        byte = 0x3B
	PublishKeysReqOp   byte = 0x43
	PublishSourceReqOp byte = 0x44
	PublishNotesReqOp  byte = 0x45
	PublishResOp       byte = 0x4B
	PingOp             byte = 0x60
	PongOp             byte = 0x61

	FindValue byte = 0x02
	Store     byte = 0x04
	FindNode  byte = 0x0B
)

func encodePacket(opcode byte, payload []byte) []byte {
	packet := make([]byte, 0, 2+len(payload))
	packet = append(packet, ProtocolHeader, opcode)
	packet = append(packet, payload...)
	return packet
}

// BootstrapReq asks a known node for contacts.
type BootstrapReq struct{}

// Pack encodes the request.
func (BootstrapReq) Pack() ([]byte, error) {
	return encodePacket(BootstrapReqOp, nil), nil
}

// BootstrapRes returns contacts near the responder.
type BootstrapRes struct {
	ID       ID
	TCPPort  uint16
	Version  byte
	Contacts []EntryV6
}

// Unpack decodes the response payload.
func (b *BootstrapRes) Unpack(payload []byte) error {
	reader := bytes.NewReader(payload)
	if err := b.ID.Get(reader); err != nil {
		return err
	}
	port, err := protocol.ReadUInt16(reader)
	if err != nil {
		return err
	}
	version, err := reader.ReadByte()
	if err != nil {
		return err
	}
	count, err := protocol.ReadUInt16(reader)
	if err != nil {
		return err
	}
	b.TCPPort = port
	b.Version = version
	b.Contacts = make([]EntryV6, int(count))
	for idx := range b.Contacts {
		if err := b.Contacts[idx].Get(reader); err != nil {
			return err
		}
	}
	return nil
}

// Pack encodes the response payload.
func (b BootstrapRes) Pack() ([]byte, error) {
	var payload bytes.Buffer
	if err := b.ID.Put(&payload); err != nil {
		return nil, err
	}
	if err := protocol.WriteUInt16(&payload, b.TCPPort); err != nil {
		return nil, err
	}
	version := b.Version
	if version == 0 {
		version = KademliaVersion
	}
	if err := payload.WriteByte(version); err != nil {
		return nil, err
	}
	if err := protocol.WriteUInt16(&payload, uint16(len(b.Contacts))); err != nil {
		return nil, err
	}
	for _, entry := range b.Contacts {
		if err := entry.Put(&payload); err != nil {
			return nil, err
		}
	}
	return encodePacket(BootstrapResOp, payload.Bytes()), nil
}

// Hello confirms node identity and service port.
type Hello struct {
	ID      ID
	TCPPort uint16
	Version byte
	Tags    []Tag
}

// Unpack decodes a hello payload.
func (h *Hello) Unpack(payload []byte) error {
	reader := bytes.NewReader(payload)
	if err := h.ID.Get(reader); err != nil {
		return err
	}
	port, err := protocol.ReadUInt16(reader)
	if err != nil {
		return err
	}
	version, err := reader.ReadByte()
	if err != nil {
		return err
	}
	count, err := reader.ReadByte()
	if err != nil {
		return err
	}
	h.TCPPort = port
	h.Version = version
	h.Tags = make([]Tag, int(count))
	for idx := range h.Tags {
		if err := h.Tags[idx].Get(reader); err != nil {
			return err
		}
	}
	return nil
}

// Pack encodes the hello payload with the given opcode.
func (h Hello) Pack(opcode byte) ([]byte, error) {
	var payload bytes.Buffer
	if err := h.ID.Put(&payload); err != nil {
		return nil, err
	}
	if err := protocol.WriteUInt16(&payload, h.TCPPort); err != nil {
		return nil, err
	}
	version := h.Version
	if version == 0 {
		version = KademliaVersion
	}
	if err := payload.WriteByte(version); err != nil {
		return nil, err
	}
	if err := payload.WriteByte(byte(len(h.Tags))); err != nil {
		return nil, err
	}
	for _, tag := range h.Tags {
		if err := tag.Put(&payload); err != nil {
			return nil, err
		}
	}
	return encodePacket(opcode, payload.Bytes()), nil
}

// FindNodeReq asks for closer contacts to a target id.
type FindNodeReq struct {
	SearchType byte
	Target     ID
	Receiver   ID
}

// Unpack decodes a find-node request.
func (r *FindNodeReq) Unpack(payload []byte) error {
	reader := bytes.NewReader(payload)
	op, err := reader.ReadByte()
	if err != nil {
		return err
	}
	r.SearchType = op
	if err := r.Target.Get(reader); err != nil {
		return err
	}
	return r.Receiver.Get(reader)
}

// Pack encodes a find-node request.
func (r FindNodeReq) Pack() ([]byte, error) {
	var payload bytes.Buffer
	if err := payload.WriteByte(r.SearchType); err != nil {
		return nil, err
	}
	if err := r.Target.Put(&payload); err != nil {
		return nil, err
	}
	if err := r.Receiver.Put(&payload); err != nil {
		return nil, err
	}
	return encodePacket(FindNodeReqOp, payload.Bytes()), nil
}

// FindNodeRes returns contacts for a target id.
type FindNodeRes struct {
	Target  ID
	Results []EntryV6
}

// Unpack decodes a find-node response.
func (r *FindNodeRes) Unpack(payload []byte) error {
	reader := bytes.NewReader(payload)
	if err := r.Target.Get(reader); err != nil {
		return err
	}
	count, err := protocol.ReadUInt16(reader)
	if err != nil {
		return err
	}
	r.Results = make([]EntryV6, int(count))
	for idx := range r.Results {
		if err := r.Results[idx].Get(reader); err != nil {
			return err
		}
	}
	return nil
}

// Pack encodes a find-node response.
func (r FindNodeRes) Pack() ([]byte, error) {
	var payload bytes.Buffer
	if err := r.Target.Put(&payload); err != nil {
		return nil, err
	}
	if err := protocol.WriteUInt16(&payload, uint16(len(r.Results))); err != nil {
		return nil, err
	}
	for _, entry := range r.Results {
		if err := entry.Put(&payload); err != nil {
			return nil, err
		}
	}
	return encodePacket(FindNodeResOp, payload.Bytes()), nil
}

// SearchSourcesReq requests source records for a file.
type SearchSourcesReq struct {
	Target   ID
	StartPos uint16
	Size     uint64
}

// Unpack decodes a source search request.
func (s *SearchSourcesReq) Unpack(payload []byte) error {
	reader := bytes.NewReader(payload)
	if err := s.Target.Get(reader); err != nil {
		return err
	}
	startPos, err := protocol.ReadUInt16(reader)
	if err != nil {
		return err
	}
	size, err := protocol.ReadUInt64(reader)
	if err != nil {
		return err
	}
	s.StartPos = startPos
	s.Size = size
	return nil
}

// Pack encodes a source search request.
func (s SearchSourcesReq) Pack() ([]byte, error) {
	var payload bytes.Buffer
	if err := s.Target.Put(&payload); err != nil {
		return nil, err
	}
	if err := protocol.WriteUInt16(&payload, s.StartPos); err != nil {
		return nil, err
	}
	if err := protocol.WriteUInt64(&payload, s.Size); err != nil {
		return nil, err
	}
	return encodePacket(SearchSrcReqOp, payload.Bytes()), nil
}

// SearchKeysReq requests keyword records.
type SearchKeysReq struct {
	Target   ID
	StartPos uint16
}

// Unpack decodes a keyword search request.
func (s *SearchKeysReq) Unpack(payload []byte) error {
	reader := bytes.NewReader(payload)
	if err := s.Target.Get(reader); err != nil {
		return err
	}
	startPos, err := protocol.ReadUInt16(reader)
	if err != nil {
		return err
	}
	s.StartPos = startPos
	return nil
}

// Pack encodes a keyword search request.
func (s SearchKeysReq) Pack() ([]byte, error) {
	var payload bytes.Buffer
	if err := s.Target.Put(&payload); err != nil {
		return nil, err
	}
	if err := protocol.WriteUInt16(&payload, s.StartPos); err != nil {
		return nil, err
	}
	return encodePacket(SearchKeysReqOp, payload.Bytes()), nil
}

// SearchNotesReq requests note records.
type SearchNotesReq struct {
	Target   ID
	StartPos uint16
}

// Unpack decodes a notes search request.
func (s *SearchNotesReq) Unpack(payload []byte) error {
	reader := bytes.NewReader(payload)
	if err := s.Target.Get(reader); err != nil {
		return err
	}
	startPos, err := protocol.ReadUInt16(reader)
	if err != nil {
		return err
	}
	s.StartPos = startPos
	return nil
}

// Pack encodes a notes search request.
func (s SearchNotesReq) Pack() ([]byte, error) {
	var payload bytes.Buffer
	if err := s.Target.Put(&payload); err != nil {
		return nil, err
	}
	if err := protocol.WriteUInt16(&payload, s.StartPos); err != nil {
		return nil, err
	}
	return encodePacket(SearchNotesReqOp, payload.Bytes()), nil
}

// SearchRes returns search entries for a target.
type SearchRes struct {
	Source  ID
	Target  ID
	Results []SearchEntry
}

// Unpack decodes a search response.
func (s *SearchRes) Unpack(payload []byte) error {
	reader := bytes.NewReader(payload)
	if err := s.Source.Get(reader); err != nil {
		return err
	}
	if err := s.Target.Get(reader); err != nil {
		return err
	}
	count, err := protocol.ReadUInt16(reader)
	if err != nil {
		return err
	}
	s.Results = make([]SearchEntry, int(count))
	for idx := range s.Results {
		if err := s.Results[idx].Get(reader); err != nil {
			return err
		}
	}
	return nil
}

// Pack encodes a search response.
func (s SearchRes) Pack() ([]byte, error) {
	var payload bytes.Buffer
	if err := s.Source.Put(&payload); err != nil {
		return nil, err
	}
	if err := s.Target.Put(&payload); err != nil {
		return nil, err
	}
	if err := protocol.WriteUInt16(&payload, uint16(len(s.Results))); err != nil {
		return nil, err
	}
	for _, result := range s.Results {
		if err := result.Put(&payload); err != nil {
			return nil, err
		}
	}
	return encodePacket(SearchResOp, payload.Bytes()), nil
}

// PublishSourcesReq publishes one source record.
type PublishSourcesReq struct {
	FileID ID
	Source SearchEntry
}

// Unpack decodes a source publish request.
func (p *PublishSourcesReq) Unpack(payload []byte) error {
	reader := bytes.NewReader(payload)
	if err := p.FileID.Get(reader); err != nil {
		return err
	}
	return p.Source.Get(reader)
}

// Pack encodes a source publish request.
func (p PublishSourcesReq) Pack() ([]byte, error) {
	var payload bytes.Buffer
	if err := p.FileID.Put(&payload); err != nil {
		return nil, err
	}
	if err := p.Source.Put(&payload); err != nil {
		return nil, err
	}
	return encodePacket(PublishSourceReqOp, payload.Bytes()), nil
}

// PublishKeysReq publishes keyword records.
type PublishKeysReq struct {
	KeywordID ID
	Sources   []SearchEntry
}

// Unpack decodes a keyword publish request.
func (p *PublishKeysReq) Unpack(payload []byte) error {
	reader := bytes.NewReader(payload)
	if err := p.KeywordID.Get(reader); err != nil {
		return err
	}
	count, err := protocol.ReadUInt16(reader)
	if err != nil {
		return err
	}
	p.Sources = make([]SearchEntry, int(count))
	for idx := range p.Sources {
		if err := p.Sources[idx].Get(reader); err != nil {
			return err
		}
	}
	return nil
}

// Pack encodes a keyword publish request.
func (p PublishKeysReq) Pack() ([]byte, error) {
	var payload bytes.Buffer
	if err := p.KeywordID.Put(&payload); err != nil {
		return nil, err
	}
	if err := protocol.WriteUInt16(&payload, uint16(len(p.Sources))); err != nil {
		return nil, err
	}
	for _, source := range p.Sources {
		if err := source.Put(&payload); err != nil {
			return nil, err
		}
	}
	return encodePacket(PublishKeysReqOp, payload.Bytes()), nil
}

// PublishNotesReq publishes note records.
type PublishNotesReq struct {
	FileID ID
	Notes  []SearchEntry
}

// Unpack decodes a notes publish request.
func (p *PublishNotesReq) Unpack(payload []byte) error {
	reader := bytes.NewReader(payload)
	if err := p.FileID.Get(reader); err != nil {
		return err
	}
	count, err := protocol.ReadUInt16(reader)
	if err != nil {
		return err
	}
	p.Notes = make([]SearchEntry, int(count))
	for idx := range p.Notes {
		if err := p.Notes[idx].Get(reader); err != nil {
			return err
		}
	}
	return nil
}

// Pack encodes a notes publish request.
func (p PublishNotesReq) Pack() ([]byte, error) {
	var payload bytes.Buffer
	if err := p.FileID.Put(&payload); err != nil {
		return nil, err
	}
	if err := protocol.WriteUInt16(&payload, uint16(len(p.Notes))); err != nil {
		return nil, err
	}
	for _, note := range p.Notes {
		if err := note.Put(&payload); err != nil {
			return nil, err
		}
	}
	return encodePacket(PublishNotesReqOp, payload.Bytes()), nil
}

// PublishRes acknowledges one or more published records.
type PublishRes struct {
	FileID ID
	Count  byte
}

// Unpack decodes a publish response.
func (p *PublishRes) Unpack(payload []byte) error {
	reader := bytes.NewReader(payload)
	if err := p.FileID.Get(reader); err != nil {
		return err
	}
	count, err := reader.ReadByte()
	if err != nil {
		return err
	}
	p.Count = count
	return nil
}

// Pack encodes a publish response.
func (p PublishRes) Pack() ([]byte, error) {
	var payload bytes.Buffer
	if err := p.FileID.Put(&payload); err != nil {
		return nil, err
	}
	if err := payload.WriteByte(p.Count); err != nil {
		return nil, err
	}
	return encodePacket(PublishResOp, payload.Bytes()), nil
}

// Ping is an empty liveness probe.
type Ping struct{}

// Pack encodes a ping packet.
func (Ping) Pack() ([]byte, error) {
	return encodePacket(PingOp, nil), nil
}

// Pong returns the active UDP port.
type Pong struct {
	UDPPort uint16
}

// Unpack decodes a pong payload.
func (p *Pong) Unpack(payload []byte) error {
	reader := bytes.NewReader(payload)
	port, err := protocol.ReadUInt16(reader)
	if err != nil {
		return err
	}
	p.UDPPort = port
	return nil
}

// Pack encodes a pong payload.
func (p Pong) Pack() ([]byte, error) {
	var payload bytes.Buffer
	if err := protocol.WriteUInt16(&payload, p.UDPPort); err != nil {
		return nil, err
	}
	return encodePacket(PongOp, payload.Bytes()), nil
}
