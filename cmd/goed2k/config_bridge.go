package main

import (
	"github.com/goed2k/core/bootstrap"
)

func (cfg runConfig) bootstrapConfig() bootstrap.Config {
	// 从 DefaultConfig 起步，避免零值把 PartialKadPublish/EnableWebDownload 等映射成 false。
	b := bootstrap.DefaultConfig()
	b.ApplyEnv("GOED2K_")
	b.Links = append([]string(nil), cfg.links...)
	b.OutDir = cfg.outDir
	b.ServerAddr = cfg.serverAddr
	b.ServerMetPath = cfg.serverMetPath
	b.ListenPort = cfg.listenPort
	b.UDPPort = cfg.udpPort
	b.UDPPortV6 = cfg.udpPortV6
	b.EnableKAD = cfg.enableKAD
	b.EnableKADV6 = cfg.enableKADV6
	b.EnableUPnP = cfg.enableUPnP
	b.EnableCryptLayer = cfg.enableCryptLayer
	b.EnableCryptLayerRequired = cfg.enableCryptLayerRequired
	b.EnableSecIdent = cfg.enableSecIdent
	b.SecIdentRequired = cfg.secIdentRequired
	b.CreditsOnlyVerified = cfg.creditsOnlyVerified
	b.IdentityKeyPath = cfg.identityKeyPath
	b.CategoriesConfig = cfg.categoriesConfig
	b.KADNodesDat = cfg.kadNodesDat
	b.KADNodes = cfg.kadNodes
	b.KADV6NodesDat = cfg.kadv6NodesDat
	b.KADV6Nodes = cfg.kadv6Nodes
	b.PeerTimeout = cfg.peerTimeout
	b.MaxDownloadRateKB = cfg.maxDownloadRateKB
	b.MaxUploadRateKB = cfg.maxUploadRateKB
	b.Timeout = cfg.timeout
	b.StatePath = cfg.statePath
	b.DisableState = cfg.disableState
	return b
}
