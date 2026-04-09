package kadv6

import "errors"

// PacketCombiner packs and unpacks KADV6 packets by opcode.
type PacketCombiner struct{}

// Unpack decodes a full packet into a typed message.
func (PacketCombiner) Unpack(packet []byte) (byte, any, error) {
	opcode, payload, err := DecodePacket(packet)
	if err != nil {
		return 0, nil, err
	}
	msg, err := unpackByOpcode(opcode, payload)
	if err != nil {
		return 0, nil, err
	}
	return opcode, msg, nil
}

// Pack encodes a typed message into a full packet.
func (PacketCombiner) Pack(packet any, extra ...byte) ([]byte, error) {
	switch p := packet.(type) {
	case BootstrapReq:
		return p.Pack()
	case BootstrapRes:
		return p.Pack()
	case Hello:
		opcode := HelloReqOp
		if len(extra) > 0 && extra[0] != 0 {
			opcode = extra[0]
		}
		return p.Pack(opcode)
	case FindNodeReq:
		return p.Pack()
	case FindNodeRes:
		return p.Pack()
	case SearchSourcesReq:
		return p.Pack()
	case SearchKeysReq:
		return p.Pack()
	case SearchNotesReq:
		return p.Pack()
	case SearchRes:
		return p.Pack()
	case PublishSourcesReq:
		return p.Pack()
	case PublishKeysReq:
		return p.Pack()
	case PublishNotesReq:
		return p.Pack()
	case PublishRes:
		return p.Pack()
	case Ping:
		return p.Pack()
	case Pong:
		return p.Pack()
	default:
		return nil, errors.New("unsupported kadv6 packet type")
	}
}

func unpackByOpcode(opcode byte, payload []byte) (any, error) {
	var msg any
	switch opcode {
	case BootstrapReqOp:
		msg = &BootstrapReq{}
	case BootstrapResOp:
		msg = &BootstrapRes{}
	case HelloReqOp, HelloResOp:
		msg = &Hello{}
	case FindNodeReqOp:
		msg = &FindNodeReq{}
	case FindNodeResOp:
		msg = &FindNodeRes{}
	case SearchSrcReqOp:
		msg = &SearchSourcesReq{}
	case SearchKeysReqOp:
		msg = &SearchKeysReq{}
	case SearchNotesReqOp:
		msg = &SearchNotesReq{}
	case SearchResOp:
		msg = &SearchRes{}
	case PublishSourceReqOp:
		msg = &PublishSourcesReq{}
	case PublishKeysReqOp:
		msg = &PublishKeysReq{}
	case PublishNotesReqOp:
		msg = &PublishNotesReq{}
	case PublishResOp:
		msg = &PublishRes{}
	case PingOp:
		msg = &Ping{}
	case PongOp:
		msg = &Pong{}
	default:
		return nil, errors.New("unsupported kadv6 opcode")
	}
	switch p := msg.(type) {
	case *BootstrapReq, *Ping:
		return msg, nil
	case *BootstrapRes:
		return msg, p.Unpack(payload)
	case *Hello:
		return msg, p.Unpack(payload)
	case *FindNodeReq:
		return msg, p.Unpack(payload)
	case *FindNodeRes:
		return msg, p.Unpack(payload)
	case *SearchSourcesReq:
		return msg, p.Unpack(payload)
	case *SearchKeysReq:
		return msg, p.Unpack(payload)
	case *SearchNotesReq:
		return msg, p.Unpack(payload)
	case *SearchRes:
		return msg, p.Unpack(payload)
	case *PublishSourcesReq:
		return msg, p.Unpack(payload)
	case *PublishKeysReq:
		return msg, p.Unpack(payload)
	case *PublishNotesReq:
		return msg, p.Unpack(payload)
	case *PublishRes:
		return msg, p.Unpack(payload)
	case *Pong:
		return msg, p.Unpack(payload)
	default:
		return nil, errors.New("unsupported kadv6 packet payload")
	}
}
