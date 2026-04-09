package goed2k

import (
	"sort"
	"sync"

	"github.com/monkeyWie/goed2k/protocol"
)

// SharedStore 内存中的共享文件索引（按 hash 去重）。
type SharedStore struct {
	mu     sync.RWMutex
	byHash map[protocol.Hash]*SharedFile
	order  []protocol.Hash
}

func NewSharedStore() *SharedStore {
	return &SharedStore{
		byHash: make(map[protocol.Hash]*SharedFile),
		order:  make([]protocol.Hash, 0),
	}
}

// Add 添加共享文件；若 hash 已存在则返回 false 且不覆盖。
func (st *SharedStore) Add(f *SharedFile) bool {
	if st == nil || f == nil || f.Hash.Equal(protocol.Invalid) {
		return false
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, ok := st.byHash[f.Hash]; ok {
		return false
	}
	st.byHash[f.Hash] = f
	st.order = append(st.order, f.Hash)
	return true
}

// Remove 按 hash 删除。
func (st *SharedStore) Remove(hash protocol.Hash) bool {
	if st == nil || hash.Equal(protocol.Invalid) {
		return false
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if _, ok := st.byHash[hash]; !ok {
		return false
	}
	delete(st.byHash, hash)
	dst := st.order[:0]
	for _, h := range st.order {
		if h.Equal(hash) {
			continue
		}
		dst = append(dst, h)
	}
	st.order = dst
	return true
}

// Get 按 hash 查询。
func (st *SharedStore) Get(hash protocol.Hash) *SharedFile {
	if st == nil {
		return nil
	}
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.byHash[hash]
}

// List 返回当前共享文件列表（稳定排序）。
func (st *SharedStore) List() []*SharedFile {
	if st == nil {
		return nil
	}
	st.mu.RLock()
	defer st.mu.RUnlock()
	out := make([]*SharedFile, 0, len(st.order))
	for _, h := range st.order {
		if f := st.byHash[h]; f != nil {
			out = append(out, f)
		}
	}
	return out
}

// Len 条目数量。
func (st *SharedStore) Len() int {
	if st == nil {
		return 0
	}
	st.mu.RLock()
	defer st.mu.RUnlock()
	return len(st.byHash)
}

// ReplaceAll 用快照替换整个存储（用于从磁盘恢复）。
func (st *SharedStore) ReplaceAll(files []*SharedFile) {
	if st == nil {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	st.byHash = make(map[protocol.Hash]*SharedFile, len(files))
	st.order = st.order[:0]
	seen := make(map[protocol.Hash]struct{}, len(files))
	for _, f := range files {
		if f == nil || f.Hash.Equal(protocol.Invalid) {
			continue
		}
		if _, ok := seen[f.Hash]; ok {
			continue
		}
		seen[f.Hash] = struct{}{}
		st.byHash[f.Hash] = f
		st.order = append(st.order, f.Hash)
	}
	sort.Slice(st.order, func(i, j int) bool {
		return st.order[i].String() < st.order[j].String()
	})
}
