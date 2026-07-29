package goed2k

import (
	"net"
	"sort"

	"github.com/goed2k/core/protocol"
	kadv6proto "github.com/goed2k/core/protocol/kadv6"
)

const (
	kadv6ObserverFlagQueried      = 1 << 0
	kadv6ObserverFlagInitial      = 1 << 1
	kadv6ObserverFlagNoID         = 1 << 2
	kadv6ObserverFlagShortTimeout = 1 << 3
	kadv6ObserverFlagFailed       = 1 << 4
	kadv6ObserverFlagAlive        = 1 << 5
	kadv6ObserverFlagDone         = 1 << 6

	kadv6TraversalPreventRequest = 1
	kadv6TraversalShortTimeout   = 2
	kadv6TraversalMaxResults     = 100
)

type kadv6TraversalKind string

const (
	kadv6TraversalBootstrap     kadv6TraversalKind = "bootstrap"
	kadv6TraversalFindSources   kadv6TraversalKind = "find-sources"
	kadv6TraversalSearchSources kadv6TraversalKind = "search-sources"
	kadv6TraversalFindKeywords  kadv6TraversalKind = "find-keywords"
	kadv6TraversalSearchKeyword kadv6TraversalKind = "search-keywords"
	kadv6TraversalFindNotes     kadv6TraversalKind = "find-notes"
	kadv6TraversalSearchNotes   kadv6TraversalKind = "search-notes"
	kadv6TraversalRefresh       kadv6TraversalKind = "refresh"
)

type kadv6Observer struct {
	traversal          *kadv6Traversal
	endpoint           *net.UDPAddr
	id                 kadv6proto.ID
	portTCP            uint16
	version            byte
	flags              int
	processedResponses int
	entries            []kadv6proto.SearchEntry
}

func (o *kadv6Observer) expectMultipleResponses() bool {
	if o == nil || o.traversal == nil {
		return false
	}
	switch o.traversal.kind {
	case kadv6TraversalSearchSources, kadv6TraversalSearchKeyword, kadv6TraversalSearchNotes:
		return true
	default:
		return false
	}
}

func (o *kadv6Observer) shortTimeout() {
	if o == nil || o.traversal == nil || o.flags&kadv6ObserverFlagShortTimeout != 0 {
		return
	}
	o.traversal.failed(o, kadv6TraversalShortTimeout)
}

func (o *kadv6Observer) timeout() {
	if o == nil || o.traversal == nil || o.flags&kadv6ObserverFlagDone != 0 {
		return
	}
	if o.expectMultipleResponses() && o.processedResponses > 0 {
		o.done()
		return
	}
	o.flags |= kadv6ObserverFlagDone
	o.traversal.failed(o, 0)
}

func (o *kadv6Observer) abort() {
	if o == nil || o.traversal == nil || o.flags&kadv6ObserverFlagDone != 0 {
		return
	}
	o.flags |= kadv6ObserverFlagDone
	o.traversal.failed(o, kadv6TraversalPreventRequest)
}

func (o *kadv6Observer) done() {
	if o == nil || o.traversal == nil || o.flags&kadv6ObserverFlagDone != 0 {
		return
	}
	o.flags |= kadv6ObserverFlagDone
	o.traversal.finished(o)
}

type kadv6Traversal struct {
	node           *kadv6NodeImpl
	target         kadv6proto.ID
	kind           kadv6TraversalKind
	results        []*kadv6Observer
	invokeCount    int
	branchFactor   int
	responses      int
	timeouts       int
	numTargetNodes int
	size           int64
	listener       func([]kadv6proto.SearchEntry)
	accum          []kadv6proto.SearchEntry
}

