package goed2k

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/goed2k/core/data"
	"github.com/goed2k/core/disk"
	"github.com/goed2k/core/protocol"
)

const InvalidETA int64 = -1

// 向 ED2K 服务器请求文件源（GetFileSources）的重试间隔，对齐 aMule
// include/protocol/ed2k/Constants.h：FILEREASKTIME、SERVERREASKTIME。
// 秒级重试易被服务器忽略或触发限流。
const (
	fileReaskServerTCPMS = 1_300_000 // FILEREASKTIME（约 21.7 分钟）
	serverReaskTCPMS     = 800_000   // SERVERREASKTIME（约 13.3 分钟），无活跃连接与候选时略缩短
)

// DHT（Kad）要源：eMule 中对同一文件重复发起 Kad 源搜索的间隔通常为 KADEMLIAREASKTIME
// （约 1 小时量级，见 eMule Kademlia 侧实现）；本实现为更快发现源采用更短默认间隔。
// 下列毫秒值与 nextDHTSourcesInterval 各分支一致。
const (
	dhtSourcesReaskStarvedMS  = 30_000  // 成功发出且无任何连接/候选
	dhtSourcesReaskSparseMS   = 60_000  // 成功发出且连接或候选稀疏（≤1）
	dhtSourcesReaskNormalMS   = 120_000 // 成功发出时的默认间隔
	dhtSourcesReaskFailFastMS = 30_000  // 未能发起 Kad 搜索：无连接且无候选
	dhtSourcesReaskFailSlowMS = 60_000  // 未能发起：否则
)

type Transfer struct {
	hash               protocol.Hash
	aichRoot           protocol.AICHHash
	createTime         int64
	size               int64
	numPieces          int
	filePath           string
	finalName          string
	fileComment        string
	stat               Statistics
	closedStat         Statistics
	picker             PiecePicker
	policy             Policy
	pm                 *PieceManager
	hashSet            []protocol.Hash
	aichPieceBlocks    map[int][]protocol.AICHHash
	session            *Session
	pause              bool
	abort              bool
	handler            disk.FileHandler
	needSaveResumeData bool
	resumeDirtyGen     uint64
	state              TransferState
	peersInfo          []PeerInfo
	connections        []*PeerConnection
	nextSourcesRequest int64
	nextDHTRequest     int64
	speedMon           SpeedMonitor
	pendingResumeIO    int
	uploadPriority     UploadPriority
	downloadPriority   TransferPriority
	pendingPieceHashes map[int]bool
	aichPendingPiece   map[int]bool
	previewMu          sync.Mutex
	previewPieces      map[uint16][]byte
	httpSources        *httpSourceManager
}

func NewTransfer(s *Session, atp AddTransferParams) (*Transfer, error) {
	t := &Transfer{
		hash:               atp.Hash,
		aichRoot:           atp.AICHRootHash,
		createTime:         atp.CreateTime,
		size:               atp.Size,
		numPieces:          int(DivCeil(atp.Size, PieceSize)),
		filePath:           atp.FilePath,
		finalName:          sanitizeTransferFinalName(atp.FinalName),
		fileComment:        strings.TrimSpace(atp.FileComment),
		stat:               NewStatistics(),
		closedStat:         NewStatistics(),
		hashSet:            make([]protocol.Hash, 0),
		aichPieceBlocks:    make(map[int][]protocol.AICHHash),
		session:            s,
		pause:              atp.Paused,
		handler:            atp.Handler,
		state:              LoadingResumeData,
		connections:        make([]*PeerConnection, 0),
		speedMon:           NewSpeedMonitor(30),
		uploadPriority:     UploadPriorityNormal,
		downloadPriority:   TransferPriorityNormal,
		pendingPieceHashes: make(map[int]bool),
		aichPendingPiece:   make(map[int]bool),
	}
	if len(atp.PieceHashes) > 0 {
		t.hashSet = append(t.hashSet, atp.PieceHashes...)
	}
	blocksInLastPiece := blocksInLastPieceForSize(atp.Size)
	t.picker = NewPiecePicker(t.numPieces, blocksInLastPiece)
	t.policy = NewPolicy(t)

	if t.handler == nil && atp.FilePath != "" {
		t.handler = disk.NewDesktopFileHandler(atp.FilePath)
	}
	if t.handler != nil {
		t.pm = NewPieceManager(t.handler, t.numPieces, blocksInLastPiece)
	}

	if len(atp.HttpSources) > 0 {
		t.httpSources = newHTTPSourceManager(atp.HttpSources)
	} else {
		t.httpSources = newHTTPSourceManager(nil)
	}

	if t.handler != nil && t.size > 0 && s != nil {
		sparse := s.settings.UseSparseFiles
		prealloc := s.settings.PreallocateDiskSpace
		if sparse || prealloc {
			_ = t.handler.Preallocate(t.size, sparse)
		}
	}

	if atp.ResumeData != nil {
		t.restoreResumeData(atp.ResumeData)
	} else {
		t.state = Downloading
		t.markResumeDirty()
	}
	if t.IsFinished() && s != nil && s.settings.UseEmuleTempLayout && isEmuleTempPartPath(t.GetFilePath()) {
		if t.handler != nil {
			_ = t.handler.Close()
		}
		t.sealHandler()
		t.promoteEmuleTempPartIfNeeded()
	}

	return t, nil
}

