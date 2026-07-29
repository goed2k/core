package goed2k

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/goed2k/core/protocol"
)

const partMetFormat = "goed2k.part.met"
const partMetVersion = 1

// PartMetDocument 为 <file>.part.met JSON 旁注格式（非 eMule 二进制 .part.met 兼容）。
// 详见 docs/part-met-format-CN.md。
type PartMetDocument struct {
	Format           string          `json:"format"`
	Version          int             `json:"version"`
	PieceHashes      []protocol.Hash `json:"piece_hashes,omitempty"`
	CompletedPieces  []bool          `json:"completed_pieces,omitempty"`
	DownloadedBlocks []partMetBlock  `json:"downloaded_blocks,omitempty"`
	KnownPeers       []string        `json:"known_peers,omitempty"`
}

type partMetBlock struct {
	Piece int `json:"piece"`
	Block int `json:"block"`
}

// ExportPartMet 从 TransferResumeData 导出简易 JSON 旁注到 <path>.part.met。
func ExportPartMet(path string, resume *protocol.TransferResumeData) error {
	if path == "" {
		return errors.New("path is empty")
	}
	if resume == nil {
		return errors.New("resume data is nil")
	}
	doc := PartMetDocument{
		Format:  partMetFormat,
		Version: partMetVersion,
	}
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
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	target := partMetPath(path)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func partMetPath(path string) string {
	return path + ".part.met"
}
