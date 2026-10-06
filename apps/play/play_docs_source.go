package play

// The Docs pane's pluggable seam: what a name's documentation is and where it
// comes from, decoupled from ClickHouse. play ships ClickHouseDocsSource
// (play_docs_clickhouse.go) — the corpus an unconfigured PlayApp already
// answers from — but a library re-using PlayApp can install any DocsSourceI
// with SetDocsSource to point the pane at its own corpus instead. See
// doc/howto/play-pluggable-docs.md.

import (
	"context"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// DocsEntry is one documentation result for a name, source-agnostic: the pane
// renders whatever a DocsSourceI returns without knowing where it came from.
type DocsEntry struct {
	Name string
	// Kind labels this entry when a name carries more than one — the pane
	// offers a selector built from it. A source with no such ambiguity can
	// leave it empty; a single unlabelled entry renders with no selector.
	Kind string
	// Body is the entry's prose, as Markdown.
	Body string
	// Source is shown under the body as provenance (e.g. a file path);
	// empty when unknown.
	Source string
}

// DocsSourceI is how the Docs pane finds documentation for a name and makes
// sense of its corpus's own links. Install one with PlayApp.SetDocsSource;
// ClickHouseDocsSource wraps ClickHouse's own system.documentation and is
// what an unconfigured PlayApp with a live client already uses.
type DocsSourceI interface {
	// Lookup starts or continues resolving name. The Docs pane polls it at
	// most once per frame — docsDriver's single-slot debounce is the only
	// caller — and it must not block: ready is false while resolution is
	// still in flight (or has not started), and the caller retries next
	// frame. A name with no documentation is ready=true with entries empty,
	// not an error.
	Lookup(name string) (entries []DocsEntry, ready bool, err error)

	// LinkClaimed reports whether a link target names something this source
	// documents — kept in the pane, resolved via LinkCandidates — versus an
	// ordinary hyperlink left for a browser. Runs once per link per frame
	// during layout: a syntactic test, never a lookup.
	LinkClaimed(url string) bool
	// LinkCandidates ranks the names a claimed link might be naming, best
	// first, for the pane to try against Lookup in order.
	LinkCandidates(label, url string) []string
	// AbsolutiseLinks rewrites an entry's own site-relative link targets,
	// once, before its body is parsed as markdown, so they resolve outside
	// the pane. A source whose bodies carry no relative links can return md
	// unchanged.
	AbsolutiseLinks(md string) string
	// AbsoluteURL turns a link target into something a browser can open —
	// the escape hatch offered when a claimed link resolves to nothing.
	AbsoluteURL(url string) string

	// EmptyHint is the pane's idle-state explanation, shown before anything
	// is looked up or typed.
	EmptyHint() string
	// ExplainError turns a failed Lookup into pane copy, so a source can
	// separate a cause its reader can act on from the rest.
	ExplainError(err error) string

	// Close releases whatever Lookup holds open (e.g. a query lane). Called
	// once, from PlayApp.Close or when SetDocsSource replaces it; a source
	// with nothing to release can no-op.
	Close()
}

// DocsLookupNowI is the optional capability of a DocsSourceI to answer one
// name at once, off the render goroutine: play's lookup_docs operation
// (ADR-0270, update of 2026-10-05). Lookup cannot serve it — it polls, and
// its single slot and memo belong to the pane — so a source that wants to
// answer agents implements this beside it. obo is the calling agent's
// context, nil for the person; a source that reaches outside the process
// checks it as the window's other external reads do and refuses with the
// destination it lacks. A name with no documentation is entries empty, not
// an error. A source without it leaves lookup_docs refused in its window.
type DocsLookupNowI interface {
	LookupNow(ctx context.Context, name string, obo *app.OnBehalfOf) (entries []DocsEntry, err error)
}
