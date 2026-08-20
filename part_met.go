package goed2k

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/goed2k/core/data"
	"github.com/goed2k/core/protocol"
)

const partMetFormat = "goed2k.part.met"
const partMetVersion = 1

var partMetWriteMu sync.Mutex

// PartMetDocument 为 <file>.part.met JSON 旁注格式。
type PartMetDocument struct {
	Format           string          `json:"format"`
	Version          int             `json:"version"`
	FileHash         protocol.Hash   `json:"file_hash,omitempty"`
	FileSize         int64           `json:"file_size,omitempty"`
	PieceHashes      []protocol.Hash `json:"piece_hashes,omitempty"`
	CompletedPieces  []bool          `json:"completed_pieces,omitempty"`
	DownloadedBlocks []partMetBlock  `json:"downloaded_blocks,omitempty"`
	KnownPeers       []string        `json:"known_peers,omitempty"`
	HttpSources      []string        `json:"http_sources,omitempty"`
}

type partMetBlock struct {
	Piece int `json:"piece"`
	Block int `json:"block"`
}

// PartMetInfo 为导入/导出 .part.met 的统一结构。
type PartMetInfo struct {
	Hash        protocol.Hash
	FileSize    int64
	Filename    string
	Resume      *protocol.TransferResumeData
	HttpSources []string
}

// ExportPartMet 导出 eMule 二进制 .part.met（主格式）及 goed2k JSON 旁注（.part.met.json）。
func ExportPartMet(path string, info PartMetInfo) error {
	if path == "" {
		return errors.New("path is empty")
	}
	if info.Resume == nil {
		return errors.New("resume data is nil")
	}
	if info.Hash.IsZero() {
		return errors.New("file hash is required")
	}
	if info.FileSize <= 0 {
		return errors.New("file size is required")
	}
	target := partMetPath(path)
	gaps := gapsFromResume(info.FileSize, info.Resume)
	filename := info.Filename
	if filename == "" {
		filename = filepath.Base(path)
	}
	emuleRaw, err := protocol.BuildEmulePartMet(protocol.EmulePartMetOptions{
		Hash:        info.Hash,
		FileSize:    info.FileSize,
		Filename:    filename,
		Transferred: transferredFromResume(info.FileSize, info.Resume),
		PieceHashes: info.Resume.Hashes,
		Gaps:        gaps,
	})
	if err != nil {
		return err
	}
	if err := writePartMetAtomic(target, emuleRaw); err != nil {
		return err
	}
	return exportPartMetJSON(path, info)
}

func exportPartMetJSON(path string, info PartMetInfo) error {
	doc := PartMetDocument{
		Format:   partMetFormat,
		Version:  partMetVersion,
		FileHash: info.Hash,
		FileSize: info.FileSize,
	}
	resume := info.Resume
	if len(resume.Hashes) > 0 {
		doc.PieceHashes = append([]protocol.Hash(nil), resume.Hashes...)
	}
	if resume.Pieces.Len() > 0 {
		doc.CompletedPieces = resume.Pieces.Bits()
	}
	for _, block := range resume.DownloadedBlocks {
		doc.DownloadedBlocks = append(doc.DownloadedBlocks, partMetBlock{
			Piece: block.PieceIndex,
			Block: block.PieceBlock,
		})
	}
	for _, peer := range resume.Peers {
		if peer.Defined() {
			doc.KnownPeers = append(doc.KnownPeers, peer.String())
		}
	}
	if len(info.HttpSources) > 0 {
		doc.HttpSources = append([]string(nil), info.HttpSources...)
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return writePartMetAtomic(partMetPath(path)+".json", raw)
}

// ImportPartMet 自动识别 eMule 二进制或 goed2k JSON .part.met。
// 导入前会尝试恢复未完成的原子写出（合法 .tmp 提升，损坏 .tmp 删除，不覆盖已有合法文件）。
func ImportPartMet(path string) (PartMetInfo, error) {
	target := partMetPath(path)
	recoverPartMetSidecar(target)
	recoverPartMetSidecar(target + ".json")
	raw, err := os.ReadFile(target)
	if err != nil {
		return PartMetInfo{}, err
	}
	return ParsePartMetBytes(raw)
}

// ParsePartMetBytes 解析 .part.met 字节内容。
func ParsePartMetBytes(raw []byte) (PartMetInfo, error) {
	if protocol.IsEmulePartMetBytes(raw) {
		return importEmulePartMet(raw)
	}
	if protocol.IsGoed2kPartMetJSON(raw) {
		return importGoed2kPartMetJSON(raw)
	}
	return PartMetInfo{}, errors.New("part.met: unrecognized format")
}

func importEmulePartMet(raw []byte) (PartMetInfo, error) {
	met, err := protocol.ParseEmulePartMet(raw)
	if err != nil {
		return PartMetInfo{}, err
	}
	size, err := protocol.FileSizeFromEmulePartMet(met)
	if err != nil {
		return PartMetInfo{}, err
	}
	gaps, err := protocol.GapsFromEmulePartMet(met)
	if err != nil {
		return PartMetInfo{}, err
	}
	resume := resumeFromGaps(size, met.PieceHashes, gaps)
	return PartMetInfo{
		Hash:     met.Hash,
		FileSize: size,
		Filename: protocol.FilenameFromEmulePartMet(met),
		Resume:   resume,
	}, nil
}

func importGoed2kPartMetJSON(raw []byte) (PartMetInfo, error) {
	var doc PartMetDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return PartMetInfo{}, err
	}
	if doc.Format != partMetFormat {
		return PartMetInfo{}, errors.New("part.met: unknown json format")
	}
	bits := protocol.NewBitField(len(doc.CompletedPieces))
	for i, done := range doc.CompletedPieces {
		if done {
			bits.SetBit(i)
		}
	}
	blocks := make([]data.PieceBlock, 0, len(doc.DownloadedBlocks))
	for _, b := range doc.DownloadedBlocks {
		blocks = append(blocks, data.NewPieceBlock(b.Piece, b.Block))
	}
	peers := make([]protocol.Endpoint, 0, len(doc.KnownPeers))
	for _, s := range doc.KnownPeers {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		host, portStr, ok := strings.Cut(s, ":")
		if !ok {
			continue
		}
		port, err := strconv.Atoi(portStr)
		if err != nil {
			continue
		}
		ep, err := protocol.EndpointFromString(host, port)
		if err != nil {
			continue
		}
		peers = append(peers, ep)
	}
	return PartMetInfo{
		Hash:     doc.FileHash,
		FileSize: doc.FileSize,
		Resume: &protocol.TransferResumeData{
			Hashes:           append([]protocol.Hash(nil), doc.PieceHashes...),
			Pieces:           bits,
			DownloadedBlocks: blocks,
			Peers:            peers,
		},
		HttpSources: append([]string(nil), doc.HttpSources...),
	}, nil
}

