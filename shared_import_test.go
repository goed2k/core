package goed2k

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathIsUnderRoot(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "share")
	sub := filepath.Join(root, "nested", "f.txt")
	if err := os.MkdirAll(filepath.Dir(sub), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sub, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !pathIsUnderRoot(sub, root) {
		t.Fatal("sub file should be under root")
	}
	outside := filepath.Join(tmp, "other", "a.txt")
	if err := os.MkdirAll(filepath.Dir(outside), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if pathIsUnderRoot(outside, root) {
		t.Fatal("outside file should not be under root")
	}
}
