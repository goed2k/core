package goed2k

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/goed2k/core/disk"
	"github.com/goed2k/core/internal/logx"
	"github.com/goed2k/core/protocol"
	kadproto "github.com/goed2k/core/protocol/kad"
	kadv6proto "github.com/goed2k/core/protocol/kadv6"
	serverproto "github.com/goed2k/core/protocol/server"
)

var ErrClientStopped = errors.New("client stopped")

// defaultPartMetFlushInterval 下载中自动写出 .part.met 的最小间隔。
const defaultPartMetFlushInterval = 15 * time.Second

var remoteResourceHTTPClient = &http.Client{
	Timeout: 30 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("stopped after %d redirects", len(via))
		}
		return nil
	},
}

type Client struct {
	session              *Session
	tickInterval         time.Duration
	statusInterval       time.Duration
	autoSaveTick         time.Duration
	partMetFlushInterval time.Duration
	partMetFlushMu       sync.Mutex
	lastPartMetFlush     map[protocol.Hash]time.Time
	pendingPartMet       map[protocol.Hash]bool
	stopCh               chan struct{}
	doneCh               chan struct{}
	startOnce            sync.Once
	closeOnce            sync.Once
	started              bool
	serverAddr           string
	stateStore           ClientStateStore
	listenersMu          sync.Mutex
	listeners            map[int]chan ClientStatusEvent
	nextListenerID       int
	progressMu           sync.Mutex
	progressListeners    map[int]chan TransferProgressEvent
	nextProgressID       int
	lastProgress         map[protocol.Hash]TransferProgressSnapshot
}

func NewClient(settings Settings) *Client {
	if settings.Logger != nil {
		logx.SetLogger(settings.Logger)
	}
	return &Client{
		session:              NewSession(settings),
		tickInterval:         100 * time.Millisecond,
		statusInterval:       time.Second,
		autoSaveTick:         5 * time.Second,
		partMetFlushInterval: defaultPartMetFlushInterval,
		lastPartMetFlush:     make(map[protocol.Hash]time.Time),
		pendingPartMet:       make(map[protocol.Hash]bool),
		stopCh:               make(chan struct{}),
		doneCh:               make(chan struct{}),
	}
}

func (c *Client) Session() *Session {
	return c.session
}

// LoadIdentity 从 PEM 文件加载或创建 Secure Ident 密钥对。
func (c *Client) LoadIdentity(path string) error {
	if c == nil || c.session == nil {
		return errors.New("client is nil")
	}
	return c.session.LoadIdentity(path)
}

func (c *Client) SetDHTv6Tracker(tracker *KADV6Tracker) {
	c.session.SetDHTv6Tracker(tracker)
}

func (c *Client) GetDHTv6Tracker() *KADV6Tracker {
	return c.session.GetDHTv6Tracker()
}

func (c *Client) EnableDHTv6() *KADV6Tracker {
	if tracker := c.GetDHTv6Tracker(); tracker != nil {
		return tracker
	}
	timeout := time.Duration(c.session.settings.DHTv6SearchTimeout) * time.Second
	tracker := NewKADV6Tracker(c.session.settings.UDPPortV6, timeout)
	c.SetDHTv6Tracker(tracker)
	return tracker
}

