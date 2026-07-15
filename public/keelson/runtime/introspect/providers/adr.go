package providers

import (
	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/gov/adrcorpus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

// The ADR corpus as three keelson tables — `adr` (one row per decision),
// `subtask` (one row per sub-item a decision declares for itself) and `coderef`
// (one row per §-pinned citation) — so the state of the repository's decisions
// is queryable from any host pointed at this process (ADR-0122 §SD4).
//
// The table names and schemas are the ones `boxer adr` already binds over its
// Arrow dump (public/app/commands/adr). That symmetry is the point: a query
// written against one runs verbatim against the other, and a schema-parity test
// keeps the two from drifting.
//
// # These tables are not like the others
//
// Every other keelson table answers "what does *this process* contain?" — its
// env, its apps, its build, the passes it registered. ADR-0094 founds the
// family on state that "exists only inside a running process". These answer
// what the *repository* contains, and they are the only providers that do
// filesystem I/O at query time, so their rows depend on where the process was
// started rather than on what the process is. ADR-0122 §SD4 records that
// tension and why it was accepted rather than resolved.
//
// Three mitigations make it honest rather than merely convenient. The tables
// are Live, so they never serve a stale answer for a corpus that is files on
// disk changing under a running read. They are **empty rather than erroring**
// off-repo — a shipped binary with no checkout around it has no corpus, which
// is a fact about the process and not a failure. And the corpus root is pinned
// by an explicit env var (BOXER_ADR_DIR), discovered by walking up from the
// working directory only when it is unset.
//
// # Cost
//
// A Snapshot re-reads the corpus: parsing the markdown is cheap, but the
// citation scan walks the source tree. Live means that happens per query, and a
// query joining two of these tables pays it twice. Caching it behind an mtime
// check is deferred (§SD5) — it buys latency at the cost of the one property
// that makes a filesystem-backed table defensible.

// adrProvider exposes each decision as keelson.adr: its frontmatter lifecycle
// (status, dates, supersession) alongside the code-evidence columns folded in
// from the citation scan.
//
// Read impl_evidence and code_refs as a lower bound. They report what a text
// scan for §-pinned references found; code that implements a decision without
// naming it is invisible here, so an absent citation is the absence of a
// finding, not evidence of absence.
type adrProvider struct{}

func (adrProvider) Name() string                         { return "adr" }
func (adrProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (adrProvider) Schema() *arrow.Schema                { return adrTable(nil).Schema() }

func (adrProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	rows, _, _ := adrcorpus.Load()
	return adrTable(rows).Build(proj, len(rows)), nil
}

// subtaskProvider exposes each declared sub-item as keelson.subtask: the
// decisions a decision decomposed itself into, with the author's ✓ (done) and
// the citations that name it by its §marker.
//
// done and code_refs > 0 are different claims and overlap. Only an author can
// declare a sub-item done; a citation is evidence that something is being
// built, not that it is finished. Bucketing them wants a first-match rule
// (done, then cited, then neither), not a subtraction.
type subtaskProvider struct{}

func (subtaskProvider) Name() string                         { return "subtask" }
func (subtaskProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (subtaskProvider) Schema() *arrow.Schema                { return subtaskTable(nil).Schema() }

func (subtaskProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	_, rows, _ := adrcorpus.Load()
	return subtaskTable(rows).Build(proj, len(rows)), nil
}

// coderefProvider exposes each citation as keelson.coderef — the drill-down
// behind the counts on `adr` and `subtask`: which file, which line, and the
// §qualifier that pins it to a sub-item.
type coderefProvider struct{}

func (coderefProvider) Name() string                         { return "coderef" }
func (coderefProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (coderefProvider) Schema() *arrow.Schema                { return coderefTable(nil).Schema() }

func (coderefProvider) Snapshot(proj introspect.Projection) (arrow.RecordBatch, error) {
	_, _, rows := adrcorpus.Load()
	return coderefTable(rows).Build(proj, len(rows)), nil
}

// adrTable mirrors WriteAdrArrow's schema (public/app/commands/adr/arrowemit.go)
// field for field and in order; adrSchemaParity pins them equal.
func adrTable(rows []adrcorpus.Adr) *introspect.Table {
	return introspect.NewTable().
		Int32("num", func(i int) int32 { return int32(rows[i].Num) }).
		String("slug", func(i int) string { return rows[i].Slug }).
		String("title", func(i int) string { return rows[i].Title }).
		String("status", func(i int) string { return rows[i].Status }).
		String("date", func(i int) string { return rows[i].Date }).
		String("reviewed_by", func(i int) string { return rows[i].ReviewedBy }).
		String("reviewed_date", func(i int) string { return rows[i].ReviewedDate }).
		String("superseded_by", func(i int) string { return rows[i].SupersededBy }).
		String("withdrawn_date", func(i int) string { return rows[i].WithdrawnDate }).
		Int64("body_bytes", func(i int) int64 { return int64(rows[i].BodyBytes) }).
		Bool("has_update", func(i int) bool { return rows[i].HasUpdate }).
		Int32("update_count", func(i int) int32 { return int32(rows[i].UpdateCount) }).
		String("last_date", func(i int) string { return rows[i].LastDate }).
		StringList("plan_markers", func(i int) []string { return rows[i].PlanMarkers }).
		Int32("plan_max_phase", func(i int) int32 { return int32(rows[i].PlanMaxPhase) }).
		Int32("code_refs", func(i int) int32 { return int32(rows[i].CodeRefs) }).
		Int32("code_files", func(i int) int32 { return int32(rows[i].CodeFiles) }).
		Int32("code_pkgs", func(i int) int32 { return int32(rows[i].CodePkgs) }).
		StringList("code_langs", func(i int) []string { return rows[i].CodeLangs }).
		StringList("code_qualifiers", func(i int) []string { return rows[i].CodeQualifiers }).
		String("impl_evidence", func(i int) string { return rows[i].ImplEvidence }).
		Int32("subtasks_total", func(i int) int32 { return int32(rows[i].SubtasksTotal) }).
		Int32("subtasks_done", func(i int) int32 { return int32(rows[i].SubtasksDone) }).
		Int32("subtasks_cited", func(i int) int32 { return int32(rows[i].SubtasksCited) }).
		String("path", func(i int) string { return rows[i].Path })
}

// subtaskTable mirrors WriteSubtaskArrow's schema.
func subtaskTable(rows []adrcorpus.Subtask) *introspect.Table {
	return introspect.NewTable().
		Int32("num", func(i int) int32 { return int32(rows[i].Num) }).
		String("marker", func(i int) string { return rows[i].Marker }).
		String("kind", func(i int) string { return rows[i].Kind }).
		Int32("ordinal", func(i int) int32 { return int32(rows[i].Ordinal) }).
		String("title", func(i int) string { return rows[i].Title }).
		Bool("done", func(i int) bool { return rows[i].Done }).
		String("shape", func(i int) string { return rows[i].Shape }).
		Int32("line", func(i int) int32 { return int32(rows[i].Line) }).
		Int32("code_refs", func(i int) int32 { return int32(rows[i].CodeRefs) })
}

// coderefTable mirrors WriteCoderefArrow's schema.
func coderefTable(rows []adrcorpus.CodeRef) *introspect.Table {
	return introspect.NewTable().
		Int32("num", func(i int) int32 { return int32(rows[i].Num) }).
		String("path", func(i int) string { return rows[i].Path }).
		Int32("line", func(i int) int32 { return int32(rows[i].Line) }).
		String("lang", func(i int) string { return rows[i].Lang }).
		String("pkg", func(i int) string { return rows[i].Pkg }).
		String("qualifier", func(i int) string { return rows[i].Qualifier }).
		String("snippet", func(i int) string { return rows[i].Snippet })
}
