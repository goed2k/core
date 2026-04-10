package goed2k

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/goed2k/core/protocol"
)

// SharedStore 返回会话级共享库（非 nil）。
func (s *Session) SharedStore() *SharedStore {
	if s == nil {
		return nil
	}
	return s.sharedStore
}

// SharedFiles 返回共享文件快照（只读遍历）。
func (s *Session) SharedFiles() []*SharedFile {
	if s == nil || s.sharedStore == nil {
		return nil
	}
	return s.sharedStore.List()
}

// AddSharedDir 注册一个用于扫描的目录（去重）。
func (s *Session) AddSharedDir(path string) error {
	if s == nil {
		return NewError(InternalError)
	}
	abs, err := normalizeSharedPath(path)
	if err != nil {
		return err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return NewError(IllegalArgument)
	}
	s.mu.Lock()
	for _, existing := range s.sharedDirs {
		if strings.EqualFold(existing, abs) {
			s.mu.Unlock()
			return nil
		}
	}
	s.sharedDirs = append(s.sharedDirs, abs)
	s.mu.Unlock()
	// 扫描由 Client.AddSharedDir 异步触发，避免大目录阻塞 RPC 超时。
	return nil
}

// RemoveSharedDir 移除扫描目录，并删除共享库中路径落在该目录下的条目（与列表一致）。
func (s *Session) RemoveSharedDir(path string) error {
	abs, err := normalizeSharedPath(path)
	if err != nil {
		return err
	}
	s.mu.Lock()
	dst := s.sharedDirs[:0]
	removed := false
	for _, d := range s.sharedDirs {
		if strings.EqualFold(d, abs) {
			removed = true
			continue
		}
		dst = append(dst, d)
	}
	s.sharedDirs = dst
	s.mu.Unlock()
	if removed {
		s.pruneSharedFilesUnderRoot(abs)
	}
	return nil
}

func (s *Session) pruneSharedFilesUnderRoot(root string) {
	if s == nil || s.sharedStore == nil {
		return
	}
	list := s.sharedStore.List()
	var toRemove []protocol.Hash
	for _, sf := range list {
		if sf == nil {
			continue
		}
		if pathIsUnderRoot(sf.Path, root) {
			toRemove = append(toRemove, sf.Hash)
		}
	}
	for _, h := range toRemove {
		s.sharedStore.Remove(h)
	}
}

// ListSharedDirs 返回已注册的共享目录副本。
func (s *Session) ListSharedDirs() []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.sharedDirs))
	copy(out, s.sharedDirs)
	return out
}

// ImportSharedFile 计算 ed2k 哈希并将文件加入共享库。
func (s *Session) ImportSharedFile(path string) error {
	if s == nil {
		return NewError(InternalError)
	}
	abs, err := normalizeSharedPath(path)
	if err != nil {
		return err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return err
	}
	if fi.IsDir() || fi.Size() == 0 {
		return NewError(IllegalArgument)
	}
	root, size, pieces, err := ComputeEd2kFileMeta(abs)
	if err != nil {
		return err
	}
	sf := &SharedFile{
		Hash:        root,
		FileSize:    size,
		Path:        abs,
		Name:        filepath.Base(abs),
		PieceHashes: pieces,
		Origin:      SharedOriginImported,
		Completed:   true,
		LastHashAt:  CurrentTime(),
	}
	if !s.sharedStore.Add(sf) {
		return nil
	}
	return nil
}

// RescanSharedDirs 递归扫描已注册目录下的普通文件并导入（与 eMule 共享文件夹行为一致）。
func (s *Session) RescanSharedDirs() error {
	if s == nil {
		return NewError(InternalError)
	}
	dirs := s.ListSharedDirs()
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if isSkippableSharedFileName(d.Name()) {
				return nil
			}
			fi, err := d.Info()
			if err != nil || fi.Size() == 0 {
				return nil
			}
			_ = s.ImportSharedFile(path)
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// RemoveSharedFile 从共享库移除指定哈希。
func (s *Session) RemoveSharedFile(hash protocol.Hash) bool {
	if s == nil || s.sharedStore == nil {
		return false
	}
	return s.sharedStore.Remove(hash)
}

// attachIncomingSharedUpload 将仅上传连接绑定到共享文件（不写 Transfer 策略表）。
func (s *Session) attachIncomingSharedUpload(p *PeerConnection, sf *SharedFile) error {
	if s == nil || p == nil || sf == nil || !sf.CanUpload() {
		return NewError(IllegalArgument)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.connections {
		if c == p {
			p.SetUploadResource(sf)
			return nil
		}
	}
	s.connections = append(s.connections, p)
	p.SetUploadResource(sf)
	return nil
}

// tryAddCompletedTransferToSharedStore 下载完成后自动加入共享库（同 hash 不覆盖）。
func (s *Session) tryAddCompletedTransferToSharedStore(t *Transfer) {
	if s == nil || t == nil || s.sharedStore == nil {
		return
	}
	if !t.isFinishedForSharePublish() {
		return
	}
	path := t.GetFilePath()
	if path == "" {
		return
	}
	parts := t.UploadHashSet()
	if len(parts) == 0 {
		return
	}
	if s.sharedStore.Get(t.GetHash()) != nil {
		return
	}
	sf := &SharedFile{
		Hash:        t.GetHash(),
		FileSize:    t.Size(),
		Path:        path,
		Name:        filepath.Base(path),
		PieceHashes: append([]protocol.Hash(nil), parts...),
		Origin:      SharedOriginDownloaded,
		Completed:   true,
		LastHashAt:  CurrentTime(),
	}
	_ = s.sharedStore.Add(sf)
}