func (c *Client) LoadDHTv6NodesDat(path ...string) error {
	if len(path) == 0 {
		return errors.New("nodes6.dat path is empty")
	}
	var errs []error
	loaded := false
	for _, source := range path {
		for _, part := range strings.Split(source, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			nodes, err := c.loadDHTv6NodesDat(part)
			if err != nil {
				logx.Debug("load nodes6.dat failed", "source", part, "err", err)
				errs = append(errs, fmt.Errorf("%s: %w", part, err))
				continue
			}
			if err := c.EnableDHTv6().ApplyNodesDat(nodes); err != nil {
				logx.Debug("apply nodes6.dat failed", "source", part, "err", err)
				errs = append(errs, fmt.Errorf("%s: %w", part, err))
				continue
			}
			logx.Debug("nodes6.dat loaded", "source", part, "entries", len(nodes.Contacts))
			loaded = true
		}
	}
	if loaded {
		return nil
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return errors.New("nodes6.dat path is empty")
}

func (c *Client) AddDHTv6BootstrapNodes(nodes ...string) error {
	tracker := c.EnableDHTv6()
	for _, item := range nodes {
		for _, part := range strings.Split(item, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			addr, err := resolveKADV6BootstrapAddr(part)
			if err != nil {
				return err
			}
			tracker.AddNode(addr)
		}
	}
	return nil
}

func (c *Client) PublishDHTv6Source(hash protocol.Hash, tcpAddr *net.TCPAddr, size int64) bool {
	return c.EnableDHTv6().PublishSource(hash, tcpAddr, size)
}

func (c *Client) PublishDHTv6Keyword(keywordHash protocol.Hash, entries ...kadv6proto.SearchEntry) bool {
	return c.EnableDHTv6().PublishKeyword(keywordHash, entries...)
}

func (c *Client) PublishDHTv6Notes(fileHash protocol.Hash, entries ...kadv6proto.SearchEntry) bool {
	return c.EnableDHTv6().PublishNotes(fileHash, entries...)
}

func (c *Client) SearchDHTv6Sources(hash protocol.Hash, size int64, cb func([]kadv6proto.SearchEntry)) bool {
	return c.EnableDHTv6().SearchSources(hash, size, cb)
}

func (c *Client) SearchDHTv6Keywords(keywordHash protocol.Hash, cb func([]kadv6proto.SearchEntry)) bool {
	return c.EnableDHTv6().SearchKeywords(keywordHash, cb)
}

func (c *Client) SearchDHTv6Notes(fileHash protocol.Hash, cb func([]kadv6proto.SearchEntry)) bool {
	return c.EnableDHTv6().SearchNotes(fileHash, cb)
}

func (c *Client) SetDHTv6StoragePoint(address string) error {
	if strings.TrimSpace(address) == "" {
		c.EnableDHTv6().SetStoragePoint(nil)
		return nil
	}
	addr, err := net.ResolveUDPAddr("udp6", address)
	if err != nil {
		return err
	}
	c.EnableDHTv6().SetStoragePoint(addr)
	return nil
}

func (c *Client) DHTv6Status() KADV6Status {
	if tracker := c.GetDHTv6Tracker(); tracker != nil {
		return tracker.Status()
	}
	return KADV6Status{}
}

func (c *Client) SetDHTTracker(tracker *DHTTracker) {
	c.session.SetDHTTracker(tracker)
}

func (c *Client) GetDHTTracker() *DHTTracker {
	return c.session.GetDHTTracker()
}

func (c *Client) EnableDHT() *DHTTracker {
	if tracker := c.GetDHTTracker(); tracker != nil {
		return tracker
	}
	timeout := time.Duration(c.session.settings.DHTSearchTimeout) * time.Second
	tracker := NewDHTTracker(c.session.settings.UDPPort, timeout)
	c.SetDHTTracker(tracker)
	return tracker
}

func (c *Client) LoadDHTNodesDat(path ...string) error {
	if len(path) == 0 {
		return errors.New("nodes.dat path is empty")
	}
	var errs []error
	loaded := false
	for _, source := range path {
		for _, part := range strings.Split(source, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			nodes, err := c.loadDHTNodesDat(part)
			if err != nil {
				logx.Debug("load nodes.dat failed", "source", part, "err", err)
				errs = append(errs, fmt.Errorf("%s: %w", part, err))
				continue
			}
			if err := c.EnableDHT().ApplyNodesDat(nodes); err != nil {
				logx.Debug("apply nodes.dat failed", "source", part, "err", err)
				errs = append(errs, fmt.Errorf("%s: %w", part, err))
				continue
			}
			logx.Debug("nodes.dat loaded", "source", part, "entries", len(nodes.Contacts))
			loaded = true
		}
	}
	if loaded {
		return nil
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return errors.New("nodes.dat path is empty")
}

func (c *Client) AddDHTBootstrapNodes(nodes ...string) error {
	tracker := c.EnableDHT()
	for _, item := range nodes {
		for _, part := range strings.Split(item, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			addr, err := net.ResolveUDPAddr("udp", part)
			if err != nil {
				return err
			}
			tracker.AddNode(addr)
		}
	}
	return nil
}

func (c *Client) PublishDHTSource(hash protocol.Hash, endpoint protocol.Endpoint, size int64) bool {
	return c.EnableDHT().PublishSource(hash, endpoint, size)
}

func (c *Client) PublishDHTKeyword(keywordHash protocol.Hash, entries ...kadproto.SearchEntry) bool {
	return c.EnableDHT().PublishKeyword(keywordHash, entries...)
}

func (c *Client) PublishDHTNotes(fileHash protocol.Hash, entries ...kadproto.SearchEntry) bool {
	return c.EnableDHT().PublishNotes(fileHash, entries...)
}

func (c *Client) SearchDHTKeywords(keywordHash protocol.Hash, cb func([]kadproto.SearchEntry)) bool {
	return c.EnableDHT().SearchKeywords(keywordHash, cb)
}

func (c *Client) SearchDHTNotes(fileHash protocol.Hash, cb func([]kadproto.SearchEntry)) bool {
	return c.EnableDHT().SearchNotes(fileHash, cb)
}

func (c *Client) StartSearch(params SearchParams) (SearchHandle, error) {
	return c.session.StartSearch(params)
}

func (c *Client) StopSearch() error {
	return c.session.StopSearch(0)
}

func (c *Client) SearchSnapshot() SearchSnapshot {
	return c.session.SearchSnapshot()
}

func (c *Client) SetDHTStoragePoint(address string) error {
	if strings.TrimSpace(address) == "" {
		c.EnableDHT().SetStoragePoint(nil)
		return nil
	}
	addr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return err
	}
	c.EnableDHT().SetStoragePoint(addr)
	return nil
}

func (c *Client) DHTStatus() DHTStatus {
	if tracker := c.GetDHTTracker(); tracker != nil {
		return tracker.Status()
	}
	return DHTStatus{}
}

// DHTEnabled 返回 settings 中是否启用 DHT（与 UDP 是否已成功监听无关）。
func (c *Client) DHTEnabled() bool {
	return c != nil && c.session != nil && c.session.settings.EnableDHT
}

func (c *Client) Start() error {
	var err error
	c.startOnce.Do(func() {
		if c.session.settings.EnableDHT && c.GetDHTTracker() == nil {
			c.EnableDHT()
		}
		if c.session.settings.EnableDHTv6 && c.GetDHTv6Tracker() == nil {
			c.EnableDHTv6()
		}
		err = c.session.Listen()
		if err != nil {
			return
		}
		if err = c.session.EnsureServerStatUDPListener(); err != nil {
			c.session.CloseListener()
			return
		}
		if tracker := c.GetDHTTracker(); tracker != nil {
			if startErr := tracker.Start(); startErr != nil {
				err = startErr
				c.session.CloseListener()
				return
			}
			c.session.SyncDHTListenPort()
		}
		if tracker := c.GetDHTv6Tracker(); tracker != nil {
			if startErr := tracker.Start(); startErr != nil {
				err = startErr
				if c.GetDHTTracker() != nil {
					c.GetDHTTracker().Close()
				}
				c.session.CloseListener()
				return
			}
			c.session.SyncDHTv6ListenPort()
		}
		if c.GetDHTTracker() != nil || c.GetDHTv6Tracker() != nil {
			if c.session.settings.EnableUPnP {
				c.session.RefreshUPnPMapping()
			}
		}
		c.started = true
		go c.loop()
		go c.statusLoop()
		c.emitStatusUpdate()
		c.emitTransferProgressUpdate(true)
	})
	return err
}

func (c *Client) Wait() error {
	if !c.started {
		return nil
	}
	ticker := time.NewTicker(c.tickInterval)
	defer ticker.Stop()
	for {
		handles := c.session.GetTransfers()
		if len(handles) == 0 {
			return nil
		}
		allFinished := true
		for _, handle := range handles {
			if !handle.IsFinished() {
				allFinished = false
				break
			}
		}
		if allFinished {
			return nil
		}
		select {
		case <-c.doneCh:
			return ErrClientStopped
		case <-ticker.C:
		}
	}
}

func (c *Client) Stop() error {
	var err error
	c.closeOnce.Do(func() {
		if c.started {
			close(c.stopCh)
			c.session.closeServerStatUDPListener()
			if tracker := c.GetDHTTracker(); tracker != nil {
				tracker.Close()
			}
			if tracker := c.GetDHTv6Tracker(); tracker != nil {
				tracker.Close()
			}
			c.session.DisconnectFrom()
			c.session.CloseListener()
			select {
			case <-c.doneCh:
			case <-time.After(2 * time.Second):
			}
		} else {
			c.session.closeServerStatUDPListener()
			if tracker := c.GetDHTTracker(); tracker != nil {
				tracker.Close()
			}
			if tracker := c.GetDHTv6Tracker(); tracker != nil {
				tracker.Close()
			}
			c.session.DisconnectFrom()
			c.session.CloseListener()
		}
		flushErr := c.flushPartMet(time.Now(), true)
		if c.stateStore != nil {
			err = c.SaveState("")
		}
		if err == nil {
			err = flushErr
		}
	})
	return err
}

func (c *Client) Close() {
	_ = c.Stop()
}

func (c *Client) Connect(serverAddr string) error {
	logx.Debug("connect server", "server", serverAddr)
	c.serverAddr = serverAddr
	addr, err := net.ResolveTCPAddr("tcp", serverAddr)
	if err != nil {
		return err
	}
	return c.session.ConnectTo(serverAddr, addr)
}

func (c *Client) ConnectServers(serverAddrs ...string) error {
	normalized := make([]string, 0, len(serverAddrs))
	seen := make(map[string]struct{}, len(serverAddrs))
	for _, item := range serverAddrs {
		for _, part := range strings.Split(item, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if _, ok := seen[part]; ok {
				continue
			}
			seen[part] = struct{}{}
			normalized = append(normalized, part)
		}
	}
	if len(normalized) == 0 {
		return errors.New("no server address provided")
	}
	c.serverAddr = strings.Join(normalized, ",")
	for _, serverAddr := range normalized {
		addr, err := net.ResolveTCPAddr("tcp", serverAddr)
		if err != nil {
			return err
		}
		if err := c.session.ConnectTo(serverAddr, addr); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) LoadServerMet(path string) ([]serverproto.ServerMetEntry, error) {
	met, err := c.loadServerMet(path)
	if err != nil {
		return nil, err
	}
	logx.Debug("server.met loaded", "source", path, "servers", len(met.Servers))
	entries := make([]serverproto.ServerMetEntry, len(met.Servers))
	copy(entries, met.Servers)
	return entries, nil
}

func (c *Client) ConnectServerMet(path string) error {
	met, err := c.loadServerMet(path)
	if err != nil {
		return err
	}
	for _, e := range met.Servers {
		addr := e.Address()
		if addr == "" {
			continue
		}
		c.session.SetServerMetadata(addr, e.Name(), e.Description())
		tcpAddr, err := net.ResolveTCPAddr("tcp", addr)
		if err != nil {
			return err
		}
		if err := c.session.ConnectTo(addr, tcpAddr); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) ConnectServerLink(linkValue string) error {
	link, err := ParseEMuleLink(linkValue)
	if err != nil {
		return err
	}
	switch link.Type {
	case LinkServer:
		return c.ConnectServers(net.JoinHostPort(link.StringValue, fmt.Sprintf("%d", link.NumberValue)))
	case LinkServers:
		return c.ConnectServerMet(link.StringValue)
	default:
		return errors.New("unsupported server link type")
	}
}

func (c *Client) SetAutoSaveInterval(interval time.Duration) {
	c.autoSaveTick = interval
}

// SetPartMetFlushInterval 设置自动写出 .part.met 的最小间隔；0 表示每次脏进度都写。
func (c *Client) SetPartMetFlushInterval(interval time.Duration) {
	c.partMetFlushMu.Lock()
	c.partMetFlushInterval = interval
	c.partMetFlushMu.Unlock()
}

func (c *Client) ServerAddress() string {
	return c.serverAddr
}

func (c *Client) ConnectSavedServer() error {
	if c.serverAddr == "" {
		return errors.New("saved server address is empty")
	}
	return c.ConnectServers(c.serverAddr)
}

func (c *Client) AddLink(linkValue, outputDir string) (TransferHandle, string, error) {
	link, err := ParseEMuleLink(linkValue)
	if err != nil {
		return TransferHandle{}, "", err
	}
	if link.Type != LinkFile {
		return TransferHandle{}, "", errors.New("unsupported link type")
	}
	outputDir = ResolveCategoryOutputDir(c.session.settings.Categories, link.StringValue, outputDir)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return TransferHandle{}, "", err
	}
	targetPath, cleanup, err := ResolveEmuleDownloadPath(c.session.settings, outputDir, link.StringValue)
	if err != nil {
		return TransferHandle{}, "", err
	}
	handler := disk.NewDesktopFileHandler(targetPath)
	atp := NewAddTransferParamsFromHandler(link.Hash, CurrentTimeMillis(), link.NumberValue, handler, false)
	atp.AICHRootHash = link.AICHRootHash
	if len(link.PartHashes) > 0 {
		atp.PieceHashes = append(atp.PieceHashes, link.PartHashes...)
	}
	handle, err := c.session.AddTransferParams(atp)
	if err != nil {
		cleanup()
		return handle, targetPath, err
	}
	logx.Debug("transfer added", "file", link.StringValue, "hash", link.Hash.String(), "size", link.NumberValue, "path", targetPath)
	if handle.transfer != nil {
		requested := c.session.RequestSourcesNow(handle.transfer)
		logx.Debug("initial source discovery requested", "hash", link.Hash.String(), "requested", requested)
	}
	_ = c.saveStateIfConfigured()
	_ = c.flushOnePartMet(handle, time.Now(), true)
	c.emitStatusUpdate()
	c.emitTransferProgressUpdate(true)
	return handle, targetPath, err
}

type CollectionAddResult struct {
	Handles    []TransferHandle
	TargetPath []string
}

func (c *Client) AddCollectionLink(linkValue, outputDir string) (CollectionAddResult, error) {
	linkValue = strings.TrimSpace(linkValue)
	if linkValue == "" {
		return CollectionAddResult{}, NewError(LinkMailformed)
	}

	var fileLinks []EMuleLink
	if strings.HasPrefix(strings.ToLower(linkValue), "ed2k://") {
		link, err := ParseEMuleLink(linkValue)
		if err != nil {
			return CollectionAddResult{}, err
		}
		switch link.Type {
		case LinkCollection:
			fileLinks = link.FileLinks
		case LinkFile:
			fileLinks = []EMuleLink{link}
		default:
			return CollectionAddResult{}, errors.New("unsupported collection link type")
		}
	} else {
		links, err := ParseEMuleCollectionFile(linkValue)
		if err != nil {
			return CollectionAddResult{}, err
		}
		fileLinks = links
	}
	if len(fileLinks) == 0 {
		return CollectionAddResult{}, NewError(LinkMailformed)
	}

	result := CollectionAddResult{
		Handles:    make([]TransferHandle, 0, len(fileLinks)),
		TargetPath: make([]string, 0, len(fileLinks)),
	}
	for _, fileLink := range fileLinks {
		handle, targetPath, err := c.AddLink(FormatLink(fileLink.StringValue, fileLink.NumberValue, fileLink.Hash), outputDir)
		if err != nil {
			return result, err
		}
		result.Handles = append(result.Handles, handle)
		result.TargetPath = append(result.TargetPath, targetPath)
	}
	return result, nil
}

func (c *Client) loadServerMet(source string) (*serverproto.ServerMet, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil, errors.New("server.met source is empty")
	}
	if strings.HasPrefix(strings.ToLower(source), "ed2k://") {
		link, err := ParseEMuleLink(source)
		if err != nil {
			return nil, err
		}
		if link.Type != LinkServers {
			return nil, errors.New("ed2k link is not a serverlist link")
		}
		source = link.StringValue
	}

	parsedURL, err := url.Parse(source)
	if err == nil && parsedURL.Scheme != "" {
		switch parsedURL.Scheme {
		case "file":
			return serverproto.LoadServerMet(localPathFromFileURL(parsedURL))
		case "http", "https":
			data, err := fetchRemoteResource(source)
			if err != nil {
				return nil, err
			}
			return serverproto.ParseServerMet(data)
		}
	}

	return serverproto.LoadServerMet(source)
}

func (c *Client) loadDHTv6NodesDat(source string) (*kadv6proto.NodesDat, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil, errors.New("nodes6.dat source is empty")
	}
	parsedURL, err := url.Parse(source)
	if err == nil && parsedURL.Scheme != "" {
		switch parsedURL.Scheme {
		case "file":
			return kadv6proto.LoadNodesDat(localPathFromFileURL(parsedURL))
		case "http", "https":
			data, err := fetchRemoteResource(source)
			if err != nil {
				return nil, err
			}
			return kadv6proto.ParseNodesDat(data)
		}
	}
	return kadv6proto.LoadNodesDat(source)
}