func (t *Transfer) GetHash() protocol.Hash {
	return t.hash
}

func (t *Transfer) GetCreateTime() int64 {
	return t.createTime
}

func (t *Transfer) Size() int64 {
	return t.size
}

func (t *Transfer) GetFilePath() string {
	if t.filePath != "" {
		return t.filePath
	}
	if t.handler != nil {
		return t.handler.Path()
	}
	return ""
}

func (t *Transfer) GetFile() *os.File {
	if t.pm == nil {
		return nil
	}
	return t.pm.GetFile()
}

func (t *Transfer) FileName() string {
	if t != nil && t.finalName != "" {
		return t.finalName
	}
	path := t.GetFilePath()
	if path == "" {
		return t.hash.String()
	}
	return filepath.Base(path)
}

func (t *Transfer) FileComment() string {
	if t == nil {
		return ""
	}
	return t.fileComment
}

func (t *Transfer) UploadPriority() UploadPriority {
	return t.uploadPriority
}

func (t *Transfer) SetUploadPriority(priority UploadPriority) {
	t.uploadPriority = priority
}

func (t *Transfer) DownloadPriority() TransferPriority {
	return t.downloadPriority
}

func (t *Transfer) SetDownloadPriority(priority TransferPriority) {
	t.downloadPriority = priority
}

func (t *Transfer) Pause() {
	t.pause = true
}

func (t *Transfer) Resume() {
	t.pause = false
}

func (t *Transfer) IsPaused() bool {
	return t.pause
}

func (t *Transfer) IsAborted() bool {
	return t.abort
}

func (t *Transfer) WantMorePeers() bool {
	return !t.IsPaused() && !t.IsFinished() && t.policy.NumConnectCandidates() > 0
}

func (t *Transfer) AddStats(s Statistics) {
	t.closedStat.Add(s)
	t.refreshStats()
}

func (t *Transfer) AddPeer(endpoint protocol.Endpoint, sourceFlag int) error {
	if t.session != nil {
		t.session.mu.Lock()
		defer t.session.mu.Unlock()
	}
	peer := NewPeerWithSource(endpoint, true, sourceFlag)
	_, err := t.policy.AddPeer(peer)
	return err
}

func (t *Transfer) RemovePeerConnection(c *PeerConnection) {
	if t.session != nil {
		t.session.mu.Lock()
		defer t.session.mu.Unlock()
	}
	t.policy.ConnectionClosed(c, CurrentTime())
	c.SetPeer(nil)
	dst := t.connections[:0]
	for _, existing := range t.connections {
		if existing != c {
			dst = append(dst, existing)
		}
	}
	t.connections = dst
}

func (t *Transfer) ConnectToPeer(peerInfo *Peer) (*PeerConnection, error) {
	if peerInfo == nil {
		return nil, NewError(IllegalArgument)
	}
	if t.session != nil {
		t.session.mu.Lock()
	}
	peerInfo.LastConnected = CurrentTime()
	peerInfo.NextConnection = 0
	c := NewPeerConnection(t.session, peerInfo.Endpoint, t, peerInfo)
	t.session.connections = append(t.session.connections, c)
	t.connections = append(t.connections, c)
	t.policy.SetConnection(peerInfo, c)
	if t.session != nil {
		t.session.mu.Unlock()
	}
	if err := c.Connect(); err != nil {
		return nil, err
	}
	peerInfo.Connection = c
	return c, nil
}

func (t *Transfer) AttachPeer(c *PeerConnection) error {
	if c == nil {
		return NewError(IllegalArgument)
	}
	if t.IsPaused() {
		return NewError(TransferPaused)
	}
	if t.IsAborted() {
		return NewError(TransferAborted)
	}
	if t.IsFinished() {
		return NewError(TransferFinished)
	}
	if t.session != nil {
		t.session.mu.Lock()
		defer t.session.mu.Unlock()
	}
	if err := t.policy.NewConnection(c); err != nil {
		return err
	}
	t.connections = append(t.connections, c)
	t.session.connections = append(t.session.connections, c)
	c.SetTransfer(t)
	return nil
}

func (t *Transfer) AttachIncomingPeer(c *PeerConnection) error {
	if c == nil {
		return NewError(IllegalArgument)
	}
	if t.IsAborted() {
		return NewError(TransferAborted)
	}
	if t.session != nil {
		t.session.mu.Lock()
		defer t.session.mu.Unlock()
	}
	for _, existing := range t.connections {
		if existing == c {
			c.SetTransfer(t)
			return nil
		}
	}
	if err := t.policy.NewConnection(c); err != nil {
		return err
	}
	t.connections = append(t.connections, c)
	c.SetTransfer(t)
	return nil
}

