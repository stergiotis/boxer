package identgen

import "github.com/stergiotis/boxer/public/identity/identifier"

// KeysColumn is a columnar (struct-of-arrays) batch of natural keys: every key's
// bytes are concatenated in Data, and Ends[i] is the exclusive end offset of key
// i — so key 0 is Data[:Ends[0]] and key i is Data[Ends[i-1]:Ends[i]]. This is the
// Arrow/Leeway varbinary layout: one backing allocation, cache-friendly, and what
// a columnar ingest pipeline already holds. The zero value is an empty column.
type KeysColumn struct {
	Data []byte
	Ends []uint32
}

// Len returns the number of keys in the column.
func (inst KeysColumn) Len() (n int) {
	return len(inst.Ends)
}

// At returns key i as a sub-slice of Data (no copy). It panics for an out-of-range
// index, matching slice indexing.
func (inst KeysColumn) At(i int) (key []byte) {
	lo := uint32(0)
	if i > 0 {
		lo = inst.Ends[i-1]
	}
	return inst.Data[lo:inst.Ends[i]]
}

// AppendKey appends one key and returns the grown column. It is a convenience for
// callers that do not already hold columnar data; a columnar pipeline should
// populate Data/Ends directly.
func (inst KeysColumn) AppendKey(key []byte) (out KeysColumn) {
	inst.Data = append(inst.Data, key...)
	inst.Ends = append(inst.Ends, uint32(len(inst.Data)))
	return inst
}

// BatchInternalizerI is an internalizing generator that resolves a whole column
// of natural keys under its tag in a single storage transaction. The single-key
// seam is embedded, so a batch generator is also a plain identifier.IdGeneratorI.
type BatchInternalizerI interface {
	identifier.IdGeneratorI

	// AppendIds resolves every key in keys under this generator's tag and appends
	// the resulting ids to dst, returning the grown slice: in the returned ids,
	// element len(dst)+i is the id of keys.At(i). If fresh is non-nil it is grown
	// in lockstep and carries the newly-minted flags (pass nil to skip tracking).
	//
	// On any error dst and fresh are returned unmodified. Keys are validated up
	// front (an empty key assigns nothing anywhere). A batch whose distinct
	// fresh keys exceed the tag's remaining id space fails with
	// ErrIdSpaceExhausted: a backend that can count the space up front assigns
	// nothing, a store-backed one persists the mappings minted before the
	// overrun (consumed sequence values cannot be returned). Fresh mappings
	// commit in one or more storage transactions. Either way a persisted
	// prefix is harmless — interning is idempotent get-or-assign, so retried
	// keys resolve to the ids already assigned.
	AppendIds(dst []identifier.TaggedId, keys KeysColumn, fresh []bool) (ids []identifier.TaggedId, freshOut []bool, err error)
}
