package server

import (
	"bytes"
	"strings"

	"github.com/goed2k/core/protocol"
)

const (
	searchTypeBool   byte = 0x00
	searchTypeString byte = 0x01
	searchTypeStrTag byte = 0x02
	searchTypeUint32 byte = 0x03
	searchTypeUint64 byte = 0x08
	searchOpEqual    byte = 0x00
	searchOpGreater  byte = 0x01
	searchOpLess     byte = 0x02
	searchBoolAND    byte = 0x00
	searchBoolOR     byte = 0x01
	searchBoolNOT    byte = 0x02
)

type SearchRequest struct {
	Query              string
	MinSize            int64
	MaxSize            int64
	MinSources         int
	MinCompleteSources int
	FileType           string
	Extension          string
}

func (s *SearchRequest) Get(src *bytes.Reader) error {
	return nil
}

func (s *SearchRequest) filterTerms() []searchTerm {
	terms := make([]searchTerm, 0, 6)
	if s.FileType != "" {
		terms = append(terms, searchStringTag(protocol.FTFileType, s.FileType))
	}
	if s.Extension != "" {
		terms = append(terms, searchStringTag(protocol.FTFileFormat, s.Extension))
	}
	if s.MinSize > 0 {
		terms = append(terms, searchNumericTag(protocol.FTFileSize, searchOpGreater, uint64(s.MinSize)))
	}
	if s.MaxSize > 0 {
		terms = append(terms, searchNumericTag(protocol.FTFileSize, searchOpLess, uint64(s.MaxSize)))
	}
	if s.MinSources > 0 {
		terms = append(terms, searchNumericTag(protocol.FTSources, searchOpGreater, uint64(s.MinSources)))
	}
	if s.MinCompleteSources > 0 {
		terms = append(terms, searchNumericTag(protocol.FTCompleteSources, searchOpGreater, uint64(s.MinCompleteSources)))
	}
	return terms
}

func (s *SearchRequest) Put(dst *bytes.Buffer) error {
	ops, words := parseSearchQuery(s.Query)
	queryTerms := make([]searchTerm, 0, len(words))
	for _, token := range words {
		queryTerms = append(queryTerms, searchString(token))
	}
	filters := s.filterTerms()
	if len(queryTerms) == 0 {
		return putSearchTerms(dst, filters, nil)
	}
	if err := putSearchTerms(dst, queryTerms, ops); err != nil {
		return err
	}
	for _, term := range filters {
		if err := dst.WriteByte(searchTypeBool); err != nil {
			return err
		}
		if err := dst.WriteByte(searchBoolAND); err != nil {
			return err
		}
		if err := term.put(dst); err != nil {
			return err
		}
	}
	return nil
}

func (s *SearchRequest) BytesCount() int {
	var buf bytes.Buffer
	if err := s.Put(&buf); err != nil {
		return 0
	}
	return buf.Len()
}

type SearchMore struct{}

func (*SearchMore) Get(src *bytes.Reader) error { return nil }
func (*SearchMore) Put(dst *bytes.Buffer) error { return nil }
func (*SearchMore) BytesCount() int             { return 0 }

type SharedFileEntry struct {
	Hash     protocol.Hash
	ClientID int32
	Port     uint16
	Tags     protocol.TagList
}

func (s *SharedFileEntry) Get(src *bytes.Reader) error {
	hash, err := protocol.ReadHash(src)
	if err != nil {
		return err
	}
	clientID, err := protocol.ReadInt32(src)
	if err != nil {
		return err
	}
	port, err := protocol.ReadUInt16(src)
	if err != nil {
		return err
	}
	var tags protocol.TagList
	if err := tags.Get(src); err != nil {
		return err
	}
	s.Hash = hash
	s.ClientID = clientID
	s.Port = port
	s.Tags = tags
	return nil
}

