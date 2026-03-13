package protocol

import "github.com/monkeyWie/goed2k/data"

type TransferResumeData struct {
	Hashes           []Hash
	Pieces           BitField
	DownloadedBlocks []data.PieceBlock
	Peers            []Endpoint
}
