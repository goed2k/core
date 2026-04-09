# KADV6 Implementation Plan

## 1. Objective

Implement `KADV6` as an isolated IPv6-only DHT overlay for `goed2k`, based on the protocol documents:

- [kadv6-protocol.md](/mnt/e/code/goed2k/docs/kadv6-protocol.md)
- [kadv6-protocol-CN.md](/mnt/e/code/goed2k/docs/kadv6-protocol-CN.md)

Phase 1 scope:

- independent `KADV6` protocol package
- independent `KADV6` tracker and routing runtime
- IPv6 node bootstrap, discovery, publish, and search
- independent state persistence and status reporting
- no `kad4` bridge
- no `kadv6 -> kad4`
- no download-path integration

Out of scope for Phase 1:

- file transfer over IPv6 peers
- source results entering the current peer connection pipeline
- bridge, proxy, or relay features
- protocol compatibility with legacy Kad

## 2. Delivery Strategy

Use a layered rollout. Each layer must compile and be testable before moving to the next.

Recommended order:

1. `protocol/kadv6` binary definitions
2. `KADV6` runtime skeleton
3. routing, RPC, traversal
4. publish/search indexes
5. persistence and configuration
6. CLI/TUI status exposure
7. verification and hardening

## 3. Work Breakdown

### Milestone 1: Protocol Package

Goal:

- define `protocol/kadv6` without touching legacy `protocol/kad`

Files to add:

- `protocol/kadv6/types.go`
- `protocol/kadv6/packets.go`
- `protocol/kadv6/packet_combiner.go`
- `protocol/kadv6/types_test.go`
- `protocol/kadv6/packet_combiner_test.go`

Tasks:

1. define constants:
   - protocol header
   - version
   - opcodes
   - tag types
   - tag ids
2. implement binary structs:
   - `ID`
   - `EndpointV6`
   - `EntryV6`
   - `Tag`
   - `SearchEntry`
   - `NodesDat`
3. implement packet pack/unpack:
   - `BootstrapReq/Res`
   - `HelloReq/Res`
   - `FindNodeReq/Res`
   - `SearchSourcesReq`
   - `SearchKeysReq`
   - `SearchNotesReq`
   - `SearchRes`
   - `PublishSourcesReq`
   - `PublishKeysReq`
   - `PublishNotesReq`
   - `PublishRes`
   - `Ping/Pong`
4. implement validation helpers:
   - IPv6 endpoint validation
   - source tag extraction
   - `nodes6.dat` parsing

Acceptance criteria:

- protocol tests pass
- malformed packets are rejected
- `nodes6.dat` can round-trip through encode/decode

### Milestone 2: Runtime Skeleton

Goal:

- make a `KADV6Tracker` that can listen on `udp6`, read packets, and dispatch by opcode

Files to add:

- `kadv6_tracker.go`
- `kadv6_status.go`

Tasks:

1. define `KADV6Tracker` fields:
   - UDP connection
   - self ID
   - node map
   - routing table
   - rpc manager
   - search indexes
   - timestamps
2. implement lifecycle:
   - `NewKADV6Tracker`
   - `Start`
   - `Close`
3. implement socket rules:
   - listen with `udp6`
   - reject IPv4 and IPv4-mapped peers
4. implement packet dispatch loop
5. define `KADV6Status`

Acceptance criteria:

- tracker starts on IPv6 UDP
- tracker can receive and decode a valid `Ping`
- tracker ignores malformed or non-IPv6 traffic safely

### Milestone 3: Routing, RPC, Traversal

Goal:

- support bootstrap and iterative lookup

Files to add:

- `kadv6_node.go`
- `kadv6_routing.go`
- `kadv6_rpc.go`
- `kadv6_traversal.go`
- `kadv6_routing_test.go`

Tasks:

1. implement routing node model
2. implement bucket table:
   - heard-about
   - node-seen
   - node-failed
   - find-closest
   - refresh scheduling
3. implement RPC transaction manager:
   - invoke
   - incoming match
   - short timeout
   - full timeout
4. implement traversal kinds:
   - bootstrap
   - find-node
   - search-sources
   - search-keywords
   - search-notes
5. wire `Hello`, `Bootstrap`, `FindNode`, `Ping/Pong`

Acceptance criteria:

- two local IPv6 nodes can bootstrap
- a node can learn contacts from another node
- traversal converges and times out correctly

### Milestone 4: Publish/Search Indexes

Goal:

- support local indexing and distributed search for source, keyword, and notes

Files to add:

- extend `kadv6_tracker.go`
- `kadv6_tracker_test.go`

Tasks:

1. add in-memory indexes:
   - source index
   - keyword index
   - notes index
2. implement local store helpers
3. implement:
   - `PublishSource`
   - `PublishKeyword`
   - `PublishNotes`
4. implement:
   - `SearchSources`
   - `SearchKeywords`
   - `SearchNotes`
5. add result dedup logic
6. enforce packet entry limits

Acceptance criteria:

- published records can be searched from another IPv6 node
- duplicate results are collapsed
- invalid source tags are rejected

### Milestone 5: Persistence and Configuration

Goal:

- make `KADV6` survive restart and configurable from `Client`

Files to update:

- `client.go`
- `session.go`
- `settings.go`
- `client_state.go`
- `dht_status.go`

New data model:

- `ClientDHTv6State`
- `ClientDHTv6NodeState`
- `KADV6Status`

Tasks:

1. add settings:
   - `EnableDHTv6`
   - `UDPPortV6`
   - `DHTv6SearchTimeout`
2. add session field:
   - `dhtv6Tracker`