func (t *Transfer) TryConnectPeer(sessionTime int64) (bool, error) {
	if !t.WantMorePeers() {
		return false, nil
	}
	return t.policy.ConnectOnePeer(sessionTime)
}

func (t *Transfer) IsFinished() bool {
	return t.numPieces == 0 || t.picker.NumHave() == t.picker.NumPieces()
}

func (t *Transfer) isFinishedForSharePublish() bool {
	if t == nil {
		return false
	}
	if t.state == Finished {
		return true
	}
	return t.IsFinished()
}

// isKadPublishable 是否可向 KAD 发布源（含 eMule 部分完成文件策略）。
func (t *Transfer) isKadPublishable() bool {
	if t == nil || t.IsPaused() || t.IsAborted() {
		return false
	}
	if t.isFinishedForSharePublish() {
		return true
	}
	if t.session == nil || !t.session.settings.PartialKadPublish {
		return false
	}
	return t.CanUpload()
}

// StorePreviewPiece 缓存来自对端的预览分片数据。
func (t *Transfer) StorePreviewPiece(index uint16, data []byte) {
	if t == nil || len(data) == 0 {
		return
	}
	t.previewMu.Lock()
	defer t.previewMu.Unlock()
	if t.previewPieces == nil {
		t.previewPieces = make(map[uint16][]byte)
	}
	t.previewPieces[index] = append([]byte(nil), data...)
}

// PreviewPiece 返回已缓存的预览分片。
func (t *Transfer) PreviewPiece(index uint16) ([]byte, bool) {
	if t == nil {
		return nil, false
	}
	t.previewMu.Lock()
	defer t.previewMu.Unlock()
	data, ok := t.previewPieces[index]
	if !ok || len(data) == 0 {
		return nil, false
	}
	return append([]byte(nil), data...), true
}

func (t *Transfer) ResumeData() *protocol.TransferResumeData {
	trd := t.snapshotResumeData()
	t.needSaveResumeData = false
	return trd
}

func (t *Transfer) markResumeDirty() {
	if t == nil {
		return
	}
	t.needSaveResumeData = true
	t.resumeDirtyGen++
}

func (t *Transfer) MarkResumeDataSaved() {
	if t != nil {
		t.needSaveResumeData = false
	}
}

func (t *Transfer) markResumeSavedIfGen(gen uint64) {
	if t == nil {
		return
	}
	if t.resumeDirtyGen != gen {
		t.needSaveResumeData = true
		return
	}
	t.needSaveResumeData = false
}

func (t *Transfer) ResumeDirtyGen() uint64 {
	if t == nil {
		return 0
	}
	return t.resumeDirtyGen
}

func (t *Transfer) snapshotResumeData() *protocol.TransferResumeData {
	trd := &protocol.TransferResumeData{}
	trd.Hashes = append(trd.Hashes, t.hashSet...)
	trd.Pieces = protocol.NewBitField(t.picker.NumPieces())
	for i := 0; i < t.numPieces; i++ {
		if t.picker.HavePiece(i) {
			trd.Pieces.SetBit(i)
		}
	}
	for _, dp := range t.picker.GetDownloadingQueue() {
		for j := 0; j < dp.BlocksCount(); j++ {
			if dp.IsFinished(j) {
				trd.DownloadedBlocks = append(trd.DownloadedBlocks, data.NewPieceBlock(dp.PieceIndex, j))
			}
		}
	}
	for _, peer := range t.policy.peers {
		if peer.Endpoint.Defined() {
			trd.Peers = append(trd.Peers, peer.Endpoint)
		}
	}
	return trd
}

func (t *Transfer) GetPeersInfo() []PeerInfo {
	t.peersInfo = t.peersInfo[:0]
	for _, c := range t.connections {
		if c == nil {
			continue
		}
		t.peersInfo = append(t.peersInfo, c.GetInfo())
	}
	out := make([]PeerInfo, len(t.peersInfo))
	copy(out, t.peersInfo)
	return out
}

func (t *Transfer) ActiveConnections() int {
	res := 0
	for _, c := range t.connections {
		if c != nil && !c.IsDisconnecting() {
			res++
		}
	}
	return res
}

func (t *Transfer) NeedMoreSources() bool {
	if t.IsPaused() || t.IsAborted() || t.IsFinished() {
		return false
	}
	return t.ActiveConnections() < t.session.settings.SessionConnectionsLimit
}

func (t *Transfer) refreshStats() {
	t.stat = NewStatistics()
	t.stat.Add(t.closedStat)
	for _, c := range t.connections {
		if c == nil || c.IsDisconnecting() {
			continue
		}
		t.stat.Merge(c.Statistics())
	}
}

