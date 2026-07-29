package goed2k

import (
	"net"
	"sort"
	"time"

	"github.com/goed2k/core/protocol"
	kadv6proto "github.com/goed2k/core/protocol/kadv6"
)

type KADV6RoutingNode struct {
	ID        kadv6proto.ID
	Addr      *net.UDPAddr
	TCPPort   uint16
	Version   byte
	Seed      bool
	HelloSent bool
	Pinged    bool
	FailCount int
	FirstSeen time.Time
	LastSeen  time.Time
}

func (n *KADV6RoutingNode) Key() string {
	if n == nil || n.Addr == nil {
		return ""
	}
	return n.Addr.String()
}

func (n *KADV6RoutingNode) KnownID() bool {
	if n == nil {
		return false
	}
	return !n.ID.Hash.Equal(protocol.Invalid)
}

func sortKADV6NodesByDistance(nodes []*KADV6RoutingNode, target kadv6proto.ID) {
	sort.Slice(nodes, func(i, j int) bool {
		return kadv6proto.DistanceCompare(nodes[i].ID, nodes[j].ID, target) < 0
	})
}

type kadv6RoutingBucket struct {
	live         []*KADV6RoutingNode
	replacements []*KADV6RoutingNode
	lastActive   time.Time
}

type kadv6RoutingTable struct {
	self            kadv6proto.ID
	bucketSize      int
	buckets         []kadv6RoutingBucket
	routerNodes     map[string]*net.UDPAddr
	lastBootstrap   time.Time
	lastRefresh     time.Time
	lastSelfRefresh time.Time
}

func newKADV6RoutingTable(self kadv6proto.ID, bucketSize int) *kadv6RoutingTable {
	if bucketSize <= 0 {
		bucketSize = 10
	}
	buckets := make([]kadv6RoutingBucket, 128)
	now := time.Now()
	for i := range buckets {
		buckets[i].lastActive = now
	}
	return &kadv6RoutingTable{
		self:        self,
		bucketSize:  bucketSize,
		buckets:     buckets,
		routerNodes: make(map[string]*net.UDPAddr),
	}
}

func (t *kadv6RoutingTable) AddRouterNode(addr *net.UDPAddr) {
	if t == nil || addr == nil {
		return
	}
	if normalized := normalizeUDPAddrV6(addr); normalized != nil {
		t.routerNodes[normalized.String()] = normalized
	}
}

func (t *kadv6RoutingTable) RouterNodes() []*net.UDPAddr {
	if t == nil {
		return nil
	}
	res := make([]*net.UDPAddr, 0, len(t.routerNodes))
	for _, addr := range t.routerNodes {
		if addr != nil {
			res = append(res, addr)
		}
	}
	return res
}

func (t *kadv6RoutingTable) HeardAbout(node *KADV6RoutingNode) {
	t.addNode(node, false)
}

func (t *kadv6RoutingTable) NodeSeen(node *KADV6RoutingNode) {
	t.addNode(node, true)
}

func (t *kadv6RoutingTable) addNode(node *KADV6RoutingNode, confirmed bool) {
	if t == nil || node == nil || !node.KnownID() {
		return
	}
	index := kadv6BucketIndex(t.self, node.ID)
	bucket := &t.buckets[index]
	bucket.lastActive = time.Now()

	if existing, pos := findKADV6Node(bucket.live, node); pos >= 0 {
		existing.Addr = node.Addr
		existing.TCPPort = node.TCPPort
		existing.Version = node.Version
		existing.LastSeen = time.Now()
		if confirmed {
			existing.Pinged = true
			existing.FailCount = 0
		}
		moveKADV6NodeToEnd(bucket.live, pos)
		return
	}
	if existing, pos := findKADV6Node(bucket.replacements, node); pos >= 0 {
		existing.Addr = node.Addr
		existing.TCPPort = node.TCPPort
		existing.Version = node.Version
		existing.LastSeen = time.Now()
		if confirmed {
			existing.Pinged = true
			existing.FailCount = 0
			bucket.replacements = append(bucket.replacements[:pos], bucket.replacements[pos+1:]...)
			if len(bucket.live) < t.bucketSize {
				bucket.live = append(bucket.live, existing)
			} else {
				bucket.replacements = append(bucket.replacements, existing)
			}
		}
		return
	}

	node.LastSeen = time.Now()
	if node.FirstSeen.IsZero() {
		node.FirstSeen = node.LastSeen
	}
	if confirmed {
		node.Pinged = true
		node.FailCount = 0
	}
	if len(bucket.live) < t.bucketSize {
		bucket.live = append(bucket.live, node)
		return
	}
	if len(bucket.replacements) < t.bucketSize {
		bucket.replacements = append(bucket.replacements, node)
		return
	}
	bucket.replacements = append(bucket.replacements[1:], node)
}

