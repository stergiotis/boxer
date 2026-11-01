package servicehost

import (
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// TableName is the introspection table's name, read as keelson('services').
const TableName = "services"

// RegisterIntrospect registers keelson('services') over host (ADR-0352
// (proposed) §SD4): one row per headless app registered at boot, with its
// selection and state. A nil host answers with an empty table, so the name
// is always there.
func RegisterIntrospect(reg *introspect.Registry, host *Inst) (err error) {
	err = reg.Register(servicesProvider{host: host})
	return
}

type servicesProvider struct{ host *Inst }

func (servicesProvider) Name() string                         { return TableName }
func (servicesProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (servicesProvider) Schema() *arrow.Schema                { return servicesTable(nil).Schema() }

func (p servicesProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	var rows []Row
	if p.host != nil {
		rows = p.host.Rows()
	}
	return servicesTable(rows).Build(proj, len(rows)), nil
}

func rfc3339OrEmpty(t time.Time) (s string) {
	if !t.IsZero() {
		s = t.UTC().Format(time.RFC3339)
	}
	return
}

func servicesTable(rows []Row) *introspect.Table {
	return introspect.NewTable().
		String("id", func(i int) string { return string(rows[i].Id) }).
		// Whether KEELSON_SERVICES_WHERE, composed with the launch limit,
		// selected it at boot.
		Bool("selected", func(i int) bool { return rows[i].Selected }).
		String("state", func(i int) string { return rows[i].State.String() }).
		// The host-minted key its lifecycle rows and bus client carry; zero
		// when never mounted. Joins keelson('subscriptions').instance_key.
		Uint64("instance_key", func(i int) uint64 { return rows[i].InstanceKey }).
		// Lifecycle edges; empty until reached.
		String("started_at", func(i int) string { return rfc3339OrEmpty(rows[i].StartedAt) }).
		String("stopped_at", func(i int) string { return rfc3339OrEmpty(rows[i].StoppedAt) }).
		String("stop_reason", func(i int) string { return rows[i].StopReason }).
		String("last_error", func(i int) string { return rows[i].LastError })
}
