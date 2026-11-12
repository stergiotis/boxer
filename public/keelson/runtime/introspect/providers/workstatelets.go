// workstatelets — the minted workstatelet documents (ADR-0369 (proposed)
// §SD8). keelson('apps') lists each one as a launcher entry of kind
// workstatelet; this table adds what the entry opens and the document's
// text, which is how a document captured from the desktop reaches a book.

package providers

import (
	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/workstatelet"
)

// RegisterWorkstatelets registers a workstatelets provider reading cat into
// r. A host that mints none passes nil and gets an empty table rather than
// an absent one, the keelson('workingsets') precedent.
func RegisterWorkstatelets(r *introspect.Registry, cat *workstatelet.Catalog) error {
	return r.Register(workstateletsProvider{cat: cat})
}

// workstateletsProvider exposes the catalog as keelson.workstatelets: one row
// per minted document, of either origin. Live: the catalog is read per
// query, so a document captured a moment ago is listed.
type workstateletsProvider struct {
	cat *workstatelet.Catalog
}

func (workstateletsProvider) Name() string                         { return "workstatelets" }
func (workstateletsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (workstateletsProvider) Schema() *arrow.Schema                { return workstateletsTable(nil).Schema() }

func (p workstateletsProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	var rows []workstatelet.Entry
	if p.cat != nil {
		rows = p.cat.Entries()
	}
	return workstateletsTable(rows).Build(proj, len(rows)), nil
}

// workstateletsTable declares the row shape. A window's config is not a
// column of its own: it is facts-CBOR only the owning app's codec decodes,
// and it is already in doc as base64. The table reports how many windows
// carry one and how many bytes they hold.
func workstateletsTable(rows []workstatelet.Entry) *introspect.Table {
	return introspect.NewTable().
		// app_id joins to keelson('apps').id, and to the caller column of
		// the launch rows the document's windows wrote (§SD6).
		String("app_id", func(i int) string { return string(rows[i].AppId) }).
		String("slug", func(i int) string { return rows[i].Doc.Slug }).
		// origin is book for a committed document and stored for a captured
		// one; book names the book and is empty for a stored one.
		String("origin", func(i int) string { return rows[i].Origin.String() }).
		String("book", func(i int) string { return rows[i].Book }).
		String("title", func(i int) string { return rows[i].Doc.Title }).
		String("summary", func(i int) string { return rows[i].Doc.Summary }).
		// layout is empty when the document leaves the desktop's mode.
		String("layout", func(i int) string { return rows[i].Doc.Desktop.Layout }).
		Int64("windows", func(i int) int64 { return int64(len(rows[i].Doc.Windows)) }).
		Int64("configured_windows", func(i int) int64 {
			n := 0
			for _, w := range rows[i].Doc.Windows {
				if len(w.Config) > 0 {
					n++
				}
			}
			return int64(n)
		}).
		Int64("config_bytes", func(i int) int64 {
			n := 0
			for _, w := range rows[i].Doc.Windows {
				n += len(w.Config)
			}
			return int64(n)
		}).
		// doc is the markdown document as parsed or as the capture wrote it.
		String("doc", func(i int) string { return string(rows[i].Doc.Source) })
}
