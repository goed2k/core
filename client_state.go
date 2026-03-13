package goed2k

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/monkeyWie/goed2k/data"
	"github.com/monkeyWie/goed2k/disk"
	"github.com/monkeyWie/goed2k/protocol"
)

const clientStateVersion = 2

type ClientStateStore interface {
	Load() (*ClientState, error)
	Save(state *ClientState) error
}

type ClientState struct {
	Version       int
	ServerAddress string
	Transfers     []ClientTransferState
	Credits       []ClientCreditState
	FriendSlots   []protocol.Hash
	DHT           *ClientDHTState
}

type ClientTransferState struct {
	Hash       protocol.Hash
	Size       int64
	CreateTime int64
	TargetPath string
	Paused     bool
	UploadPrio UploadPriority
	ResumeData *protocol.TransferResumeData
}

type ClientDHTState struct {
	SelfID              protocol.Hash
	Firewalled          bool
	LastBootstrap       int64
	LastRefresh         int64
	LastFirewalledCheck int64
	StoragePoint        string
	Nodes               []ClientDHTNodeState
	RouterNodes         []string
}

type ClientDHTNodeState struct {
	ID        protocol.Hash
	Addr      string
	TCPPort   uint16
	Version   byte
	Seed      bool
	HelloSent bool
	Pinged    bool
	FailCount int
	FirstSeen int64
	LastSeen  int64
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
	var wire clientStateWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, err
	}
	return wire.toRuntime()
}

func (s *FileClientStateStore) Save(state *ClientState) error {
	if s == nil || s.path == "" {
		return errors.New("state path is empty")
	}
	wire, err := newClientStateWire(state)
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(wire, "", "  ")
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
	state := &ClientState{
		Version:       clientStateVersion,
		ServerAddress: c.serverAddr,
		Transfers:     make([]ClientTransferState, 0, len(handles)),
		Credits:       c.session.Credits().Snapshot(),
		FriendSlots:   c.session.friendSlotSnapshot(),
	}
	if tracker := c.GetDHTTracker(); tracker != nil {
		state.DHT = tracker.SnapshotState()
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
			Hash:       handle.GetHash(),
			Size:       handle.GetSize(),
			CreateTime: handle.GetCreateTime(),
			TargetPath: path,
			Paused:     handle.IsPaused(),
			UploadPrio: handle.transfer.UploadPriority(),
			ResumeData: handle.GetResumeData(),
		})
	}
	return state, nil
}

func (c *Client) applyState(state *ClientState) error {
	if state == nil {
		return nil
	}
	if state.Version != 0 && state.Version != 1 && state.Version != clientStateVersion {
		return errors.New("unsupported state version")
	}
	c.serverAddr = state.ServerAddress
	c.session.Credits().ApplySnapshot(state.Credits)
	c.session.applyFriendSlotSnapshot(state.FriendSlots)
	if state.DHT != nil {
		if err := c.EnableDHT().ApplyState(state.DHT); err != nil {
			return err
		}
	}
	for _, record := range state.Transfers {
		if record.TargetPath == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(record.TargetPath), 0o755); err != nil {
			return err
		}
		atp := AddTransferParams{
			Hash:       record.Hash,
			CreateTime: record.CreateTime,
			Size:       record.Size,
			FilePath:   record.TargetPath,
			Paused:     record.Paused,
			ResumeData: cloneResumeData(record.ResumeData),
			Handler:    disk.NewDesktopFileHandler(record.TargetPath),
		}
		handle, err := c.session.AddTransferParams(atp)
		if err != nil {
			return err
		}
		if handle.IsValid() {
			handle.transfer.SetUploadPriority(record.UploadPrio)
		}
	}
	return nil
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

type clientStateWire struct {
	Version       int                 `json:"version"`
	ServerAddress string              `json:"server_address,omitempty"`
	Transfers     []transferStateWire `json:"transfers"`
	Credits       []creditStateWire   `json:"credits,omitempty"`
	FriendSlots   []string            `json:"friend_slots,omitempty"`
	DHT           *dhtStateWire       `json:"dht,omitempty"`
}

type transferStateWire struct {
	Hash       string          `json:"hash"`
	Size       int64           `json:"size"`
	CreateTime int64           `json:"create_time"`
	TargetPath string          `json:"target_path"`
	Paused     bool            `json:"paused"`
	UploadPrio int             `json:"upload_prio,omitempty"`
	ResumeData *resumeDataWire `json:"resume_data,omitempty"`
}

type resumeDataWire struct {
	Hashes           []string         `json:"hashes,omitempty"`
	Pieces           []bool           `json:"pieces,omitempty"`
	DownloadedBlocks []pieceBlockWire `json:"downloaded_blocks,omitempty"`
	Peers            []string         `json:"peers,omitempty"`
}

type pieceBlockWire struct {
	PieceIndex int `json:"piece_index"`
	PieceBlock int `json:"piece_block"`
}

