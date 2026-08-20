package goed2k

import (
	"testing"

	"github.com/goed2k/core/protocol"
	kadproto "github.com/goed2k/core/protocol/kad"
	kadv6proto "github.com/goed2k/core/protocol/kadv6"
)

func TestMakeSearchResultFromKADV6UsesPublishTags(t *testing.T) {
	t.Parallel()
	hash := protocol.MustHashFromString("CFB72F36AE2B939C787EA9F64A9506B1")
	entry := kadv6proto.SearchEntry{
		ID:   kadv6proto.NewID(hash),
		Tags: kadv6TagsForSharedFile("demo.epub", 4096),
	}
	result := makeSearchResultFromKADV6(entry)
	if result.Hash != hash {
		t.Fatalf("hash %s", result.Hash)
	}
	if result.FileName != "demo.epub" || result.FileSize != 4096 {
		t.Fatalf("got name=%q size=%d", result.FileName, result.FileSize)
	}
	if result.Source != SearchResultKAD {
		t.Fatalf("source %d", result.Source)
	}
}

func TestApplyKADV6KeywordResultsMergesAndDedupsKad4(t *testing.T) {
	session := NewSession(NewSettings())
	params := SearchParams{Query: "barbaz", Scope: SearchScopeDHT}
	task := newSearchTask(1, params, CurrentTime())
	session.activeSearch = task

	hash := protocol.MustHashFromString("CFB72F36AE2B939C787EA9F64A9506B1")
	task.mergeResult(makeSearchResultFromKAD(kadproto.SearchEntry{
		ID: kadproto.NewID(hash),
		Tags: []kadproto.Tag{
			{Type: kadproto.TagTypeString, ID: protocol.FTFilename, String: "old-name.bin"},
			{Type: kadproto.TagTypeUint32, ID: protocol.FTFileSize, UInt64: 100},
			{Type: kadproto.TagTypeUint32, ID: protocol.FTSources, UInt64: 2},
		},
	}))

	session.applyKADV6KeywordResults(task, params, []kadv6proto.SearchEntry{{
		ID: kadv6proto.NewID(hash),
		Tags: []kadv6proto.Tag{
			{Type: kadv6proto.TagTypeString, ID: kadv6proto.TagName, String: "old-name.bin"},
			{Type: kadv6proto.TagTypeUint64, ID: kadv6proto.TagFileSize, UInt64: 100},
			{Type: kadv6proto.TagTypeUint32, ID: protocol.FTSources, UInt64: 7},
		},
	}})

	snap := task.snapshot()
	if len(snap.Results) != 1 {
		t.Fatalf("expected 1 merged result, got %d", len(snap.Results))
	}
	if snap.Results[0].Sources != 7 {
		t.Fatalf("expected max sources 7, got %d", snap.Results[0].Sources)
	}
	if snap.Results[0].Source&SearchResultKAD == 0 {
		t.Fatalf("expected KAD source bit, got %d", snap.Results[0].Source)
	}
}

func TestApplyKADV6KeywordResultsHonorsMaxSizeFilter(t *testing.T) {
	session := NewSession(NewSettings())
	params := SearchParams{Query: "barbaz", Scope: SearchScopeDHT, MaxSize: 10}
	task := newSearchTask(2, params, CurrentTime())
	session.activeSearch = task
	session.applyKADV6KeywordResults(task, params, []kadv6proto.SearchEntry{{
		ID:   kadv6proto.NewID(protocol.MustHashFromString("CFB72F36AE2B939C787EA9F64A9506B1")),
		Tags: kadv6TagsForSharedFile("too-big.bin", 4096),
	}})
	if got := len(task.snapshot().Results); got != 0 {
		t.Fatalf("oversized KADV6 hit should be filtered, got %d", got)
	}
}

func TestStartDHTSearchWithoutTrackersFailsClosed(t *testing.T) {
	session := NewSession(NewSettings())
	handle, err := session.StartSearch(SearchParams{Query: "barbaz", Scope: SearchScopeDHT})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if handle.Snapshot().State != SearchStateFailed {
		t.Fatalf("expected failed search without backends, got %s", handle.Snapshot().State)
	}
}

func TestStartKADV6KeywordSearchNoNodesDoesNotStayBusy(t *testing.T) {
	session := NewSession(NewSettings())
	session.dhtv6Tracker = NewKADV6Tracker(0, 0)
	params := SearchParams{Query: "barbaz", Scope: SearchScopeDHT}
	task := newSearchTask(3, params, CurrentTime())
	session.activeSearch = task
	keywordHash, err := protocol.HashFromData([]byte("barbaz"))
	if err != nil {
		t.Fatal(err)
	}
	if session.startKADV6KeywordSearch(task, params, keywordHash) {
		t.Fatal("empty tracker should not start a live search")
	}
	if task.dhtBusy || task.dhtPending != 0 {
		t.Fatalf("failed KADV6 start left busy pending=%d", task.dhtPending)
	}
}

func TestSearchTaskDHTPendingWaitsForBothBackends(t *testing.T) {
	task := newSearchTask(4, SearchParams{Query: "barbaz"}, CurrentTime())
	task.beginDHT()
	task.beginDHT()
	task.finishDHT()
	if !task.dhtBusy || task.state != SearchStateRunning {
		t.Fatalf("first finish should keep DHT busy, state=%s busy=%v", task.state, task.dhtBusy)
	}
	task.finishDHT()
	if task.dhtBusy || task.state != SearchStateFinished {
		t.Fatalf("second finish should complete search, state=%s busy=%v", task.state, task.dhtBusy)
	}
}
