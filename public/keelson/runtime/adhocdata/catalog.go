package adhocdata

import (
	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// CatalogTableName is the keelson table that lists the live datasets
// (ADR-0134 SD6): the operator's window onto what ad-hoc data exists now.
const CatalogTableName = "adhoc"

// catalogRow is a snapshot of one dataset's catalog columns.
type catalogRow struct {
	handle          string
	alias           string
	publisher       string
	rows            uint64
	bytes           uint64
	revision        uint64
	createdAtUnixUs int64
}

// catalogRows returns a stable snapshot of the current datasets, sorted
// by handle for a deterministic table.
func (inst *Service) catalogRows() (rows []catalogRow) {
	inst.mu.RLock()
	rows = make([]catalogRow, 0, len(inst.datasets))
	for _, ds := range inst.datasets {
		rows = append(rows, catalogRow{
			handle: ds.handle, alias: ds.alias, publisher: ds.publisher,
			rows: ds.rows, bytes: ds.bytes, revision: ds.revision,
			createdAtUnixUs: ds.createdAtUnixUs,
		})
	}
	inst.mu.RUnlock()
	sortCatalogRows(rows)
	return
}

// sortCatalogRows orders rows by handle (an insertion sort — the set is
// small, bounded by MaxDatasets).
func sortCatalogRows(rows []catalogRow) {
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && rows[j-1].handle > rows[j].handle; j-- {
			rows[j-1], rows[j] = rows[j], rows[j-1]
		}
	}
}

// catalogProvider serves keelson('adhoc') over the service's live dataset
// table. It is an ordinary snapshot provider (not encrypted), so it
// queries and JOINs like any introspection table.
type catalogProvider struct {
	svc *Service
}

func newCatalogProvider(svc *Service) *catalogProvider { return &catalogProvider{svc: svc} }

func (c *catalogProvider) Name() string { return CatalogTableName }

// Freshness is Live: the catalog reflects the mutable dataset set and a
// publish/retract must not serve a cached snapshot.
func (c *catalogProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }

func (c *catalogProvider) Schema() *arrow.Schema { return catalogTable(nil).Schema() }

func (c *catalogProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	rows := c.svc.catalogRows()
	return catalogTable(rows).Build(proj, len(rows)), nil
}

// catalogTable declares the catalog columns over rows. Called with nil to
// read the schema without data.
func catalogTable(rows []catalogRow) *introspect.Table {
	return introspect.NewTable().
		String("handle", func(i int) string { return rows[i].handle }).
		String("alias", func(i int) string { return rows[i].alias }).
		String("publisher", func(i int) string { return rows[i].publisher }).
		Uint64("rows", func(i int) uint64 { return rows[i].rows }).
		Uint64("bytes", func(i int) uint64 { return rows[i].bytes }).
		Uint64("revision", func(i int) uint64 { return rows[i].revision }).
		Int64("created_at_unix_us", func(i int) int64 { return rows[i].createdAtUnixUs })
}
