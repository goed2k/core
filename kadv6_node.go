package goed2k

import (
	"net"
	"time"

	"github.com/goed2k/core/protocol"
	kadv6proto "github.com/goed2k/core/protocol/kadv6"
)

type kadv6NodeImpl struct {
	tracker                  *KADV6Tracker
	running                  map[string]*kadv6Traversal
	initialBootstrapRequired bool
}

func newKADV6NodeImpl(tracker *KADV6Tracker) *kadv6NodeImpl {
	return &kadv6NodeImpl{
		tracker:                  tracker,
		running:                  make(map[string]*kadv6Traversal),
		initialBootstrapRequired: true,
	}
}

func (n *kadv6NodeImpl) searchBranching() int {
	return 5
}

func (n *kadv6NodeImpl) listenPort() int {
	if n == nil || n.tracker == nil {
		return 0
	}
	return n.tracker.ListenPort()
}

func (n *kadv6NodeImpl) storagePoint() *net.UDPAddr {
	if n == nil || n.tracker == nil {
		return nil
	}
	return n.tracker.storagePoint
}

func (n *kadv6NodeImpl) addTraversal(t *kadv6Traversal) bool {
	if n == nil || t == nil {
		return false
	}
	key := t.key()
	if _, exists := n.running[key]; exists {
		return false
	}
	n.running[key] = t
	return true
}

func (n *kadv6NodeImpl) removeTraversal(t *kadv6Traversal) {
	if n == nil || t == nil {
		return
	}
	delete(n.running, t.key())
}

func (n *kadv6NodeImpl) addRouterNode(addr *net.UDPAddr) {
	if n == nil || n.tracker == nil || n.tracker.table == nil {
		return
	}
	n.tracker.table.AddRouterNode(addr)
}

func (n *kadv6NodeImpl) addNode(addr *net.UDPAddr, id kadv6proto.ID, portTCP uint16, version byte) {
	if n == nil || n.tracker == nil || addr == nil {
		return
	}
	node := n.tracker.addOrUpdateNodeLocked(id, addr, portTCP, version, false)
	if node == nil {
		return
	}
	n.tracker.maybeSendHelloLocked(node)
}

func (n *kadv6NodeImpl) bootstrap(addrs []*net.UDPAddr) bool {
	if n == nil {
		return false
	}
	t := newKADV6Traversal(n, n.tracker.selfID, kadv6TraversalBootstrap, 0, nil)
	for _, addr := range addrs {
		t.addEntry(kadv6proto.ID{}, addr, kadv6ObserverFlagInitial, 0, 0)
	}
	for _, addr := range n.tracker.table.RouterNodes() {
		t.addEntry(kadv6proto.ID{}, addr, kadv6ObserverFlagInitial, 0, 0)
	}
	return t.start()
}

func (n *kadv6NodeImpl) searchSources(hash protocol.Hash, size int64, cb func([]kadv6proto.SearchEntry)) bool {
	if n == nil || n.tracker == nil || size <= 0 || cb == nil {
		return false
	}
	t := newKADV6Traversal(n, kadv6proto.NewID(hash), kadv6TraversalFindSources, size, cb)
	for _, node := range n.tracker.closestNodesLocked(kadv6proto.NewID(hash), 50, true) {
		t.addNode(node.Addr, node.ID, node.TCPPort, node.Version)
	}
	return t.start()
}

func (n *kadv6NodeImpl) searchKeywords(hash protocol.Hash, cb func([]kadv6proto.SearchEntry)) bool {
	if n == nil || n.tracker == nil || cb == nil {
		return false
	}
	t := newKADV6Traversal(n, kadv6proto.NewID(hash), kadv6TraversalFindKeywords, 0, cb)
	for _, node := range n.tracker.closestNodesLocked(kadv6proto.NewID(hash), 50, true) {
		t.addNode(node.Addr, node.ID, node.TCPPort, node.Version)
	}
	return t.start()
}

func (n *kadv6NodeImpl) searchNotes(hash protocol.Hash, cb func([]kadv6proto.SearchEntry)) bool {
	if n == nil || n.tracker == nil || cb == nil {
		return false
	}
	t := newKADV6Traversal(n, kadv6proto.NewID(hash), kadv6TraversalFindNotes, 0, cb)
	for _, node := range n.tracker.closestNodesLocked(kadv6proto.NewID(hash), 50, true) {
		t.addNode(node.Addr, node.ID, node.TCPPort, node.Version)
	}
	return t.start()
}