func (t *Transfer) GetStatus() TransferStatus {
	totalDone := int64(0)
	for pieceIndex := 0; pieceIndex < t.picker.NumPieces(); pieceIndex++ {
		if t.picker.HavePiece(pieceIndex) {
			totalDone += t.pieceSize(pieceIndex)
		}
	}
	for _, dp := range t.picker.GetDownloadingQueue() {
		if t.picker.HavePiece(dp.PieceIndex) {
			continue
		}
		for blockIndex := 0; blockIndex < dp.BlocksCount(); blockIndex++ {
			if !dp.IsDownloaded(blockIndex) {
				continue
			}
			totalDone += int64(data.NewPieceBlock(dp.PieceIndex, blockIndex).Size(t.size))
		}
	}
	if totalDone > t.size {
		totalDone = t.size
	}
	totalReceived := t.receivedBytes(totalDone)
	state := t.state
	if t.pause {
		state = PausedState
	} else if state != Finished && totalDone >= t.size && t.size > 0 {
		state = Verifying
	}

	status := TransferStatus{
		Paused:            t.pause,
		DownloadRate:      int(t.stat.DownloadRate()),
		Upload:            t.stat.TotalUpload(),
		UploadRate:        int(t.stat.UploadRate()),
		NumPeers:          t.policy.Size(),
		DownloadingPieces: t.picker.NumDownloadingPieces(),
		TotalDone:         totalDone,
		TotalReceived:     totalReceived,
		TotalWanted:       t.size,
		ETA:               InvalidETA,
		Pieces:            protocol.NewBitField(t.picker.NumPieces()),
		NumPieces:         t.picker.NumHave(),
		State:             state,
	}
	for i := 0; i < t.picker.NumPieces(); i++ {
		if t.picker.HavePiece(i) {
			status.Pieces.SetBit(i)
		}
	}
	averageSpeed := t.speedMon.AverageSpeed()
	if averageSpeed != InvalidSpeed {
		if averageSpeed == 0 {
			status.ETA = InvalidETA
		} else {
			status.ETA = (status.TotalWanted - status.TotalDone) / averageSpeed
		}
	}
	return status
}

func (t *Transfer) pieceSize(pieceIndex int) int64 {
	if pieceIndex < 0 || pieceIndex >= t.numPieces {
		return 0
	}
	begin := int64(pieceIndex) * PieceSize
	if begin >= t.size {
		return 0
	}
	end := begin + PieceSize
	if end > t.size {
		end = t.size
	}
	return end - begin
}

func (t *Transfer) receivedBytes(totalDone int64) int64 {
	totalReceived := totalDone
	inflight := t.inflightBlockReceived()
	for _, received := range inflight {
		totalReceived += received
	}
	if totalReceived > t.size {
		totalReceived = t.size
	}
	return totalReceived
}

func (t *Transfer) PieceSnapshots() []PieceSnapshot {
	inflight := t.inflightBlockReceived()
	pieces := make([]PieceSnapshot, 0, t.picker.NumPieces())
	for pieceIndex := 0; pieceIndex < t.picker.NumPieces(); pieceIndex++ {
		totalBytes := t.pieceSize(pieceIndex)
		blocksTotal := t.picker.BlocksInPiece(pieceIndex)
		snapshot := PieceSnapshot{
			Index:       pieceIndex,
			State:       PieceSnapshotMissing,
			TotalBytes:  totalBytes,
			BlocksTotal: blocksTotal,
		}
		if t.picker.HavePiece(pieceIndex) {
			snapshot.State = PieceSnapshotFinished
			snapshot.DoneBytes = totalBytes
			snapshot.ReceivedBytes = totalBytes
			snapshot.BlocksDone = blocksTotal
			pieces = append(pieces, snapshot)
			continue
		}
		if dp := t.picker.GetDownloadingPiece(pieceIndex); dp != nil {
			snapshot.State = PieceSnapshotDownloading
			for blockIndex := 0; blockIndex < dp.BlocksCount(); blockIndex++ {
				block := data.NewPieceBlock(pieceIndex, blockIndex)
				blockSize := int64(block.Size(t.size))
				switch {
				case dp.Blocks[blockIndex].IsFinished():
					snapshot.BlocksDone++
					snapshot.DoneBytes += blockSize
					snapshot.ReceivedBytes += blockSize
				case dp.Blocks[blockIndex].IsWriting():
					snapshot.BlocksWriting++
					snapshot.DoneBytes += blockSize
					snapshot.ReceivedBytes += blockSize
				case dp.Blocks[blockIndex].IsRequested():
					snapshot.BlocksPending++
					snapshot.ReceivedBytes += inflight[block.BlocksOffset()]
				}
			}
		}
		if snapshot.ReceivedBytes > snapshot.TotalBytes {
			snapshot.ReceivedBytes = snapshot.TotalBytes
		}
		if snapshot.DoneBytes > snapshot.TotalBytes {
			snapshot.DoneBytes = snapshot.TotalBytes
		}
		pieces = append(pieces, snapshot)
	}
	return pieces
}

