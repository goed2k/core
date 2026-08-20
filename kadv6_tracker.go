package goed2k

import (
	"errors"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/goed2k/core/internal/logx"
	"github.com/goed2k/core/protocol"
	kadv6proto "github.com/goed2k/core/protocol/kadv6"
)

type KADV6Tracker struct {
	mu            sync.Mutex
	conn          *net.UDPConn
	listenPort    int
	searchTimeout time.Duration
	combiner      kadv6proto.PacketCombiner
	selfID        kadv6proto.ID
	node          *kadv6NodeImpl
	nodes         map[string]*KADV6RoutingNode
	table         *kadv6RoutingTable
	rpc           *kadRPCManager
	sourceIndex   map[string]map[string]kadv6proto.SearchEntry
	keywordIndex  map[string]map[string]kadv6proto.SearchEntry
	notesIndex    map[string]map[string]kadv6proto.SearchEntry
	stopCh        chan struct{}
	startOnce     sync.Once
	closeOnce     sync.Once
	lastBootstrap time.Time
	lastRefresh   time.Time
	storagePoint  *net.UDPAddr
}

func NewKADV6Tracker(listenPort int, timeout time.Duration) *KADV6Tracker {
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	selfID := kadv6proto.NewID(protocol.Invalid)
	if randomID, err := protocol.RandomHash(false); err == nil {
		selfID = kadv6proto.NewID(randomID)
	}
	tracker := &KADV6Tracker{
		listenPort:    listenPort,
		searchTimeout: timeout,
		selfID:        selfID,
		nodes:         make(map[string]*KADV6RoutingNode),
		table:         newKADV6RoutingTable(selfID, 10),
		rpc:           newKadRPCManager(),
		sourceIndex:   make(map[string]map[string]kadv6proto.SearchEntry),
		keywordIndex:  make(map[string]map[string]kadv6proto.SearchEntry),
		notesIndex:    make(map[string]map[string]kadv6proto.SearchEntry),
		stopCh:        make(chan struct{}),
	}
	tracker.node = newKADV6NodeImpl(tracker)
	return tracker
}

func (t *KADV6Tracker) Start() error {
	var err error
	t.startOnce.Do(func() {
		port := t.ListenPort()
		conn, listenErr := net.ListenUDP("udp6", &net.UDPAddr{IP: net.IPv6unspecified, Port: port})
		if listenErr != nil {
			err = listenErr
			return
		}
		t.conn = conn
		if udpAddr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
			t.setListenPort(udpAddr.Port)
		}
		go t.readLoop()
	})
	return err
}

func (t *KADV6Tracker) Close() {
	t.closeOnce.Do(func() {
		close(t.stopCh)
		if t.conn != nil {
			_ = t.conn.Close()
		}
	})
}

func (t *KADV6Tracker) UDPConn() *net.UDPConn {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.conn
}

func (t *KADV6Tracker) AddNode(addr *net.UDPAddr) {
	addr = normalizeUDPAddrV6(addr)
	if addr == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	key := addr.String()
	node := t.nodes[key]
	if node == nil {
		node = &KADV6RoutingNode{}
		t.nodes[key] = node
	}
	node.Addr = addr
	node.Seed = true
	node.LastSeen = time.Now()
}

func (t *KADV6Tracker) AddNodes(addrs ...*net.UDPAddr) {
	for _, addr := range addrs {
		t.AddNode(addr)
	}
}

func (t *KADV6Tracker) LoadNodesDat(path string) error {
	nodes, err := kadv6proto.LoadNodesDat(path)
	if err != nil {
		return err
	}
	return t.ApplyNodesDat(nodes)
}

func (t *KADV6Tracker) ApplyNodesDat(nodes *kadv6proto.NodesDat) error {
	if nodes == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	haveVerified := false
	loaded := make([]*KADV6RoutingNode, 0, len(nodes.Contacts))
	for _, entry := range nodes.Contacts {
		addr := entry.Endpoint.UDPAddr()
		if normalizeUDPAddrV6(addr) == nil {
			continue
		}
		node := t.addOrUpdateNodeLocked(entry.ID, addr, entry.Endpoint.TCPPort, entry.Version, true)
		if node == nil {
			continue
		}
		loaded = append(loaded, node)
		if nodes.BootstrapEdition == 1 {
			t.table.AddRouterNode(addr)
			continue
		}
		if entry.Verified {
			haveVerified = true
			t.table.NodeSeen(node)
		} else {
			t.table.HeardAbout(node)
		}
	}
	if nodes.BootstrapEdition == 0 && len(loaded) > 0 && !haveVerified {
		for _, node := range loaded {
			t.table.NodeSeen(node)
		}
	}
	return nil
}

