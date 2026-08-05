package providers

import (
	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/gov/capmapcorpus"
	"github.com/stergiotis/boxer/public/gov/capmapfacts"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// The business-capability corpus as three keelson tables — `capability` (one
// row per capability), `capsection` (one row per body section) and
// `caprelation` (one row per link between capabilities) — so a capability map
// is queryable from any host pointed at this process (ADR-0168 §SD8).
//
// # They read the vault, not boxer.facts
//
// `boxer capmap ingest` writes the same corpus into `boxer.facts`, and these
// tables deliberately do not read it back. Two reasons, both learned the hard
// way elsewhere in this tree. An applet whose table only exists once some
// other command has been run is the failure mode the pprof datasets hit — the
// table is simply absent, and explaining that to the reader needs machinery
// that watches for it to appear. And decoding memberships from SQL means
// spelling out leeway physical column names, a coupling with no compiler
// behind it: a hundred of them already sit in hand-written queries here, and
// nothing caught the last time an aspect change invalidated six.
//
// Reading the vault has neither problem, and costs about 150 ms for a
// ~1,700-capability tree, memoised for a short window by capmapcorpus.Load.
// The facts table keeps its own job: history, and joins on the ClickHouse side.
//
// # Like the ADR tables, unlike the rest
//
// These answer what the *repository* contains rather than what this process
// contains, which is the tension ADR-0122 §SD4 records for the ADR corpus and
// accepts rather than resolves. The same three mitigations apply: the tables
// are Live so a read never outlives an edit; they are **empty rather than
// erroring** off-repo, because a binary with no checkout around it has no
// corpus and that is a fact about the process; and the corpus root is pinned
// by BOXER_CAPMAP_VAULT, discovered by walking up only when unset.

// capabilityProvider exposes each capability as keelson.capability: the
// frontmatter metadata, without the prose.
//
// The body lives in `capsection` rather than here for the reason `adrcontent`
// is split from `adr` — it is the bulk of the corpus by bytes, and a query
// about maturity should not pay for it.
//
// fact_id is the id `boxer capmap ingest` writes for this capability, so a
// reader who wants the persisted trail can join this table to boxer.facts
// without knowing how the id is derived.
type capabilityProvider struct{}

func (capabilityProvider) Name() string                         { return "capability" }
func (capabilityProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (capabilityProvider) Schema() *arrow.Schema                { return capabilityTable(nil).Schema() }

func (capabilityProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	rows := capmapcorpus.Load().Capabilities
	return capabilityTable(rows).Build(proj, len(rows)), nil
}

// capsectionProvider exposes each body section as keelson.capsection: one row
// per heading, in document order.
//
// One row per section rather than parallel arrays on `capability` because a
// section is its own grain — "which capabilities have a Standards section", or
// the text of one heading across the corpus, are both filters here and array
// gymnastics there.
//
// The text column declares its media type in its name (ADR-0123 §SD2), so a
// pane that knows the convention renders the cell as markdown. That also means
// it can only be written quoted:
//
//	SELECT slug, `text@text/markdown` FROM keelson('capsection') WHERE heading = 'Standards'
type capsectionProvider struct{}

func (capsectionProvider) Name() string                         { return "capsection" }
func (capsectionProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (capsectionProvider) Schema() *arrow.Schema                { return capsectionTable(nil).Schema() }

func (capsectionProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	rows := flattenSections(capmapcorpus.Load().Capabilities)
	return capsectionTable(rows).Build(proj, len(rows)), nil
}

// caprelationProvider exposes each link as keelson.caprelation — the corpus's
// edge list, and its lint.
//
// `resolution` is the column to read before drawing conclusions from this
// table. Only `unresolved` is a defect: `external` is a citation whose target
// was never a capability, and on a real catalog those outnumber genuine broken
// links several times over. `dirref` resolves here and dangles in Obsidian.
type caprelationProvider struct{}

func (caprelationProvider) Name() string                         { return "caprelation" }
func (caprelationProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (caprelationProvider) Schema() *arrow.Schema                { return caprelationTable(nil).Schema() }

func (caprelationProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	rows := capmapcorpus.Load().Relations
	return caprelationTable(rows).Build(proj, len(rows)), nil
}

// capsectionRow is one flattened body section. It carries the owning slug so
// the table stands on its own, and an ordinal so document order survives a
// query that does not preserve row order.
type capsectionRow struct {
	Slug    string
	Ordinal int
	Heading string
	Text    string
}

func flattenSections(caps []capmapcorpus.Capability) (rows []capsectionRow) {
	n := 0
	for i := range caps {
		n += len(caps[i].Sections)
	}
	rows = make([]capsectionRow, 0, n)
	for i := range caps {
		for j, sec := range caps[i].Sections {
			rows = append(rows, capsectionRow{
				Slug: caps[i].Slug, Ordinal: j, Heading: sec.Heading, Text: sec.Text,
			})
		}
	}
	return rows
}

// isoOrEmpty renders a timestamp for a text column, leaving an unrecorded one
// empty rather than printing the zero instant as though it were a date.
func isoOrEmpty(t interface{ IsZero() bool }, format func() string) (s string) {
	if t.IsZero() {
		return ""
	}
	return format()
}

func capabilityTable(rows []capmapcorpus.Capability) *introspect.Table {
	return introspect.NewTable().
		String("slug", func(i int) string { return rows[i].Slug }).
		String("name", func(i int) string { return rows[i].Name }).
		String("abbrev", func(i int) string { return rows[i].Abbrev }).
		String("synopsis", func(i int) string { return rows[i].Synopsis }).
		String("domain", func(i int) string { return rows[i].Domain }).
		String("catalog", func(i int) string { return rows[i].Catalog }).
		String("owner", func(i int) string { return rows[i].Owner }).
		Int32("level", func(i int) int32 { return int32(rows[i].Level) }).
		Int32("maturity", func(i int) int32 { return int32(rows[i].Maturity) }).
		Int32("pain", func(i int) int32 { return int32(rows[i].Pain) }).
		Int32("section_count", func(i int) int32 { return int32(len(rows[i].Sections)) }).
		String("vault_path", func(i int) string { return rows[i].VaultPath }).
		Uint64("fact_id", func(i int) uint64 { return capmapfacts.DeriveId(rows[i].NaturalKey) }).
		StringList("lifecycle_phases", func(i int) []string {
			out := make([]string, 0, len(rows[i].Lifecycle))
			for _, ev := range rows[i].Lifecycle {
				out = append(out, string(ev.Phase))
			}
			return out
		}).
		StringList("lifecycle_by", func(i int) []string {
			out := make([]string, 0, len(rows[i].Lifecycle))
			for _, ev := range rows[i].Lifecycle {
				out = append(out, ev.By)
			}
			return out
		}).
		StringList("lifecycle_at", func(i int) []string {
			out := make([]string, 0, len(rows[i].Lifecycle))
			for _, ev := range rows[i].Lifecycle {
				ev := ev
				out = append(out, isoOrEmpty(ev.At, func() string { return ev.At.UTC().Format("2006-01-02 15:04:05") }))
			}
			return out
		})
}

func capsectionTable(rows []capsectionRow) *introspect.Table {
	return introspect.NewTable().
		String("slug", func(i int) string { return rows[i].Slug }).
		Int32("ordinal", func(i int) int32 { return int32(rows[i].Ordinal) }).
		String("heading", func(i int) string { return rows[i].Heading }).
		Int64("bytes", func(i int) int64 { return int64(len(rows[i].Text)) }).
		String("text@text/markdown", func(i int) string { return rows[i].Text })
}

func caprelationTable(rows []capmapcorpus.Relation) *introspect.Table {
	return introspect.NewTable().
		String("source_slug", func(i int) string { return rows[i].SourceSlug }).
		String("target", func(i int) string { return rows[i].Target }).
		String("kind", func(i int) string { return string(rows[i].Kind) }).
		String("resolution", func(i int) string { return rows[i].Resolution.String() }).
		String("section", func(i int) string { return rows[i].Section }).
		Float64("ncd", func(i int) float64 { return rows[i].Ncd }).
		Uint64("source_fact_id", func(i int) uint64 {
			return capmapfacts.DeriveId(capmapcorpus.NaturalKey(rows[i].SourceSlug))
		}).
		Uint64("target_fact_id", func(i int) uint64 {
			// Zero when the target is not a capability: there is no row to
			// point at, and a derived id would invite a join that silently
			// matches nothing.
			switch rows[i].Resolution {
			case capmapcorpus.ResolutionDirect, capmapcorpus.ResolutionDirRef:
				return capmapfacts.DeriveId(capmapcorpus.NaturalKey(rows[i].Target))
			default:
				return 0
			}
		})
}
