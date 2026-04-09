package goed2k

import (
	"testing"

	"github.com/monkeyWie/goed2k/protocol"
)

func TestSharedStoreAddDedupe(t *testing.T) {
	st := NewSharedStore()
	h := protocol.MustHashFromString("31D6CFE0D16AE931B73C59D7E0C089C0")
	a := &SharedFile{Hash: h, FileSize: 100, Completed: true}
	if !st.Add(a) {
		t.Fatalf("first add should succeed")
	}
	b := &SharedFile{Hash: h, FileSize: 100, Completed: true}
	if st.Add(b) {
		t.Fatalf("duplicate hash should not add")
	}
	if st.Len() != 1 {
		t.Fatalf("len=%d", st.Len())
	}
}

func TestSharedStoreRemove(t *testing.T) {
	st := NewSharedStore()
	h1 := protocol.MustHashFromString("31D6CFE0D16AE931B73C59D7E0C089C0")
	h2 := protocol.MustHashFromString("23A8CEFF57A7A32D562D649ED7893796")
	st.Add(&SharedFile{Hash: h1, FileSize: 1, Completed: true})
	st.Add(&SharedFile{Hash: h2, FileSize: 1, Completed: true})
	if !st.Remove(h1) {
		t.Fatal("remove existing")
	}
	if st.Get(h1) != nil {
		t.Fatal("still present")
	}
	if st.Len() != 1 {
		t.Fatalf("len=%d", st.Len())
	}
}