func (t *KADV6Tracker) SearchSources(hash protocol.Hash, size int64, cb func([]kadv6proto.SearchEntry)) bool {
	if cb == nil || size <= 0 {
		return false
	}
	if err := t.Start(); err != nil {
		logx.Debug("kadv6 source search start failed", "hash", hash.String(), "err", err)
		return false
	}
	t.mu.Lock()
	if len(t.nodes) == 0 && len(t.table.RouterNodes()) == 0 {
		t.mu.Unlock()
		logx.Debug("kadv6 source search skipped: no bootstrap nodes", "hash", hash.String())
		return false
	}
	t.mu.Unlock()
	logx.Debug("kadv6 source search started", "hash", hash.String(), "size", size)
	return t.node.searchSources(hash, size, cb)
}

func (t *KADV6Tracker) hasSearchContacts() bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.nodes) > 0 || len(t.table.RouterNodes()) > 0
}

func (t *KADV6Tracker) SearchKeywords(hash protocol.Hash, cb func([]kadv6proto.SearchEntry)) bool {
	if cb == nil {
		return false
	}
	if err := t.Start(); err != nil {
		return false
	}
	t.mu.Lock()
	if len(t.nodes) == 0 && len(t.table.RouterNodes()) == 0 {
		t.mu.Unlock()
		return false
	}
	t.mu.Unlock()
	return t.node.searchKeywords(hash, cb)
}

func (t *KADV6Tracker) SearchNotes(hash protocol.Hash, cb func([]kadv6proto.SearchEntry)) bool {
	if cb == nil {
		return false
	}
	if err := t.Start(); err != nil {
		return false
	}
	t.mu.Lock()
	if len(t.nodes) == 0 && len(t.table.RouterNodes()) == 0 {
		t.mu.Unlock()
		return false
	}
	t.mu.Unlock()
	return t.node.searchNotes(hash, cb)
}

func (t *KADV6Tracker) readLoop() {
	buffer := make([]byte, 8192)
	for {
		if t.conn == nil {
			return
		}
		_ = t.conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, addr, err := t.conn.ReadFromUDP(buffer)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				t.node.tick()
				select {
				case <-t.stopCh:
					return
				default:
				}
				continue
			}
			if errorsIsClosed(err) {
				return
			}
			continue
		}
		addr = normalizeUDPAddrV6(addr)
		if addr == nil {
			continue
		}
		opcode, message, err := t.combiner.Unpack(buffer[:n])
		if err != nil {
			continue
		}
		switch opcode {
		case kadv6proto.SearchResOp:
			t.node.processSearchRes(addr, *(message.(*kadv6proto.SearchRes)))
		case kadv6proto.SearchSrcReqOp:
			t.node.processSearchSourcesReq(addr, *(message.(*kadv6proto.SearchSourcesReq)))
		case kadv6proto.SearchKeysReqOp:
			t.node.processSearchKeysReq(addr, *(message.(*kadv6proto.SearchKeysReq)))
		case kadv6proto.SearchNotesReqOp:
			t.node.processSearchNotesReq(addr, *(message.(*kadv6proto.SearchNotesReq)))
		case kadv6proto.BootstrapResOp:
			t.node.processBootstrapRes(addr, *(message.(*kadv6proto.BootstrapRes)))
		case kadv6proto.FindNodeResOp:
			t.node.processFindRes(addr, *(message.(*kadv6proto.FindNodeRes)))
		case kadv6proto.HelloReqOp:
			t.node.processHelloReq(addr, *(message.(*kadv6proto.Hello)))
		case kadv6proto.HelloResOp:
			t.node.processHelloRes(addr, *(message.(*kadv6proto.Hello)))
		case kadv6proto.BootstrapReqOp:
			t.node.processBootstrapReq(addr)
		case kadv6proto.FindNodeReqOp:
			t.node.processFindReq(addr, *(message.(*kadv6proto.FindNodeReq)))
		case kadv6proto.PublishSourceReqOp:
			t.node.processPublishSourcesReq(addr, *(message.(*kadv6proto.PublishSourcesReq)))
		case kadv6proto.PublishKeysReqOp:
			t.node.processPublishKeysReq(addr, *(message.(*kadv6proto.PublishKeysReq)))
		case kadv6proto.PublishNotesReqOp:
			t.node.processPublishNotesReq(addr, *(message.(*kadv6proto.PublishNotesReq)))
		case kadv6proto.PublishResOp:
			t.node.processPublishRes(addr, *(message.(*kadv6proto.PublishRes)))
		case kadv6proto.PingOp:
			t.node.processPing(addr)
		case kadv6proto.PongOp:
			t.node.processPong(addr, *(message.(*kadv6proto.Pong)))
		}
	}
}

