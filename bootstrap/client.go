package bootstrap

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	ed2k "github.com/goed2k/core"
)

// Notifier receives non-fatal bootstrap warnings.
type Notifier func(format string, args ...any)

// BuildSettings maps Config to library Settings.
func BuildSettings(cfg Config) (ed2k.Settings, error) {
	settings := ed2k.NewSettings()
	settings.ReconnectToServer = true
	settings.ListenPort = cfg.ListenPort
	settings.UDPPort = cfg.UDPPort
	settings.UDPPortV6 = cfg.UDPPortV6
	settings.EnableDHT = cfg.EnableKAD
	settings.EnableDHTv6 = cfg.EnableKADV6
	settings.EnableUPnP = cfg.EnableUPnP
	settings.EnableCryptLayer = cfg.EnableCryptLayer
	settings.CryptLayerRequired = cfg.EnableCryptLayerRequired
	settings.EnableSecIdent = cfg.EnableSecIdent
	settings.SecIdentRequired = cfg.SecIdentRequired
	settings.CreditsOnlyVerified = cfg.CreditsOnlyVerified
	settings.IdentityKeyPath = cfg.IdentityKeyPath
	settings.PeerConnectionTimeout = cfg.PeerTimeout
	settings.MaxDownloadRateKB = cfg.MaxDownloadRateKB
	settings.MaxUploadRateKB = cfg.MaxUploadRateKB
	settings.UseEmuleTempLayout = cfg.UseEmuleTempLayout
	settings.PartialKadPublish = cfg.PartialKadPublish
	settings.PreallocateDiskSpace = cfg.PreallocateDiskSpace
	settings.UseSparseFiles = cfg.UseSparseFiles
	settings.EnableWebDownload = cfg.EnableWebDownload
	settings.MaxHttpSources = cfg.MaxHttpSources
	settings.MaxConcurrentHttpBlocks = cfg.MaxConcurrentHttpBlocks
	settings.WebCacheDir = cfg.WebCacheDir
	settings.HttpRequestTimeoutSec = cfg.HttpRequestTimeoutSec
	if cfg.CategoriesConfig != "" {
		cats, err := ed2k.ParseCategoriesConfig(cfg.CategoriesConfig)
		if err != nil {
			return ed2k.Settings{}, fmt.Errorf("parse categories: %w", err)
		}
		settings.Categories = cats
	}
	return settings, nil
}

// InitClient creates a client, optionally loads persisted state, and starts listening.
func InitClient(cfg Config, logger *slog.Logger) (*ed2k.Client, error) {
	settings, err := BuildSettings(cfg)
	if err != nil {
		return nil, err
	}
	if logger != nil {
		settings.Logger = logger
	}

	client := ed2k.NewClient(settings)
	if cfg.EnableKAD {
		client.EnableDHT()
	}
	if cfg.EnableKADV6 {
		client.EnableDHTv6()
	}
	if cfg.EnableSecIdent {
		path := ed2k.EnsureIdentityKeyForSecIdent(&settings)
		if path != "" {
			if err := client.LoadIdentity(path); err != nil {
				return nil, fmt.Errorf("load identity key: %w", err)
			}
		}
	} else if path := strings.TrimSpace(cfg.IdentityKeyPath); path != "" {
		if err := client.LoadIdentity(path); err != nil {
			return nil, fmt.Errorf("load identity key: %w", err)
		}
	}

	if !cfg.DisableState && strings.TrimSpace(cfg.StatePath) != "" {
		if err := os.MkdirAll(filepathDir(cfg.StatePath), 0o755); err != nil {
			return nil, fmt.Errorf("create state directory: %w", err)
		}
		client.SetStatePath(cfg.StatePath)
		if err := client.LoadState(""); err != nil {
			return nil, fmt.Errorf("load state: %w", err)
		}
		// bootstrap/CLI 配置覆盖状态里恢复的可持久化策略，避免旧 state 压过本次启动参数。
		// 空路径字段不覆盖 LoadState 已恢复的非空 IncomingDir / WebCacheDir。
		client.OverlayPersistableSettings(settings)
	}

	if err := client.Start(); err != nil {
		return nil, fmt.Errorf("listen failed on port %d: %w", settings.ListenPort, err)
	}

	outDir := strings.TrimSpace(cfg.OutDir)
	if outDir == "" {
		outDir = "."
	}
	for _, link := range cfg.Links {
		link = strings.TrimSpace(link)
		if link == "" {
			continue
		}
		if _, _, err := client.AddLink(link, outDir); err != nil {
			return nil, fmt.Errorf("add link %q: %w", link, err)
		}
	}

	return client, nil
}

// RunBackground starts server/DHT bootstrap goroutines.
func RunBackground(client *ed2k.Client, cfg Config, notify Notifier) {
	if client == nil {
		return
	}
	if cfg.ServerAddr != "" {
		go connectServersBestEffort(client, notify, SplitCommaList(cfg.ServerAddr))
	}
	if cfg.ServerMetPath != "" {
		go loadServerMetBestEffort(client, notify, SplitCommaList(cfg.ServerMetPath))
	}
	if cfg.EnableKAD && cfg.KADNodesDat != "" {
		go func() {
			if err := client.LoadDHTNodesDat(cfg.KADNodesDat); err != nil && notify != nil {
				notify("KAD nodes.dat unavailable: %v", err)
			}
		}()
	}
	if cfg.EnableKAD && cfg.KADNodes != "" {
		go func() {
			if err := client.AddDHTBootstrapNodes(cfg.KADNodes); err != nil && notify != nil {
				notify("KAD bootstrap nodes ignored: %v", err)
			}
		}()
	}
	if cfg.EnableKADV6 && cfg.KADV6NodesDat != "" {
		go func() {
			if err := client.LoadDHTv6NodesDat(cfg.KADV6NodesDat); err != nil && notify != nil {
				notify("KADV6 nodes6.dat unavailable: %v", err)
			}
		}()
	}
	if cfg.EnableKADV6 && cfg.KADV6Nodes != "" {
		go func() {
			if err := client.AddDHTv6BootstrapNodes(cfg.KADV6Nodes); err != nil && notify != nil {
				notify("KADV6 bootstrap nodes ignored: %v", err)
			}
		}()
	}
}

func loadServerMetBestEffort(client *ed2k.Client, notify Notifier, sources []string) {
	for _, item := range sources {
		entries, err := client.LoadServerMet(item)
		if err != nil {
			if notify != nil {
				notify("server.met unavailable: %v", err)
			}
			continue
		}
		addrs := make([]string, 0, len(entries))
		for _, entry := range entries {
			if addr := entry.Address(); addr != "" {
				addrs = append(addrs, addr)
			}
		}
		connectServersBestEffort(client, notify, addrs)
	}
}

func connectServersBestEffort(client *ed2k.Client, notify Notifier, servers []string) {
	seen := make(map[string]struct{}, len(servers))
	for _, serverAddr := range servers {
		serverAddr = strings.TrimSpace(serverAddr)
		if serverAddr == "" {
			continue
		}
		if _, ok := seen[serverAddr]; ok {
			continue
		}
		seen[serverAddr] = struct{}{}
		if err := client.Connect(serverAddr); err != nil && notify != nil {
			notify("server unavailable: %s (%v)", serverAddr, err)
		}
	}
}

func filepathDir(path string) string {
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return "."
	}
	return dir
}