func newKADV6Traversal(node *kadv6NodeImpl, target kadv6proto.ID, kind kadv6TraversalKind, size int64, listener func([]kadv6proto.SearchEntry)) *kadv6Traversal {
	traversal := &kadv6Traversal{
		node:        node,
		target:      target,
		kind:        kind,
		size:        size,
		listener:    listener,
		results:     make([]*kadv6Observer, 0, 16),
		accum:       make([]kadv6proto.SearchEntry, 0, 16),
	}
	if node != nil && node.tracker != nil && node.tracker.table != nil {
		traversal.numTargetNodes = node.tracker.table.bucketSize * 2
	}
	if traversal.numTargetNodes <= 0 {
		traversal.numTargetNodes = 20
	}
	return traversal
}

func (t *kadv6Traversal) key() string {
	if t == nil {
		return ""
	}
	return string(t.kind) + ":" + t.target.Hash.String()
}

func (t *kadv6Traversal) start() bool {
	if t == nil || t.node == nil || t.node.tracker == nil {
		return false
	}
	if !t.node.addTraversal(t) {
		return false
	}
	t.branchFactor = t.node.searchBranching()
	if t.kind == kadv6TraversalRefresh {
		t.numTargetNodes = 1
	}
	if t.kind == kadv6TraversalSearchSources || t.kind == kadv6TraversalSearchKeyword || t.kind == kadv6TraversalSearchNotes {
		t.numTargetNodes = len(t.results)
	}
	if t.node.tracker.table != nil {
		t.node.tracker.table.TouchBucket(t.target)
	}
	if len(t.results) == 0 {
		t.addRouterEntries()
	}
	t.addRequests()
	if t.invokeCount == 0 {
		t.done()
	}
	return true
}

func (t *kadv6Traversal) addRouterEntries() {
	if t == nil || t.node == nil || t.node.tracker == nil || t.node.tracker.table == nil {
		return
	}
	for _, addr := range t.node.tracker.table.RouterNodes() {
		t.addEntry(kadv6proto.ID{}, addr, kadv6ObserverFlagInitial, 0, 0)
	}
}

func (t *kadv6Traversal) newObserver(endpoint *net.UDPAddr, id kadv6proto.ID, portTCP uint16, version byte) *kadv6Observer {
	return &kadv6Observer{
		traversal: t,
		endpoint:  normalizeUDPAddrV6(endpoint),
		id:        id,
		portTCP:   portTCP,
		version:   version,
	}
}

func (t *kadv6Traversal) addNode(endpoint *net.UDPAddr, id kadv6proto.ID, portTCP uint16, version byte) {
	o := t.newObserver(endpoint, id, portTCP, version)
	t.results = append(t.results, o)
	t.numTargetNodes = len(t.results)
}

func (t *kadv6Traversal) addEntry(id kadv6proto.ID, endpoint *net.UDPAddr, flags int, portTCP uint16, version byte) {
	if t == nil || endpoint == nil {
		return
	}
	o := t.newObserver(endpoint, id, portTCP, version)
	if o.endpoint == nil {
		return
	}
	if o.id.Hash.Equal(protocol.Invalid) {
		randomID, err := protocol.RandomHash(false)
		if err == nil {
			o.id = kadv6proto.NewID(randomID)
			o.flags |= kadv6ObserverFlagNoID
		}
	}
	o.flags |= flags
	insertPos := sort.Search(len(t.results), func(i int) bool {
		return kadv6proto.DistanceCompare(t.results[i].id, o.id, t.target) >= 0
	})
	if insertPos < len(t.results) {
		existing := t.results[insertPos]
		if existing != nil && existing.id.Hash.Equal(o.id.Hash) {
			return
		}
	}
	t.results = append(t.results, nil)
	copy(t.results[insertPos+1:], t.results[insertPos:])
	t.results[insertPos] = o
	if len(t.results) > kadv6TraversalMaxResults {
		t.results = t.results[:kadv6TraversalMaxResults]
	}
}

func (t *kadv6Traversal) addRequests() {
	resultsTarget := t.numTargetNodes
	for i := 0; i < len(t.results) && resultsTarget > 0 && t.invokeCount < t.branchFactor; i++ {
		o := t.results[i]
		if o == nil {
			continue
		}
		if o.flags&kadv6ObserverFlagAlive != 0 {
			resultsTarget--
		}
		if o.flags&kadv6ObserverFlagQueried != 0 {
			continue
		}
		o.flags |= kadv6ObserverFlagQueried
		if t.invoke(o) {
			t.invokeCount++
		} else {
			o.flags |= kadv6ObserverFlagFailed
		}
	}
}

