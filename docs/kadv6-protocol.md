# KADV6 Protocol Draft

## 1. Scope

This document defines an isolated IPv6-only DHT overlay named `KADV6` for `goed2k`.

Goals:

- allow IPv6 nodes to bootstrap, discover each other, publish metadata, and search metadata
- keep the routing algorithm close to classic Kad semantics
- avoid any binary compatibility requirement with the existing `kad4` implementation
- keep the first version small enough to implement and test incrementally

Non-goals:

- no `kad4 <-> kadv6` bridge
- no `kadv6 -> kad4` propagation
- no IPv4 transport inside `kadv6`
- no direct reuse of the classic Kad packet format
- no relay or proxy transport for file transfer

`KADV6` is a separate overlay. A node is either on `kad4`, on `kadv6`, or on both via independent tracker instances, but the overlays do not exchange state in this version.

## 2. Transport

- transport: `UDP/IPv6` only
- listen network: `udp6`
- all multi-hop DHT traffic uses UDP
- all node addresses are native IPv6 addresses
- IPv4-mapped IPv6 addresses must be rejected

Implementation rule:

- accept only addresses where `ip.To16() != nil` and `ip.To4() == nil`

## 3. Wire Conventions

- byte order: little-endian unless explicitly noted otherwise
- hash size: 16 bytes
- packet framing: `header(1) + opcode(1) + payload(n)`

Packet header:

| Field | Size | Value |
|---|---:|---|
| `ProtocolHeader` | 1 byte | `0xE6` |
| `Opcode` | 1 byte | see section 8 |

Protocol version:

- `KADV6Version = 0x01`

Version is carried in selected payloads such as `Hello` and `BootstrapRes`.

## 4. Core Model

### 4.1 Node ID

- size: 128 bits
- type: same logical size as the existing `protocol.Hash`
- routing metric: XOR distance

`KADV6` reuses the 128-bit Kad routing model. The bucket split and traversal logic may stay conceptually aligned with existing Kad code, but the address and packet encoding are independent.

### 4.2 EndpointV6

All routable contacts in `KADV6` use `EndpointV6`.

Binary layout:

| Field | Size | Notes |
|---|---:|---|
| `IP` | 16 bytes | raw IPv6 address bytes |
| `UDPPort` | 2 bytes | UDP port for DHT |
| `TCPPort` | 2 bytes | TCP port for peer traffic or advertised service |

Go shape:

```go
type EndpointV6 struct {
    IP      [16]byte
    UDPPort uint16
    TCPPort uint16
}
```

Validation:

- `IP` must not be unspecified (`::`)
- `IP` must not be IPv4-mapped
- `UDPPort` must be non-zero for routable contacts

String form:

- canonical display form should use RFC 5952 IPv6 text
- endpoint display form should be `[ipv6]:port`

### 4.3 EntryV6

Binary layout:

| Field | Size |
|---|---:|
| `ID` | 16 bytes |
| `Endpoint` | 20 bytes |
| `Version` | 1 byte |
| `Flags` | 1 byte |

Flags:

- bit `0`: `Verified`
- bits `1-7`: reserved, must be zero in version 1

Go shape:

```go
type EntryV6 struct {
    ID       ID
    Endpoint EndpointV6
    Version  byte
    Verified bool
}
```

## 5. Search Entry Model

`KADV6` keeps Kad-style search results, but extends the tag system so IPv6 sources can be represented without hacks.

### 5.1 SearchEntry

Binary layout:

| Field | Size |
|---|---:|
| `ID` | 16 bytes |
| `TagCount` | 1 byte |
| `Tags` | variable |

Go shape:

```go
type SearchEntry struct {
    ID   ID
    Tags []Tag
}
```

### 5.2 Tag Encoding

Each tag has an explicit value type and tag identifier.

Binary layout:

| Field | Size | Notes |
|---|---:|---|
| `Type` | 1 byte | see section 5.3 |
| `ID` | 1 byte | semantic field id |
| `Value` | variable | depends on `Type` |

For fixed-width integer types, `Value` is stored directly.

For `string` and `bytes`, `Value` is encoded as:

| Field | Size |
|---|---:|
| `Length` | 2 bytes |
| `RawBytes` | `Length` |