func (t *KADV6Tracker) handleBootstrapResponse(addr *net.UDPAddr, res kadv6proto.BootstrapRes) {
	t.mu.Lock()
	node := t.addOrUpdateNodeLocked(res.ID, addr, res.TCPPort, res.Version, false)
	t.confirmNodeLocked(node)
	for _, contact := range res.Contacts {
		contactNode := t.addOrUpdateNodeLocked(contact.ID, contact.Endpoint.UDPAddr(), contact.Endpoint.TCPPort, contact.Version, false)
		t.maybeSendHelloLocked(contactNode)
	}
	t.mu.Unlock()
}

func (t *KADV6Tracker) handleFindResponse(addr *net.UDPAddr, res kadv6proto.FindNodeRes) {
	t.mu.Lock()
	for _, entry := range res.Results {
		entryNode := t.addOrUpdateNodeLocked(entry.ID, entry.Endpoint.UDPAddr(), entry.Endpoint.TCPPort, entry.Version, false)
		t.maybeSendHelloLocked(entryNode)
	}
	if node := t.nodes[addr.String()]; node != nil {
		node.LastSeen = time.Now()
		t.confirmNodeLocked(node)
	}
	t.mu.Unlock()
}

func (t *KADV6Tracker) handleHello(addr *net.UDPAddr, hello kadv6proto.Hello, reply bool) {
	t.mu.Lock()
	node := t.addOrUpdateNodeLocked(hello.ID, addr, hello.TCPPort, hello.Version, false)
	if node != nil {
		node.HelloSent = true
		t.confirmNodeLocked(node)
	}
	t.mu.Unlock()
	if reply {
		t.writePacket(addr, kadv6proto.Hello{
			ID:      t.selfID,
			TCPPort: uint16(t.ListenPort()),
			Version: kadv6proto.KademliaVersion,
		}, kadv6proto.HelloResOp)
	}
}

func (t *KADV6Tracker) handleBootstrapRequest(addr *net.UDPAddr) {
	t.mu.Lock()
	contacts := t.closestEntriesLocked(t.selfID, 20)
	t.mu.Unlock()
	_, _ = t.writePacket(addr, kadv6proto.BootstrapRes{
		ID:       t.selfID,
		TCPPort:  uint16(t.ListenPort()),
		Version:  kadv6proto.KademliaVersion,
		Contacts: contacts,
	})
}

func (t *KADV6Tracker) handleFindRequest(addr *net.UDPAddr, req kadv6proto.FindNodeReq) {
	t.mu.Lock()
	contacts := t.closestEntriesLocked(req.Target, 20)
	t.mu.Unlock()
	_, _ = t.writePacket(addr, kadv6proto.FindNodeRes{
		Target:  req.Target,
		Results: contacts,
	})
}

func (t *KADV6Tracker) handleSearchSourcesRequest(addr *net.UDPAddr, req kadv6proto.SearchSourcesReq) {
	t.mu.Lock()
	results := t.searchEntriesLocked(req.Target.Hash)
	t.mu.Unlock()
	if len(results) == 0 {
		return
	}
	t.sendSearchResults(addr, req.Target, results)
}

func (t *KADV6Tracker) handleSearchKeysRequest(addr *net.UDPAddr, req kadv6proto.SearchKeysReq) {
	t.mu.Lock()
	results := t.keywordEntriesLocked(req.Target.Hash)
	t.mu.Unlock()
	if len(results) == 0 {
		return
	}
	t.sendSearchResults(addr, req.Target, results)
}

