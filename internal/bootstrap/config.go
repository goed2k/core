package bootstrap

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	DefaultServerList = "45.82.80.155:5687,176.123.5.89:4725,85.121.5.137:4232,176.123.2.239:4232,145.239.2.134:4661,91.208.162.87:4232,37.15.61.236:4232"
	DefaultServerMet  = "ed2k://|serverlist|http://upd.emule-security.org/server.met|/"
	DefaultNodesDat   = "http://www.alldivx.de/nodes/nodes.dat,https://upd.emule-security.org/nodes.dat"
)

// Config holds runtime options for the goed2k CLI.
type Config struct {
	Links                    []string
	OutDir                   string
	ServerAddr               string
	ServerMetPath            string
	ListenPort               int
	UDPPort                  int
	UDPPortV6                int
	EnableKAD                bool
	EnableKADV6              bool
	EnableUPnP               bool
	EnableCryptLayer         bool
	EnableCryptLayerRequired bool
	EnableSecIdent           bool
	CreditsOnlyVerified      bool
	IdentityKeyPath          string
	CategoriesConfig         string
	KADNodesDat              string
	KADNodes                 string
	KADV6NodesDat            string
	KADV6Nodes               string
	PeerTimeout              int
	MaxDownloadRateKB        int
	Timeout                  time.Duration
	StatePath                string
	DisableState             bool
}

// DefaultConfig returns library defaults aligned with the CLI.
func DefaultConfig() Config {
	return Config{
		OutDir:        ".",
		ServerAddr:    DefaultServerList,
		ServerMetPath: DefaultServerMet,
		ListenPort:    4661,
		UDPPort:       4662,
		UDPPortV6:     4672,
		EnableKAD:     true,
		EnableKADV6:   false,
		EnableUPnP:    true,
		KADNodesDat:   DefaultNodesDat,
		PeerTimeout:   30,
		StatePath:     DefaultStatePath(),
	}
}

// DefaultStatePath returns the per-user state file location.
func DefaultStatePath() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "goed2k", "state.json")
	}
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		return filepath.Join(home, ".config", "goed2k", "state.json")
	}
	return "goed2k-state.json"
}

// ApplyEnv overlays known environment variables onto cfg (later keys win over earlier).
func (cfg *Config) ApplyEnv(prefix string) {
	if prefix == "" {
		prefix = "GOED2K_"
	}
	setString := func(key string, dst *string) {
		if v := strings.TrimSpace(os.Getenv(prefix + key)); v != "" {
			*dst = v
		}
	}
	setInt := func(key string, dst *int) {
		if v := strings.TrimSpace(os.Getenv(prefix + key)); v != "" {
			if n, err := parseInt(v); err == nil {
				*dst = n
			}
		}
	}
	setBool := func(key string, dst *bool) {
		if v := strings.TrimSpace(os.Getenv(prefix + key)); v != "" {
			if b, ok := parseBool(v); ok {
				*dst = b
			}
		}
	}

	setString("OUTDIR", &cfg.OutDir)
	setString("SERVERS", &cfg.ServerAddr)
	setString("SERVER_MET", &cfg.ServerMetPath)
	setString("KAD_NODES_DAT", &cfg.KADNodesDat)
	setString("KAD_BOOTSTRAP", &cfg.KADNodes)
	setString("KADV6_NODES_DAT", &cfg.KADV6NodesDat)
	setString("KADV6_BOOTSTRAP", &cfg.KADV6Nodes)
	setString("IDENTITY_KEY", &cfg.IdentityKeyPath)
	setString("CATEGORIES", &cfg.CategoriesConfig)
	setString("STATE_PATH", &cfg.StatePath)
	setInt("LISTEN_PORT", &cfg.ListenPort)
	setInt("UDP_PORT", &cfg.UDPPort)
	setInt("UDP_PORT_V6", &cfg.UDPPortV6)
	setInt("PEER_TIMEOUT", &cfg.PeerTimeout)
	setInt("MAX_DOWNLOAD_RATE_KB", &cfg.MaxDownloadRateKB)
	setBool("KAD", &cfg.EnableKAD)
	setBool("KADV6", &cfg.EnableKADV6)
	setBool("UPNP", &cfg.EnableUPnP)
	setBool("CRYPT_LAYER", &cfg.EnableCryptLayer)
	setBool("CRYPT_LAYER_REQUIRED", &cfg.EnableCryptLayerRequired)
	setBool("SEC_IDENT", &cfg.EnableSecIdent)
	setBool("CREDITS_ONLY_VERIFIED", &cfg.CreditsOnlyVerified)
	setBool("NO_STATE", &cfg.DisableState)
	if links := strings.TrimSpace(os.Getenv(prefix + "LINKS")); links != "" {
		cfg.Links = SplitCommaList(links)
	}
}

func parseInt(v string) (int, error) {
	var n int
	_, err := fmt.Sscanf(v, "%d", &n)
	return n, err
}

func parseBool(v string) (bool, bool) {
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		return false, false
	}
}