func (s *SharedFileEntry) Put(dst *bytes.Buffer) error {
	if err := protocol.WriteHash(dst, s.Hash); err != nil {
		return err
	}
	if err := protocol.WriteInt32(dst, s.ClientID); err != nil {
		return err
	}
	if err := protocol.WriteUInt16(dst, s.Port); err != nil {
		return err
	}
	return s.Tags.Put(dst)
}

func (s *SharedFileEntry) BytesCount() int {
	return 16 + 4 + 2 + s.Tags.BytesCount()
}

func (s SharedFileEntry) StringTag(id byte) (string, bool) {
	for _, tag := range s.Tags {
		if tag.ID == id {
			return tag.String, true
		}
	}
	return "", false
}

func (s SharedFileEntry) UIntTag(id byte) (uint64, bool) {
	for _, tag := range s.Tags {
		if tag.ID == id {
			if tag.Type == protocol.TagTypeString || tag.Type >= protocol.TagTypeStr1 {
				return 0, false
			}
			return tag.UInt64, true
		}
	}
	return 0, false
}

type SearchResult struct {
	Results     []SharedFileEntry
	MoreResults bool
}

func (s *SearchResult) Get(src *bytes.Reader) error {
	count, err := protocol.ReadUInt32(src)
	if err != nil {
		return err
	}
	s.Results = make([]SharedFileEntry, int(count))
	for i := 0; i < int(count); i++ {
		if err := s.Results[i].Get(src); err != nil {
			return err
		}
	}
	if src.Len() > 0 {
		flag, err := src.ReadByte()
		if err != nil {
			return err
		}
		s.MoreResults = flag != 0
	}
	return nil
}

func (s *SearchResult) Put(dst *bytes.Buffer) error {
	if err := protocol.WriteUInt32(dst, uint32(len(s.Results))); err != nil {
		return err
	}
	for i := range s.Results {
		if err := s.Results[i].Put(dst); err != nil {
			return err
		}
	}
	if s.MoreResults {
		return dst.WriteByte(1)
	}
	return dst.WriteByte(0)
}

func (s *SearchResult) BytesCount() int {
	size := 4 + 1
	for i := range s.Results {
		size += s.Results[i].BytesCount()
	}
	return size
}

type searchTerm interface {
	put(dst *bytes.Buffer) error
	bytesCount() int
}

type searchStringTerm struct {
	value  string
	tagID  byte
	tagged bool
}

func searchString(value string) searchStringTerm {
	return searchStringTerm{value: value}
}

func searchStringTag(id byte, value string) searchStringTerm {
	return searchStringTerm{value: value, tagID: id, tagged: true}
}

func (s searchStringTerm) put(dst *bytes.Buffer) error {
	if s.tagged {
		if err := dst.WriteByte(searchTypeStrTag); err != nil {
			return err
		}
	} else {
		if err := dst.WriteByte(searchTypeString); err != nil {
			return err
		}
	}
	if err := protocol.WriteUInt16(dst, uint16(len(s.value))); err != nil {
		return err
	}
	if _, err := dst.WriteString(s.value); err != nil {
		return err
	}
	if s.tagged {
		if err := protocol.WriteUInt16(dst, 1); err != nil {
			return err
		}
		return dst.WriteByte(s.tagID)
	}
	return nil
}

func (s searchStringTerm) bytesCount() int {
	size := 1 + 2 + len(s.value)
	if s.tagged {
		size += 2 + 1
	}
	return size
}

type searchNumericTerm struct {
	tagID    byte
	operator byte
	value    uint64
}

func searchNumericTag(id, operator byte, value uint64) searchNumericTerm {
	return searchNumericTerm{tagID: id, operator: operator, value: value}
}

