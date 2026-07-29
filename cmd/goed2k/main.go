package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	ed2k "github.com/goed2k/core"
	"github.com/goed2k/core/bootstrap"
)

type runConfig struct {
	links                    []string
	outDir                   string
	serverAddr               string
	serverMetPath            string
	listenPort               int
	udpPort                  int
	udpPortV6                int
	enableKAD                bool
	enableKADV6              bool
	enableUPnP               bool
	enableCryptLayer         bool
	enableCryptLayerRequired bool
	enableSecIdent           bool
	secIdentRequired         bool
	creditsOnlyVerified      bool
	identityKeyPath          string
	categoriesConfig         string
	kadNodesDat              string
	kadNodes                 string
	kadv6NodesDat            string
	kadv6Nodes               string
	peerTimeout              int
	maxDownloadRateKB        int
	maxUploadRateKB          int
	timeout                  time.Duration
	statePath                string
	disableState             bool
}

type linksFlag []string

func (f *linksFlag) String() string { return strings.Join(*f, ",") }

func (f *linksFlag) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("link is empty")
	}
	*f = append(*f, value)
	return nil
}

type appContext struct {
	client       *ed2k.Client
	targetPaths  []string
	includeDHT   bool
	includeDHTv6 bool
	deadline     time.Time
	noticeCh     chan string
}