func (t *kadv6RoutingTable) NodeFailed(node *KADV6RoutingNode) {
	if t == nil || node == nil || !node.KnownID() {
		return
	}
	index := kadv6BucketIndex(t.self, node.ID)
	bucket := &t.buckets[index]
	if existing, pos := findKADV6Node(bucket.live, node); pos >= 0 {
		if !existing.Pinged {
			bucket.live = append(bucket.live[:pos], bucket.live[pos+1:]...)
			return
		}
		existing.FailCount++
		if len(bucket.replacements) > 0 {
			repl := bucket.replacements[0]
			bucket.replacements = bucket.replacements[1:]
			bucket.live[pos] = repl
			return
		}
		if existing.FailCount >= 20 {
			bucket.live = append(bucket.live[:pos], bucket.live[pos+1:]...)
		}
		return
	}
	if _, pos := findKADV6Node(bucket.replacements, node); pos >= 0 {
		bucket.replacements = append(bucket.replacements[:pos], bucket.replacements[pos+1:]...)
	}
}

func (t *kadv6RoutingTable) FindClosest(target kadv6proto.ID, limit int, includeUnconfirmed bool) []*KADV6RoutingNode {
	if t == nil {
		return nil
	}
	nodes := make([]*KADV6RoutingNode, 0, len(t.buckets)*t.bucketSize)
	for i := range t.buckets {
		for _, node := range t.buckets[i].live {
			if node == nil {
				continue
			}
			if !includeUnconfirmed && !node.Pinged {
				continue
			}
			nodes = append(nodes, node)
		}
	}
	sortKADV6NodesByDistance(nodes, target)
	if limit > 0 && len(nodes) > limit {
		nodes = nodes[:limit]
	}
	return nodes
}

func (t *kadv6RoutingTable) TouchBucket(target kadv6proto.ID) {
	if t == nil {
		return
	}
	t.buckets[kadv6BucketIndex(t.self, target)].lastActive = time.Now()
}

func (t *kadv6RoutingTable) NeedBootstrap(now time.Time) bool {
	if t == nil {
		return false
	}
	live, replacements := t.Size()
	if live > 0 || replacements >= t.bucketSize {
		return false
	}
	if t.lastBootstrap.IsZero() || now.Sub(t.lastBootstrap) >= 30*time.Second {
		t.lastBootstrap = now
		return true
	}
	return false
}

func (t *kadv6RoutingTable) NeedRefresh(now time.Time) *kadv6proto.ID {
	if t == nil {
		return nil
	}
	if t.lastSelfRefresh.IsZero() || now.Sub(t.lastSelfRefresh) >= 15*time.Minute {
		t.lastSelfRefresh = now
		target := t.self
		return &target
	}
	index := -1
	var oldest time.Time
	for i := range t.buckets {
		last := t.buckets[i].lastActive
		if now.Sub(last) < 15*time.Minute {
			continue
		}
		if index < 0 || last.Before(oldest) {
			index = i
			oldest = last
		}
	}
	if index < 0 {
		return nil
	}
	if !t.lastRefresh.IsZero() && now.Sub(t.lastRefresh) < 45*time.Second {
		return nil
	}
	t.lastRefresh = now
	target := randomKADV6IDWithinBucket(index, t.self)
	return &target
}

func (t *kadv6RoutingTable) Size() (live int, replacements int) {
	if t == nil {
		return 0, 0
	}
	for i := range t.buckets {
		live += len(t.buckets[i].live)
		replacements += len(t.buckets[i].replacements)
	}
	return live, replacements
}

func kadv6BucketIndex(self, other kadv6proto.ID) int {
	for i := 0; i < 16; i++ {
		x := self.Hash.At(i) ^ other.Hash.At(i)
		if x == 0 {
			continue
		}
		for bit := 0; bit < 8; bit++ {
			mask := byte(0x80 >> bit)
			if x&mask != 0 {
				return i*8 + bit
			}
		}
	}
	return 127
}

func randomKADV6IDWithinBucket(index int, self kadv6proto.ID) kadv6proto.ID {
	hash, err := protocol.RandomHash(false)
	if err != nil {
		return self
	}
	id := kadv6proto.NewID(hash)
	byteIndex := index / 8
	bitIndex := index % 8
	for i := 0; i < byteIndex; i++ {
		id.Hash.Set(i, self.Hash.At(i))
	}
	mask := byte(0x80 >> bitIndex)
	id.Hash.Set(byteIndex, (self.Hash.At(byteIndex)&^mask)|(^self.Hash.At(byteIndex)&mask))
	return id
}

func findKADV6Node(nodes []*KADV6RoutingNode, needle *KADV6RoutingNode) (*KADV6RoutingNode, int) {
	for i, node := range nodes {
		if node == nil || needle == nil {
			continue
		}
		if node.KnownID() && needle.KnownID() && node.ID.Hash.Equal(needle.ID.Hash) {
			return node, i
		}
		if node.Addr != nil && needle.Addr != nil && node.Addr.String() == needle.Addr.String() {
			return node, i
		}
	}
	return nil, -1
}

func moveKADV6NodeToEnd(nodes []*KADV6RoutingNode, index int) {
	if index < 0 || index >= len(nodes) {
		return
	}
	node := nodes[index]
	copy(nodes[index:], nodes[index+1:])
	nodes[len(nodes)-1] = node
}
