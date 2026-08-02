package goed2k

import (
	"bytes"

	"github.com/goed2k/core/protocol"
	clientproto "github.com/goed2k/core/protocol/client"
)

func (p *PeerConnection) FlushOutgoing() error {
	p.tryCoalesceOutgoingMultiPacket()
	return p.Connection.FlushOutgoing()
}

func (p *PeerConnection) peerSupportsMultiPacket() bool {
	if p == nil {
		return false
	}
	if p.remotePeerInfo.Misc1.MultiPacket > 0 {
		return true
	}
	return p.remotePeerInfo.Misc2.SupportExtMultipacket()
}

func (p *PeerConnection) tryCoalesceOutgoingMultiPacket() {
	// 出站 MultiPacket 合并暂不启用：与 CryptLayer 在 CI 环境下存在互操作问题。
}

func (p *PeerConnection) expandIncomingMultiPackets() error {
	for {
		raw := p.DrainIncoming()
		if len(raw) < protocol.PacketHeaderSize {
			if len(raw) > 0 {
				p.prependIncoming(raw)
			}
			return nil
		}
		reader := bytes.NewReader(raw)
		var header protocol.PacketHeader
		if err := header.Get(reader); err != nil {
			p.prependIncoming(raw)
			return err
		}
		if header.Protocol != protocol.PackedProt || header.Packet != clientproto.MultiPacketOpcode() {
			p.prependIncoming(raw)
			return nil
		}
		bodySize := int(header.SizePacket())
		if reader.Len() < bodySize {
			p.prependIncoming(raw)
			return nil
		}
		body, err := protocol.ReadBytes(reader, bodySize)
		if err != nil {
			p.prependIncoming(raw)
			return err
		}
		frames, err := clientproto.UnpackMultiPacketExt2(body)
		if err != nil {
			p.prependIncoming(raw)
			return nil
		}
		if len(frames) == 0 {
			p.prependIncoming(raw)
			return nil
		}
		inner := make([]byte, 0, len(raw))
		for _, f := range frames {
			inner = append(inner, f...)
		}
		if tail := raw[len(raw)-reader.Len():]; len(tail) > 0 {
			inner = append(inner, tail...)
		}
		p.prependIncoming(inner)
	}
}

func (p *PeerConnection) maybeSendSourceExchange() {
	if p.sourceExchangeSent || p.transfer == nil {
		return
	}
	if p.remotePeerInfo.Misc1.SourceExchange1Ver == 0 {
		return
	}
	var err error
	if p.remotePeerInfo.SourceExchange2Ver >= clientproto.SourceExchange2Version {
		err = p.SendRequestSources2(p.transfer.GetHash())
	} else {
		err = p.SendRequestSources(p.transfer.GetHash())
	}
	if err != nil {
		return
	}
	p.sourceExchangeSent = true
}

func (p *PeerConnection) SendRequestSources(hash protocol.Hash) error {
	pkt := clientproto.RequestSources{Hash: hash}
	raw, err := p.combiner.Pack("client.RequestSources", &pkt)
	if err != nil {
		return err
	}
	debugPeerf("peer %s -> RequestSources", p.endpoint.String())
	p.QueuePacket(raw)
	return nil
}

func (p *PeerConnection) HandleRequestSources(req *clientproto.RequestSources) {
	if req == nil {
		return
	}
	res := p.attachUploadByHash(req.Hash)
	if tf, ok := res.(*Transfer); ok && tf != nil {
		peers := tf.policy.PeersForSourceExchange(p.endpoint, SourceExchangePeerLimit)
		if len(peers) == 0 {
			return
		}
		entries := p.buildSourceExchangeEntriesSX1(peers)
		p.sendAnswerSources(req.Hash, entries)
		return
	}
	if sf, ok := res.(*SharedFile); ok && sf != nil {
		_ = sf
		p.sendAnswerSourcesSelfOnlySX1(req.Hash)
	}
}

func (p *PeerConnection) sendAnswerSourcesSelfOnlySX1(hash protocol.Hash) {
	ep := p.session.kadPublishEndpoint()
	if !ep.Defined() {
		return
	}
	serverIP, serverPort := p.session.serverSourceExchangeIDs()
	sx1 := p.remotePeerInfo.Misc1.SourceExchange1Ver
	uid := uint32(ep.IP())
	if sx1 <= 2 {
		uid = clientproto.SwapUint32(uid)
	}
	entries := []clientproto.SourceExchangeEntry{{
		UserID:     uid,
		TCPPort:    uint16(ep.Port()),
		ServerIP:   serverIP,
		ServerPort: serverPort,
	}}
	p.sendAnswerSources(hash, entries)
}

