package goed2k

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAllocEmuleTempPartSlot(t *testing.T) {
	dir := t.TempDir()
	slot, path, err := AllocEmuleTempPartSlot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if slot != 1 || filepath.Base(path) != "001.part" {
		t.Fatalf("unexpected slot %d path %s", slot, path)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	slot2, path2, err := AllocEmuleTempPartSlot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if slot2 != 2 || filepath.Base(path2) != "002.part" {
		t.Fatalf("expected slot 2, got %d %s", slot2, path2)
	}
}

func TestResolveEmuleDownloadPathDefault(t *testing.T) {
	dir := t.TempDir()
	path, cleanup, err := ResolveEmuleDownloadPath(NewSettings(), dir, "movie.avi")
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	if filepath.Base(path) != "movie.avi" {
		t.Fatalf("unexpected path %s", path)
	}
}

func TestResolveEmuleDownloadPathTempLayout(t *testing.T) {
	dir := t.TempDir()
	st := NewSettings()
	st.UseEmuleTempLayout = true
	path, cleanup, err := ResolveEmuleDownloadPath(st, dir, "movie.avi")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if filepath.Base(path) != "001.part" {
		t.Fatalf("expected 001.part, got %s", path)
	}
}

func TestResolveEmuleDownloadPathTempLayoutIgnoresUnsafeName(t *testing.T) {
	dir := t.TempDir()
	st := NewSettings()
	st.UseEmuleTempLayout = true
	path, cleanup, err := ResolveEmuleDownloadPath(st, dir, `../CON:evil<>.txt`)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if filepath.Base(path) != "001.part" {
		t.Fatalf("temp layout should keep NNN.part, got %s", path)
	}
}

func TestResolveEmuleDownloadPathSanitizesTraversal(t *testing.T) {
	dir := t.TempDir()
	path, cleanup, err := ResolveEmuleDownloadPath(NewSettings(), dir, "../etc/passwd")
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	if filepath.Dir(path) != dir {
		t.Fatalf("escaped output dir: %s", path)
	}
	if filepath.Base(path) != "passwd" {
		t.Fatalf("unexpected base %s", path)
	}
}

func TestResolveEmuleDownloadPathSanitizesWindowsReservedWhenOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("保留名清洗只在 Windows 上生效")
	}
	dir := t.TempDir()
	path, cleanup, err := ResolveEmuleDownloadPath(NewSettings(), dir, "CON.txt")
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	if filepath.Base(path) != "CON_.txt" {
		t.Fatalf("expected CON_.txt, got %s", path)
	}
}

func TestResolveEmuleDownloadPathKeepsUnixReservedName(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("CON 作为普通名仅在非 Windows 上保留")
	}
	dir := t.TempDir()
	path, cleanup, err := ResolveEmuleDownloadPath(NewSettings(), dir, "CON")
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	if filepath.Base(path) != "CON" {
		t.Fatalf("unix legal reserved-looking name changed: %s", path)
	}
}

func TestResolveEmuleDownloadPathKeepsUnixColon(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix 合法冒号仅在非 Windows 上保留")
	}
	dir := t.TempDir()
	path, cleanup, err := ResolveEmuleDownloadPath(NewSettings(), dir, "foo:bar.bin")
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	if filepath.Base(path) != "foo:bar.bin" {
		t.Fatalf("unix legal name changed: %s", path)
	}
}
