package goed2k

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/goed2k/core/disk"
	"github.com/goed2k/core/protocol"
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

func TestResolveEmuleDownloadPathSeparatorOnlyName(t *testing.T) {
	dir := t.TempDir()
	path, cleanup, err := ResolveEmuleDownloadPath(NewSettings(), dir, "/")
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	if path == dir || filepath.Dir(path) != dir {
		t.Fatalf("separator-only name must stay inside outDir as a file, got %s", path)
	}
	if filepath.Base(path) != "_" {
		t.Fatalf("expected placeholder _, got %s", path)
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

func TestPromoteEmulePartFileRenamesToFinalName(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "001.part")
	if err := os.WriteFile(src, []byte("hello-part"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src+".met", []byte("met"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest, err := promoteEmulePartFile(src, dir, "movie.avi", int64(len("hello-part")))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(dest) != "movie.avi" {
		t.Fatalf("dest %s", dest)
	}
	raw, err := os.ReadFile(dest)
	if err != nil || string(raw) != "hello-part" {
		t.Fatalf("data %q err=%v", raw, err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("temp part should be gone")
	}
	if _, err := os.Stat(src + ".met"); !os.IsNotExist(err) {
		t.Fatal("sidecar should be gone")
	}
}

func TestPromoteEmulePartFileIncomingDirAndCollision(t *testing.T) {
	tempDir := t.TempDir()
	incoming := t.TempDir()
	src := filepath.Join(tempDir, "002.part")
	if err := os.WriteFile(src, []byte("payload-ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	exist := filepath.Join(incoming, "clip.bin")
	if err := os.WriteFile(exist, []byte("other"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest, err := promoteEmulePartFile(src, incoming, "clip.bin", int64(len("payload-ok")))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(dest) != "clip (1).bin" {
		t.Fatalf("collision dest %s", dest)
	}
	raw, err := os.ReadFile(dest)
	if err != nil || string(raw) != "payload-ok" {
		t.Fatalf("moved data %q", raw)
	}
}

func TestPromoteEmulePartFileCrashRetrySameSize(t *testing.T) {
	tempDir := t.TempDir()
	incoming := t.TempDir()
	src := filepath.Join(tempDir, "003.part")
	if err := os.WriteFile(src, []byte("same-size"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(incoming, "done.bin")
	if err := os.WriteFile(dest, []byte("same-size"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := promoteEmulePartFile(src, incoming, "done.bin", int64(len("same-size")))
	if err != nil {
		t.Fatal(err)
	}
	if got != dest {
		t.Fatalf("retry dest %s", got)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("stale part should be removed")
	}
}

func TestPromoteEmulePartFileRejectsUnsafeName(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "004.part")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest, err := promoteEmulePartFile(src, dir, "../escape.bin", 1)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(dest) != dir || filepath.Base(dest) != "escape.bin" {
		t.Fatalf("escaped dest %s", dest)
	}
}

func TestFinishedPromotesEmuleTempPart(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "001.part")
	payload := []byte("tiny-complete")
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	settings := NewSettings()
	settings.UseEmuleTempLayout = true
	settings.ListenPort = 0
	session := NewSession(settings)
	handle, err := session.AddTransferParams(AddTransferParams{
		Hash:       protocol.EMule,
		CreateTime: CurrentTimeMillis(),
		Size:       int64(len(payload)),
		FilePath:   src,
		FinalName:  "tiny.bin",
		Handler:    disk.NewDesktopFileHandler(src),
	})
	if err != nil {
		t.Fatal(err)
	}
	registerTransferFileCleanup(t, handle)
	handle.transfer.picker.WeHave(0)
	handle.transfer.finished()
	if filepath.Base(handle.GetFilePath()) != "tiny.bin" {
		t.Fatalf("path %s", handle.GetFilePath())
	}
	if handle.transfer.FileName() != "tiny.bin" {
		t.Fatalf("name %s", handle.transfer.FileName())
	}
	raw, err := os.ReadFile(handle.GetFilePath())
	if err != nil || string(raw) != string(payload) {
		t.Fatalf("promoted data %q err=%v", raw, err)
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