func (p *PeerConnection) buildSourceExchangeEntriesSX1(peers []Peer) []clientproto.SourceExchangeEntry {
	sx1 := p.remotePeerInfo.Misc1.SourceExchange1Ver
	serverIP, serverPort := p.session.serverSourceExchangeIDs()
	out := make([]clientproto.SourceExchangeEntry, 0, len(peers))
	for _, pe := range peers {
		entry, ok := pe.ToSourceExchangeEntry(sx1, clientproto.SourceExchange2Version, cryptOptionsForLocal(p.session.settings))
		if !ok {
			continue
		}
		entry.ServerIP = serverIP
		entry.ServerPort = serverPort
		out = append(out, entry)
	}
	return out
}

func (p *PeerConnection) sendAnswerSources(hash protocol.Hash, entries []clientproto.SourceExchangeEntry) {
	if len(entries) == 0 {
		return
	}
	ans := clientproto.AnswerSources{Hash: hash, Entries: entries}
	raw, err := p.combiner.Pack("client.AnswerSources", &ans)
	if err != nil {
		return
	}
	debugPeerf("peer %s -> AnswerSources entries=%d", p.endpoint.String(), len(entries))
	p.QueuePacket(raw)
}

func (p *PeerConnection) HandleAnswerSources(ans *clientproto.AnswerSources) {
	if ans == nil || p.transfer == nil {
		return
	}
	if !ans.Hash.Equal(p.transfer.GetHash()) {
		return
	}
	packetVer := byte(2)
	if p.remotePeerInfo.Misc1.SourceExchange1Ver >= 3 {
		packetVer = 3
	}
	peers := make([]Peer, 0, len(ans.Entries))
	for _, e := range ans.Entries {
		peer, ok := peerFromSourceExchangeEntry(e, packetVer)
		if !ok {
			continue
		}
		if peer.Endpoint.Defined() && !p.isAcceptableSourceExchangeEndpoint(peer.Endpoint) {
			continue
		}
		if peer.DialAddr != nil && isFilteredPeerTCPAddr(peer.DialAddr) {
			continue
		}
		peers = append(peers, peer)
	}
	if n := p.transfer.policy.MergeSourceExchangePeers(peers); n > 0 {
		debugPeerf("peer %s <- AnswerSources merged=%d", p.endpoint.String(), n)
	}
}

func (p *PeerConnection) SendRequestPreview(hash protocol.Hash, pieceIndex uint16) {
	if p.remotePeerInfo.Misc1.SupportsPreview == 0 {
		return
	}
	pkt := clientproto.RequestPreview{Hash: hash, PieceIndex: pieceIndex}
	if raw, err := p.combiner.Pack("client.RequestPreview", &pkt); err == nil {
		debugPeerf("peer %s -> RequestPreview piece=%d", p.endpoint.String(), pieceIndex)
		p.QueuePacket(raw)
	}
}

func (p *PeerConnection) maybeSendPreviewRequest() {
	if p.transfer == nil || p.remotePeerInfo.Misc1.SupportsPreview == 0 {
		return
	}
	if _, ok := p.transfer.PreviewPiece(0); ok {
		return
	}
	p.SendRequestPreview(p.transfer.GetHash(), 0)
}

func (p *PeerConnection) HandleClientRequestPreview(req *clientproto.RequestPreview) {
	if req == nil || p.remotePeerInfo.Misc1.SupportsPreview == 0 {
		return
	}
	res := p.attachUploadByHash(req.Hash)
	if res == nil {
		return
	}
	pieceIndex := int(req.PieceIndex)
	begin := int64(pieceIndex) * PieceSize
	var end int64
	if tf, ok := res.(*Transfer); ok && tf != nil {
		end = begin + tf.pieceSize(pieceIndex)
	} else {
		end = begin + PieceSize
		if end > res.Size() {
			end = res.Size()
		}
	}
	if !res.CanUploadRange(begin, end) {
		return
	}
	data, err := res.ReadRange(begin, end)
	if err != nil || len(data) == 0 {
		return
	}
	if len(data) > clientproto.MaxPreviewBytes {
		data = data[:clientproto.MaxPreviewBytes]
	}
	ans := clientproto.PreviewAnswer{
		Hash:       req.Hash,
		PieceIndex: req.PieceIndex,
		Data:       data,
	}
	if raw, err := p.combiner.Pack("client.PreviewAnswer", &ans); err == nil {
		p.QueuePacket(raw)
	}
}

func (p *PeerConnection) HandlePreviewAnswer(ans *clientproto.PreviewAnswer) {
	if ans == nil || p.transfer == nil || !ans.Hash.Equal(p.transfer.GetHash()) {
		return
	}
	p.transfer.StorePreviewPiece(ans.PieceIndex, ans.Data)
}