func (t *kadv6Traversal) invoke(o *kadv6Observer) bool {
	if t == nil || t.node == nil || o == nil || o.endpoint == nil {
		return false
	}
	switch t.kind {
	case kadv6TraversalBootstrap:
		return t.node.invoke(kadv6proto.BootstrapReq{}, o.endpoint, o, kadv6proto.BootstrapResOp, nil, false)
	case kadv6TraversalFindSources, kadv6TraversalRefresh:
		return t.node.invoke(kadv6proto.FindNodeReq{
			SearchType: kadv6proto.FindNode,
			Target:     t.target,
			Receiver:   o.id,
		}, o.endpoint, o, kadv6proto.FindNodeResOp, &t.target.Hash, false)
	case kadv6TraversalFindKeywords, kadv6TraversalFindNotes:
		return t.node.invoke(kadv6proto.FindNodeReq{
			SearchType: kadv6proto.FindValue,
			Target:     t.target,
			Receiver:   o.id,
		}, o.endpoint, o, kadv6proto.FindNodeResOp, &t.target.Hash, false)
	case kadv6TraversalSearchSources:
		return t.node.invoke(kadv6proto.SearchSourcesReq{
			Target:   t.target,
			StartPos: 0,
			Size:     uint64(t.size),
		}, o.endpoint, o, kadv6proto.SearchResOp, &t.target.Hash, true)
	case kadv6TraversalSearchKeyword:
		return t.node.invoke(kadv6proto.SearchKeysReq{
			Target:   t.target,
			StartPos: 0,
		}, o.endpoint, o, kadv6proto.SearchResOp, &t.target.Hash, true)
	case kadv6TraversalSearchNotes:
		return t.node.invoke(kadv6proto.SearchNotesReq{
			Target:   t.target,
			StartPos: 0,
		}, o.endpoint, o, kadv6proto.SearchResOp, &t.target.Hash, true)
	default:
		return false
	}
}

func (t *kadv6Traversal) failed(o *kadv6Observer, flags int) {
	if t == nil || o == nil {
		return
	}
	if flags&kadv6TraversalShortTimeout != 0 {
		t.branchFactor++
		o.flags |= kadv6ObserverFlagShortTimeout
		return
	}
	o.flags |= kadv6ObserverFlagFailed
	if o.flags&kadv6ObserverFlagShortTimeout != 0 && t.branchFactor > 1 {
		t.branchFactor--
	}
	t.writeFailedObserver(o)
	t.timeouts++
	if t.invokeCount > 0 {
		t.invokeCount--
	}
	if flags&kadv6TraversalPreventRequest != 0 && t.branchFactor > 1 {
		t.branchFactor--
	}
	t.addRequests()
	if t.invokeCount == 0 {
		t.done()
	}
}

func (t *kadv6Traversal) writeFailedObserver(o *kadv6Observer) {
	if t == nil || t.node == nil || t.node.tracker == nil || o == nil {
		return
	}
	switch t.kind {
	case kadv6TraversalFindSources, kadv6TraversalSearchSources,
		kadv6TraversalFindKeywords, kadv6TraversalSearchKeyword,
		kadv6TraversalFindNotes, kadv6TraversalSearchNotes:
		return
	}
	if o.flags&kadv6ObserverFlagNoID != 0 {
		return
	}
	node := t.node.tracker.nodes[o.endpoint.String()]
	if node != nil {
		t.node.tracker.table.NodeFailed(node)
	}
}