func (t *KADV6Tracker) handleSearchNotesRequest(addr *net.UDPAddr, req kadv6proto.SearchNotesReq) {
	t.mu.Lock()
	results := t.notesEntriesLocked(req.Target.Hash)
	t.mu.Unlock()
	if len(results) == 0 {
		return
	}
	t.sendSearchResults(addr, req.Target, results)
}

func (t *KADV6Tracker) handlePublishSourcesRequest(addr *net.UDPAddr, req kadv6proto.PublishSourcesReq) {
	t.mu.Lock()
	if addr != nil {
		t.addSourceIPv6Locked(&req.Source, addr)
	}
	t.storeSourceLocked(req.FileID.Hash, req.Source)
	t.mu.Unlock()
	if addr != nil && t.storagePoint != nil {
		_, _ = t.writePacket(t.storagePoint, req)
	}
	_, _ = t.writePacket(addr, kadv6proto.PublishRes{
		FileID: req.FileID,
		Count:  1,
	})
}

func (t *KADV6Tracker) handlePublishKeysRequest(addr *net.UDPAddr, req kadv6proto.PublishKeysReq) {
	t.mu.Lock()
	for i, source := range req.Sources {
		if addr != nil {
			t.addSourceIPv6Locked(&source, addr)
			req.Sources[i] = source
		}
		t.storeKeywordLocked(req.KeywordID.Hash, source)
	}
	t.mu.Unlock()
	if addr != nil && t.storagePoint != nil {
		_, _ = t.writePacket(t.storagePoint, req)
	}
	_, _ = t.writePacket(addr, kadv6proto.PublishRes{
		FileID: req.KeywordID,
		Count:  1,
	})
}

func (t *KADV6Tracker) handlePublishNotesRequest(addr *net.UDPAddr, req kadv6proto.PublishNotesReq) {
	t.mu.Lock()
	for i, note := range req.Notes {
		if addr != nil {
			t.addSourceIPv6Locked(&note, addr)
			req.Notes[i] = note
		}
		t.storeNotesLocked(req.FileID.Hash, note)
	}
	t.mu.Unlock()
	if addr != nil && t.storagePoint != nil {
		_, _ = t.writePacket(t.storagePoint, req)
	}
	_, _ = t.writePacket(addr, kadv6proto.PublishRes{
		FileID: req.FileID,
		Count:  1,
	})
}

func (t *KADV6Tracker) maybeSendHelloLocked(node *KADV6RoutingNode) {
	if node == nil || node.Addr == nil || node.HelloSent {
		return
	}
	node.HelloSent = true
	t.writePacketLocked(node.Addr, kadv6proto.Hello{
		ID:      t.selfID,
		TCPPort: uint16(t.listenPort),
		Version: kadv6proto.KademliaVersion,
	}, kadv6proto.HelloReqOp)
	t.rpc.Invoke(&kadRPCTransaction{endpointKey: node.Addr.String(), opcode: kadv6proto.HelloResOp})
}

func (t *KADV6Tracker) knownNodesLocked(requireID bool) []*KADV6RoutingNode {
	nodes := make([]*KADV6RoutingNode, 0, len(t.nodes))
	for _, node := range t.nodes {
		if node == nil || node.Addr == nil {
			continue
		}
		if requireID && !node.KnownID() {
			continue
		}
		nodes = append(nodes, node)
	}
	return nodes
}