3. add client API:
   - `EnableDHTv6`
   - `GetDHTv6Tracker`
   - `LoadDHTv6NodesDat`
   - `AddDHTv6BootstrapNodes`
   - `DHTv6Status`
4. implement state snapshot/restore
5. add `nodes6.dat` loading path

Acceptance criteria:

- state save/load restores IPv6 node table
- `Client` can start `KADV6`
- `nodes6.dat` can be loaded from file and URL if desired

### Milestone 6: CLI and TUI Exposure

Goal:

- expose `KADV6` as an optional independent feature

Files to update:

- `cmd/goed2k/main.go`
- `cmd/goed2k/setup_tui.go`
- `cmd/goed2k/tui.go`

Tasks:

1. add CLI flags:
   - `--kadv6`
   - `--udp6-port`
   - `--kadv6-nodes-dat`
   - `--kadv6-bootstrap`
2. add TUI config fields
3. expose status lines:
   - `kadv6 live`
   - `kadv6 known`
   - `kadv6 traversals`
   - `kadv6 listen port`
4. keep `kad4` and `kadv6` visibly separate

Acceptance criteria:

- users can enable `KADV6` without touching `kad4`
- TUI clearly distinguishes `kad4` from `kadv6`

### Milestone 7: Verification and Hardening

Goal:

- verify correctness and prevent regressions

Tasks:

1. run all new unit tests
2. run existing DHT and client tests to ensure no `kad4` regression
3. add negative tests:
   - IPv4 endpoint rejection
   - IPv4-mapped address rejection
   - malformed tag rejection
   - oversize packet rejection
4. add small multi-node IPv6 integration test if environment allows

Acceptance criteria:

- new `kadv6` tests pass
- existing `kad4` tests still pass
- no shared state confusion between `kad4` and `kadv6`

## 4. Proposed File Map

### New Files

- `docs/kadv6-protocol.md`
- `docs/kadv6-protocol-CN.md`
- `protocol/kadv6/types.go`
- `protocol/kadv6/packets.go`
- `protocol/kadv6/packet_combiner.go`
- `protocol/kadv6/types_test.go`
- `protocol/kadv6/packet_combiner_test.go`
- `kadv6_tracker.go`
- `kadv6_status.go`
- `kadv6_node.go`
- `kadv6_routing.go`
- `kadv6_rpc.go`
- `kadv6_traversal.go`
- `kadv6_tracker_test.go`
- `kadv6_routing_test.go`

### Existing Files Likely to Change

- `client.go`
- `session.go`
- `settings.go`
- `client_state.go`
- `dht_status.go`
- `cmd/goed2k/main.go`
- `cmd/goed2k/setup_tui.go`
- `cmd/goed2k/tui.go`

## 5. Technical Rules

These rules should be followed during implementation.

1. Do not reuse `protocol.Endpoint` for `kadv6`.
2. Do not modify legacy `protocol/kad` wire semantics.
3. Keep `kad4` and `kadv6` state, config, and UI separate.
4. Reject non-native IPv6 addresses early.
5. Do not connect `kadv6` search results into the current peer transfer path in Phase 1.
6. Keep comments concise and only where logic is non-obvious.
7. Prefer small testable commits or reviewable patches per milestone.

## 6. Risks

### Risk 1: Address Model Leakage

Current code uses IPv4-centric types in several places. If those leak into `kadv6`, the design will rot quickly.

Mitigation:

- keep `kadv6` packet and runtime types separate
- use `net.IP` or `[16]byte` based helpers for IPv6 only

### Risk 2: Dual-Stack Socket Ambiguity

Some platforms handle dual-stack sockets differently.

Mitigation:

- explicitly use `udp6`
- reject IPv4-mapped addresses
- test with local IPv6 loopback first

### Risk 3: Scope Creep into Download Path

It is tempting to immediately attach `kadv6` source results to peer connection logic.

Mitigation:

- keep that out of Phase 1
- finish DHT overlay correctness first

### Risk 4: Nodes File Churn

If `nodes6.dat` is underspecified now, later changes will be painful.

Mitigation:

- keep a magic header and version field
- test round-trip from the start

## 7. Test Plan

Minimum tests by layer:

### Protocol

- endpoint encode/decode
- entry encode/decode
- tag encode/decode including bytes
- source address extraction
- each packet round-trip
- `nodes6.dat` parse/load/round-trip

### Runtime

- tracker start/stop
- ping/pong
- hello exchange
- bootstrap response population
- routing table replacement behavior
- rpc timeout behavior

### Search/Publish

- publish source then search source
- publish keyword then search keyword
- publish notes then search notes
- dedup result keys

### Rejection

- IPv4 endpoint rejected
- IPv4-mapped endpoint rejected
- malformed tag length rejected
- oversize payload rejected

## 8. Completion Definition

Phase 1 is complete when all of the following are true:

1. `protocol/kadv6` exists and is fully unit tested.
2. `KADV6Tracker` can bootstrap over IPv6 UDP.
3. IPv6 nodes can publish and search source/keyword/notes.
4. `nodes6.dat` can be loaded and saved through the chosen state path.
5. `Client` can enable and report `KADV6` independently from `kad4`.
6. CLI/TUI can display `KADV6` state.
7. Existing `kad4` behavior remains unchanged.
8. No `kad4` bridge behavior exists in code.

## 9. Recommended Next Action

The next implementation step should be:

1. create `protocol/kadv6/types.go`
2. create `protocol/kadv6/packets.go`
3. create the corresponding unit tests before writing runtime code

Reason:

- protocol definitions are the narrowest dependency base
- they clarify all later runtime code
- they are easy to test without touching the rest of the system
