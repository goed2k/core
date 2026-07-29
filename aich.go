package goed2k

import (
	"crypto/sha1"
	"io"
	"os"

	"github.com/goed2k/core/protocol"
)

const (
	AICHBlockSize       = 180 * 1024
	AICHPieceSize       = int64(9728000)
	AICHBlocksPerPiece  = 53
	AICHLastBlockSize   = AICHPieceSize - int64(AICHBlocksPerPiece-1)*AICHBlockSize
)

// ComputeAICHHash returns the SHA-1 block hash for a single AICH sub-block.
func ComputeAICHHash(data []byte) protocol.AICHHash {
	return protocol.AICHHashFromSHA1(data)
}

// VerifyAICHBlock checks whether pieceData matches the expected AICH block hash.
func VerifyAICHBlock(pieceData []byte, expected protocol.AICHHash) bool {
	return ComputeAICHHash(pieceData).Equal(expected)
}

// AICHBlockCount returns how many 180KB AICH blocks cover size bytes.
func AICHBlockCount(size int64) int {
	if size <= 0 {
		return 0
	}
	return int((size + int64(AICHBlockSize) - 1) / int64(AICHBlockSize))
}

// BuildAICHBlockHashes splits data into 180KB blocks and hashes each with SHA-1.
func BuildAICHBlockHashes(data []byte) []protocol.AICHHash {
	if len(data) == 0 {
		return nil
	}
	count := AICHBlockCount(int64(len(data)))
	out := make([]protocol.AICHHash, 0, count)
	for offset := 0; offset < len(data); offset += AICHBlockSize {
		end := offset + AICHBlockSize
		if end > len(data) {
			end = len(data)
		}
		out = append(out, ComputeAICHHash(data[offset:end]))
	}
	return out
}

// BuildAICHTreeRoot builds the binary SHA-1 tree over leaf hashes (eMule AICH).
func BuildAICHTreeRoot(leaves []protocol.AICHHash) protocol.AICHHash {
	if len(leaves) == 0 {
		return protocol.InvalidAICH
	}
	level := append([]protocol.AICHHash(nil), leaves...)
	for len(level) > 1 {
		next := make([]protocol.AICHHash, 0, (len(level)+1)/2)
		for i := 0; i < len(level); i += 2 {
			if i+1 < len(level) {
				next = append(next, protocol.CombineAICHHashes(level[i], level[i+1]))
			} else {
				next = append(next, level[i])
			}
		}
		level = next
	}
	return level[0]
}

// BuildAICHPieceRoot computes the AICH tree root for one ed2k piece/chunk.
func BuildAICHPieceRoot(pieceData []byte) protocol.AICHHash {
	return BuildAICHTreeRoot(BuildAICHBlockHashes(pieceData))
}

// BuildAICHRootFromReader streams file data and returns the file-level AICH root hash.
func BuildAICHRootFromReader(r io.Reader, size int64) (protocol.AICHHash, error) {
	if size <= 0 {
		return protocol.InvalidAICH, os.ErrInvalid
	}
	if size < int64(AICHBlockSize) {
		buf := make([]byte, size)
		if _, err := io.ReadFull(r, buf); err != nil {
			return protocol.InvalidAICH, err
		}
		return ComputeAICHHash(buf), nil
	}

	pieceRoots := make([]protocol.AICHHash, 0, int(DivCeil(size, AICHPieceSize)))
	buf := make([]byte, AICHPieceSize)
	for offset := int64(0); offset < size; {
		remain := size - offset
		n := int64(len(buf))
		if remain < n {
			n = remain
		}
		if _, err := io.ReadFull(r, buf[:n]); err != nil {
			return protocol.InvalidAICH, err
		}
		pieceRoots = append(pieceRoots, BuildAICHPieceRoot(buf[:n]))
		offset += n
	}
	return BuildAICHTreeRoot(pieceRoots), nil
}

// BuildAICHRootFromFile computes the AICH root hash for a local file.
func BuildAICHRootFromFile(path string) (protocol.AICHHash, error) {
	f, err := os.Open(path)
	if err != nil {
		return protocol.InvalidAICH, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return protocol.InvalidAICH, err
	}
	return BuildAICHRootFromReader(f, fi.Size())
}

// BuildAICHRootFromData computes the AICH root hash for an in-memory blob.
func BuildAICHRootFromData(data []byte) protocol.AICHHash {
	root, err := BuildAICHRootFromReader(bytesReader(data), int64(len(data)))
	if err != nil {
		return protocol.InvalidAICH
	}
	return root
}

func bytesReader(data []byte) io.Reader {
	return &sliceReader{data: data}
}

type sliceReader struct {
	data []byte
	off  int
}

func (s *sliceReader) Read(p []byte) (int, error) {
	if s.off >= len(s.data) {
		return 0, io.EOF
	}
	n := copy(p, s.data[s.off:])
	s.off += n
	return n, nil
}

// LocateCorruptAICHBlocks compares piece data against trusted block hashes.
func LocateCorruptAICHBlocks(pieceData []byte, blockHashes []protocol.AICHHash) []int {
	if len(pieceData) == 0 || len(blockHashes) == 0 {
		return nil
	}
	bad := make([]int, 0)
	for i, expected := range blockHashes {
		begin := i * AICHBlockSize
		if begin >= len(pieceData) {
			break
		}
		end := begin + AICHBlockSize
		if end > len(pieceData) {
			end = len(pieceData)
		}
		if !VerifyAICHBlock(pieceData[begin:end], expected) {
			bad = append(bad, i)
		}
	}
	return bad
}