func (t *KADV6Tracker) PublishSource(hash protocol.Hash, tcpAddr *net.TCPAddr, size int64) bool {
	if tcpAddr == nil || tcpAddr.Port == 0 {
		return false
	}
	ip := tcpAddr.IP.To16()
	if ip == nil || ip.To4() != nil {
		return false
	}
	if err := t.Start(); err != nil {
		return false
	}
	entry := kadv6proto.SearchEntry{
		ID: kadv6proto.NewID(hash),
		Tags: []kadv6proto.Tag{
			{Type: kadv6proto.TagTypeUint8, ID: kadv6proto.TagSourceType, UInt64: 1},
			{Type: kadv6proto.TagTypeUint8, ID: kadv6proto.TagAddrFamily, UInt64: uint64(kadv6proto.AddrFamilyIPv6)},
			{Type: kadv6proto.TagTypeBytes, ID: kadv6proto.TagSourceIP6, Bytes: append([]byte(nil), ip...)},
			{Type: kadv6proto.TagTypeUint16, ID: kadv6proto.TagSourcePort, UInt64: uint64(tcpAddr.Port)},
		},
	}
	if size > 0 {
		entry.Tags = append(entry.Tags, kadv6proto.Tag{Type: kadv6proto.TagTypeUint64, ID: kadv6proto.TagFileSize, UInt64: uint64(size)})
	}
	t.mu.Lock()
	t.storeSourceLocked(hash, entry)
	nodes := t.closestNodesLocked(kadv6proto.NewID(hash), 5, true)
	t.mu.Unlock()
	if len(nodes) == 0 {
		return false
	}
	req := kadv6proto.PublishSourcesReq{FileID: kadv6proto.NewID(hash), Source: entry}
	for _, node := range nodes {
		if node == nil || node.Addr == nil {
			continue
		}
		_, _ = t.writePacket(node.Addr, req)
	}
	return true
}

func (t *KADV6Tracker) PublishKeyword(keywordHash protocol.Hash, entries ...kadv6proto.SearchEntry) bool {
	if len(entries) == 0 {
		return false
	}
	if err := t.Start(); err != nil {
		return false
	}
	t.mu.Lock()
	for _, entry := range entries {
		t.storeKeywordLocked(keywordHash, entry)
	}
	nodes := t.closestNodesLocked(kadv6proto.NewID(keywordHash), 5, true)
	t.mu.Unlock()
	if len(nodes) == 0 {
		return false
	}
	req := kadv6proto.PublishKeysReq{KeywordID: kadv6proto.NewID(keywordHash), Sources: entries}
	for _, node := range nodes {
		if node == nil || node.Addr == nil {
			continue
		}
		_, _ = t.writePacket(node.Addr, req)
	}
	return true
}

func (t *KADV6Tracker) PublishNotes(fileHash protocol.Hash, entries ...kadv6proto.SearchEntry) bool {
	if len(entries) == 0 {
		return false
	}
	if err := t.Start(); err != nil {
		return false
	}
	t.mu.Lock()
	for _, entry := range entries {
		t.storeNotesLocked(fileHash, entry)
	}
	nodes := t.closestNodesLocked(kadv6proto.NewID(fileHash), 5, true)
	t.mu.Unlock()
	if len(nodes) == 0 {
		return false
	}
	req := kadv6proto.PublishNotesReq{FileID: kadv6proto.NewID(fileHash), Notes: entries}
	for _, node := range nodes {
		if node == nil || node.Addr == nil {
			continue
		}
		_, _ = t.writePacket(node.Addr, req)
	}
	return true
}

func (t *KADV6Tracker) SetStoragePoint(addr *net.UDPAddr) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.storagePoint = normalizeUDPAddrV6(addr)
}

func (t *KADV6Tracker) ListenPort() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.listenPort
}

func (t *KADV6Tracker) setListenPort(port int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.listenPort = port
}

func (t *KADV6Tracker) Status() KADV6Status {
	t.mu.Lock()
	defer t.mu.Unlock()
	live, replacements := t.table.Size()
	status := KADV6Status{
		LiveNodes:        live,
		ReplacementNodes: replacements,
		RouterNodes:      len(t.table.RouterNodes()),
		KnownNodes:       len(t.nodes),
		ListenPort:       t.listenPort,
	}
	if t.node != nil {
		status.RunningTraversals = len(t.node.running)
		status.InitialBootstrap = t.node.initialBootstrapRequired
	}
	status.Bootstrapped = live > 0
	if t.storagePoint != nil {
		status.StoragePoint = t.storagePoint.String()
	}
	return status
}

