//go:build llm_generated_opus47

package task

import (
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/inflightsnapshotreply"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// InflightSnapshotReply is the wire payload an M3 supervisor publishes on
// a list-inflight reply inbox in response to a Request on
// SubjectListInflight. Entries reflects the supervisor's in-memory map
// at the moment AtMs was sampled; order is stable but unspecified.
//
// Defined on the task package (not the supervisor) so consumers can
// decode the reply without importing the supervisor and acquiring its
// factsstore dependency.
type InflightSnapshotReply struct {
	Entries []InflightSnapshotEntry `json:"entries"`
	AtMs    int64                   `json:"atMs"`
}

// InflightSnapshotEntry is one row in the snapshot. Optional fields
// (Total, Unit, EtaMs, Current) are zero until the supervisor has
// observed at least one TaskProgress for the task. State is a plain
// string ("running" | "cancelling" | "abandoned") so the wire stays
// stable when the supervisor's internal enum evolves.
type InflightSnapshotEntry struct {
	Id          TaskIdT    `json:"id"`
	Kind        string     `json:"kind"`
	Title       string     `json:"title,omitempty"`
	OwnerAppId  app.AppIdT `json:"ownerAppId,omitempty"`
	State       string     `json:"state"`
	CreatedAtMs int64      `json:"createdAtMs"`
	LastEmitMs  int64      `json:"lastEmitMs"`
	Current     uint64     `json:"current,omitempty"`
	Total       uint64     `json:"total,omitempty"`
	Unit        string     `json:"unit,omitempty"`
	EtaMs       int64      `json:"etaMs,omitempty"`
}

// MarshalInflightSnapshotReply serialises a reply via the canonical
// bus codec. The codec wire form
// ([inflightsnapshotreply.InflightSnapshotReply]) flattens Entries
// into parallel `[]T` columns — one column per entry-field. This
// helper does the fan-out so callers keep using the broker's native
// shape.
func MarshalInflightSnapshotReply(r InflightSnapshotReply) (b []byte, err error) {
	n := len(r.Entries)
	wire := inflightsnapshotreply.InflightSnapshotReply{
		At:           time.UnixMilli(r.AtMs).UTC(),
		Ids:          make([]string, n),
		Kinds:        make([]string, n),
		Titles:       make([]string, n),
		OwnerAppIds:  make([]string, n),
		States:       make([]string, n),
		CreatedAtMss: make([]int64, n),
		LastEmitMss:  make([]int64, n),
		Currents:     make([]uint64, n),
		Totals:       make([]uint64, n),
		Units:        make([]string, n),
		EtaMss:       make([]int64, n),
	}
	for i, e := range r.Entries {
		wire.Ids[i] = string(e.Id)
		wire.Kinds[i] = e.Kind
		wire.Titles[i] = e.Title
		wire.OwnerAppIds[i] = string(e.OwnerAppId)
		wire.States[i] = e.State
		wire.CreatedAtMss[i] = e.CreatedAtMs
		wire.LastEmitMss[i] = e.LastEmitMs
		wire.Currents[i] = e.Current
		wire.Totals[i] = e.Total
		wire.Units[i] = e.Unit
		wire.EtaMss[i] = e.EtaMs
	}
	b, err = buscodec.Encode(wire)
	if err != nil {
		err = eh.Errorf("task: marshal inflight snapshot reply: %w", err)
	}
	return
}

// UnmarshalInflightSnapshotReply is the inverse of
// MarshalInflightSnapshotReply. Reconstructs `[]InflightSnapshotEntry`
// from the parallel `[]T` columns. The parallel-array contract assumes
// each column carries exactly the same N entries in slice order;
// MarshalInflightSnapshotReply guarantees this on the writer side and
// the leeway codec preserves slice ordering through the wire.
func UnmarshalInflightSnapshotReply(b []byte) (r InflightSnapshotReply, err error) {
	var wire inflightsnapshotreply.InflightSnapshotReply
	wire, err = buscodec.Decode[inflightsnapshotreply.InflightSnapshotReply](b)
	if err != nil {
		err = eh.Errorf("task: unmarshal inflight snapshot reply: %w", err)
		return
	}
	r.AtMs = wire.At.UnixMilli()
	n := len(wire.Ids)
	r.Entries = make([]InflightSnapshotEntry, n)
	for i := 0; i < n; i++ {
		r.Entries[i] = InflightSnapshotEntry{
			Id:          TaskIdT(wire.Ids[i]),
			Kind:        sliceAt(wire.Kinds, i),
			Title:       sliceAt(wire.Titles, i),
			OwnerAppId:  app.AppIdT(sliceAt(wire.OwnerAppIds, i)),
			State:       sliceAt(wire.States, i),
			CreatedAtMs: sliceAtInt64(wire.CreatedAtMss, i),
			LastEmitMs:  sliceAtInt64(wire.LastEmitMss, i),
			Current:     sliceAtUint64(wire.Currents, i),
			Total:       sliceAtUint64(wire.Totals, i),
			Unit:        sliceAt(wire.Units, i),
			EtaMs:       sliceAtInt64(wire.EtaMss, i),
		}
	}
	return
}

// sliceAt is a defensive accessor: parallel-array decoders that
// receive an unexpectedly-short column return the Go zero value for
// missing positions rather than panicking. Should never fire when the
// wire was produced by MarshalInflightSnapshotReply (which pre-sizes
// every column to N), but matters for cross-implementation interop.
func sliceAt(s []string, i int) (v string) {
	if i < len(s) {
		v = s[i]
	}
	return
}

func sliceAtInt64(s []int64, i int) (v int64) {
	if i < len(s) {
		v = s[i]
	}
	return
}

func sliceAtUint64(s []uint64, i int) (v uint64) {
	if i < len(s) {
		v = s[i]
	}
	return
}
