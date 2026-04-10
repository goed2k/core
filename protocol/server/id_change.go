package server

import (
	"bytes"

	"github.com/goed2k/core/protocol"
)

type IdChange struct {
	ClientID           int32
	TCPFlags           int32
	AuxPort            int32
	ReportedIP         uint32
	ObfuscationTCPPort uint32
}

func (i *IdChange) Get(src *bytes.Reader) error {
	clientID, err := protocol.ReadInt32(src)
	if err != nil {
		return err
	}
	i.ClientID = clientID
	if src.Len() >= 4 {
		tcpFlags, err := protocol.ReadInt32(src)
		if err != nil {
			return err
		}
		i.TCPFlags = tcpFlags
	}
	if src.Len() >= 4 {
		auxPort, err := protocol.ReadInt32(src)
		if err != nil {
			return err
		}
		i.AuxPort = auxPort
	}
	if src.Len() >= 4 {
		v, err := protocol.ReadUInt32(src)
		if err != nil {
			return err
		}
		i.ReportedIP = v
	}
	if src.Len() >= 4 {
		v, err := protocol.ReadUInt32(src)
		if err != nil {
			return err
		}
		i.ObfuscationTCPPort = v
	}
	return nil
}

func (i IdChange) Put(dst *bytes.Buffer) error {
	if err := protocol.WriteInt32(dst, i.ClientID); err != nil {
		return err
	}
	if err := protocol.WriteInt32(dst, i.TCPFlags); err != nil {
		return err
	}
	if err := protocol.WriteInt32(dst, i.AuxPort); err != nil {
		return err
	}
	if err := protocol.WriteUInt32(dst, i.ReportedIP); err != nil {
		return err
	}
	return protocol.WriteUInt32(dst, i.ObfuscationTCPPort)
}

func (i IdChange) BytesCount() int { return 20 }
