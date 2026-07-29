package goed2k

// PublicSettings 是对外暴露的客户端设置快照（不含敏感路径内容）。
type PublicSettings struct {
	EnableDHT           bool   `json:"enable_dht"`
	EnableDHTv6         bool   `json:"enable_dhtv6"`
	EnableUPnP          bool   `json:"enable_upnp"`
	EnableCryptLayer    bool   `json:"enable_crypt_layer"`
	CryptLayerRequired  bool   `json:"crypt_layer_required"`
	EnableSecIdent      bool   `json:"enable_sec_ident"`
	CreditsOnlyVerified bool   `json:"credits_only_verified"`
	ListenPort          int    `json:"listen_port"`
	UDPPort             int    `json:"udp_port"`
	UDPPortV6           int    `json:"udp_port_v6"`
	MaxDownloadRateKB   int    `json:"max_download_rate_kb"`
	IdentityKeyPath     string `json:"identity_key_path,omitempty"`
	CategoryCount       int    `json:"category_count"`
}

// SettingsSnapshot 返回当前设置快照。
func (c *Client) SettingsSnapshot() PublicSettings {
	if c == nil || c.session == nil {
		return PublicSettings{}
	}
	st := c.session.settings
	return PublicSettings{
		EnableDHT:           st.EnableDHT,
		EnableDHTv6:         st.EnableDHTv6,
		EnableUPnP:          st.EnableUPnP,
		EnableCryptLayer:    st.EnableCryptLayer,
		CryptLayerRequired:  st.CryptLayerRequired,
		EnableSecIdent:      st.EnableSecIdent,
		CreditsOnlyVerified: st.CreditsOnlyVerified,
		ListenPort:          st.ListenPort,
		UDPPort:             st.UDPPort,
		UDPPortV6:           st.UDPPortV6,
		MaxDownloadRateKB:   st.MaxDownloadRateKB,
		IdentityKeyPath:     st.IdentityKeyPath,
		CategoryCount:       len(st.Categories),
	}
}

// SharedFileSnapshot 是 Web API 用共享文件摘要。
type SharedFileSnapshot struct {
	Hash      string `json:"hash"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	Completed bool   `json:"completed"`
}

// SharedFileSnapshots 返回共享库快照列表。
func (c *Client) SharedFileSnapshots() []SharedFileSnapshot {
	files := c.SharedFiles()
	out := make([]SharedFileSnapshot, 0, len(files))
	for _, sf := range files {
		if sf == nil {
			continue
		}
		out = append(out, SharedFileSnapshot{
			Hash:      sf.Hash.String(),
			Name:      sf.FileLabel(),
			Path:      sf.Path,
			Size:      sf.Size(),
			Completed: sf.Completed,
		})
	}
	return out
}