// localPathFromFileURL converts a file:// URL to a local filesystem path (Windows-safe).
func localPathFromFileURL(u *url.URL) string {
	path := u.Path
	if path == "" || path == "/" {
		if u.Host != "" {
			path = u.Host
		} else if u.Opaque != "" {
			path = u.Opaque
		}
	}
	// file:///C:/foo on Windows yields Path=/C:/foo
	if len(path) >= 3 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	return filepath.FromSlash(path)
}

func (c *Client) loadDHTNodesDat(source string) (*kadproto.NodesDat, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return nil, errors.New("nodes.dat source is empty")
	}
	parsedURL, err := url.Parse(source)
	if err == nil && parsedURL.Scheme != "" {
		switch parsedURL.Scheme {
		case "file":
			return kadproto.LoadNodesDat(localPathFromFileURL(parsedURL))
		case "http", "https":
			data, err := fetchRemoteResource(source)
			if err != nil {
				return nil, err
			}
			return kadproto.ParseNodesDat(data)
		}
	}
	return kadproto.LoadNodesDat(source)
}

func fetchRemoteResource(source string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, source, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "goed2k/1.0")
	resp, err := remoteResourceHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	logx.Debug("fetched remote resource", "source", source, "status", resp.StatusCode, "final_url", resp.Request.URL.String())
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("download failed: %s", resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (c *Client) AddTransfer(atp AddTransferParams) (TransferHandle, error) {
	handle, err := c.session.AddTransferParams(atp)
	if err == nil {
		_ = c.saveStateIfConfigured()
		_ = c.flushOnePartMet(handle, time.Now(), true)
		c.emitStatusUpdate()
		c.emitTransferProgressUpdate(true)
	}
	return handle, err
}

// AddHttpSource 为指定任务添加 HTTP 下载源（支持 Range 请求）。
func (c *Client) AddHttpSource(hash protocol.Hash, sourceURL string) error {
	if c == nil || c.session == nil {
		return errors.New("client is not started")
	}
	handle := c.FindTransfer(hash)
	if !handle.IsValid() {
		return errors.New("transfer not found")
	}
	if err := handle.transfer.AddHttpSource(sourceURL); err != nil {
		return err
	}
	if err := c.saveStateIfConfigured(); err != nil {
		return err
	}
	c.emitStatusUpdate()
	return nil
}

func (c *Client) FindTransfer(hash protocol.Hash) TransferHandle {
	return c.session.FindTransfer(hash)
}

func (c *Client) Transfers() []TransferHandle {
	return c.session.GetTransfers()
}

func (c *Client) PauseTransfer(hash protocol.Hash) error {
	handle := c.FindTransfer(hash)
	if !handle.IsValid() {
		return errors.New("transfer not found")
	}
	handle.Pause()
	if err := c.saveStateIfConfigured(); err != nil {
		return err
	}
	c.emitStatusUpdate()
	c.emitTransferProgressUpdate(true)
	return nil
}

func (c *Client) ResumeTransfer(hash protocol.Hash) error {
	handle := c.FindTransfer(hash)
	if !handle.IsValid() {
		return errors.New("transfer not found")
	}
	handle.Resume()
	if err := c.saveStateIfConfigured(); err != nil {
		return err
	}
	c.emitStatusUpdate()
	c.emitTransferProgressUpdate(true)
	return nil
}

func (c *Client) RemoveTransfer(hash protocol.Hash, deleteFile bool) error {
	if err := c.session.RemoveTransfer(hash, deleteFile); err != nil {
		return err
	}
	c.partMetFlushMu.Lock()
	delete(c.lastPartMetFlush, hash)
	delete(c.pendingPartMet, hash)
	c.partMetFlushMu.Unlock()
	if err := c.saveStateIfConfigured(); err != nil {
		return err
	}
	c.emitStatusUpdate()
	c.emitTransferProgressUpdate(true)
	return nil
}

func (c *Client) SetTransferUploadPriority(hash protocol.Hash, priority UploadPriority) error {
	handle := c.FindTransfer(hash)
	if !handle.IsValid() {
		return errors.New("transfer not found")
	}
	handle.transfer.SetUploadPriority(priority)
	return c.saveStateIfConfigured()
}

func (c *Client) SetTransferPriority(hash protocol.Hash, priority TransferPriority) error {
	handle := c.FindTransfer(hash)
	if !handle.IsValid() {
		return errors.New("transfer not found")
	}
	handle.transfer.SetDownloadPriority(priority)
	return c.saveStateIfConfigured()
}

func (c *Client) LoadIPFilter(path string) error {
	filter, err := LoadIPFilter(path)
	if err != nil {
		return err
	}
	c.session.SetIPFilter(filter)
	return nil
}

func (c *Client) SetIPFilter(filter *IPFilter) {
	c.session.SetIPFilter(filter)
}

func (c *Client) BanPeer(endpoint protocol.Endpoint) error {
	if !endpoint.Defined() {
		return errors.New("invalid endpoint")
	}
	c.session.BanPeer(endpoint)
	c.session.removeBannedPeerFromTransfers(endpoint)
	return c.saveStateIfConfigured()
}

// FlushPartMet 按节流写出各任务旁注 .part.met。force 时忽略间隔并写出所有有路径的任务。
func (c *Client) FlushPartMet(force bool) error {
	return c.flushPartMet(time.Now(), force)
}

func (c *Client) flushPartMet(now time.Time, force bool) error {
	if c == nil || c.session == nil {
		return nil
	}
	var firstErr error
	for _, handle := range c.Transfers() {
		if err := c.flushOnePartMet(handle, now, force); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (c *Client) flushOnePartMet(handle TransferHandle, now time.Time, force bool) error {
	if !handle.IsValid() {
		return nil
	}
	if handle.GetFilePath() == "" {
		return nil
	}
	hash := handle.GetHash()
	c.partMetFlushMu.Lock()
	defer c.partMetFlushMu.Unlock()
	if c.pendingPartMet == nil {
		c.pendingPartMet = make(map[protocol.Hash]bool)
	}
	if c.lastPartMetFlush == nil {
		c.lastPartMetFlush = make(map[protocol.Hash]time.Time)
	}
	if handle.NeedResumeDataSave() {
		c.pendingPartMet[hash] = true
	}
	if !force && !c.pendingPartMet[hash] {
		return nil
	}
	interval := c.partMetFlushInterval
	last := c.lastPartMetFlush[hash]
	if !force && interval > 0 && !last.IsZero() && now.Sub(last) < interval {
		return nil
	}
	if err := c.exportPartMetForTransferLocked(handle); err != nil {
		return err
	}
	handle.MarkResumeDataSaved()
	c.pendingPartMet[hash] = false
	c.lastPartMetFlush[hash] = now
	return nil
}

func (c *Client) ExportPartMetForTransfer(hash protocol.Hash) error {
	handle := c.FindTransfer(hash)
	if !handle.IsValid() {
		return errors.New("transfer not found")
	}
	return c.exportPartMetForTransferLocked(handle)
}

func (c *Client) exportPartMetForTransferLocked(handle TransferHandle) error {
	path := handle.GetFilePath()
	if path == "" {
		return errors.New("transfer has no file path")
	}
	resume := handle.SnapshotResumeData()
	if resume == nil {
		return errors.New("resume data is nil")
	}
	return ExportPartMet(path, PartMetInfo{
		Hash:        handle.GetHash(),
		FileSize:    handle.GetSize(),
		Filename:    filepath.Base(path),
		Resume:      resume,
		HttpSources: handle.HttpSources(),
	})
}

// ImportPartMet 从 <path>.part.met 导入续传数据（自动识别 eMule 二进制或 goed2k JSON）。
func (c *Client) ImportPartMet(path string) (PartMetInfo, error) {
	return ImportPartMet(path)
}

func (c *Client) SuspendUpload(hash protocol.Hash, terminate bool) uint16 {
	removed := c.session.UploadQueue().SuspendUpload(hash, terminate)
	_ = c.saveStateIfConfigured()
	return removed
}

func (c *Client) ResumeUpload(hash protocol.Hash) {
	c.session.UploadQueue().ResumeUpload(hash)
	_ = c.saveStateIfConfigured()
}

func (c *Client) SetFriendSlot(hash protocol.Hash, enabled bool) {
	c.session.SetFriendSlot(hash, enabled)
	_ = c.saveStateIfConfigured()
}

func (c *Client) loop() {
	defer close(c.doneCh)
	ticker := time.NewTicker(c.tickInterval)
	defer ticker.Stop()
	lastTick := time.Now()
	lastSave := lastTick
	for {
		select {
		case now := <-ticker.C:
			elapsed := now.Sub(lastTick)
			lastTick = now
			UpdateCachedTime()
			c.session.SecondTick(CurrentTime(), elapsed.Milliseconds())
			_ = c.flushPartMet(now, false)
			if c.stateStore != nil && c.autoSaveTick > 0 && now.Sub(lastSave) >= c.autoSaveTick {
				_ = c.SaveState("")
				lastSave = now
			}
		case <-c.stopCh:
			return
		}
	}
}

func (c *Client) statusLoop() {
	ticker := time.NewTicker(c.statusInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.emitStatusUpdate()
			c.emitTransferProgressUpdate(false)
		case <-c.stopCh:
			return
		}
	}
}

func (c *Client) saveStateIfConfigured() error {
	if c.stateStore == nil {
		return nil
	}
	return c.SaveState("")
}
