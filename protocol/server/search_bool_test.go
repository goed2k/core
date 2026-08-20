package server

import (
	"bytes"
	"testing"
)

func TestParseSearchQueryORAndNOT(t *testing.T) {
	t.Parallel()
	ops, words := parseSearchQuery("shake OR off NOT remix")
	if len(words) != 3 || words[0] != "shake" || words[1] != "off" || words[2] != "remix" {
		t.Fatalf("words %+v", words)
	}
	if len(ops) != 2 || ops[0] != searchBoolOR || ops[1] != searchBoolNOT {
		t.Fatalf("ops %+v", ops)
	}
}

func TestParseSearchQueryDashNOTAndDefaultAND(t *testing.T) {
	t.Parallel()
	ops, words := parseSearchQuery("taylor -remix shake")
	if len(words) != 3 {
		t.Fatalf("words %+v", words)
	}
	if ops[0] != searchBoolNOT || ops[1] != searchBoolAND {
		t.Fatalf("ops %+v", ops)
	}
}

func TestParseSearchQuerySplitsPunctuationLikeOldTokenizer(t *testing.T) {
	t.Parallel()
	ops, words := parseSearchQuery("artist - title.mp3")
	if len(words) != 3 || words[0] != "artist" || words[1] != "title" || words[2] != "mp3" {
		t.Fatalf("words %+v", words)
	}
	if len(ops) != 2 || ops[0] != searchBoolAND || ops[1] != searchBoolAND {
		t.Fatalf("ops %+v", ops)
	}
}

func TestParseSearchQueryKeepsBooleanOperandWhole(t *testing.T) {
	t.Parallel()
	ops, words := parseSearchQuery("foo OR title.mp3 NOT remix-live")
	if len(words) != 3 || words[0] != "foo" || words[1] != "title.mp3" || words[2] != "remix-live" {
		t.Fatalf("words %+v", words)
	}
	if len(ops) != 2 || ops[0] != searchBoolOR || ops[1] != searchBoolNOT {
		t.Fatalf("ops %+v", ops)
	}
}

func TestTokenizeSearchQuerySkipsNOTOperands(t *testing.T) {
	t.Parallel()
	got := TokenizeSearchQuery("foo NOT barbaz OR qux")
	if len(got) != 2 || got[0] != "foo" || got[1] != "qux" {
		t.Fatalf("included words %+v", got)
	}
}

func TestSearchRequestORGoldenBytes(t *testing.T) {
	t.Parallel()
	packet := SearchRequest{Query: "foo OR bar"}
	var buf bytes.Buffer
	if err := packet.Put(&buf); err != nil {
		t.Fatal(err)
	}
	want := []byte{
		searchTypeString, 0x03, 0x00, 'f', 'o', 'o',
		searchTypeBool, searchBoolOR,
		searchTypeString, 0x03, 0x00, 'b', 'a', 'r',
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("got %x want %x", buf.Bytes(), want)
	}
	if packet.BytesCount() != len(want) {
		t.Fatalf("BytesCount %d want %d", packet.BytesCount(), len(want))
	}
}

func TestSearchRequestEncodesOR(t *testing.T) {
	t.Parallel()
	packet := SearchRequest{Query: "foo OR bar"}
	var buf bytes.Buffer
	if err := packet.Put(&buf); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	if !bytes.Contains(raw, []byte{searchTypeBool, searchBoolOR, searchTypeString}) {
		t.Fatalf("missing OR operator: %x", raw)
	}
	if bytes.Contains(raw, []byte{searchTypeBool, searchBoolAND, searchTypeString}) {
		t.Fatalf("unexpected AND in OR query: %x", raw)
	}
}

func TestSearchRequestEncodesNOTAndKeepsFilterAND(t *testing.T) {
	t.Parallel()
	packet := SearchRequest{Query: "foo NOT bar", FileType: "Audio"}
	var buf bytes.Buffer
	if err := packet.Put(&buf); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	if !bytes.Contains(raw, []byte{searchTypeBool, searchBoolNOT, searchTypeString}) {
		t.Fatalf("missing NOT: %x", raw)
	}
	if !bytes.Contains(raw, []byte{searchTypeBool, searchBoolAND}) {
		t.Fatalf("filter should still AND: %x", raw)
	}
}

func TestSearchRequestPlainQueryStillAND(t *testing.T) {
	t.Parallel()
	packet := SearchRequest{Query: "shake it off"}
	var buf bytes.Buffer
	if err := packet.Put(&buf); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	if !bytes.Contains(raw, []byte{searchTypeBool, searchBoolAND, searchTypeString}) {
		t.Fatalf("default AND missing: %x", raw)
	}
	if bytes.Contains(raw, []byte{searchTypeBool, searchBoolOR, searchTypeString}) {
		t.Fatalf("plain query should not OR: %x", raw)
	}
}