func main() {
	cfg := defaultRunConfig()
	var links linksFlag
	var setupWizard bool
	var timeoutRaw string
	var securePreset bool

	flag.BoolVar(&setupWizard, "setup", false, "run interactive setup wizard before starting")
	flag.StringVar(&cfg.outDir, "out-dir", cfg.outDir, "default download directory")
	flag.StringVar(&cfg.serverAddr, "server", cfg.serverAddr, "ED2K servers (comma-separated host:port)")
	flag.StringVar(&cfg.serverMetPath, "server-met", cfg.serverMetPath, "server.met path, URL, or ed2k serverlist link")
	flag.BoolVar(&cfg.enableKAD, "kad", cfg.enableKAD, "enable KAD DHT (use -kad=false to disable)")
	flag.BoolVar(&cfg.enableUPnP, "upnp", cfg.enableUPnP, "enable UPnP port mapping (use -upnp=false to disable)")
	flag.IntVar(&cfg.listenPort, "listen-port", cfg.listenPort, "TCP listen port")
	flag.IntVar(&cfg.udpPort, "udp-port", cfg.udpPort, "KAD UDP listen port")
	flag.StringVar(&cfg.kadNodesDat, "kad-nodes-dat", cfg.kadNodesDat, "KAD nodes.dat path or URL")
	flag.StringVar(&cfg.kadNodes, "kad-bootstrap", cfg.kadNodes, "KAD bootstrap nodes (comma-separated udp-host:port)")
	flag.BoolVar(&cfg.enableKADV6, "kadv6", cfg.enableKADV6, "enable KADV6 IPv6 DHT")
	flag.BoolVar(&cfg.enableCryptLayer, "crypt-layer", cfg.enableCryptLayer, "enable TCP protocol obfuscation (CryptLayer)")
	flag.BoolVar(&cfg.enableCryptLayerRequired, "crypt-layer-required", cfg.enableCryptLayerRequired, "require CryptLayer for peer connections")
	flag.BoolVar(&cfg.enableSecIdent, "sec-ident", cfg.enableSecIdent, "enable Secure Ident handshake")
	flag.BoolVar(&cfg.secIdentRequired, "sec-ident-required", cfg.secIdentRequired, "disconnect peers that fail Secure Ident verification")
	flag.BoolVar(&cfg.creditsOnlyVerified, "credits-only-verified", cfg.creditsOnlyVerified, "only accumulate credits for SecIdent-verified peers")
	flag.BoolVar(&securePreset, "secure", false, "enable CryptLayer and SecIdent (shorthand for --crypt-layer --sec-ident)")
	flag.StringVar(&cfg.identityKeyPath, "identity-key", cfg.identityKeyPath, "path to RSA identity private key PEM for Secure Ident")
	flag.IntVar(&cfg.maxDownloadRateKB, "max-download-rate-kb", cfg.maxDownloadRateKB, "global download rate limit in KB/s (0=unlimited)")
	flag.IntVar(&cfg.maxUploadRateKB, "max-upload-rate-kb", cfg.maxUploadRateKB, "global upload rate limit in KB/s (0=unlimited)")
	flag.StringVar(&cfg.categoriesConfig, "categories", cfg.categoriesConfig, "download categories: name:ext1,ext2:dir;name2:ext:dir2")
	flag.IntVar(&cfg.udpPortV6, "udp-port-v6", cfg.udpPortV6, "KADV6 UDP listen port")
	flag.StringVar(&cfg.kadv6NodesDat, "kadv6-nodes-dat", cfg.kadv6NodesDat, "KADV6 nodes6.dat path or URL")
	flag.StringVar(&cfg.kadv6Nodes, "kadv6-bootstrap", cfg.kadv6Nodes, "KADV6 bootstrap nodes")
	flag.IntVar(&cfg.peerTimeout, "peer-timeout", cfg.peerTimeout, "peer connection timeout in seconds")
	flag.StringVar(&timeoutRaw, "timeout", "", "exit after duration when all transfers complete (e.g. 30m, 0=disabled)")
	flag.StringVar(&cfg.statePath, "state-path", cfg.statePath, "path to JSON state file for persistence")
	flag.BoolVar(&cfg.disableState, "no-state", cfg.disableState, "disable state persistence")
	flag.Var(&links, "link", "ed2k link to queue at startup (repeatable)")
	flag.Parse()

	if securePreset {
		cfg.enableCryptLayer = true
		cfg.enableSecIdent = true
	}

	if len(links) > 0 {
		cfg.links = append(cfg.links, links...)
	}
	if strings.TrimSpace(timeoutRaw) != "" {
		d, err := time.ParseDuration(timeoutRaw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid --timeout: %v\n", err)
			os.Exit(1)
		}
		cfg.timeout = d
	}

	if setupWizard {
		next, err := runSetupTUI(cfg)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		cfg = next
		if len(links) > 0 {
			cfg.links = append(cfg.links, links...)
		}
	}

	app, err := setupClient(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer app.client.Close()

	message, err := runManagerTUI(app, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if strings.TrimSpace(message) != "" {
		fmt.Println(message)
	}
}

func defaultRunConfig() runConfig {
	base := bootstrap.DefaultConfig()
	return runConfig{
		outDir:        base.OutDir,
		serverAddr:    base.ServerAddr,
		serverMetPath: base.ServerMetPath,
		listenPort:    base.ListenPort,
		udpPort:       base.UDPPort,
		udpPortV6:     base.UDPPortV6,
		enableKAD:     base.EnableKAD,
		enableKADV6:   base.EnableKADV6,
		enableUPnP:    base.EnableUPnP,
		kadNodesDat:   base.KADNodesDat,
		peerTimeout:   base.PeerTimeout,
		statePath:     base.StatePath,
	}
}

func configureFileLogger() (*slog.Logger, error) {
	file, err := os.OpenFile("goed2k.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return slog.New(slog.NewTextHandler(file, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})), nil
}

func setupClient(cfg runConfig) (*appContext, error) {
	bcfg := cfg.bootstrapConfig()
	var logger *slog.Logger
	if l, err := configureFileLogger(); err != nil {
		fmt.Fprintf(os.Stderr, "logger init failed: %v\n", err)
	} else {
		logger = l
	}

	client, err := bootstrap.InitClient(bcfg, logger)
	if err != nil {
		return nil, err
	}

	app := &appContext{
		client:       client,
		targetPaths:  snapshotPathsFromClient(client),
		includeDHT:   cfg.enableKAD,
		includeDHTv6: cfg.enableKADV6,
		noticeCh:     make(chan string, 32),
	}
	if cfg.timeout > 0 {
		app.deadline = time.Now().Add(cfg.timeout)
	}
	bootstrap.RunBackground(client, bcfg, app.notify)
	return app, nil
}

func snapshotPathsFromClient(client *ed2k.Client) []string {
	if client == nil {
		return nil
	}
	paths := make([]string, 0, len(client.Status().Transfers))
	for _, tr := range client.Status().Transfers {
		if tr.FilePath != "" {
			paths = append(paths, tr.FilePath)
		}
	}
	return paths
}

func (a *appContext) notify(format string, args ...any) {
	if a == nil || a.noticeCh == nil {
		return
	}
	message := fmt.Sprintf(format, args...)
	select {
	case a.noticeCh <- message:
	default:
	}
}

func (a *appContext) drainNotices() []string {
	if a == nil || a.noticeCh == nil {
		return nil
	}
	messages := make([]string, 0, 4)
	for {
		select {
		case msg := <-a.noticeCh:
			if strings.TrimSpace(msg) != "" {
				messages = append(messages, msg)
			}
		default:
			return messages
		}
	}
}

func completionMessage(paths []string) string {
	if len(paths) == 1 {
		return fmt.Sprintf("completed: %s", paths[0])
	}
	if len(paths) > 1 {
		return fmt.Sprintf("completed %d transfers", len(paths))
	}
	return "completed"
}

func timeoutMessage(paths []string) string {
	if len(paths) == 1 {
		return fmt.Sprintf("stopped before completion: %s", paths[0])
	}
	if len(paths) > 1 {
		return fmt.Sprintf("stopped before completion: %d transfers", len(paths))
	}
	return "stopped before completion"
}
