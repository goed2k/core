package goed2k

import (
	"fmt"
	"log/slog"

	"github.com/goed2k/core/protocol"
)

type Settings struct {
	Logger                  *slog.Logger
	UserAgent               protocol.Hash
	ModName                 string
	ClientName              string
	ListenPort              int
	UDPPort                 int
	UDPPortV6               int
	EnableDHT               bool
	EnableDHTv6             bool
	EnableUPnP              bool
	Version                 int
	ModMajor                int
	ModMinor                int
	ModBuild                int
	MaxFailCount            int
	MaxPeerListSize         int
	MinPeerReconnectTime    int
	PeerConnectionTimeout   int
	SessionConnectionsLimit int
	UploadSlots             int
	MaxUploadRateKB         int
	MaxDownloadRateKB       int
	SlotAllocationKB        int
	UploadQueueSize         int
	BufferPoolSize          int
	MaxConnectionsPerSecond int
	CompressionVersion      int
	ServerSearchTimeout     int
	DHTSearchTimeout        int
	DHTv6SearchTimeout      int
	ReconnectToServer       bool
	ServerPingTimeout       int64
	EnableCryptLayer        bool
	CryptLayerRequired      bool
	ObfuscationTCPPort      int
	EnableSecIdent          bool
	SecIdentRequired        bool
	CreditsOnlyVerified     bool
	IdentityKeyPath         string
	Categories              []Category
	UseEmuleTempLayout      bool
	PartialKadPublish       bool
	// PreallocateDiskSpace 为 true 时，创建任务会把数据文件扩展到完整 Size。
	// Linux 上再 fallocate 尽量占盘；Windows 上再设置 NTFS FileAllocationInfo。
	// 其他系统仅 Truncate，不保证实际占盘。与 UseSparseFiles 同时打开时以稀疏为准。
	PreallocateDiskSpace bool
	// UseSparseFiles 为 true 时优先稀疏扩展，覆盖 PreallocateDiskSpace 的占盘尝试。
	// Linux 上 Truncate 本身通常生成空洞；Windows 上先 FSCTL_SET_SPARSE 再 Truncate。
	// 其他系统仅 Truncate，不保证文件系统生成稀疏孔。
	UseSparseFiles          bool
	EnableWebDownload       bool
	MaxHttpSources          int
	MaxConcurrentHttpBlocks int
	WebCacheDir             string
	HttpRequestTimeoutSec   int
}

func NewSettings() Settings {
	userAgent := protocol.EMule
	if randomUserAgent, err := protocol.RandomHash(true); err == nil {
		userAgent = randomUserAgent
	}
	return Settings{
		UserAgent:               userAgent,
		ModName:                 "goed2k",
		ClientName:              "goed2k",
		ListenPort:              4661,
		UDPPort:                 4662,
		UDPPortV6:               4672,
		EnableDHT:               false,
		EnableDHTv6:             false,
		EnableUPnP:              false,
		Version:                 0x3c,
		ModMajor:                0,
		ModMinor:                1,
		ModBuild:                0,
		MaxFailCount:            20,
		MaxPeerListSize:         100,
		MinPeerReconnectTime:    10,
		PeerConnectionTimeout:   5,
		SessionConnectionsLimit: 20,
		UploadSlots:             3,
		MaxUploadRateKB:         0,
		MaxDownloadRateKB:       0,
		SlotAllocationKB:        3,
		UploadQueueSize:         500,
		BufferPoolSize:          250,
		MaxConnectionsPerSecond: 10,
		CompressionVersion:      0,
		ServerSearchTimeout:     15,
		DHTSearchTimeout:        8,
		DHTv6SearchTimeout:      8,
		ReconnectToServer:       false,
		ServerPingTimeout:       0,
		EnableCryptLayer:        false,
		CryptLayerRequired:      false,
		ObfuscationTCPPort:      0,
		EnableSecIdent:          false,
		SecIdentRequired:        false,
		CreditsOnlyVerified:     false,
		PartialKadPublish:       true,
		EnableWebDownload:       true,
		MaxHttpSources:          4,
		MaxConcurrentHttpBlocks: 2,
		HttpRequestTimeoutSec:   30,
	}
}

func (s Settings) String() string {
	return fmt.Sprintf("Settings{userAgent=%s, modName='%s', clientName='%s', listenPort=%d, udpPort=%d, enableDHT=%t, enableUPnP=%t, version=%d, modMajor=%d, modMinor=%d, modBuild=%d, maxFailCount=%d, maxPeerListSize=%d, minPeerReconnectTime=%d, peerConnectionTimeout=%d, sessionConnectionsLimit=%d, uploadSlots=%d, maxUploadRateKB=%d, maxDownloadRateKB=%d, slotAllocationKB=%d, uploadQueueSize=%d, bufferPoolSize=%d, maxConnectionsPerSecond=%d, compressionVersion=%d, serverSearchTimeout=%d, dhtSearchTimeout=%d, serverPingTimeout=%d, reconnectToServer=%t}",
		s.UserAgent.String(), s.ModName, s.ClientName, s.ListenPort, s.UDPPort, s.EnableDHT, s.EnableUPnP, s.Version, s.ModMajor, s.ModMinor, s.ModBuild, s.MaxFailCount, s.MaxPeerListSize, s.MinPeerReconnectTime, s.PeerConnectionTimeout, s.SessionConnectionsLimit, s.UploadSlots, s.MaxUploadRateKB, s.MaxDownloadRateKB, s.SlotAllocationKB, s.UploadQueueSize, s.BufferPoolSize, s.MaxConnectionsPerSecond, s.CompressionVersion, s.ServerSearchTimeout, s.DHTSearchTimeout, s.ServerPingTimeout, s.ReconnectToServer)
}