func (t *Transfer) inflightBlockReceived() map[int64]int64 {
	inflight := make(map[int64]int64)
	for _, c := range t.connections {
		if c == nil || c.IsDisconnecting() {
			continue
		}
		for _, pb := range c.downloadQueue {
			if pb.Received <= 0 || t.picker.IsBlockDownloaded(pb.Block) {
				continue
			}
			size := int64(pb.Block.Size(t.size))
			received := pb.Received
			if received > size {
				received = size
			}
			key := pb.Block.BlocksOffset()
			if inflight[key] < received {
				inflight[key] = received
			}
		}
	}
	return inflight
}

func (t *Transfer) Abort(deleteFile bool) error {
	t.abort = true
	for _, c := range t.connections {
		if c != nil {
			c.Close(TransferAborted)
		}
	}
	t.connections = nil
	if t.session != nil {
		t.session.SubmitDiskTask(NewAsyncRelease(t, deleteFile))
	}
	return nil
}

func (t *Transfer) PauseWithDisconnect() {
	t.pause = true
	for _, c := range t.connections {
		if c != nil {
			c.Close(TransferPaused)
		}
	}
	t.connections = nil
	t.markResumeDirty()
}

func (t *Transfer) ResumeWithState() {
	t.pause = false
	for i := range t.policy.peers {
		if t.policy.peers[i].Connection == nil {
			if _, queued := t.policy.peers[i].RemoteQueueState(); queued {
				continue
			}
			t.policy.peers[i].NextConnection = 0
			t.policy.peers[i].LastConnected = 0
		}
	}
	t.nextSourcesRequest = 0
	t.nextDHTRequest = 0
	t.markResumeDirty()
}

func (t *Transfer) ForceSourceDiscoveryNow() {
	t.nextSourcesRequest = 0
	t.nextDHTRequest = 0
}

func (t *Transfer) nextServerSourcesInterval(activeConnections, connectCandidates int, sent bool) int64 {
	if sent {
		if activeConnections == 0 && connectCandidates == 0 {
			return serverReaskTCPMS
		}
		return fileReaskServerTCPMS
	}

	// 无握手完成的服务器或请求未能入队：较快重试以便连上服务器，但仍避免秒级风暴
	if activeConnections == 0 && connectCandidates == 0 {
		return Seconds(30)
	}
	return Minutes(1)
}

func (t *Transfer) nextDHTSourcesInterval(activeConnections, connectCandidates int, sent bool) int64 {
	if sent {
		if activeConnections == 0 && connectCandidates == 0 {
			return dhtSourcesReaskStarvedMS
		}
		if activeConnections <= 1 || connectCandidates <= 1 {
			return dhtSourcesReaskSparseMS
		}
		return dhtSourcesReaskNormalMS
	}

	// Kad 不可用或搜索未能启动：较快重试，但仍避免与 SecondTick 同频
	if activeConnections == 0 && connectCandidates == 0 {
		return dhtSourcesReaskFailFastMS
	}
	return dhtSourcesReaskFailSlowMS
}

func (t *Transfer) QueuePieceHash(pieceIndex int) bool {
	if pieceIndex < 0 || pieceIndex >= t.picker.NumPieces() {
		return false
	}
	if t.picker.HavePiece(pieceIndex) || t.pendingPieceHashes[pieceIndex] {
		return false
	}
	if !t.picker.IsPieceFinished(pieceIndex) {
		return false
	}
	if t.session == nil || t.pm == nil {
		return false
	}
	t.pendingPieceHashes[pieceIndex] = true
	t.session.SubmitDiskTask(NewAsyncHash(t, pieceIndex))
	return true
}

func (t *Transfer) WeHave(pieceIndex int) {
	t.picker.WeHave(pieceIndex)
}

func (t *Transfer) GetPieceManager() *PieceManager {
	return t.pm
}

func (t *Transfer) AvailablePieces() protocol.BitField {
	bits := protocol.NewBitField(t.picker.NumPieces())
	for i := 0; i < t.picker.NumPieces(); i++ {
		if t.picker.HavePiece(i) {
			bits.SetBit(i)
		}
	}
	return bits
}

func (t *Transfer) UploadHashSet() []protocol.Hash {
	if len(t.hashSet) > 0 {
		out := make([]protocol.Hash, len(t.hashSet))
		copy(out, t.hashSet)
		return out
	}
	if t.size <= PieceSize {
		return []protocol.Hash{t.hash}
	}
	return nil
}

func (t *Transfer) CanUpload() bool {
	return t != nil && !t.abort && t.pm != nil && t.picker.NumHave() > 0
}

func (t *Transfer) CanUploadRange(begin, end int64) bool {
	if !t.CanUpload() || end <= begin || begin < 0 || end > t.size {
		return false
	}
	reqs, err := data.MakePeerRequests(begin, end, t.size)
	if err != nil {
		return false
	}
	for _, req := range reqs {
		if !t.picker.HavePiece(req.Piece) {
			return false
		}
	}
	return true
}

func (t *Transfer) ReadRange(begin, end int64) ([]byte, error) {
	if t.pm == nil {
		return nil, NewError(NoTransfer)
	}
	return t.pm.ReadRange(begin, end)
}