This differs intentionally from the legacy Kad tag format. Version 1 prefers a simpler decoder over binary compatibility.

### 5.3 Tag Types

| Type Name | Code | Value |
|---|---:|---|
| `TagTypeString` | `0x02` | UTF-8 string |
| `TagTypeUint32` | `0x03` | 32-bit unsigned integer |
| `TagTypeUint16` | `0x08` | 16-bit unsigned integer |
| `TagTypeUint8` | `0x09` | 8-bit unsigned integer |
| `TagTypeUint64` | `0x0B` | 64-bit unsigned integer |
| `TagTypeBytes` | `0x0C` | raw bytes with length prefix |

### 5.4 Tag IDs

Reserved source-related tag ids:

| Tag ID | Name | Type | Meaning |
|---|---:|---|---|
| `0xEE` | `TagAddrFamily` | `uint8` | address family, value `6` for `kadv6` source |
| `0xF0` | `TagSourceIP6` | `bytes` | 16-byte IPv6 source address |
| `0xFC` | `TagSourceUDPPort` | `uint16` | source UDP port if advertised |
| `0xFD` | `TagSourcePort` | `uint16` | source TCP port |
| `0xFF` | `TagSourceType` | `uint8` | source type |
| `0xD3` | `TagFileSize` | `uint64` | file size |
| `0x01` | `TagName` | `string` | keyword or file name |

Source type values:

| Value | Meaning |
|---:|---|
| `1` | direct source |
| `4` | store/publish source |

Version 1 only uses family `6`.

### 5.5 Source Extraction Rule

A `SearchEntry` represents a direct IPv6 source when all of the following are present:

- `TagAddrFamily = 6`
- `TagSourceIP6` contains exactly 16 bytes
- `TagSourcePort` is non-zero

`TagSourceUDPPort` is optional.

Recommended helper:

```go
func (s SearchEntry) SourceAddr() (*net.TCPAddr, bool)
```

## 6. Routing and Lookup Rules

`KADV6` uses the standard Kad logical model:

- 128-bit node ids
- XOR distance
- k-buckets
- iterative lookup
- bootstrap, hello, find-node, publish, search

Recommended initial parameters:

| Parameter | Value |
|---|---:|
| `K` bucket size | `10` |
| search branch factor `alpha` | `5` |
| short RPC timeout | `2s` |
| RPC timeout | `12s` |
| bucket refresh interval | `15m` |
| bootstrap retry interval | `30s` |

These values intentionally match the current implementation shape where practical, so runtime behavior remains familiar.

## 7. Nodes File Format

`KADV6` does not reuse legacy `nodes.dat`.

Recommended file name:

- `nodes6.dat`

Binary layout:

| Field | Size | Notes |
|---|---:|---|
| `Magic` | 4 bytes | ASCII `KD6N` |
| `Version` | 4 bytes | current value `1` |
| `BootstrapEdition` | 4 bytes | `0` for ordinary nodes, `1` for router/bootstrap list |
| `ContactCount` | 4 bytes | number of contacts |
| `Contacts` | variable | repeated `EntryV6` |

Validation rules:

- `Magic` must be `KD6N`
- `Version` must be supported
- each contact must have a valid IPv6 endpoint
- duplicate endpoints may be ignored

This format is intentionally self-describing and easier to evolve than the legacy format.

## 8. Opcodes

Version 1 uses the following opcode map.

| Opcode | Name |
|---:|---|
| `0x01` | `BootstrapReq` |
| `0x09` | `BootstrapRes` |
| `0x11` | `HelloReq` |
| `0x19` | `HelloRes` |
| `0x21` | `FindNodeReq` |
| `0x29` | `FindNodeRes` |
| `0x33` | `SearchKeysReq` |
| `0x34` | `SearchSourcesReq` |
| `0x35` | `SearchNotesReq` |
| `0x3B` | `SearchRes` |
| `0x43` | `PublishKeysReq` |
| `0x44` | `PublishSourcesReq` |
| `0x45` | `PublishNotesReq` |
| `0x4B` | `PublishRes` |
| `0x60` | `Ping` |
| `0x61` | `Pong` |

Notes:

- opcode values are kept close to classic Kad for operator familiarity
- packet bodies are `KADV6`-specific and must not be decoded by the legacy Kad decoder

## 9. Packet Definitions

### 9.1 BootstrapReq

Payload:

- empty

Purpose:

- ask a known bootstrap node for initial contacts

### 9.2 BootstrapRes

Payload:

| Field | Size |
|---|---:|
| `ID` | 16 bytes |
| `TCPPort` | 2 bytes |
| `Version` | 1 byte |
| `Count` | 2 bytes |
| `Contacts` | variable, repeated `EntryV6` |

Rules:

- `Count` should be capped by implementation, recommended maximum `20`

### 9.3 HelloReq / HelloRes

Payload:

| Field | Size |
|---|---:|
| `ID` | 16 bytes |
| `TCPPort` | 2 bytes |
| `Version` | 1 byte |
| `TagCount` | 1 byte |
| `Tags` | variable |

Current version uses no mandatory tags. `TagCount` may be `0`.

Purpose:

- confirm node identity
- advertise TCP service port
- allow future extension without changing the fixed header

### 9.4 FindNodeReq

Payload:

| Field | Size |
|---|---:|
| `SearchType` | 1 byte |
| `Target` | 16 bytes |
| `Receiver` | 16 bytes |

Rules:

- `SearchType` for node lookup must be `0x0B`
- `Receiver` is the sender's current best-known peer id or zero-id if unknown

### 9.5 FindNodeRes

Payload:

| Field | Size |
|---|---:|
| `Target` | 16 bytes |
| `Count` | 2 bytes |
| `Results` | variable, repeated `EntryV6` |

### 9.6 SearchSourcesReq

Payload:

| Field | Size |
|---|---:|
| `Target` | 16 bytes |
| `StartPos` | 2 bytes |
| `Size` | 8 bytes |

Purpose:

- search source entries for a file hash and optional size hint

### 9.7 SearchKeysReq

Payload:

| Field | Size |
|---|---:|
| `Target` | 16 bytes |
| `StartPos` | 2 bytes |

### 9.8 SearchNotesReq

Payload:

| Field | Size |
|---|---:|
| `Target` | 16 bytes |
| `StartPos` | 2 bytes |

### 9.9 SearchRes

Payload:

| Field | Size |
|---|---:|
| `Source` | 16 bytes |
| `Target` | 16 bytes |
| `Count` | 2 bytes |
| `Results` | variable, repeated `SearchEntry` |

Rules:

- recommended maximum entries per packet: `50`
- recommended packet size limit before UDP send: about `8 KiB`

### 9.10 PublishSourcesReq

Payload:

| Field | Size |
|---|---:|
| `FileID` | 16 bytes |
| `Source` | variable, one `SearchEntry` |

Required source tags:

- `TagAddrFamily = 6`
- `TagSourceIP6`
- `TagSourcePort`
- `TagSourceType`

Optional source tags:

- `TagSourceUDPPort`
- `TagFileSize`

### 9.11 PublishKeysReq

Payload:

| Field | Size |
|---|---:|
| `KeywordID` | 16 bytes |
| `Count` | 2 bytes |
| `Sources` | variable, repeated `SearchEntry` |

### 9.12 PublishNotesReq

Payload:

| Field | Size |
|---|---:|
| `FileID` | 16 bytes |
| `Count` | 2 bytes |
| `Notes` | variable, repeated `SearchEntry` |

### 9.13 PublishRes

Payload:

| Field | Size |
|---|---:|
| `FileID` | 16 bytes |
| `Count` | 1 byte |

Purpose:

- acknowledge that one or more entries were stored

### 9.14 Ping

Payload:

- empty

### 9.15 Pong

Payload:

| Field | Size |
|---|---:|
| `UDPPort` | 2 bytes |

Purpose:

- basic liveness check
- confirm responder's active UDP port

## 10. Publish and Search Semantics

### 10.1 Source Publish

A node publishing itself as a source should:

1. use its file hash as `FileID`
2. publish a `SearchEntry` containing its IPv6 address and TCP port
3. include `TagFileSize` when known
4. store the entry locally before sending to closest nodes

Version 1 stores direct IPv6 sources only.

### 10.2 Keyword Publish

