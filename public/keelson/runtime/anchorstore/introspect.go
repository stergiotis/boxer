package anchorstore

import (
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// TableAnchors is keelson('anchors'): the chain heads the host's anchor
// store holds (ADR-0314 §SD2), what law L32 joins the journals' head
// entries on.
const TableAnchors = "anchors"

// AllI is a store that can list every head; the memory store is one. A
// store that cannot serves an empty table.
type AllI interface {
	All() (rows []Anchored)
}

// RegisterIntrospect registers keelson('anchors') over the store.
func RegisterIntrospect(reg *introspect.Registry, store StoreI) (err error) {
	all, _ := store.(AllI)
	return reg.Register(anchorsProvider{all: all})
}

type anchorsProvider struct{ all AllI }

func (anchorsProvider) Name() string                         { return TableAnchors }
func (anchorsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (anchorsProvider) Schema() *arrow.Schema                { return anchorsTable(nil).Schema() }

func (inst anchorsProvider) Snapshot(proj introspect.Projection) (rec arrow.RecordBatch, err error) {
	var rows []Anchored
	if inst.all != nil {
		rows = inst.all.All()
	}
	rec = anchorsTable(rows).Build(proj, len(rows))
	return
}

func anchorsTable(rows []Anchored) *introspect.Table {
	return introspect.NewTable().
		String("cell", func(i int) string { return rows[i].Cell }).
		String("run", func(i int) string { return rows[i].Run }).
		Uint64("ordinal", func(i int) uint64 { return uint64(rows[i].Ordinal) }).
		String("digest", func(i int) string { return rows[i].Digest }).
		String("receipt", func(i int) string { return rows[i].Receipt.Id }).
		String("received_at", func(i int) string { return rows[i].Receipt.At.UTC().Format(time.RFC3339Nano) }).
		String("by", func(i int) string { return rows[i].Receipt.By }).
		Bool("durable", func(i int) bool { return rows[i].Receipt.Durable })
}