func (t *Transfer) throttleDownloadWrite(bytes int) {
	if t == nil || t.session == nil || bytes <= 0 {
		return
	}
	t.session.ThrottleDownload(bytes)
}

func (t *Transfer) SecondTick(accumulator *Statistics, tickIntervalMS int64) {
	if t.NeedMoreSources() {
		now := CurrentTime()
		activeConnections := t.ActiveConnections()
		connectCandidates := t.policy.NumConnectCandidates()
		// 仅在“已排到极远的未来”（例如 resume 残留）时提前到本轮；阈值必须 ≥ 成功入队后的最大间隔，
		// 否则会与 fileReaskServerTCPMS 冲突，在缺源状态下每秒向服务器重发。
		if activeConnections <= 1 && connectCandidates <= 1 && t.nextSourcesRequest > now+fileReaskServerTCPMS {
			t.nextSourcesRequest = now
		}
		// 阈值须 ≥ 成功入队后的最大 DHT 间隔（dhtSourcesReaskNormalMS），否则缺源时会每秒重发 Kad 搜索。
		if activeConnections <= 1 && connectCandidates <= 1 && t.nextDHTRequest > now+dhtSourcesReaskNormalMS {
			t.nextDHTRequest = now
		}
		if t.nextSourcesRequest <= now {
			sent := t.session.SendSourcesRequest(t.hash, t.size)
			t.nextSourcesRequest = now + t.nextServerSourcesInterval(activeConnections, connectCandidates, sent)
		}
		if t.nextDHTRequest < now {
			sent4 := t.session.SendDHTSourcesRequest(t.hash, t.size, t)
			sent6 := t.session.SendDHTv6SourcesRequest(t.hash, t.size, t)
			t.nextDHTRequest = now + t.nextDHTSourcesInterval(activeConnections, connectCandidates, sent4 || sent6)
		}
	}

	for _, c := range t.connections {
		if c == nil {
			continue
		}
		c.SecondTick(tickIntervalMS)
	}
	t.tickHttpSources()
	t.refreshStats()
	if accumulator != nil {
		accumulator.Add(t.stat)
	}
	t.speedMon.AddSample(t.stat.DownloadRate())
}

func (t *Transfer) OnBlockWriteCompleted(block data.PieceBlock, _ [][]byte, ec BaseErrorCode) {
	if ec.Code() == NoError.Code() {
		t.picker.MarkAsFinished(block)
		t.QueuePieceHash(block.PieceIndex)
		t.markResumeDirty()
		return
	}
	t.picker.AbortDownload(block, nil)
	t.PauseWithDisconnect()
}

func (t *Transfer) OnPieceHashCompleted(pieceIndex int, hash protocol.Hash) {
	delete(t.pendingPieceHashes, pieceIndex)
	if pieceIndex < len(t.hashSet) && !t.hashSet[pieceIndex].Equal(hash) {
		debugPeerf("transfer %s piece %d hash mismatch got=%s want=%s", t.hash.String(), pieceIndex, hash.String(), t.hashSet[pieceIndex].String())
		if t.tryAICHRecoverPiece(pieceIndex) {
			t.markResumeDirty()
			return
		}
		if t.aichPendingPiece[pieceIndex] {
			t.markResumeDirty()
			return
		}
		t.picker.RestorePiece(pieceIndex)
	} else {
		debugPeerf("transfer %s piece %d hash ok=%s", t.hash.String(), pieceIndex, hash.String())
		t.WeHave(pieceIndex)
	}
	t.markResumeDirty()
	if t.IsFinished() {
		t.finished()
	}
}

func (t *Transfer) OnBlockRestoreCompleted(block data.PieceBlock, ec BaseErrorCode) {
	if t.pendingResumeIO > 0 {
		t.pendingResumeIO--
	}
	if ec.Code() != NoError.Code() {
		t.PauseWithDisconnect()
		return
	}
	t.picker.WeHaveBlock(block)
	t.markResumeDirty()
	if t.pendingResumeIO == 0 {
		if t.IsFinished() {
			t.state = Finished
		} else {
			t.state = Downloading
		}
	}
}

func (t *Transfer) OnReleaseFile(_ BaseErrorCode, _ [][]byte, deleteFile bool) {
	if deleteFile || !t.isFinishedForSharePublish() {
		t.unsealHandler()
		return
	}
	t.promoteEmuleTempPartIfNeeded()
	if t.session != nil {
		t.session.tryAddCompletedTransferToSharedStore(t)
	}
}

func (t *Transfer) finished() {
	for _, c := range t.connections {
		if c != nil {
			c.Close(TransferFinished)
		}
	}
	t.connections = nil
	t.state = Finished
	if t.session != nil {
		t.session.SubmitDiskTask(NewAsyncRelease(t, false))
		t.session.PublishTransferToServer(t)
		t.session.PublishTransferToKAD(t)
		t.session.PublishTransferToKADV6(t)
	}
	t.markResumeDirty()
}

