package goed2k

import (
	serverproto "github.com/monkeyWie/goed2k/protocol/server"
)

// collectPublishableOfferFiles 合并「已完成可发布的 Transfer」与 SharedStore，按 hash 去重。
func (s *Session) collectPublishableOfferFiles() []serverproto.OfferFile {
	if s == nil {
		return nil
	}
	seen := make(map[string]struct{})
	out := make([]serverproto.OfferFile, 0)
	for _, t := range s.snapshotTransfers() {
		if t == nil || !t.isFinishedForSharePublish() {
			continue
		}
		key := t.GetHash().String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, serverproto.OfferFile{
			Hash: t.GetHash(),
			Name: t.FileName(),
			Size: t.Size(),
		})
	}
	if st := s.SharedStore(); st != nil {
		for _, sf := range st.List() {
			if sf == nil || !sf.Completed || !validateSharedFileOnDisk(sf) {
				continue
			}
			key := sf.Hash.String()
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, serverproto.OfferFile{
				Hash: sf.Hash,
				Name: sf.FileLabel(),
				Size: sf.Size(),
			})
		}
	}
	return out
}
