package goed2k

import (
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/goed2k/core/disk"
	"github.com/goed2k/core/protocol"
)

func TestIsEmuleTempPartPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		path string
		ok   bool
	}{
		{"001.part", true},
		{filepath.Join("tmp", "999.part"), true},
		{"000.part", false},
		{"01.part", false},
		{"movie.part", false},
		{"001.part.met", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isEmuleTempPartPath(tc.path); got != tc.ok {
			t.Fatalf("%q got %v want %v", tc.path, got, tc.ok)
		}
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
	if err := os.WriteFile(src+".part.met", []byte("goed2k"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest, err := promoteEmulePartFile(src, dir, "movie.avi")
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
		t.Fatal("eMule sidecar should be gone")
	}
	if _, err := os.Stat(src + ".part.met"); !os.IsNotExist(err) {
		t.Fatal("goed2k sidecar should be gone")
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
	dest, err := promoteEmulePartFile(src, incoming, "clip.bin")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(dest) != "clip (1).bin" {
		t.Fatalf("collision dest %s", dest)
	}
	kept, err := os.ReadFile(exist)
	if err != nil || string(kept) != "other" {
		t.Fatalf("existing dest overwritten: %q", kept)
	}
}

func TestPromoteEmulePartFileSameSizeDoesNotOverwrite(t *testing.T) {
	tempDir := t.TempDir()
	incoming := t.TempDir()
	src := filepath.Join(tempDir, "003.part")
	if err := os.WriteFile(src, []byte("same-size"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(incoming, "done.bin"), []byte("same-size"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := promoteEmulePartFile(src, incoming, "done.bin")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "done (1).bin" {
		t.Fatalf("same-size dest must not be treated as crash retry, got %s", got)
	}
}

func TestPromoteEmulePartFileCopyFallback(t *testing.T) {
	old := renameCompletedFile
	renameCompletedFile = func(src, dest string) error {
		return &os.LinkError{Op: "rename", Old: src, New: dest, Err: errCrossDeviceLink}
	}
	t.Cleanup(func() { renameCompletedFile = old })
	dir := t.TempDir()
	src := filepath.Join(dir, "005.part")
	if err := os.WriteFile(src, []byte("copied"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest, err := promoteEmulePartFile(src, dir, "copied.bin")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(dest)
	if err != nil || string(raw) != "copied" {
		t.Fatalf("copied data %q err=%v", raw, err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("source should be removed after copy fallback")
	}
}

func TestPromoteEmulePartFileNonCrossDeviceRenameKeepsSource(t *testing.T) {
	old := renameCompletedFile
	renameCompletedFile = func(src, dest string) error { return os.ErrPermission }
	t.Cleanup(func() { renameCompletedFile = old })
	dir := t.TempDir()
	src := filepath.Join(dir, "006.part")
	if err := os.WriteFile(src, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := promoteEmulePartFile(src, dir, "keep.bin"); err == nil {
		t.Fatal("expected rename error")
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatal("source must remain recoverable")
	}
}

func waitDiskIdle(t *testing.T, session *Session) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		session.processDiskTasks()
		if session.diskTaskCount() == 0 {
			session.processDiskTasks()
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("disk tasks did not finish, count=%d", session.diskTaskCount())
}

func TestFinishedPromotesEmuleTempPartAfterRelease(t *testing.T) {
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
	if filepath.Base(handle.GetFilePath()) != "001.part" {
		t.Fatalf("must not rename before release, path %s", handle.GetFilePath())
	}
	waitDiskIdle(t, session)
	if filepath.Base(handle.GetFilePath()) != "tiny.bin" {
		t.Fatalf("path %s", handle.GetFilePath())
	}
	raw, err := os.ReadFile(handle.GetFilePath())
	if err != nil || string(raw) != string(payload) {
		t.Fatalf("promoted data %q err=%v", raw, err)
	}
	if sf := session.sharedStore.Get(protocol.EMule); sf == nil || filepath.Base(sf.Path) != "tiny.bin" {
		t.Fatalf("shared path %+v", sf)
	}
}

func TestFinishedSkipsPromoteWhenTempLayoutDisabled(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "001.part")
	payload := []byte("keep-part")
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	settings := NewSettings()
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
	waitDiskIdle(t, session)
	if handle.GetFilePath() != src {
		t.Fatalf("disabled layout must keep %s, got %s", src, handle.GetFilePath())
	}
}

func TestNewTransferPromotesFinishedEmulePart(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "007.part")
	payload := []byte("restored")
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	settings := NewSettings()
	settings.UseEmuleTempLayout = true
	settings.ListenPort = 0
	session := NewSession(settings)
	pieces := protocol.NewBitField(1)
	pieces.SetBit(0)
	handle, err := session.AddTransferParams(AddTransferParams{
		Hash:       protocol.EMule,
		CreateTime: CurrentTimeMillis(),
		Size:       int64(len(payload)),
		FilePath:   src,
		FinalName:  "restored.bin",
		Handler:    disk.NewDesktopFileHandler(src),
		ResumeData: &protocol.TransferResumeData{Pieces: pieces},
	})
	if err != nil {
		t.Fatal(err)
	}
	registerTransferFileCleanup(t, handle)
	if filepath.Base(handle.GetFilePath()) != "restored.bin" {
		t.Fatalf("restored finished part should promote, path %s", handle.GetFilePath())
	}
}

func TestOnReleaseFileDoesNotPromoteUnfinished(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "009.part")
	if err := os.WriteFile(src, []byte("partial"), 0o644); err != nil {
		t.Fatal(err)
	}
	settings := NewSettings()
	settings.UseEmuleTempLayout = true
	settings.ListenPort = 0
	session := NewSession(settings)
	handle, err := session.AddTransferParams(AddTransferParams{
		Hash:       protocol.EMule,
		CreateTime: CurrentTimeMillis(),
		Size:       PieceSize,
		FilePath:   src,
		FinalName:  "partial.bin",
		Handler:    disk.NewDesktopFileHandler(src),
	})
	if err != nil {
		t.Fatal(err)
	}
	registerTransferFileCleanup(t, handle)
	handle.transfer.OnReleaseFile(NoError, nil, false)
	if handle.GetFilePath() != src {
		t.Fatalf("unfinished release must keep part, got %s", handle.GetFilePath())
	}
}

func TestIsCrossDeviceRenameRejectsLinuxEexist(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("errno 17 在 Windows 上是 ERROR_NOT_SAME_DEVICE")
	}
	if isCrossDeviceRename(syscall.Errno(17)) {
		t.Fatal("Linux EEXIST(17) must not be treated as cross-device")
	}
}

func TestFinishedUsesIncomingDirWhenSet(t *testing.T) {
	tempDir := t.TempDir()
	incoming := t.TempDir()
	src := filepath.Join(tempDir, "008.part")
	payload := []byte("incoming-ok")
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	settings := NewSettings()
	settings.UseEmuleTempLayout = true
	settings.IncomingDir = incoming
	settings.ListenPort = 0
	session := NewSession(settings)
	handle, err := session.AddTransferParams(AddTransferParams{
		Hash:       protocol.EMule,
		CreateTime: CurrentTimeMillis(),
		Size:       int64(len(payload)),
		FilePath:   src,
		FinalName:  "final.bin",
		Handler:    disk.NewDesktopFileHandler(src),
	})
	if err != nil {
		t.Fatal(err)
	}
	registerTransferFileCleanup(t, handle)
	handle.transfer.picker.WeHave(0)
	handle.transfer.finished()
	waitDiskIdle(t, session)
	if filepath.Dir(handle.GetFilePath()) != incoming || filepath.Base(handle.GetFilePath()) != "final.bin" {
		t.Fatalf("incoming dest %s", handle.GetFilePath())
	}
}