func (n *kadv6NodeImpl) refresh(target kadv6proto.ID) bool {
	t := newKADV6Traversal(n, target, kadv6TraversalRefresh, 0, nil)
	for _, node := range n.tracker.closestNodesLocked(target, 50, true) {
		t.addNode(node.Addr, node.ID, node.TCPPort, node.Version)
	}
	return t.start()
}

func (n *kadv6NodeImpl) invoke(packet any, addr *net.UDPAddr, observer *kadv6Observer, opcode byte, target *protocol.Hash, multi bool) bool {
	if n == nil || n.tracker == nil || addr == nil {
		return false
	}
	if _, err := n.tracker.writePacket(addr, packet); err != nil {
		return false
	}
	n.tracker.rpc.Invoke(&kadRPCTransaction{
		endpointKey: addr.String(),
		opcode:      opcode,
		target:      target,
		multi:       multi,
		observer:    observer,
	})
	return true
}

func (n *kadv6NodeImpl) tick() {
	if n == nil || n.tracker == nil {
		return
	}
	now := time.Now()
	shortTimed, expired := n.tracker.rpc.Tick(now)
	for _, tx := range shortTimed {
		if tx == nil || tx.observer == nil {
			continue
		}
		rpcObserverShortTimeout(tx.observer)
	}
	for _, tx := range expired {
		if tx == nil {
			continue
		}
		if tx.observer != nil {
			rpcObserverTimeout(tx.observer)
			continue
		}
		node := n.tracker.nodes[tx.endpointKey]
		if node != nil {
			node.FailCount++
			n.tracker.table.NodeFailed(node)
		}
	}

	live, replacements := n.tracker.table.Size()
	if len(n.running) == 0 && n.initialBootstrapRequired && live > 0 && replacements < 10 {
		n.initialBootstrapRequired = false
		seeds := n.tracker.knownNodesLocked(false)
		addrs := make([]*net.UDPAddr, 0, len(seeds))
		for _, node := range seeds {
			if node != nil && node.Addr != nil {
				addrs = append(addrs, node.Addr)
			}
		}
		n.bootstrap(addrs)
	}
	if target := n.tracker.table.NeedRefresh(now); target != nil {
		n.tracker.lastRefresh = now
		n.refresh(*target)
	}
	if live == 0 && len(n.tracker.nodes) > 0 && n.tracker.table.NeedBootstrap(now) {
		n.tracker.lastBootstrap = now
		seeds := n.tracker.knownNodesLocked(false)
		addrs := make([]*net.UDPAddr, 0, len(seeds))
		for _, node := range seeds {
			if node != nil && node.Addr != nil {
				addrs = append(addrs, node.Addr)
			}
		}
		n.bootstrap(addrs)
	}
}

func (n *kadv6NodeImpl) processPing(addr *net.UDPAddr) {
	if n == nil || n.tracker == nil {
		return
	}
	_, _ = n.tracker.writePacket(addr, kadv6proto.Pong{UDPPort: uint16(n.tracker.ListenPort())})
}

func (n *kadv6NodeImpl) processHelloReq(addr *net.UDPAddr, hello kadv6proto.Hello) {
	if n == nil || n.tracker == nil {
		return
	}
	n.tracker.handleHello(addr, hello, true)
}

func (n *kadv6NodeImpl) processHelloRes(addr *net.UDPAddr, hello kadv6proto.Hello) {
	if n == nil || n.tracker == nil {
		return
	}
	n.tracker.rpc.Incoming(addr, kadv6proto.HelloResOp, nil)
	n.tracker.handleHello(addr, hello, false)
}

func (n *kadv6NodeImpl) processSearchNotesReq(addr *net.UDPAddr, req kadv6proto.SearchNotesReq) {
	if n == nil || n.tracker == nil {
		return
	}
	n.tracker.handleSearchNotesRequest(addr, req)
}

func (n *kadv6NodeImpl) processFindReq(addr *net.UDPAddr, req kadv6proto.FindNodeReq) {
	if n == nil || n.tracker == nil {
		return
	}
	n.tracker.handleFindRequest(addr, req)
}