func (t *KADV6Tracker) SnapshotState() *ClientDHTv6State {
	t.mu.Lock()
	defer t.mu.Unlock()
	state := &ClientDHTv6State{
		SelfID:        t.selfID.Hash,
		LastBootstrap: timeToMillis(t.lastBootstrap),
		LastRefresh:   timeToMillis(t.lastRefresh),
		Nodes:         make([]ClientDHTv6NodeState, 0, len(t.nodes)),
		RouterNodes:   make([]string, 0, len(t.table.RouterNodes())),
	}
	if t.storagePoint != nil {
		state.StoragePoint = t.storagePoint.String()
	}
	for _, addr := range t.table.RouterNodes() {
		if addr != nil {
			state.RouterNodes = append(state.RouterNodes, addr.String())
		}
	}
	for _, node := range t.nodes {
		if node == nil || node.Addr == nil {
			continue
		}
		state.Nodes = append(state.Nodes, ClientDHTv6NodeState{
			ID:        node.ID.Hash,
			Addr:      node.Addr.String(),
			TCPPort:   node.TCPPort,
			Version:   node.Version,
			Seed:      node.Seed,
			HelloSent: node.HelloSent,
			Pinged:    node.Pinged,
			FailCount: node.FailCount,
			FirstSeen: timeToMillis(node.FirstSeen),
			LastSeen:  timeToMillis(node.LastSeen),
		})
	}
	return state
}

func (t *KADV6Tracker) ApplyState(state *ClientDHTv6State) error {
	if state == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !state.SelfID.Equal(protocol.Invalid) {
		t.selfID = kadv6proto.NewID(state.SelfID)
	}
	t.table = newKADV6RoutingTable(t.selfID, 10)
	t.nodes = make(map[string]*KADV6RoutingNode, len(state.Nodes))
	t.lastBootstrap = millisToTime(state.LastBootstrap)
	t.lastRefresh = millisToTime(state.LastRefresh)
	t.table.lastBootstrap = t.lastBootstrap
	t.table.lastRefresh = t.lastRefresh
	t.storagePoint = nil
	if state.StoragePoint != "" {
		if addr, err := net.ResolveUDPAddr("udp6", state.StoragePoint); err == nil {
			t.storagePoint = normalizeUDPAddrV6(addr)
		}
	}
	for _, router := range state.RouterNodes {
		addr, err := net.ResolveUDPAddr("udp6", router)
		if err != nil {
			continue
		}
		t.table.AddRouterNode(addr)
	}
	for _, record := range state.Nodes {
		addr, err := net.ResolveUDPAddr("udp6", record.Addr)
		if err != nil {
			continue
		}
		node := &KADV6RoutingNode{
			ID:        kadv6proto.NewID(record.ID),
			Addr:      normalizeUDPAddrV6(addr),
			TCPPort:   record.TCPPort,
			Version:   record.Version,
			Seed:      record.Seed,
			HelloSent: record.HelloSent,
			Pinged:    record.Pinged,
			FailCount: record.FailCount,
			FirstSeen: millisToTime(record.FirstSeen),
			LastSeen:  millisToTime(record.LastSeen),
		}
		if node.Addr == nil {
			continue
		}
		t.nodes[node.Addr.String()] = node
		if node.Pinged {
			t.table.NodeSeen(node)
		} else {
			t.table.HeardAbout(node)
		}
	}
	if t.node != nil {
		live, _ := t.table.Size()
		t.node.initialBootstrapRequired = live == 0
	}
	return nil
}

func (t *KADV6Tracker) closestNodesLocked(target kadv6proto.ID, limit int, requireID bool) []*KADV6RoutingNode {
	var nodes []*KADV6RoutingNode
	if requireID {
		nodes = t.table.FindClosest(target, limit, true)
	} else {
		nodes = t.knownNodesLocked(false)
	}
	if len(nodes) == 0 {
		return nil
	}
	if !requireID {
		sortKADV6NodesByDistance(nodes, target)
	}
	if limit > 0 && len(nodes) > limit {
		nodes = nodes[:limit]
	}
	return nodes
}

func (t *KADV6Tracker) closestEntriesLocked(target kadv6proto.ID, limit int) []kadv6proto.EntryV6 {
	nodes := t.closestNodesLocked(target, limit, true)
	res := make([]kadv6proto.EntryV6, 0, len(nodes))
	for _, node := range nodes {
		if node == nil || node.Addr == nil {
			continue
		}
		endpoint, err := kadv6proto.EndpointFromUDPAddr(node.Addr, node.TCPPort)
		if err != nil {
			continue
		}
		res = append(res, kadv6proto.EntryV6{
			ID:       node.ID,
			Endpoint: endpoint,
			Version:  node.Version,
		})
	}
	return res
}