// AICHHasher incrementally hashes a file for AICH root computation.
type AICHHasher struct {
	pieceBuf   []byte
	pieceRoots []protocol.AICHHash
}

func NewAICHHasher() *AICHHasher {
	return &AICHHasher{pieceBuf: make([]byte, 0, AICHPieceSize)}
}

func (h *AICHHasher) Write(p []byte) (int, error) {
	h.pieceBuf = append(h.pieceBuf, p...)
	for len(h.pieceBuf) >= int(AICHPieceSize) {
		chunk := h.pieceBuf[:AICHPieceSize]
		h.pieceRoots = append(h.pieceRoots, BuildAICHPieceRoot(chunk))
		h.pieceBuf = h.pieceBuf[AICHPieceSize:]
	}
	return len(p), nil
}

func (h *AICHHasher) Sum() protocol.AICHHash {
	if len(h.pieceBuf) == 0 && len(h.pieceRoots) == 0 {
		return protocol.InvalidAICH
	}
	if len(h.pieceRoots) == 0 {
		return BuildAICHPieceRoot(h.pieceBuf)
	}
	roots := append([]protocol.AICHHash(nil), h.pieceRoots...)
	if len(h.pieceBuf) > 0 {
		roots = append(roots, BuildAICHPieceRoot(h.pieceBuf))
	}
	return BuildAICHTreeRoot(roots)
}

// SHA1Sum exposes raw SHA-1 for tests.
func SHA1Sum(data []byte) [sha1.Size]byte {
	return sha1.Sum(data)
}

const aichRequestMarker = 0xFF

func AICHRequestMarker(pieceIndex, blockIndex int) protocol.AICHHash {
	var h protocol.AICHHash
	h[0] = aichRequestMarker
	h[1] = byte(pieceIndex >> 8)
	h[2] = byte(pieceIndex)
	h[3] = byte(blockIndex >> 8)
	h[4] = byte(blockIndex)
	return h
}

func DecodeAICHRequestMarker(h protocol.AICHHash) (pieceIndex, blockIndex int, ok bool) {
	if h[0] != aichRequestMarker {
		return 0, 0, false
	}
	pieceIndex = int(h[1])<<8 | int(h[2])
	blockIndex = int(h[3])<<8 | int(h[4])
	return pieceIndex, blockIndex, true
}

func isAICHMarkerRequest(requested []protocol.AICHHash) (pieceIndex int, blockCount int, ok bool) {
	if len(requested) == 0 {
		return 0, 0, false
	}
	pieceIndex = -1
	for i, req := range requested {
		piece, block, marker := DecodeAICHRequestMarker(req)
		if !marker {
			return 0, 0, false
		}
		if pieceIndex < 0 {
			pieceIndex = piece
		} else if pieceIndex != piece {
			return 0, 0, false
		}
		if block != i {
			return 0, 0, false
		}
	}
	return pieceIndex, len(requested), true
}

type aichPieceLoader func(pieceIndex int) ([]protocol.AICHHash, error)

func matchAICHHashes(requested []protocol.AICHHash, root protocol.AICHHash, loader aichPieceLoader, fileSize int64) []protocol.AICHHash {
	if len(requested) == 0 {
		return nil
	}
	if pieceIndex, blockCount, ok := isAICHMarkerRequest(requested); ok {
		leaves, err := loader(pieceIndex)
		if err != nil || len(leaves) < blockCount {
			return nil
		}
		return append([]protocol.AICHHash(nil), leaves[:blockCount]...)
	}
	known := make(map[protocol.AICHHash]protocol.AICHHash, len(requested))
	for _, req := range requested {
		if req.Equal(root) {
			known[req] = root
		}
	}
	if len(known) == len(requested) {
		out := make([]protocol.AICHHash, len(requested))
		copy(out, requested)
		return out
	}
	pieceCount := int(DivCeil(fileSize, AICHPieceSize))
	for pieceIndex := 0; pieceIndex < pieceCount; pieceIndex++ {
		leaves, err := loader(pieceIndex)
		if err != nil || len(leaves) == 0 {
			continue
		}
		tree := buildAICHHashTree(leaves)
		for _, req := range requested {
			if _, ok := known[req]; ok {
				continue
			}
			if h, ok := tree[req]; ok {
				known[req] = h
			}
		}
		if len(known) == len(requested) {
			break
		}
	}
	out := make([]protocol.AICHHash, 0, len(requested))
	for _, req := range requested {
		if h, ok := known[req]; ok {
			out = append(out, h)
		}
	}
	if len(out) != len(requested) {
		return nil
	}
	return out
}

func buildAICHHashTree(leaves []protocol.AICHHash) map[protocol.AICHHash]protocol.AICHHash {
	known := make(map[protocol.AICHHash]protocol.AICHHash)
	if len(leaves) == 0 {
		return known
	}
	level := append([]protocol.AICHHash(nil), leaves...)
	for _, leaf := range leaves {
		known[leaf] = leaf
	}
	for len(level) > 1 {
		next := make([]protocol.AICHHash, 0, (len(level)+1)/2)
		for i := 0; i < len(level); i += 2 {
			if i+1 < len(level) {
				combined := protocol.CombineAICHHashes(level[i], level[i+1])
				known[combined] = combined
				next = append(next, combined)
			} else {
				known[level[i]] = level[i]
				next = append(next, level[i])
			}
		}
		level = next
	}
	return known
}