func (n *kadv6NodeImpl) processFindRes(addr *net.UDPAddr, res kadv6proto.FindNodeRes) {
	if n == nil || n.tracker == nil {
		return
	}
	target := res.Target.Hash
	tx := n.tracker.rpc.Incoming(addr, kadv6proto.FindNodeResOp, &target)
	n.tracker.handleFindResponse(addr, res)
	if tx == nil {
		return
	}
	observer, ok := tx.observer.(*kadv6Observer)
	if !ok || observer == nil {
		return
	}
	for _, entry := range res.Results {
		observer.traversal.traverse(entry.Endpoint.UDPAddr(), entry.ID, entry.Endpoint.TCPPort, entry.Version)
	}
	observer.done()
}

func (n *kadv6NodeImpl) processBootstrapReq(addr *net.UDPAddr) {
	if n == nil || n.tracker == nil {
		return
	}
	n.tracker.handleBootstrapRequest(addr)
}

func (n *kadv6NodeImpl) processBootstrapRes(addr *net.UDPAddr, res kadv6proto.BootstrapRes) {
	if n == nil || n.tracker == nil {
		return
	}
	tx := n.tracker.rpc.Incoming(addr, kadv6proto.BootstrapResOp, nil)
	n.tracker.handleBootstrapResponse(addr, res)
	if tx == nil {
		return
	}
	observer, ok := tx.observer.(*kadv6Observer)
	if !ok || observer == nil {
		return
	}
	for _, contact := range res.Contacts {
		observer.traversal.traverse(contact.Endpoint.UDPAddr(), contact.ID, contact.Endpoint.TCPPort, contact.Version)
	}
	observer.done()
}

func (n *kadv6NodeImpl) processPublishKeysReq(addr *net.UDPAddr, req kadv6proto.PublishKeysReq) {
	if n == nil || n.tracker == nil {
		return
	}
	n.tracker.handlePublishKeysRequest(addr, req)
}

func (n *kadv6NodeImpl) processPublishSourcesReq(addr *net.UDPAddr, req kadv6proto.PublishSourcesReq) {
	if n == nil || n.tracker == nil {
		return
	}
	n.tracker.handlePublishSourcesRequest(addr, req)
}

func (n *kadv6NodeImpl) processPublishNotesReq(addr *net.UDPAddr, req kadv6proto.PublishNotesReq) {
	if n == nil || n.tracker == nil {
		return
	}
	n.tracker.handlePublishNotesRequest(addr, req)
}

func (n *kadv6NodeImpl) processPublishRes(addr *net.UDPAddr, res kadv6proto.PublishRes) {
	if n == nil || n.tracker == nil {
		return
	}
	target := res.FileID.Hash
	n.tracker.rpc.Incoming(addr, kadv6proto.PublishResOp, &target)
}

func (n *kadv6NodeImpl) processSearchKeysReq(addr *net.UDPAddr, req kadv6proto.SearchKeysReq) {
	if n == nil || n.tracker == nil {
		return
	}
	n.tracker.handleSearchKeysRequest(addr, req)
}

func (n *kadv6NodeImpl) processSearchSourcesReq(addr *net.UDPAddr, req kadv6proto.SearchSourcesReq) {
	if n == nil || n.tracker == nil {
		return
	}
	n.tracker.handleSearchSourcesRequest(addr, req)
}

func (n *kadv6NodeImpl) processSearchRes(addr *net.UDPAddr, res kadv6proto.SearchRes) {
	if n == nil || n.tracker == nil {
		return
	}
	target := res.Target.Hash
	tx := n.tracker.rpc.Incoming(addr, kadv6proto.SearchResOp, &target)
	if tx == nil {
		return
	}
	observer, ok := tx.observer.(*kadv6Observer)
	if !ok || observer == nil {
		return
	}
	observer.entries = append(observer.entries, res.Results...)
	observer.processedResponses++
}

func (n *kadv6NodeImpl) processPong(addr *net.UDPAddr, pong kadv6proto.Pong) {
	if n == nil || n.tracker == nil {
		return
	}
	n.tracker.rpc.Incoming(addr, kadv6proto.PongOp, nil)
	n.tracker.mu.Lock()
	if node := n.tracker.nodes[addr.String()]; node != nil {
		n.tracker.confirmNodeLocked(node)
	}
	n.tracker.mu.Unlock()
	_ = pong
}
