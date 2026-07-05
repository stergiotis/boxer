package diskbacked

import "github.com/fxamacker/cbor/v2"

// EncMode for Keys: Must be Canonical (RFC 7049) to ensure deterministic hashing
// of maps/structs used as keys.
var keyEncMode, _ = cbor.CanonicalEncOptions().EncMode()

// stashRecord is the on-disk value envelope: the payload plus the entry
// state that must survive the L1→L2 round-trip (StashBackendI contract) —
// the stale flag, the monotonic version, and the freshness stamp.
// Single-letter field names keep the CBOR overhead small.
//
// Compatibility: entries written before a field existed decode with its
// zero value, which degrades gracefully — Ver 0 orders as oldest (any
// fetch supersedes it) and Stamp 0 reads as ancient (immediately stale
// under a freshness TTL). Entries from the pre-envelope bare-value format
// fail to decode and read as misses (use cleanStart to reset).
type stashRecord[V any] struct {
	Value V     `cbor:"v"`
	Ver   int64 `cbor:"o,omitempty"`
	Stamp int64 `cbor:"t,omitempty"`
	Stale bool  `cbor:"s,omitempty"`
}