func (t *Transfer) promoteEmuleTempPartIfNeeded() {
	if t == nil || t.session == nil || !t.session.settings.UseEmuleTempLayout {
		t.unsealHandler()
		return
	}
	src := t.GetFilePath()
	if !isEmuleTempPartPath(src) {
		t.unsealHandler()
		return
	}
	name := t.finalName
	if name == "" {
		t.unsealHandler()
		return
	}
	destDir := filepath.Dir(src)
	if incoming := strings.TrimSpace(t.session.settings.IncomingDir); incoming != "" {
		destDir = incoming
	}
	dest, err := promoteEmulePartFile(src, destDir, name)
	if err != nil {
		debugPeerf("transfer %s promote part failed: %v", t.hash.String(), err)
		t.unsealHandler()
		return
	}
	if dest == src {
		t.unsealHandler()
		return
	}
	t.filePath = dest
	if t.handler != nil {
		_ = t.handler.Close()
	}
	if setter, ok := t.handler.(interface{ SetPath(string) error }); ok {
		if err := setter.SetPath(dest); err != nil {
			debugPeerf("transfer %s set handler path: %v", t.hash.String(), err)
			t.unsealHandler()
		}
	} else {
		t.unsealHandler()
	}
	if t.session.sharedStore != nil {
		t.session.sharedStore.UpdatePath(t.hash, dest, t.FileName())
	}
	t.markResumeDirty()
}

func (t *Transfer) sealHandler() {
	if t == nil || t.handler == nil {
		return
	}
	if sealer, ok := t.handler.(interface{ Seal() }); ok {
		sealer.Seal()
	}
}

func (t *Transfer) unsealHandler() {
	if t == nil || t.handler == nil {
		return
	}
	if sealer, ok := t.handler.(interface{ Unseal() }); ok {
		sealer.Unseal()
	}
}

func (t *Transfer) AsyncRestoreBlock(block data.PieceBlock) {
	if t.session != nil {
		t.session.SubmitDiskTask(NewAsyncRestore(t, block, t.size))
	}
}

func (t *Transfer) SetHashSet(hash protocol.Hash, hashes []protocol.Hash) {
	if len(t.hashSet) != 0 {
		return
	}
	if !t.hash.Equal(hash) {
		return
	}
	t.hashSet = append(t.hashSet, hashes...)
	t.markResumeDirty()
}

func (t *Transfer) NeedResumeDataSave() bool {
	return t.needSaveResumeData
}

func (t *Transfer) restoreResumeData(resumeData *protocol.TransferResumeData) {
	t.state = LoadingResumeData
	t.hashSet = append(t.hashSet, resumeData.Hashes...)
	for i := 0; i < resumeData.Pieces.Len(); i++ {
		if resumeData.Pieces.GetBit(i) {
			t.picker.RestoreHave(i)
		}
	}
	for _, endpoint := range resumeData.Peers {
		if endpoint.Defined() {
			_ = t.AddPeer(endpoint, int(PeerResume))
		}
	}
	if len(resumeData.DownloadedBlocks) == 0 {
		if t.IsFinished() {
			t.state = Finished
		} else {
			t.state = Downloading
		}
		return
	}
	for _, block := range resumeData.DownloadedBlocks {
		t.pendingResumeIO++
		if t.pm != nil && t.session != nil {
			t.AsyncRestoreBlock(block)
			continue
		}
		t.picker.WeHaveBlock(block)
		t.pendingResumeIO--
	}
	if t.pendingResumeIO == 0 {
		t.state = Downloading
		if t.IsFinished() {
			t.state = Finished
		}
	}
}

func (t *Transfer) AICHRootHash() (protocol.AICHHash, bool) {
	if t == nil || t.aichRoot.IsZero() {
		return protocol.InvalidAICH, false
	}
	return t.aichRoot, true
}

func (t *Transfer) SetAICHRootHash(root protocol.AICHHash) {
	if t == nil || root.IsZero() {
		return
	}
	if !t.aichRoot.IsZero() {
		return
	}
	t.aichRoot = root
}

func (t *Transfer) UploadAICHHashes(requested []protocol.AICHHash) []protocol.AICHHash {
	if t == nil || !t.CanUpload() || len(requested) == 0 {
		return nil
	}
	root, ok := t.AICHRootHash()
	if !ok {
		if t.pm == nil {
			return nil
		}
		file := t.pm.GetFile()
		if file == nil {
			return nil
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return nil
		}
		computed, err := BuildAICHRootFromReader(file, t.size)
		if err != nil {
			return nil
		}
		t.aichRoot = computed
		root = computed
	}
	return matchAICHHashes(requested, root, func(pieceIndex int) ([]protocol.AICHHash, error) {
		begin := int64(pieceIndex) * AICHPieceSize
		end := begin + AICHPieceSize
		if end > t.size {
			end = t.size
		}
		data, err := t.ReadRange(begin, end)
		if err != nil {
			return nil, err
		}
		return BuildAICHBlockHashes(data), nil
	}, t.size)
}

