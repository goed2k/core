package goed2k

import "github.com/goed2k/core/protocol"

type KADV6Status struct {
	Bootstrapped      bool
	LiveNodes         int
	ReplacementNodes  int
	RouterNodes       int
	RunningTraversals int
	KnownNodes        int
	InitialBootstrap  bool
	ListenPort        int
	StoragePoint      string
}

type ClientDHTv6State struct {
	SelfID        protocol.Hash          `json:"self_id,omitempty"`
	LastBootstrap int64                  `json:"last_bootstrap,omitempty"`
	LastRefresh   int64                  `json:"last_refresh,omitempty"`
	StoragePoint  string                 `json:"storage_point,omitempty"`
	Nodes         []ClientDHTv6NodeState `json:"nodes,omitempty"`
	RouterNodes   []string               `json:"router_nodes,omitempty"`
}

type ClientDHTv6NodeState struct {
	ID        protocol.Hash `json:"id,omitempty"`
	Addr      string        `json:"addr"`
	TCPPort   uint16        `json:"tcp_port,omitempty"`
	Version   byte          `json:"version,omitempty"`
	Seed      bool          `json:"seed,omitempty"`
	HelloSent bool          `json:"hello_sent,omitempty"`
	Pinged    bool          `json:"pinged,omitempty"`
	FailCount int           `json:"fail_count,omitempty"`
	FirstSeen int64         `json:"first_seen,omitempty"`
	LastSeen  int64         `json:"last_seen,omitempty"`
}
