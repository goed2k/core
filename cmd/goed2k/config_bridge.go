package main

import (
	"github.com/goed2k/core/internal/bootstrap"
)

func (cfg runConfig) bootstrapConfig() bootstrap.Config {
	return bootstrap.Config{
		Links:                    append([]string(nil), cfg.links...),
		OutDir:                   cfg.outDir,
		ServerAddr:               cfg.serverAddr,
		ServerMetPath:            cfg.serverMetPath,
		ListenPort:               cfg.listenPort,
		UDPPort:                  cfg.udpPort,
		UDPPortV6:                cfg.udpPortV6,
		EnableKAD:                cfg.enableKAD,
		EnableKADV6:              cfg.enableKADV6,
		EnableUPnP:               cfg.enableUPnP,
		EnableCryptLayer:         cfg.enableCryptLayer,
		EnableCryptLayerRequired: cfg.enableCryptLayerRequired,
		EnableSecIdent:           cfg.enableSecIdent,
		CreditsOnlyVerified:      cfg.creditsOnlyVerified,
		IdentityKeyPath:          cfg.identityKeyPath,
		CategoriesConfig:         cfg.categoriesConfig,
		KADNodesDat:              cfg.kadNodesDat,
		KADNodes:                 cfg.kadNodes,
		KADV6NodesDat:            cfg.kadv6NodesDat,
		KADV6Nodes:               cfg.kadv6Nodes,
		PeerTimeout:              cfg.peerTimeout,
		MaxDownloadRateKB:        cfg.maxDownloadRateKB,
		Timeout:                  cfg.timeout,
		StatePath:                cfg.statePath,
		DisableState:             cfg.disableState,
	}
}
