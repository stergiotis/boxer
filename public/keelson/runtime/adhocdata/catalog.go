package adhocdata

import (
	"slices"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// CatalogTableName is the keelson('…') name of the live dataset catalog:
// the operator's window onto what ad-hoc data exists now.
const CatalogTableName = "adhoc"

// catalogRow is a snapshot of one dataset's catalog columns.
type catalogRow struct {
	handle            string
	alias             string
	publisher         string
	publisherInstance uint64
	keepAfterClose    bool
	rows              uint64
	bytes             uint64
	revision          uint64
	createdAtUnixUs   int64
	openReaders       int64
}

// catalogRows returns a stable snapshot of the live datasets, sorted by
// handle for a deterministic table.
func (inst *Service) catalogRows() (rows []catalogRow) {
	inst.mu.RLock()
	rows = make([]catalogRow, 0, len(inst.live))
	for _, r := range inst.live {
		r.mu.RLock()
		rows = append(rows, catalogRow{
			handle: r.handle, alias: r.alias,
			publisher: string(r.owner.App), publisherInstance: r.owner.Instance, keepAfterClose: r.keepAfterClose,
			rows: r.rows, bytes: r.bytes, revision: r.revision, createdAtUnixUs: r.createdAt,
			openReaders: int64(r.file.Readers()),
		})
		r.mu.RUnlock()
	}
	inst.mu.RUnlock()
	slices.SortFunc(rows, func(a, b catalogRow) int { return strings.Compare(a.handle, b.handle) })
	return
}

// catalogProvider serves keelson('adhoc') over the service's live dataset
// table. It is an ordinary snapshot provider (not sealed), so it queries
// and JOINs like any introspection table.
type catalogProvider struct {
	svc *Service
}

func newCatalogProvider(svc *Service) *catalogProvider { return &catalogProvider{svc: svc} }

func (c *catalogProvider) Name() string { return CatalogTableName }

// Freshness is Live: the catalog changes with every publish and retract.
func (c *catalogProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }

func (c *catalogProvider) Schema() *arrow.Schema { return catalogTable(nil).Schema() }

func (c *catalogProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	rows := c.svc.catalogRows()
	return catalogTable(rows).Build(proj, len(rows)), nil
}

// catalogTable declares the catalog's columns over a row snapshot.
func catalogTable(rows []catalogRow) *introspect.Table {
	return introspect.NewTable().
		String("handle", func(i int) string { return rows[i].handle }).
		String("alias", func(i int) string { return rows[i].alias }).
		String("publisher", func(i int) string { return rows[i].publisher }).
		Uint64("publisher_instance", func(i int) uint64 { return rows[i].publisherInstance }).
		Bool("keep_after_close", func(i int) bool { return rows[i].keepAfterClose }).
		Uint64("rows", func(i int) uint64 { return rows[i].rows }).
		Uint64("bytes", func(i int) uint64 { return rows[i].bytes }).
		Uint64("revision", func(i int) uint64 { return rows[i].revision }).
		Int64("created_at_unix_us", func(i int) int64 { return rows[i].createdAtUnixUs }).
		Int64("open_readers", func(i int) int64 { return rows[i].openReaders })
}
