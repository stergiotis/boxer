package cell

import (
	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// TableSinks is keelson('sinks'): the registry derived from the
// declaration (ADR-0312 §SD1).
const TableSinks = "sinks"

// RegisterIntrospect registers keelson('sinks') over decl.
func RegisterIntrospect(reg *introspect.Registry, decl func() Declaration) (err error) {
	return reg.Register(sinksProvider{decl: decl})
}

type sinksProvider struct{ decl func() Declaration }

func (sinksProvider) Name() string                         { return TableSinks }
func (sinksProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (sinksProvider) Schema() *arrow.Schema                { return sinksTable(nil).Schema() }

func (inst sinksProvider) Snapshot(proj introspect.Projection) (rec arrow.RecordBatch, err error) {
	var sinks []Sink
	if inst.decl != nil {
		sinks = inst.decl().Sinks()
	}
	rec = sinksTable(sinks).Build(proj, len(sinks))
	return
}

func sinksTable(rows []Sink) *introspect.Table {
	return introspect.NewTable().
		String("kind", func(i int) string { return rows[i].Kind.String() }).
		String("edge", func(i int) string { return rows[i].Edge }).
		String("clearance", func(i int) string { return rows[i].Clearance.String() }).
		Uint64("clearance_rank", func(i int) uint64 { return uint64(rows[i].Clearance) }).
		String("check", func(i int) string { return rows[i].Check })
}