type creditStateWire struct {
	PeerHash   string `json:"peer_hash"`
	Uploaded   uint64 `json:"uploaded,omitempty"`
	Downloaded uint64 `json:"downloaded,omitempty"`
}

type dhtStateWire struct {
	SelfID              string             `json:"self_id,omitempty"`
	Firewalled          bool               `json:"firewalled"`
	LastBootstrap       int64              `json:"last_bootstrap,omitempty"`
	LastRefresh         int64              `json:"last_refresh,omitempty"`
	LastFirewalledCheck int64              `json:"last_firewalled_check,omitempty"`
	StoragePoint        string             `json:"storage_point,omitempty"`
	Nodes               []dhtNodeStateWire `json:"nodes,omitempty"`
	RouterNodes         []string           `json:"router_nodes,omitempty"`
}

type dhtNodeStateWire struct {
	ID        string `json:"id,omitempty"`
	Addr      string `json:"addr"`
	TCPPort   uint16 `json:"tcp_port,omitempty"`
	Version   byte   `json:"version,omitempty"`
	Seed      bool   `json:"seed,omitempty"`
	HelloSent bool   `json:"hello_sent,omitempty"`
	Pinged    bool   `json:"pinged,omitempty"`
	FailCount int    `json:"fail_count,omitempty"`
	FirstSeen int64  `json:"first_seen,omitempty"`
	LastSeen  int64  `json:"last_seen,omitempty"`
}

func newClientStateWire(state *ClientState) (*clientStateWire, error) {
	if state == nil {
		return &clientStateWire{Version: clientStateVersion}, nil
	}
	wire := &clientStateWire{
		Version:       state.Version,
		ServerAddress: state.ServerAddress,
		Transfers:     make([]transferStateWire, 0, len(state.Transfers)),
		Credits:       make([]creditStateWire, 0, len(state.Credits)),
		FriendSlots:   make([]string, 0, len(state.FriendSlots)),
	}
	if state.DHT != nil {
		wire.DHT = newDHTStateWire(state.DHT)
	}
	if wire.Version == 0 {
		wire.Version = clientStateVersion
	}
	for _, record := range state.Transfers {
		item := transferStateWire{
			Hash:       record.Hash.String(),
			Size:       record.Size,
			CreateTime: record.CreateTime,
			TargetPath: record.TargetPath,
			Paused:     record.Paused,
			UploadPrio: int(record.UploadPrio),
		}
		if record.ResumeData != nil {
			item.ResumeData = newResumeDataWire(record.ResumeData)
		}
		wire.Transfers = append(wire.Transfers, item)
	}
	for _, credit := range state.Credits {
		if credit.PeerHash.Equal(protocol.Invalid) {
			continue
		}
		wire.Credits = append(wire.Credits, creditStateWire{
			PeerHash:   credit.PeerHash.String(),
			Uploaded:   credit.Uploaded,
			Downloaded: credit.Downloaded,
		})
	}
	for _, hash := range state.FriendSlots {
		if hash.Equal(protocol.Invalid) {
			continue
		}
		wire.FriendSlots = append(wire.FriendSlots, hash.String())
	}
	return wire, nil
}

func (w *clientStateWire) toRuntime() (*ClientState, error) {
	if w == nil {
		return &ClientState{Version: clientStateVersion}, nil
	}
	state := &ClientState{
		Version:       w.Version,
		ServerAddress: w.ServerAddress,
		Transfers:     make([]ClientTransferState, 0, len(w.Transfers)),
		Credits:       make([]ClientCreditState, 0, len(w.Credits)),
		FriendSlots:   make([]protocol.Hash, 0, len(w.FriendSlots)),
	}
	if w.DHT != nil {
		dhtState, err := w.DHT.toRuntime()
		if err != nil {
			return nil, err
		}
		state.DHT = dhtState
	}
	for _, record := range w.Transfers {
		hash, err := protocol.HashFromString(record.Hash)
		if err != nil {
			return nil, err
		}
		state.Transfers = append(state.Transfers, ClientTransferState{
			Hash:       hash,
			Size:       record.Size,
			CreateTime: record.CreateTime,
			TargetPath: record.TargetPath,
			Paused:     record.Paused,
			UploadPrio: UploadPriority(record.UploadPrio),
			ResumeData: record.ResumeData.toRuntime(),
		})
	}
	for _, record := range w.Credits {
		hash, err := protocol.HashFromString(record.PeerHash)
		if err != nil {
			return nil, err
		}
		state.Credits = append(state.Credits, ClientCreditState{
			PeerHash:   hash,
			Uploaded:   record.Uploaded,
			Downloaded: record.Downloaded,
		})
	}
	for _, value := range w.FriendSlots {
		hash, err := protocol.HashFromString(value)
		if err != nil {
			return nil, err
		}
		state.FriendSlots = append(state.FriendSlots, hash)
	}
	return state, nil
}