func (t *Transfer) StoreAICHPieceBlocks(pieceIndex int, hashes []protocol.AICHHash) {
	if t == nil || len(hashes) == 0 {
		return
	}
	t.aichPieceBlocks[pieceIndex] = append([]protocol.AICHHash(nil), hashes...)
}

func (t *Transfer) AICHPieceBlocks(pieceIndex int) []protocol.AICHHash {
	if t == nil {
		return nil
	}
	return t.aichPieceBlocks[pieceIndex]
}

func (t *Transfer) tryAICHRecoverPiece(pieceIndex int) bool {
	if t == nil || t.pieceSize(pieceIndex) <= int64(AICHBlockSize) {
		return false
	}
	root, ok := t.AICHRootHash()
	if !ok {
		t.requestAICHRootFromPeers()
		return false
	}
	blockHashes := t.AICHPieceBlocks(pieceIndex)
	if len(blockHashes) == 0 {
		t.requestAICHRecovery(pieceIndex)
		return false
	}
	if t.pm == nil {
		return false
	}
	delete(t.aichPendingPiece, pieceIndex)
	pieceRoot := BuildAICHTreeRoot(blockHashes)
	pieceBegin := int64(pieceIndex) * AICHPieceSize
	pieceEnd := pieceBegin + t.pieceSize(pieceIndex)
	pieceData, err := t.ReadRange(pieceBegin, pieceEnd)
	if err != nil {
		return false
	}
	if !pieceRoot.Equal(root) && len(t.hashSet) > pieceIndex {
		// For multi-piece files validate piece subtree when possible.
		_ = pieceRoot
	}
	bad := LocateCorruptAICHBlocks(pieceData, blockHashes)
	if len(bad) == 0 {
		return false
	}
	debugPeerf("transfer %s piece %d AICH recovered %d/%d bad blocks", t.hash.String(), pieceIndex, len(bad), len(blockHashes))
	t.picker.RestorePiece(pieceIndex)
	for _, blockIndex := range bad {
		aichBegin := int64(blockIndex) * int64(AICHBlockSize)
		aichEnd := aichBegin + int64(AICHBlockSize)
		if aichEnd > int64(len(pieceData)) {
			aichEnd = int64(len(pieceData))
		}
		t.resetOverlappingDownloadBlocks(pieceIndex, pieceBegin+aichBegin, pieceBegin+aichEnd)
	}
	return true
}

func (t *Transfer) resetOverlappingDownloadBlocks(pieceIndex int, begin, end int64) {
	first, last, ok := overlappingDownloadBlocks(pieceIndex, begin, end, t.pieceSize(pieceIndex))
	if !ok {
		return
	}
	for blockIndex := first; blockIndex <= last; blockIndex++ {
		t.picker.AbortDownload(data.NewPieceBlock(pieceIndex, blockIndex), nil)
	}
}

// overlappingDownloadBlocks 返回与半开区间 [begin,end) 相交的下载块闭区间。
// 末块按 piece 实际字节夹紧，不会把下一块或下一片算进去。
func overlappingDownloadBlocks(pieceIndex int, begin, end, pieceBytes int64) (first, last int, ok bool) {
	pieceBegin := int64(pieceIndex) * AICHPieceSize
	relBegin := begin - pieceBegin
	relEnd := end - pieceBegin
	if relBegin < 0 {
		relBegin = 0
	}
	if relEnd > pieceBytes {
		relEnd = pieceBytes
	}
	if relEnd <= relBegin || pieceBytes <= 0 {
		return 0, 0, false
	}
	first = int(relBegin / BlockSize)
	last = int((relEnd - 1) / BlockSize)
	maxIdx := int(DivCeil(pieceBytes, BlockSize)) - 1
	if first < 0 {
		first = 0
	}
	if last > maxIdx {
		last = maxIdx
	}
	if first > last {
		return 0, 0, false
	}
	return first, last, true
}

func (t *Transfer) requestAICHRecovery(pieceIndex int) {
	if t == nil {
		return
	}
	if _, ok := t.AICHRootHash(); !ok {
		t.requestAICHRootFromPeers()
		return
	}
	t.aichPendingPiece[pieceIndex] = true
	for _, c := range t.connections {
		if c == nil || c.IsDisconnecting() || c.remotePeerInfo.Misc1.AICHVersion == 0 {
			continue
		}
		if c.pendingAICHPiece >= 0 {
			continue
		}
		c.SendAICHRequestForPiece(t, pieceIndex)
		return
	}
}

func (t *Transfer) requestAICHRootFromPeers() {
	if t == nil {
		return
	}
	if _, ok := t.AICHRootHash(); ok {
		return
	}
	for _, c := range t.connections {
		if c == nil {
			continue
		}
		c.maybeRequestAICHRoot()
	}
}

func (t *Transfer) retryPendingAICHRecoveries() {
	if t == nil {
		return
	}
	if _, ok := t.AICHRootHash(); !ok {
		return
	}
	for pieceIndex := range t.aichPendingPiece {
		t.requestAICHRecovery(pieceIndex)
	}
}