func (t *KADV6Tracker) addOrUpdateNodeLocked(id kadv6proto.ID, addr *net.UDPAddr, tcpPort uint16, version byte, seed bool) *KADV6RoutingNode {
	addr = normalizeUDPAddrV6(addr)
	if addr == nil {
		return nil
	}
	key := addr.String()
	node := t.nodes[key]
	if node == nil {
		node = &KADV6RoutingNode{Addr: addr}
		t.nodes[key] = node
	}
	node.Addr = addr
	if !id.Hash.Equal(protocol.Invalid) {
		node.ID = id
	}
	if tcpPort != 0 {
		node.TCPPort = tcpPort
	}
	if version != 0 {
		node.Version = version
	}
	node.Seed = node.Seed || seed
	node.LastSeen = time.Now()
	if node.FirstSeen.IsZero() {
		node.FirstSeen = node.LastSeen
	}
	if node.KnownID() {
		t.table.HeardAbout(node)
	}
	return node
}

func (t *KADV6Tracker) storeSourceLocked(hash protocol.Hash, entry kadv6proto.SearchEntry) {
	key := hash.String()
	bucket := t.sourceIndex[key]
	if bucket == nil {
		bucket = make(map[string]kadv6proto.SearchEntry)
		t.sourceIndex[key] = bucket
	}
	entryKey := entry.ID.Hash.String()
	if tcp, ok := entry.SourceAddr(); ok {
		entryKey = tcp.String()
	}
	bucket[entryKey] = entry
}

func (t *KADV6Tracker) storeKeywordLocked(hash protocol.Hash, entry kadv6proto.SearchEntry) {
	key := hash.String()
	bucket := t.keywordIndex[key]
	if bucket == nil {
		bucket = make(map[string]kadv6proto.SearchEntry)
		t.keywordIndex[key] = bucket
	}
	entryKey := entry.ID.Hash.String()
	if name, ok := entry.StringTag(kadv6proto.TagName); ok && name != "" {
		entryKey = entryKey + ":" + name
	}
	bucket[entryKey] = entry
}

func (t *KADV6Tracker) storeNotesLocked(hash protocol.Hash, entry kadv6proto.SearchEntry) {
	key := hash.String()
	bucket := t.notesIndex[key]
	if bucket == nil {
		bucket = make(map[string]kadv6proto.SearchEntry)
		t.notesIndex[key] = bucket
	}
	entryKey := entry.ID.Hash.String()
	if tcp, ok := entry.SourceAddr(); ok {
		entryKey = tcp.String()
	}
	bucket[entryKey] = entry
}

func (t *KADV6Tracker) searchEntriesLocked(hash protocol.Hash) []kadv6proto.SearchEntry {
	bucket := t.sourceIndex[hash.String()]
	if len(bucket) == 0 {
		return nil
	}
	results := make([]kadv6proto.SearchEntry, 0, len(bucket))
	for _, entry := range bucket {
		results = append(results, entry)
	}
	return results
}

func (t *KADV6Tracker) keywordEntriesLocked(hash protocol.Hash) []kadv6proto.SearchEntry {
	bucket := t.keywordIndex[hash.String()]
	if len(bucket) == 0 {
		return nil
	}
	results := make([]kadv6proto.SearchEntry, 0, len(bucket))
	for _, entry := range bucket {
		results = append(results, entry)
	}
	return results
}

func (t *KADV6Tracker) notesEntriesLocked(hash protocol.Hash) []kadv6proto.SearchEntry {
	bucket := t.notesIndex[hash.String()]
	if len(bucket) == 0 {
		return nil
	}
	results := make([]kadv6proto.SearchEntry, 0, len(bucket))
	for _, entry := range bucket {
		results = append(results, entry)
	}
	return results
}

func (t *KADV6Tracker) writePacket(addr *net.UDPAddr, packet any, extra ...byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.writePacketWithOpcodeLocked(addr, packet, extra...)
}

func (t *KADV6Tracker) writePacketLocked(addr *net.UDPAddr, packet any, extra ...byte) {
	_, _ = t.writePacketWithOpcodeLocked(addr, packet, extra...)
}