func (t *kadv6Traversal) finished(o *kadv6Observer) {
	if t == nil || o == nil {
		return
	}
	if o.flags&kadv6ObserverFlagShortTimeout != 0 && t.branchFactor > 1 {
		t.branchFactor--
	}
	o.flags |= kadv6ObserverFlagAlive
	t.responses++
	if t.invokeCount > 0 {
		t.invokeCount--
	}
	switch t.kind {
	case kadv6TraversalSearchSources, kadv6TraversalSearchKeyword, kadv6TraversalSearchNotes:
		if len(o.entries) > 0 {
			t.accum = append(t.accum, o.entries...)
		}
	}
	t.addRequests()
	if t.invokeCount == 0 {
		t.done()
	}
}

func (t *kadv6Traversal) traverse(endpoint *net.UDPAddr, id kadv6proto.ID, portTCP uint16, version byte) {
	if t == nil || t.node == nil || t.node.tracker == nil {
		return
	}
	node := t.node.tracker.addOrUpdateNodeLocked(id, endpoint, portTCP, version, false)
	if node != nil && node.KnownID() {
		t.node.tracker.table.HeardAbout(node)
	}
	t.addEntry(id, endpoint, 0, portTCP, version)
}

func (t *kadv6Traversal) done() {
	if t == nil || t.node == nil {
		return
	}
	t.node.removeTraversal(t)
	switch t.kind {
	case kadv6TraversalBootstrap:
		for _, o := range t.results {
			if o == nil || o.flags&kadv6ObserverFlagQueried != 0 {
				continue
			}
			t.node.addNode(o.endpoint, o.id, o.portTCP, o.version)
		}
	case kadv6TraversalFindSources:
		direct := newKADV6Traversal(t.node, t.target, kadv6TraversalSearchSources, t.size, t.listener)
		if sp := t.node.storagePoint(); sp != nil {
			direct.addNode(sp, t.target, 0, 0)
		}
		for _, o := range t.results {
			if o == nil || o.flags&kadv6ObserverFlagFailed != 0 {
				continue
			}
			direct.addNode(o.endpoint, o.id, o.portTCP, o.version)
		}
		_ = direct.start()
	case kadv6TraversalFindKeywords:
		direct := newKADV6Traversal(t.node, t.target, kadv6TraversalSearchKeyword, 0, t.listener)
		if sp := t.node.storagePoint(); sp != nil {
			direct.addNode(sp, t.target, 0, 0)
		}
		for _, o := range t.results {
			if o == nil || o.flags&kadv6ObserverFlagFailed != 0 {
				continue
			}
			direct.addNode(o.endpoint, o.id, o.portTCP, o.version)
		}
		_ = direct.start()
	case kadv6TraversalFindNotes:
		direct := newKADV6Traversal(t.node, t.target, kadv6TraversalSearchNotes, 0, t.listener)
		if sp := t.node.storagePoint(); sp != nil {
			direct.addNode(sp, t.target, 0, 0)
		}
		for _, o := range t.results {
			if o == nil || o.flags&kadv6ObserverFlagFailed != 0 {
				continue
			}
			direct.addNode(o.endpoint, o.id, o.portTCP, o.version)
		}
		_ = direct.start()
	case kadv6TraversalSearchSources:
		for _, entry := range t.accum {
			t.node.processPublishSourcesReq(nil, kadv6proto.PublishSourcesReq{
				FileID: t.target,
				Source: entry,
			})
		}
		if t.listener != nil {
			t.listener(t.accum)
		}
	case kadv6TraversalSearchKeyword:
		for _, entry := range t.accum {
			t.node.processPublishKeysReq(nil, kadv6proto.PublishKeysReq{
				KeywordID: t.target,
				Sources:   []kadv6proto.SearchEntry{entry},
			})
		}
		if t.listener != nil {
			t.listener(t.accum)
		}
	case kadv6TraversalSearchNotes:
		for _, entry := range t.accum {
			t.node.processPublishNotesReq(nil, kadv6proto.PublishNotesReq{
				FileID: t.target,
				Notes:  []kadv6proto.SearchEntry{entry},
			})
		}
		if t.listener != nil {
			t.listener(t.accum)
		}
	}
}