Keyword entries are application-defined metadata records keyed by a keyword hash.

Minimum expected metadata:

- `TagName`

Additional metadata may be added later via new tags.

### 10.3 Notes Publish

Notes are file-associated metadata records keyed by file hash.

Version 1 keeps notes as generic `SearchEntry` payloads.

### 10.4 Search Behavior

A node executing a search should:

1. bootstrap if no live nodes exist
2. start from the closest known verified nodes
3. send iterative parallel queries using `alpha`
4. deduplicate entries by semantic key
5. stop when traversal converges or timeout expires

Recommended dedup keys:

- sources: `[ipv6]:tcpPort`
- keyword entries: `entry.ID + ":" + TagName`
- notes: `entry.ID` or `entry.ID + source endpoint` if present

## 11. State and Persistence

Runtime state that should be persistable:

- self node id
- known nodes
- router nodes
- last bootstrap time
- last refresh time
- source index
- keyword index
- notes index

Recommended persisted node text form:

- `"[2001:db8::1]:4665"`

Do not persist IPv4 addresses in `KADV6` state.

## 12. Validation Rules

A `KADV6` implementation must reject:

- packets with header not equal to `0xE6`
- unsupported opcodes
- malformed length-prefixed tags
- `TagSourceIP6` values not exactly 16 bytes
- contacts with IPv4 or IPv4-mapped addresses
- `SearchEntry` source records where `TagAddrFamily != 6`
- packets exceeding implementation safety limits

Recommended safety limits:

| Item | Limit |
|---|---:|
| UDP packet size accepted | `8192` bytes |
| contacts per routing response | `20` |
| search entries per response | `50` |
| tags per search entry | `64` |

## 13. Compatibility Statement

`KADV6` version 1 is not wire-compatible with:

- classic eMule Kad
- aMule Kad
- the current `goed2k/protocol/kad` implementation

The following are intentionally different:

- protocol header
- endpoint size
- nodes file format
- tag encoding for variable-length bytes
- source address representation

This is by design. `KADV6` is a new overlay, not an extension patch on top of classic Kad.

## 14. Implementation Mapping for `goed2k`

Recommended package and file layout:

- `protocol/kadv6/types.go`
- `protocol/kadv6/packets.go`
- `protocol/kadv6/packet_combiner.go`
- `kadv6_tracker.go`
- `kadv6_node.go`
- `kadv6_routing.go`
- `kadv6_rpc.go`
- `kadv6_traversal.go`

Recommended tracker API:

```go
type KADV6Tracker struct{}

func NewKADV6Tracker(listenPort int, timeout time.Duration) *KADV6Tracker
func (t *KADV6Tracker) Start() error
func (t *KADV6Tracker) Close()
func (t *KADV6Tracker) AddNode(addr *net.UDPAddr)
func (t *KADV6Tracker) LoadNodesDat(path string) error
func (t *KADV6Tracker) SearchSources(hash protocol.Hash, size int64, cb func([]kadv6.SearchEntry)) bool
func (t *KADV6Tracker) SearchKeywords(hash protocol.Hash, cb func([]kadv6.SearchEntry)) bool
func (t *KADV6Tracker) PublishSource(hash protocol.Hash, endpoint *net.TCPAddr, size int64) bool
```

The source publish API should accept a native IPv6 address type, not the current IPv4-only `protocol.Endpoint`.

## 15. Open Questions

Items deliberately left for later versions:

1. whether to add authenticated node ids or signed publish records
2. whether to compress large search result packets
3. whether to define richer keyword and note tag schemas
4. whether to add capability flags to `Hello`
5. whether to support scoped local addresses for test-only deployments
6. whether future versions should define a `kad4 -> kadv6` search bridge

## 16. Summary

`KADV6` version 1 is an isolated IPv6-native Kad-style DHT with:

- UDP/IPv6 transport only
- 128-bit node ids and XOR routing
- 16-byte IPv6 endpoints
- dedicated `nodes6.dat`
- explicit IPv6-capable source tags
- no bridge and no compatibility burden with legacy Kad

That boundary keeps the first implementation small, testable, and aligned with the stated requirement: IPv6 nodes can discover and interact with other IPv6 nodes without involving `kad4`.