func (t *KADV6Tracker) writePacketWithOpcodeLocked(addr *net.UDPAddr, packet any, extra ...byte) (int, error) {
	if t.conn == nil || addr == nil {
		return 0, errors.New("kadv6 tracker socket is not ready")
	}
	addr = normalizeUDPAddrV6(addr)
	if addr == nil {
		return 0, errors.New("kadv6 tracker requires native ipv6 peer")
	}
	raw, err := t.combiner.Pack(packet, extra...)
	if err != nil {
		return 0, err
	}
	return t.conn.WriteToUDP(raw, addr)
}

func (t *KADV6Tracker) confirmNodeLocked(node *KADV6RoutingNode) {
	if node == nil || !node.KnownID() {
		return
	}
	node.Pinged = true
	node.FailCount = 0
	node.LastSeen = time.Now()
	t.table.NodeSeen(node)
}

func normalizeUDPAddrV6(addr *net.UDPAddr) *net.UDPAddr {
	if addr == nil {
		return nil
	}
	ip := addr.IP.To16()
	if ip == nil || ip.To4() != nil {
		return nil
	}
	if ip.Equal(net.IPv6zero) {
		return nil
	}
	return &net.UDPAddr{IP: ip, Port: addr.Port, Zone: addr.Zone}
}

func (t *KADV6Tracker) sendSearchResults(addr *net.UDPAddr, target kadv6proto.ID, results []kadv6proto.SearchEntry) {
	if addr == nil || len(results) == 0 {
		return
	}
	const maxEntriesPerPacket = 50
	const outputLimit = 8128
	packet := kadv6proto.SearchRes{Source: t.selfID, Target: target, Results: make([]kadv6proto.SearchEntry, 0, maxEntriesPerPacket)}
	bytesAllocated := 0
	for _, entry := range results {
		entrySize := kadv6SearchEntrySize(entry)
		if len(packet.Results) >= maxEntriesPerPacket || bytesAllocated+entrySize+32 > outputLimit {
			_, _ = t.writePacket(addr, packet)
			packet = kadv6proto.SearchRes{Source: t.selfID, Target: target, Results: make([]kadv6proto.SearchEntry, 0, maxEntriesPerPacket)}
			bytesAllocated = 0
		}
		packet.Results = append(packet.Results, entry)
		bytesAllocated += entrySize
	}
	if len(packet.Results) > 0 {
		_, _ = t.writePacket(addr, packet)
	}
}

func kadv6SearchEntrySize(entry kadv6proto.SearchEntry) int {
	size := 16 + 1
	for _, tag := range entry.Tags {
		size += 2
		switch tag.Type {
		case kadv6proto.TagTypeUint8:
			size++
		case kadv6proto.TagTypeUint16:
			size += 2
		case kadv6proto.TagTypeUint32:
			size += 4
		case kadv6proto.TagTypeUint64:
			size += 8
		case kadv6proto.TagTypeString:
			size += 2 + len(tag.String)
		case kadv6proto.TagTypeBytes:
			size += 2 + len(tag.Bytes)
		}
	}
	return size
}

func (t *KADV6Tracker) addSourceIPv6Locked(entry *kadv6proto.SearchEntry, addr *net.UDPAddr) {
	if entry == nil || addr == nil {
		return
	}
	for _, tag := range entry.Tags {
		if tag.ID == kadv6proto.TagSourceIP6 {
			return
		}
	}
	ip := addr.IP.To16()
	if ip == nil || ip.To4() != nil {
		return
	}
	entry.Tags = append([]kadv6proto.Tag{
		{Type: kadv6proto.TagTypeUint8, ID: kadv6proto.TagAddrFamily, UInt64: uint64(kadv6proto.AddrFamilyIPv6)},
		{Type: kadv6proto.TagTypeBytes, ID: kadv6proto.TagSourceIP6, Bytes: append([]byte(nil), ip...)},
	}, entry.Tags...)
}

func resolveKADV6BootstrapAddr(value string) (*net.UDPAddr, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, errors.New("kadv6 bootstrap address is empty")
	}
	addr, err := net.ResolveUDPAddr("udp6", value)
	if err != nil {
		return nil, err
	}
	if normalized := normalizeUDPAddrV6(addr); normalized != nil {
		return normalized, nil
	}
	return nil, errors.New("kadv6 bootstrap address must be native ipv6")
}
