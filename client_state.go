package goed2k

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/goed2k/core/data"
	"github.com/goed2k/core/disk"
	"github.com/goed2k/core/protocol"
)

const clientStateVersion = 9

// 本仓库从未发布过独立的状态 schema 5/6（从 4 直接跳到 7）。
// 它们与 v4–v7 同为可叠加 JSON，按当前版本兼容加载。
// v8 及更早的 DownloadedBlocks 按 190 KiB 索引，加载时重映射到 180 KiB。
const legacyDownloadBlockSize190 = 190 * 1024

type ClientCategoryState struct {
	Name          string `json:"name"`
	OutputDir     string `json:"output_dir"`
	AutoExtension string `json:"auto_extension"`
}

type ClientStateStore interface {
	Load() (*ClientState, error)
	Save(state *ClientState) error
}

type ClientState struct {
	Version         int                     `json:"version"`
	ServerAddress   string                  `json:"server_address,omitempty"`
	IdentityVersion int                     `json:"identity_version,omitempty"`
	IdentityKeyPath string                  `json:"identity_key_path,omitempty"`
	Transfers       []ClientTransferState   `json:"transfers"`
	Credits         []ClientCreditState     `json:"credits,omitempty"`
	FriendSlots     []protocol.Hash         `json:"friend_slots,omitempty"`
	DHT             *ClientDHTState         `json:"dht,omitempty"`
	DHTv6           *ClientDHTv6State       `json:"dhtv6,omitempty"`
	SharedDirs      []string                `json:"shared_dirs,omitempty"`
	SharedFiles     []ClientSharedFileState `json:"shared_files,omitempty"`
	BannedPeers     []protocol.Endpoint     `json:"banned_peers,omitempty"`
	Categories      []ClientCategoryState   `json:"categories,omitempty"`
	// Settings 是可持久化的运行策略。旧版本无此字段时保持进程内默认值。
	Settings *ClientSettingsState `json:"settings,omitempty"`
}

// ClientSettingsState 是 Settings 中会跨重启保留的策略子集。
// 刻意不持久化：Logger、UserAgent、Mod/协议版本、端口与 DHT 开关（由 bootstrap/CLI 或 DHT 快照拥有）、
// 连接池/超时等过程调优字段。
type ClientSettingsState struct {
	UseEmuleTempLayout      bool   `json:"use_emule_temp_layout"`
	PartialKadPublish       bool   `json:"partial_kad_publish"`
	PreallocateDiskSpace    bool   `json:"preallocate_disk_space"`
	UseSparseFiles          bool   `json:"use_sparse_files"`
	EnableWebDownload       bool   `json:"enable_web_download"`
	MaxHttpSources          int    `json:"max_http_sources"`
	MaxConcurrentHttpBlocks int    `json:"max_concurrent_http_blocks"`
	WebCacheDir             string `json:"web_cache_dir,omitempty"`
	HttpRequestTimeoutSec   int    `json:"http_request_timeout_sec"`
	MaxDownloadRateKB       int    `json:"max_download_rate_kb"`
	MaxUploadRateKB         int    `json:"max_upload_rate_kb"`
}

// ClientSharedFileState 持久化的共享文件元数据。
type ClientSharedFileState struct {
	Hash        protocol.Hash   `json:"hash"`
	Size        int64           `json:"size"`
	Path        string          `json:"path"`
	Name        string          `json:"name"`
	PieceHashes []protocol.Hash `json:"piece_hashes,omitempty"`
	Origin      SharedOrigin    `json:"origin"`
	Completed   bool            `json:"completed"`
	LastHashAt  int64           `json:"last_hash_at,omitempty"`
}

type ClientTransferState struct {
	Hash         protocol.Hash                `json:"hash"`
	Size         int64                        `json:"size"`
	CreateTime   int64                        `json:"create_time"`
	TargetPath   string                       `json:"target_path"`
	FinalName    string                       `json:"final_name,omitempty"`
	Paused       bool                         `json:"paused"`
	UploadPrio   UploadPriority               `json:"upload_prio,omitempty"`
	DownloadPrio TransferPriority             `json:"download_prio,omitempty"`
	ResumeData   *protocol.TransferResumeData `json:"resume_data,omitempty"`
	HttpSources  []string                     `json:"http_sources,omitempty"`
}

