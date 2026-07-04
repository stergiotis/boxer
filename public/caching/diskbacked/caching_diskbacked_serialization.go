package diskbacked

import "github.com/fxamacker/cbor/v2"

// EncMode for Keys: Must be Canonical (RFC 7049) to ensure deterministic hashing
// of maps/structs used as keys.
var keyEncMode, _ = cbor.CanonicalEncOptions().EncMode()

// stashRecord is the on-disk value envelope: the payload plus the stale
// flag that must survive the L1→L2 round-trip (StashBackendI contract).
// Single-letter field names keep the CBOR overhead small.
//
// Note: this envelope replaced a bare-value encoding; entries written by
// the pre-envelope format fail to decode and read as misses, which the
// best-effort stash contract tolerates (use cleanStart to reset).
type stashRecord[V any] struct {
	Value V    `cbor:"v"`
	Stale bool `cbor:"s,omitempty"`
}