func gapsFromResume(fileSize int64, resume *protocol.TransferResumeData) []protocol.PartMetGap {
	if resume == nil || fileSize <= 0 {
		return []protocol.PartMetGap{{Start: 0, End: uint64(fileSize)}}
	}
	ranges := []protocol.PartMetGap{{Start: 0, End: uint64(fileSize)}}
	numPieces := len(resume.Hashes)
	if resume.Pieces.Len() > numPieces {
		numPieces = resume.Pieces.Len()
	}
	for i := 0; i < numPieces; i++ {
		start := uint64(int64(i) * PieceSize)
		end := start + uint64(pieceByteLength(fileSize, i))
		if resume.Pieces.Len() > i && resume.Pieces.GetBit(i) {
			ranges = subtractGapRange(ranges, start, end)
		}
	}
	blockSize := uint64(BlockSize)
	for _, block := range resume.DownloadedBlocks {
		start := uint64(int64(block.PieceIndex)*PieceSize + int64(block.PieceBlock)*BlockSize)
		end := start + blockSize
		if end > uint64(fileSize) {
			end = uint64(fileSize)
		}
		ranges = subtractGapRange(ranges, start, end)
	}
	return mergeAdjacentGaps(ranges)
}

func pieceByteLength(fileSize int64, pieceIndex int) int64 {
	start := int64(pieceIndex) * PieceSize
	remain := fileSize - start
	if remain < PieceSize {
		return remain
	}
	return PieceSize
}

func subtractGapRange(gaps []protocol.PartMetGap, start, end uint64) []protocol.PartMetGap {
	if end <= start {
		return gaps
	}
	out := make([]protocol.PartMetGap, 0, len(gaps)+1)
	for _, g := range gaps {
		if end <= g.Start || start >= g.End {
			out = append(out, g)
			continue
		}
		if start > g.Start {
			out = append(out, protocol.PartMetGap{Start: g.Start, End: start})
		}
		if end < g.End {
			out = append(out, protocol.PartMetGap{Start: end, End: g.End})
		}
	}
	return out
}

func mergeAdjacentGaps(gaps []protocol.PartMetGap) []protocol.PartMetGap {
	if len(gaps) <= 1 {
		return gaps
	}
	out := append([]protocol.PartMetGap(nil), gaps...)
	for i := 0; i < len(out)-1; {
		if out[i].End >= out[i+1].Start {
			if out[i+1].End > out[i].End {
				out[i].End = out[i+1].End
			}
			out = append(out[:i+1], out[i+2:]...)
			continue
		}
		i++
	}
	return out
}

