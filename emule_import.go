package goed2k

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// EmulePreferences 为从 eMule/aMule 文本配置解析出的常用字段。
type EmulePreferences struct {
	NickName          string
	ClientName        string
	ModName           string
	ListenPort        int
	UDPPort           int
	MaxUploadRateKB   int
	MaxDownloadRateKB int
	EnableDHT         bool
	EnableKad         bool
	ServerHost        string
	ServerPort        int
	TempDir           string
	IncomingDir       string
	AllocFull         bool
	SparseFiles       bool
}

// ParseEmulePreferencesINI 解析 eMule preferences.ini / aMule amule.conf 风格键值。
func ParseEmulePreferencesINI(text string) (EmulePreferences, error) {
	var out EmulePreferences
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			key, val, ok = strings.Cut(line, ":")
		}
		if !ok {
			continue
		}
		key = strings.TrimSpace(strings.ToLower(key))
		val = strings.TrimSpace(val)
		val = strings.Trim(val, `"`)
		switch key {
		case "nick", "nickname", "emulepref_nick":
			out.NickName = val
		case "clientname", "emulepref_clientname":
			out.ClientName = val
		case "modname":
			out.ModName = val
		case "port", "emulepref_port", "tcpport":
			out.ListenPort = atoiDefault(val, out.ListenPort)
		case "udpport", "emulepref_udpport":
			out.UDPPort = atoiDefault(val, out.UDPPort)
		case "maxupload", "maxuploadrate", "maxup":
			out.MaxUploadRateKB = atoiDefault(val, out.MaxUploadRateKB)
		case "maxdownload", "maxdownloadrate", "maxdown":
			out.MaxDownloadRateKB = atoiDefault(val, out.MaxDownloadRateKB)
		case "serverip", "serveraddr", "serveraddress":
			out.ServerHost = val
		case "serverport":
			out.ServerPort = atoiDefault(val, out.ServerPort)
		case "enabledht", "enabled_kad", "enabledkad":
			out.EnableDHT = parseBool(val)
			out.EnableKad = out.EnableDHT
		case "tempdir", "tempdirectory":
			out.TempDir = val
		case "incomingdir", "incomingdirectory":
			out.IncomingDir = val
		case "allocfull", "preallocatediskspace":
			out.AllocFull = parseBool(val)
		case "sparsefiles", "usesparsefiles":
			out.SparseFiles = parseBool(val)
		}
	}
	if err := scanner.Err(); err != nil {
		return out, err
	}
	return out, nil
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// LoadEmulePreferences 从文件加载 eMule/aMule 配置。
func LoadEmulePreferences(path string) (EmulePreferences, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return EmulePreferences{}, err
	}
	return ParseEmulePreferencesINI(string(raw))
}

// ApplyEmulePreferences 将解析结果合并到 Settings（仅覆盖非零/非空字段）。
func ApplyEmulePreferences(settings *Settings, prefs EmulePreferences) {
	if settings == nil {
		return
	}
	if prefs.ClientName != "" {
		settings.ClientName = prefs.ClientName
	}
	if prefs.ModName != "" {
		settings.ModName = prefs.ModName
	}
	if prefs.ListenPort > 0 {
		settings.ListenPort = prefs.ListenPort
	}
	if prefs.UDPPort > 0 {
		settings.UDPPort = prefs.UDPPort
	}
	if prefs.MaxUploadRateKB > 0 {
		settings.MaxUploadRateKB = prefs.MaxUploadRateKB
	}
	if prefs.MaxDownloadRateKB > 0 {
		settings.MaxDownloadRateKB = prefs.MaxDownloadRateKB
	}
	if prefs.EnableDHT {
		settings.EnableDHT = true
	}
	if prefs.TempDir != "" {
		settings.UseEmuleTempLayout = true
	}
	if prefs.IncomingDir != "" {
		settings.IncomingDir = prefs.IncomingDir
	}
	if prefs.AllocFull {
		settings.PreallocateDiskSpace = true
	}
	if prefs.SparseFiles {
		settings.UseSparseFiles = true
	}
}

// ImportEmulePreferences 加载配置文件并应用到 Client。
func (c *Client) ImportEmulePreferences(path string) error {
	if c == nil || c.session == nil {
		return fmt.Errorf("nil client")
	}
	prefs, err := LoadEmulePreferences(path)
	if err != nil {
		return err
	}
	c.session.applyEmulePreferences(prefs)
	if prefs.ServerHost != "" && prefs.ServerPort > 0 {
		if err := c.ConnectServers(fmt.Sprintf("%s:%d", prefs.ServerHost, prefs.ServerPort)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Session) applyEmulePreferences(prefs EmulePreferences) {
	if s == nil {
		return
	}
	s.mu.Lock()
	ApplyEmulePreferences(&s.settings, prefs)
	s.mu.Unlock()
}