type ClientDHTState struct {
	SelfID              protocol.Hash        `json:"self_id,omitempty"`
	Firewalled          bool                 `json:"firewalled"`
	LastBootstrap       int64                `json:"last_bootstrap,omitempty"`
	LastRefresh         int64                `json:"last_refresh,omitempty"`
	LastFirewalledCheck int64                `json:"last_firewalled_check,omitempty"`
	StoragePoint        string               `json:"storage_point,omitempty"`
	Nodes               []ClientDHTNodeState `json:"nodes,omitempty"`
	RouterNodes         []string             `json:"router_nodes,omitempty"`
}

type ClientDHTNodeState struct {
	ID        protocol.Hash `json:"id,omitempty"`
	Addr      string        `json:"addr"`
	TCPPort   uint16        `json:"tcp_port,omitempty"`
	Version   byte          `json:"version,omitempty"`
	Seed      bool          `json:"seed,omitempty"`
	HelloSent bool          `json:"hello_sent,omitempty"`
	Pinged    bool          `json:"pinged,omitempty"`
	FailCount int           `json:"fail_count,omitempty"`
	FirstSeen int64         `json:"first_seen,omitempty"`
	LastSeen  int64         `json:"last_seen,omitempty"`
}

type FileClientStateStore struct {
	path string
}

func NewFileClientStateStore(path string) *FileClientStateStore {
	return &FileClientStateStore{path: path}
}

func (s *FileClientStateStore) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

func (s *FileClientStateStore) Load() (*ClientState, error) {
	if s == nil || s.path == "" {
		return nil, errors.New("state path is empty")
	}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}
	var state ClientState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, err
	}
	if err := migrateClientState(&state); err != nil {
		return nil, err
	}
	return &state, nil
}