func newResumeDataWire(src *protocol.TransferResumeData) *resumeDataWire {
	if src == nil {
		return nil
	}
	dst := &resumeDataWire{
		Hashes:           make([]string, 0, len(src.Hashes)),
		Pieces:           src.Pieces.Bits(),
		DownloadedBlocks: make([]pieceBlockWire, 0, len(src.DownloadedBlocks)),
		Peers:            make([]string, 0, len(src.Peers)),
	}
	for _, hash := range src.Hashes {
		dst.Hashes = append(dst.Hashes, hash.String())
	}
	for _, block := range src.DownloadedBlocks {
		dst.DownloadedBlocks = append(dst.DownloadedBlocks, pieceBlockWire{
			PieceIndex: block.PieceIndex,
			PieceBlock: block.PieceBlock,
		})
	}
	for _, peer := range src.Peers {
		dst.Peers = append(dst.Peers, peer.String())
	}
	return dst
}

func (w *resumeDataWire) toRuntime() *protocol.TransferResumeData {
	if w == nil {
		return nil
	}
	resume := &protocol.TransferResumeData{
		Hashes:           make([]protocol.Hash, 0, len(w.Hashes)),
		Pieces:           protocol.NewBitField(len(w.Pieces)),
		DownloadedBlocks: make([]data.PieceBlock, 0, len(w.DownloadedBlocks)),
		Peers:            make([]protocol.Endpoint, 0, len(w.Peers)),
	}
	for i, piece := range w.Pieces {
		if piece {
			resume.Pieces.SetBit(i)
		}
	}
	for _, hashString := range w.Hashes {
		hash, err := protocol.HashFromString(hashString)
		if err != nil {
			continue
		}
		resume.Hashes = append(resume.Hashes, hash)
	}
	for _, block := range w.DownloadedBlocks {
		resume.DownloadedBlocks = append(resume.DownloadedBlocks, data.NewPieceBlock(block.PieceIndex, block.PieceBlock))
	}
	for _, peerString := range w.Peers {
		host, port, err := splitHostPort(peerString)
		if err != nil {
			continue
		}
		endpoint, err := protocol.EndpointFromString(host, port)
		if err != nil {
			continue
		}
		resume.Peers = append(resume.Peers, endpoint)
	}
	return resume
}

func splitHostPort(value string) (string, int, error) {
	host, portString, err := net.SplitHostPort(value)
	if err != nil {
		return "", 0, err
	}
	port, err := strconv.Atoi(portString)
	if err != nil {
		return "", 0, err
	}
	return host, port, nil
}

func newDHTStateWire(state *ClientDHTState) *dhtStateWire {
	if state == nil {
		return nil
	}
	wire := &dhtStateWire{
		Firewalled:          state.Firewalled,
		LastBootstrap:       state.LastBootstrap,
		LastRefresh:         state.LastRefresh,
		LastFirewalledCheck: state.LastFirewalledCheck,
		StoragePoint:        state.StoragePoint,
		Nodes:               make([]dhtNodeStateWire, 0, len(state.Nodes)),
		RouterNodes:         append([]string(nil), state.RouterNodes...),
	}
	if !state.SelfID.Equal(protocol.Invalid) {
		wire.SelfID = state.SelfID.String()
	}
	for _, node := range state.Nodes {
		item := dhtNodeStateWire{
			Addr:      node.Addr,
			TCPPort:   node.TCPPort,
			Version:   node.Version,
			Seed:      node.Seed,
			HelloSent: node.HelloSent,
			Pinged:    node.Pinged,
			FailCount: node.FailCount,
			FirstSeen: node.FirstSeen,
			LastSeen:  node.LastSeen,
		}
		if !node.ID.Equal(protocol.Invalid) {
			item.ID = node.ID.String()
		}
		wire.Nodes = append(wire.Nodes, item)
	}
	return wire
}

func (w *dhtStateWire) toRuntime() (*ClientDHTState, error) {
	if w == nil {
		return nil, nil
	}
	state := &ClientDHTState{
		Firewalled:          w.Firewalled,
		LastBootstrap:       w.LastBootstrap,
		LastRefresh:         w.LastRefresh,
		LastFirewalledCheck: w.LastFirewalledCheck,
		StoragePoint:        w.StoragePoint,
		Nodes:               make([]ClientDHTNodeState, 0, len(w.Nodes)),
		RouterNodes:         append([]string(nil), w.RouterNodes...),
	}
	if w.SelfID != "" {
		hash, err := protocol.HashFromString(w.SelfID)
		if err != nil {
			return nil, err
		}
		state.SelfID = hash
	}
	for _, node := range w.Nodes {
		item := ClientDHTNodeState{
			Addr:      node.Addr,
			TCPPort:   node.TCPPort,
			Version:   node.Version,
			Seed:      node.Seed,
			HelloSent: node.HelloSent,
			Pinged:    node.Pinged,
			FailCount: node.FailCount,
			FirstSeen: node.FirstSeen,
			LastSeen:  node.LastSeen,
		}
		if node.ID != "" {
			hash, err := protocol.HashFromString(node.ID)
			if err != nil {
				return nil, err
			}
			item.ID = hash
		}
		state.Nodes = append(state.Nodes, item)
	}
	return state, nil
}