func (s searchNumericTerm) put(dst *bytes.Buffer) error {
	if s.value <= 0xffffffff {
		if err := dst.WriteByte(searchTypeUint32); err != nil {
			return err
		}
		if err := protocol.WriteUInt32(dst, uint32(s.value)); err != nil {
			return err
		}
	} else {
		if err := dst.WriteByte(searchTypeUint64); err != nil {
			return err
		}
		if err := protocol.WriteUInt64(dst, s.value); err != nil {
			return err
		}
	}
	if err := dst.WriteByte(s.operator); err != nil {
		return err
	}
	if err := protocol.WriteUInt16(dst, 1); err != nil {
		return err
	}
	return dst.WriteByte(s.tagID)
}

func (s searchNumericTerm) bytesCount() int {
	size := 1 + 1 + 2 + 1
	if s.value <= 0xffffffff {
		return size + 4
	}
	return size + 8
}

func TokenizeSearchQuery(query string) []string {
	ops, words := parseSearchQuery(query)
	out := make([]string, 0, len(words))
	for i, word := range words {
		if i > 0 && i-1 < len(ops) && ops[i-1] == searchBoolNOT {
			continue
		}
		out = append(out, word)
	}
	return out
}

func tokenizeSearchQuery(query string) []string {
	return TokenizeSearchQuery(query)
}

func putSearchTerms(dst *bytes.Buffer, terms []searchTerm, ops []byte) error {
	for i, term := range terms {
		if i > 0 {
			op := searchBoolAND
			if i-1 < len(ops) {
				op = ops[i-1]
			}
			if err := dst.WriteByte(searchTypeBool); err != nil {
				return err
			}
			if err := dst.WriteByte(op); err != nil {
				return err
			}
		}
		if err := term.put(dst); err != nil {
			return err
		}
	}
	return nil
}

// parseSearchQuery 解析最小布尔查询：默认 AND；OR / NOT（或 -word）为二元算子。
// 左结合，不支持括号。单独的前导算子忽略。
// 默认 AND 的普通词仍按原分词器切开点/下划线/连字符等，以便与 Kad 索引词对齐。
// OR/NOT/`-word` 的操作数只去边缘标点、不再切开，避免 `NOT name.ext` 变成 NOT name AND ext。
func parseSearchQuery(query string) (ops []byte, words []string) {
	raw := strings.Fields(strings.TrimSpace(query))
	pendingOp := byte(0)
	hasPending := false
	appendWord := func(word string, forcedOp byte, force bool) {
		if word == "" {
			return
		}
		if len(words) > 0 {
			switch {
			case force:
				ops = append(ops, forcedOp)
			case hasPending:
				ops = append(ops, pendingOp)
			default:
				ops = append(ops, searchBoolAND)
			}
		}
		words = append(words, word)
		hasPending = false
	}
	for _, field := range raw {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		if op, ok := searchOperator(field); ok {
			if len(words) == 0 {
				continue
			}
			pendingOp = op
			hasPending = true
			continue
		}
		if strings.HasPrefix(field, "-") && len(field) > 1 {
			word := strings.Trim(field[1:], "()[]{}<>,._!?:;\\/\"")
			if word == "" {
				continue
			}
			if len(words) == 0 {
				words = append(words, word)
				continue
			}
			appendWord(word, searchBoolNOT, true)
			continue
		}
		if hasPending {
			word := strings.Trim(field, "()[]{}<>,._!?:;\\/\"")
			appendWord(word, 0, false)
			continue
		}
		for _, word := range splitSearchWord(field) {
			appendWord(word, 0, false)
		}
	}
	return ops, words
}

func splitSearchWord(field string) []string {
	parts := strings.FieldsFunc(field, func(r rune) bool {
		return strings.ContainsRune(" ()[]{}<>,._-!?:;\\/\"\t\r\n", r)
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func searchOperator(field string) (byte, bool) {
	switch strings.ToUpper(field) {
	case "AND":
		return searchBoolAND, true
	case "OR":
		return searchBoolOR, true
	case "NOT":
		return searchBoolNOT, true
	default:
		return 0, false
	}
}