func (s *FileClientStateStore) Save(state *ClientState) error {
	if s == nil || s.path == "" {
		return errors.New("state path is empty")
	}
	if state == nil {
		state = &ClientState{Version: clientStateVersion}
	}
	if state.Version == 0 {
		state.Version = clientStateVersion
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, raw, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func (c *Client) SetStateStore(store ClientStateStore) {
	c.stateStore = store
}

func (c *Client) StateStore() ClientStateStore {
	return c.stateStore
}

func (c *Client) SetStatePath(path string) {
	if path == "" {
		c.stateStore = nil
		return
	}
	c.stateStore = NewFileClientStateStore(path)
}

func (c *Client) StatePath() string {
	fileStore, ok := c.stateStore.(*FileClientStateStore)
	if !ok || fileStore == nil {
		return ""
	}
	return fileStore.Path()
}

func (c *Client) SaveState(path string) error {
	if path != "" {
		c.SetStatePath(path)
	}
	if c.stateStore == nil {
		return errors.New("state store is not configured")
	}
	state, err := c.snapshotState()
	if err != nil {
		return err
	}
	return c.stateStore.Save(state)
}

func (c *Client) LoadState(path string) error {
	if path != "" {
		c.SetStatePath(path)
	}
	if c.stateStore == nil {
		return errors.New("state store is not configured")
	}
	state, err := c.stateStore.Load()
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return c.applyState(state)
}

func (c *Client) snapshotState() (*ClientState, error) {
	handles := c.session.GetTransfers()
	sort.Slice(handles, func(i, j int) bool {
		return handles[i].GetHash().String() < handles[j].GetHash().String()
	})
	persistable := persistableSettingsFrom(c.session.settings)
	state := &ClientState{
		Version:         clientStateVersion,
		ServerAddress:   c.serverAddr,
		IdentityVersion: 0,
		IdentityKeyPath: c.session.settings.IdentityKeyPath,
		Settings:        &persistable,
		Transfers:       make([]ClientTransferState, 0, len(handles)),
		Credits:         c.session.Credits().Snapshot(),
		FriendSlots:     c.session.friendSlotSnapshot(),
		BannedPeers:     c.session.snapshotBannedPeers(),
	}
	if len(c.session.settings.Categories) > 0 {
		state.Categories = make([]ClientCategoryState, 0, len(c.session.settings.Categories))
		for _, cat := range c.session.settings.Categories {
			state.Categories = append(state.Categories, ClientCategoryState{
				Name:          cat.Name,
				OutputDir:     cat.OutputDir,
				AutoExtension: cat.AutoExtension,
			})
		}
	}
	if id := c.session.Identity(); id != nil {
		state.IdentityVersion = id.Version
		if id.KeyPath() != "" {
			state.IdentityKeyPath = id.KeyPath()
		}
	}
	if tracker := c.GetDHTTracker(); tracker != nil {
		state.DHT = tracker.SnapshotState()
	}
	if tracker := c.GetDHTv6Tracker(); tracker != nil {
		state.DHTv6 = tracker.SnapshotState()
	}
	state.SharedDirs = c.session.ListSharedDirs()
	for _, sf := range c.session.SharedFiles() {
		if sf == nil {
			continue
		}
		state.SharedFiles = append(state.SharedFiles, ClientSharedFileState{
			Hash:        sf.Hash,
			Size:        sf.FileSize,
			Path:        sf.Path,
			Name:        sf.Name,
			PieceHashes: append([]protocol.Hash(nil), sf.PieceHashes...),
			Origin:      sf.Origin,
			Completed:   sf.Completed,
			LastHashAt:  sf.LastHashAt,
		})
	}
	for _, handle := range handles {
		if !handle.IsValid() {
			continue
		}
		path := handle.GetFilePath()
		if path == "" {
			continue
		}
		state.Transfers = append(state.Transfers, ClientTransferState{
			Hash:         handle.GetHash(),
			Size:         handle.GetSize(),
			CreateTime:   handle.GetCreateTime(),
			TargetPath:   path,
			FinalName:    handle.transfer.finalName,
			Paused:       handle.IsPaused(),
			UploadPrio:   handle.transfer.UploadPriority(),
			DownloadPrio: handle.transfer.DownloadPriority(),
			ResumeData:   handle.GetResumeData(),
			HttpSources:  handle.transfer.HttpSources(),
		})
	}
	return state, nil
}

func (c *Client) applyState(state *ClientState) error {
	if state == nil {
		return nil
	}
	if err := migrateClientState(state); err != nil {
		return err
	}
	if state.Settings != nil {
		c.applyPersistableSettingsLive(*state.Settings)
	}
	c.serverAddr = state.ServerAddress
	if state.IdentityKeyPath != "" {
		c.session.settings.IdentityKeyPath = state.IdentityKeyPath
		if err := c.session.LoadIdentity(state.IdentityKeyPath); err != nil {
			return fmt.Errorf("load identity from %q: %w", state.IdentityKeyPath, err)
		}
	} else if state.IdentityVersion != 0 {
		if id := c.session.Identity(); id != nil {
			id.Version = state.IdentityVersion
		}
	}
	if len(state.Categories) > 0 {
		cats := make([]Category, 0, len(state.Categories))
		for _, rec := range state.Categories {
			cats = append(cats, Category{
				Name:          rec.Name,
				OutputDir:     rec.OutputDir,
				AutoExtension: rec.AutoExtension,
			})
		}
		c.session.settings.Categories = cats
	}
	c.session.Credits().ApplySnapshot(state.Credits)
	c.session.applyFriendSlotSnapshot(state.FriendSlots)
	if state.DHT != nil {
		if err := c.EnableDHT().ApplyState(state.DHT); err != nil {
			return err
		}
	}
	if state.DHTv6 != nil {
		if err := c.EnableDHTv6().ApplyState(state.DHTv6); err != nil {
			return err
		}
	}
	c.session.mu.Lock()
	c.session.sharedDirs = make([]string, 0, len(state.SharedDirs))
	for _, d := range state.SharedDirs {
		if nd, err := normalizeSharedPath(d); err == nil {
			c.session.sharedDirs = append(c.session.sharedDirs, nd)
		}
	}
	c.session.mu.Unlock()
	restored := make([]*SharedFile, 0, len(state.SharedFiles))
	for _, rec := range state.SharedFiles {
		sf := &SharedFile{
			Hash:        rec.Hash,
			FileSize:    rec.Size,
			Path:        rec.Path,
			Name:        rec.Name,
			PieceHashes: append([]protocol.Hash(nil), rec.PieceHashes...),
			Origin:      rec.Origin,
			Completed:   rec.Completed,
			LastHashAt:  rec.LastHashAt,
		}
		if !validateSharedFileOnDisk(sf) {
			continue
		}
		restored = append(restored, sf)
	}
	c.session.sharedStore.ReplaceAll(restored)
	c.session.applyBannedPeers(state.BannedPeers)

	for _, record := range state.Transfers {
		if record.TargetPath == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(record.TargetPath), 0o755); err != nil {
			return err
		}
		atp := AddTransferParams{
			Hash:        record.Hash,
			CreateTime:  record.CreateTime,
			Size:        record.Size,
			FilePath:    record.TargetPath,
			FinalName:   record.FinalName,
			Paused:      record.Paused,
			ResumeData:  cloneResumeData(record.ResumeData),
			Handler:     disk.NewDesktopFileHandler(record.TargetPath),
			HttpSources: append([]string(nil), record.HttpSources...),
		}
		handle, err := c.session.AddTransferParams(atp)
		if err != nil {
			return err
		}
		if handle.IsValid() {
			handle.transfer.SetUploadPriority(record.UploadPrio)
			handle.transfer.SetDownloadPriority(record.DownloadPrio)
		}
	}
	return nil
}

func migrateClientState(state *ClientState) error {
	if state == nil {
		return nil
	}
	if state.Version < 0 || state.Version > clientStateVersion {
		return fmt.Errorf("unsupported state version %d (accepted 0–%d; versions 5 and 6 were never shipped as distinct schemas and migrate as v7-compatible)", state.Version, clientStateVersion)
	}
	if state.Version < 9 {
		for i := range state.Transfers {
			if state.Transfers[i].ResumeData == nil {
				continue
			}
			state.Transfers[i].ResumeData.DownloadedBlocks = remapDownloadedBlocks(
				state.Transfers[i].ResumeData.DownloadedBlocks,
				state.Transfers[i].Size,
				legacyDownloadBlockSize190,
				state.Transfers[i].ResumeData.Pieces,
			)
		}
	}
	state.Version = clientStateVersion
	return nil
}

type byteSpan struct {
	begin int64
	end   int64
}

// remapDownloadedBlocks 把旧块索引按字节区间并集后，只保留新粒度下被完整覆盖的块。
// 已完成 piece 由位图单独保存，对应块索引直接丢弃。未完整覆盖的新块丢弃并重下。
func remapDownloadedBlocks(blocks []data.PieceBlock, fileSize, fromBlockSize int64, completed protocol.BitField) []data.PieceBlock {
	if len(blocks) == 0 || fromBlockSize <= 0 || fromBlockSize == BlockSize {
		return blocks
	}
	byPiece := make(map[int][]byteSpan)
	for _, old := range blocks {
		if completed.GetBit(old.PieceIndex) {
			continue
		}
		begin := int64(old.PieceIndex)*PieceSize + int64(old.PieceBlock)*fromBlockSize
		end := begin + fromBlockSize
		pieceEnd := int64(old.PieceIndex+1) * PieceSize
		if end > pieceEnd {
			end = pieceEnd
		}
		if fileSize > 0 && end > fileSize {
			end = fileSize
		}
		if fileSize > 0 && begin >= fileSize {
			continue
		}
		if end <= begin {
			continue
		}
		byPiece[old.PieceIndex] = append(byPiece[old.PieceIndex], byteSpan{begin, end})
	}
	out := make([]data.PieceBlock, 0, len(blocks))
	pieces := make([]int, 0, len(byPiece))
	for p := range byPiece {
		pieces = append(pieces, p)
	}
	sort.Ints(pieces)
	for _, piece := range pieces {
		if completed.GetBit(piece) {
			continue
		}
		merged := mergeByteSpans(byPiece[piece])
		count := BlocksPerPiece
		if fileSize > 0 {
			pieceLen := fileSize - int64(piece)*PieceSize
			if pieceLen <= 0 {
				continue
			}
			if pieceLen < PieceSize {
				count = int(DivCeil(pieceLen, BlockSize))
			}
		}
		for j := 0; j < count; j++ {
			nb := data.NewPieceBlock(piece, j)
			r := nb.Range(fileSize)
			if r.Right <= r.Left {
				continue
			}
			if spanFullyCovered(merged, r.Left, r.Right) {
				out = append(out, nb)
			}
		}
	}
	return out
}

func mergeByteSpans(in []byteSpan) []byteSpan {
	if len(in) == 0 {
		return nil
	}
	sort.Slice(in, func(i, j int) bool {
		if in[i].begin == in[j].begin {
			return in[i].end < in[j].end
		}
		return in[i].begin < in[j].begin
	})
	out := []byteSpan{in[0]}
	for _, s := range in[1:] {
		last := &out[len(out)-1]
		if s.begin <= last.end {
			if s.end > last.end {
				last.end = s.end
			}
			continue
		}
		out = append(out, s)
	}
	return out
}

func spanFullyCovered(spans []byteSpan, begin, end int64) bool {
	for _, s := range spans {
		if s.begin <= begin && s.end >= end {
			return true
		}
	}
	return false
}

func persistableSettingsFrom(s Settings) ClientSettingsState {
	return ClientSettingsState{
		UseEmuleTempLayout:      s.UseEmuleTempLayout,
		PartialKadPublish:       s.PartialKadPublish,
		PreallocateDiskSpace:    s.PreallocateDiskSpace,
		UseSparseFiles:          s.UseSparseFiles,
		EnableWebDownload:       s.EnableWebDownload,
		MaxHttpSources:          s.MaxHttpSources,
		MaxConcurrentHttpBlocks: s.MaxConcurrentHttpBlocks,
		WebCacheDir:             s.WebCacheDir,
		HttpRequestTimeoutSec:   s.HttpRequestTimeoutSec,
		MaxDownloadRateKB:       s.MaxDownloadRateKB,
		MaxUploadRateKB:         s.MaxUploadRateKB,
	}
}

func applyPersistableSettings(dst *Settings, src ClientSettingsState) {
	if dst == nil {
		return
	}
	dst.UseEmuleTempLayout = src.UseEmuleTempLayout
	dst.PartialKadPublish = src.PartialKadPublish
	dst.PreallocateDiskSpace = src.PreallocateDiskSpace
	dst.UseSparseFiles = src.UseSparseFiles
	dst.EnableWebDownload = src.EnableWebDownload
	dst.MaxHttpSources = src.MaxHttpSources
	dst.MaxConcurrentHttpBlocks = src.MaxConcurrentHttpBlocks
	dst.WebCacheDir = src.WebCacheDir
	dst.HttpRequestTimeoutSec = src.HttpRequestTimeoutSec
	dst.MaxDownloadRateKB = src.MaxDownloadRateKB
	dst.MaxUploadRateKB = src.MaxUploadRateKB
}

// PersistableSettings 返回当前会写入 state 的策略子集。
func (c *Client) PersistableSettings() ClientSettingsState {
	if c == nil || c.session == nil {
		return ClientSettingsState{}
	}
	return persistableSettingsFrom(c.session.settings)
}

// OverlayPersistableSettings 用 src 覆盖当前可持久化策略（bootstrap/CLI 在 LoadState 之后调用，保证进程配置胜出）。
func (c *Client) OverlayPersistableSettings(src Settings) {
	if c == nil || c.session == nil {
		return
	}
	c.applyPersistableSettingsLive(persistableSettingsFrom(src))
}

func (c *Client) applyPersistableSettingsLive(src ClientSettingsState) {
	applyPersistableSettings(&c.session.settings, src)
	c.session.ConfigureSession(c.session.settings)
}

func cloneResumeData(src *protocol.TransferResumeData) *protocol.TransferResumeData {
	if src == nil {
		return nil
	}
	dst := &protocol.TransferResumeData{
		Hashes:           append([]protocol.Hash(nil), src.Hashes...),
		Pieces:           protocol.NewBitField(src.Pieces.Len()),
		DownloadedBlocks: append([]data.PieceBlock(nil), src.DownloadedBlocks...),
		Peers:            append([]protocol.Endpoint(nil), src.Peers...),
	}
	for i := 0; i < src.Pieces.Len(); i++ {
		if src.Pieces.GetBit(i) {
			dst.Pieces.SetBit(i)
		}
	}
	return dst
}