func resumeFromGaps(fileSize int64, pieceHashes []protocol.Hash, gaps []protocol.PartMetGap) *protocol.TransferResumeData {
	numPieces := len(pieceHashes)
	if numPieces == 0 && fileSize > 0 {
		numPieces = int(DivCeil(fileSize, PieceSize))
	}
	bits := protocol.NewBitField(numPieces)
	downloaded := make([]data.PieceBlock, 0)
	for i := 0; i < numPieces; i++ {
		start := uint64(int64(i) * PieceSize)
		end := start + uint64(pieceByteLength(fileSize, i))
		if !rangeOverlapsGaps(start, end, gaps) {
			bits.SetBit(i)
			continue
		}
		downloaded = append(downloaded, completedBlocksInPiece(fileSize, i, gaps)...)
	}
	return &protocol.TransferResumeData{
		Hashes:           append([]protocol.Hash(nil), pieceHashes...),
		Pieces:           bits,
		DownloadedBlocks: downloaded,
	}
}

// completedBlocksInPiece 把 eMule gap 之外、且完整覆盖本实现 BlockSize 的块记入续传。
// 不改 180/190 KiB 常量；只承认当前 BlockSize 下整块已下载的区间，尾片短块按 PieceBlock.Range 夹紧。
func completedBlocksInPiece(fileSize int64, pieceIndex int, gaps []protocol.PartMetGap) []data.PieceBlock {
	pieceLen := pieceByteLength(fileSize, pieceIndex)
	if pieceLen <= 0 {
		return nil
	}
	count := int(DivCeil(pieceLen, BlockSize))
	out := make([]data.PieceBlock, 0, count)
	for j := 0; j < count; j++ {
		block := data.NewPieceBlock(pieceIndex, j)
		r := block.Range(fileSize)
		if r.Right <= r.Left || rangeOverlapsGaps(uint64(r.Left), uint64(r.Right), gaps) {
			continue
		}
		out = append(out, block)
	}
	return out
}

func rangeOverlapsGaps(start, end uint64, gaps []protocol.PartMetGap) bool {
	for _, g := range gaps {
		if end <= g.Start || start >= g.End {
			continue
		}
		return true
	}
	return false
}

func transferredFromResume(fileSize int64, resume *protocol.TransferResumeData) uint64 {
	if resume == nil {
		return 0
	}
	var done uint64
	numPieces := resume.Pieces.Len()
	for i := 0; i < numPieces; i++ {
		if resume.Pieces.GetBit(i) {
			done += uint64(pieceByteLength(fileSize, i))
		}
	}
	for _, block := range resume.DownloadedBlocks {
		if block.PieceIndex >= 0 && block.PieceIndex < numPieces && resume.Pieces.GetBit(block.PieceIndex) {
			continue
		}
		r := block.Range(fileSize)
		if r.Right > r.Left {
			done += uint64(r.Right - r.Left)
		}
	}
	return done
}

func writePartMetAtomic(target string, raw []byte) error {
	partMetWriteMu.Lock()
	defer partMetWriteMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp := target + ".tmp." + strconv.FormatInt(time.Now().UnixNano(), 10)
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := replaceFile(tmp, target); err != nil {
		// 保留完整 tmp，供下次导入/写出时 recover。
		return err
	}
	return nil
}

func replaceFile(tmp, target string) error {
	if err := os.Rename(tmp, target); err == nil {
		return nil
	}
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(tmp, target)
}

// recoverPartMetSidecar 处理崩溃留下的 <file>.tmp：
// 目标已是合法 .part.met 则丢掉过期 tmp；目标缺失或损坏且 tmp 可解析则提升 tmp。
func listPartMetTmps(target string) []string {
	dir := filepath.Dir(target)
	base := filepath.Base(target)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	prefix := base + ".tmp"
	out := make([]string, 0)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if name != prefix && !strings.HasPrefix(name, prefix+".") {
			continue
		}
		out = append(out, filepath.Join(dir, name))
	}
	sort.Slice(out, func(i, j int) bool {
		fi, errI := os.Stat(out[i])
		fj, errJ := os.Stat(out[j])
		if errI != nil || errJ != nil {
			return out[i] > out[j]
		}
		return fi.ModTime().After(fj.ModTime())
	})
	return out
}

func recoverPartMetSidecar(target string) {
	tmps := listPartMetTmps(target)
	if raw, err := os.ReadFile(target); err == nil {
		if _, err := ParsePartMetBytes(raw); err == nil {
			for _, tmp := range tmps {
				_ = os.Remove(tmp)
			}
			return
		}
	}
	for _, tmp := range tmps {
		raw, err := os.ReadFile(tmp)
		if err != nil {
			continue
		}
		if _, err := ParsePartMetBytes(raw); err != nil {
			_ = os.Remove(tmp)
			continue
		}
		if replaceFile(tmp, target) != nil {
			continue
		}
		for _, leftover := range listPartMetTmps(target) {
			_ = os.Remove(leftover)
		}
		return
	}
}

func partMetPath(path string) string {
	return path + ".part.met"
}
