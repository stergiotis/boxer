package play

// ClickHouseDocsSource is the Docs pane's default DocsSourceI: ClickHouse's
// own `system.documentation`, a table the server ships (ClickHouse 26.x) —
// one row per documented entity — functions, aggregate functions, data
// types, table engines, table functions, formats, settings, system tables
// and a dozen more kinds — carrying the reference prose as Markdown. It is
// the same content published on the website, answered by the server actually
// being queried, which is why it is worth a live query rather than a
// vendored copy: the docs then match the server's version by construction.
//
// The query runs through the ordinary lane machinery (newNodeLane over
// clientExecutor), so it inherits endpoint routing, auth, the pre-execute
// pass registry and the Arrow decode — and, being a lane, it runs off the
// render thread and memoises.

import (
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

var _ DocsSourceI = (*ClickHouseDocsSource)(nil)

const (
	// docsProbeTimeout bounds one lookup. The table is a few thousand rows
	// held in memory server-side, so a slow answer means the endpoint is in
	// trouble, not that the query is hard.
	docsProbeTimeout = 10 * time.Second
	// defaultDocsSiteBase is where the site-relative links in the corpus
	// point. ClickHouse writes them for its own documentation site
	// (`/sql-reference/…`), and left alone they render as hyperlinks that
	// resolve to nothing.
	defaultDocsSiteBase = "https://clickhouse.com/docs"
)

// defaultDocsQuery is the lookup ClickHouseDocsSource ships with. It matches
// case-insensitively and then prefers the exact spelling, because
// ClickHouse's own naming is inconsistent about it — `count` is lower-case,
// `INET6_ATON` upper — and a reader who typed one casing of a
// case-insensitive function should still get the page.
//
// The kind is not filtered here: ~70 names carry more than one (`Array` is a
// data type AND an aggregate-function combinator; `JSON` a data type AND a
// format), and which one the reader meant is a question for the pane, not the
// query. Ordering by the rendered type keeps the kind list stable and
// alphabetical across lookups.
//
// `toString(type)` is load-bearing, not decoration. `type` is an Enum8, and
// ClickHouse ships an Enum8 over Arrow as the raw int8 ordinal — the names do
// not cross the wire at all, so a client reading the column directly gets
// numbers and no way back to "Table Engine". Rendering it server-side is the
// only place the enum's dictionary exists.
//
// A re-user pointing ClickHouseDocsSource.Query at a different table must
// keep this four-column shape (aliased name/type/description/source) and the
// {n:String} parameter — see decodeDocRows.
const defaultDocsQuery = "SELECT name, toString(type) AS type, description, source " +
	"FROM system.documentation " +
	"WHERE lower(name) = lower({n:String}) " +
	"ORDER BY name = {n:String} DESC, type"

// ClickHouseDocsSource wraps a ClickHouse Client as a DocsSourceI. It is the
// source an unconfigured PlayApp with a live client already uses
// (NewLivePlayApp), and it is meant to be reused rather than reimplemented: a
// ClickHouse-backed re-user with its own documentation table points Query
// (and SiteBase, if that table's prose links elsewhere) at it and installs
// the result with PlayApp.SetDocsSource, instead of writing a new DocsSourceI.
// A re-user wanting BOTH ClickHouse's builtins and its own vocabulary in one
// pane can `UNION ALL` them into a single Query — this type never needs to
// know about more than one source.
type ClickHouseDocsSource struct {
	// Query is the parametrized lookup, run once per name. It must select
	// four columns aliased name, type, description, source, accept the
	// looked-up name as {n:String}, and order so the caller's preferred kind
	// sorts first — see defaultDocsQuery for the shape decodeDocRows expects.
	Query string
	// SiteBase absolutises the corpus's site-relative links (`](/…)`) and
	// anchors LinkClaimed/AbsoluteURL. Empty disables link absolutisation;
	// LinkClaimed then only matches relative link forms, never an absolute
	// site prefix.
	SiteBase string

	lane *nodeLane
}

// NewClickHouseDocsSource builds the default Docs source: ClickHouse's own
// system.documentation, queried against client exactly as NewLivePlayApp
// wires it — the ordinary lane machinery, off the render thread, memoised,
// through whatever endpoint client resolves to. Query and SiteBase carry
// today's defaults and may be overridden before installing (SetDocsSource).
func NewClickHouseDocsSource(client *Client) *ClickHouseDocsSource {
	return &ClickHouseDocsSource{
		Query:    defaultDocsQuery,
		SiteBase: defaultDocsSiteBase,
		lane: newNodeLane(clientExecutor{client: client, opts: newExecOptions("docs")},
			memory.NewGoAllocator(), docsProbeTimeout),
	}
}

// Close tears down the lookup lane. Idempotent, nil-safe.
func (inst *ClickHouseDocsSource) Close() {
	if inst != nil && inst.lane != nil {
		inst.lane.close()
	}
}

// Lookup runs Query for name through the lane. See DocsSourceI for the
// polling contract this implements against nodeLane.demand.
func (inst *ClickHouseDocsSource) Lookup(name string) (entries []DocsEntry, ready bool, err error) {
	node := compiledNode{SQL: inst.Query, Params: map[string]string{"n": name}}
	view := inst.lane.demand(node)
	defer func() {
		if view.rec != nil {
			view.rec.Release()
		}
	}()
	if view.key != node.key() {
		// Nothing served for THIS name yet — either the first demand or a
		// stale last-good from the previous one.
		return nil, false, nil
	}
	if view.err != nil {
		return nil, true, view.err
	}
	return decodeDocRows(view.rec), true, nil
}

// EmptyHint names this source so a reader knows what a blank pane is waiting
// on.
func (inst *ClickHouseDocsSource) EmptyHint() string {
	return "Put the caret on a function, data type, table engine, format or setting name — or type one above. Documentation comes from this server's own `system.documentation`."
}

// ExplainError explains a failed lookup, separating the one cause a reader
// can act on from the rest.
//
// `system.documentation` arrived in ClickHouse 26.x. Against an older server
// the query fails with an unknown-table error, and saying "this endpoint does
// not ship the documentation table" is a different message from "the lookup
// failed" — the first is a fact about the server, the second a fault.
//
// The match is gated on THIS instance's own Query mentioning
// system.documentation, not on the error text alone: a re-user who
// repurposed Query for a different table would otherwise have every
// unknown-table error on THEIR table misreported as "needs ClickHouse 26.x".
func (inst *ClickHouseDocsSource) ExplainError(err error) string {
	text := err.Error()
	if strings.Contains(inst.Query, "system.documentation") &&
		strings.Contains(text, "UNKNOWN_TABLE") {
		return "This endpoint has no `system.documentation` — it arrived in ClickHouse 26.x. Documentation lookup needs a newer server."
	}
	return "Documentation lookup failed: " + text
}

// LinkClaimed reports whether a link target is a ClickHouse documentation
// page, and so belongs in this pane rather than in a browser.
//
// It runs once per link per frame during layout, so it is a cheap syntactic
// test and never a lookup: whether the target names something this server
// documents is decided on the click, where a query is affordable. Claiming a
// page that turns out to be undocumented is recoverable — the pane says so and
// offers the original URL — whereas consulting the cache here would make a
// link's appearance depend on what happened to be cached, and links would
// change shape as the reader scrolled.
//
// Absolute URLs are claimed only for SiteBase; a link out to GitHub or an RFC
// is exactly the case that should still leave for a browser.
func (inst *ClickHouseDocsSource) LinkClaimed(url string) bool {
	switch {
	case url == "":
		return false
	case strings.HasPrefix(url, "#"):
		// A fragment alone addresses this same page. There is nothing to
		// navigate TO, and the widget cannot scroll to it from here.
		return false
	case inst.SiteBase != "" && strings.HasPrefix(url, inst.SiteBase):
		return true
	case strings.HasPrefix(url, "http://"), strings.HasPrefix(url, "https://"):
		return false
	case strings.HasPrefix(url, "/"), strings.HasPrefix(url, "../"), strings.HasPrefix(url, "./"):
		// The corpus's own relative and root-relative forms.
		return true
	}
	return false
}

// LinkCandidates ranks the names a claimed link might be naming, best first,
// for the pane to try in order.
//
// The LABEL leads, because it is what the author wrote to name the thing:
// "[`UInt8`](/sql-reference/data-types/int-uint)" points at a page covering
// a dozen types and only the label says which one. Measured over the corpus
// the label and the URL's last segment each resolve about 60% of links on
// their own, and they fail on different links — the page-per-family targets
// (`int-uint`, `special-data-types/expression`) are exactly where the label
// carries the answer.
//
// The fragment comes second: doc URLs point at a section of a page, and the
// section is usually the entity (`.../date-time-functions#tohour`).
func (inst *ClickHouseDocsSource) LinkCandidates(label string, url string) (out []string) {
	seen := make(map[string]struct{}, 3)
	add := func(n string) {
		n = strings.TrimSpace(strings.Trim(n, "`"))
		if n == "" || strings.ContainsAny(n, " \t/\\") {
			return
		}
		k := strings.ToLower(n)
		if _, dup := seen[k]; dup {
			return
		}
		seen[k] = struct{}{}
		out = append(out, n)
	}
	add(label)

	path, frag, _ := strings.Cut(url, "#")
	add(frag)
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		path = path[i+1:]
	}
	add(strings.TrimSuffix(path, ".md"))
	return
}

// AbsolutiseLinks rewrites the corpus's site-relative link targets onto
// SiteBase.
//
// ClickHouse authors these for its own site — `[DateTime](/sql-reference/…)` —
// and the widget has no base to resolve them against, so left alone they
// render as hyperlinks that go nowhere. This is a textual pre-pass rather than
// a resolver because the markdown widget's resolver seam covers wikilinks and
// embeds, not plain CommonMark links.
//
// Deliberately narrow: only `](/` is rewritten. A protocol-relative `](//host`
// is already absolute and must not gain a prefix, and an in-document anchor
// `](#section)` resolves within the page.
func (inst *ClickHouseDocsSource) AbsolutiseLinks(md string) string {
	if inst.SiteBase == "" || !strings.Contains(md, "](/") {
		return md
	}
	var b strings.Builder
	b.Grow(len(md) + 64)
	for {
		i := strings.Index(md, "](/")
		if i < 0 {
			b.WriteString(md)
			return b.String()
		}
		b.WriteString(md[:i+2])
		md = md[i+2:]
		if strings.HasPrefix(md, "//") {
			continue // protocol-relative: already absolute
		}
		b.WriteString(inst.SiteBase)
	}
}

// AbsoluteURL turns a corpus link target into something a browser can open.
// Relative forms are resolved against SiteBase rather than against the
// entity's own page, whose location the corpus does not record — good enough
// to land the reader on the right site, and honest about being a fallback for
// the case where nothing here could answer.
func (inst *ClickHouseDocsSource) AbsoluteURL(url string) string {
	switch {
	case strings.HasPrefix(url, "http://"), strings.HasPrefix(url, "https://"):
		return url
	case inst.SiteBase == "":
		return url
	case strings.HasPrefix(url, "/"):
		return inst.SiteBase + url
	}
	return inst.SiteBase + "/" + strings.TrimLeft(strings.TrimPrefix(url, "./"), "./")
}

// decodeDocRows lifts the four string columns out of the served record.
//
// `type` is an Enum8 server-side; the Arrow stream carries it as a dictionary
// or as a plain string depending on the server's encoding choice, so both are
// read through the same value accessor.
func decodeDocRows(rec arrow.RecordBatch) (out []DocsEntry) {
	if rec == nil || rec.NumRows() == 0 {
		return
	}
	col := func(name string) func(int) string {
		idx := rec.Schema().FieldIndices(name)
		if len(idx) == 0 {
			return func(int) string { return "" }
		}
		return stringAccessor(rec.Column(idx[0]))
	}
	name, kind, body, source := col("name"), col("type"), col("description"), col("source")
	out = make([]DocsEntry, 0, rec.NumRows())
	for i := 0; i < int(rec.NumRows()); i++ {
		out = append(out, DocsEntry{
			Name: name(i), Kind: kind(i), Body: body(i), Source: source(i),
		})
	}
	return
}

// stringAccessor reads an Arrow column as text, covering the encodings a
// String or Enum column arrives in. Anything else reads as empty rather than
// panicking: a server whose schema drifted should degrade to a blank field,
// not take the pane down.
func stringAccessor(a arrow.Array) func(int) string {
	switch v := a.(type) {
	case *array.String:
		return func(i int) string {
			if v.IsNull(i) {
				return ""
			}
			return v.Value(i)
		}
	case *array.LargeString:
		return func(i int) string {
			if v.IsNull(i) {
				return ""
			}
			return v.Value(i)
		}
	case *array.Binary:
		return func(i int) string {
			if v.IsNull(i) {
				return ""
			}
			return string(v.Value(i))
		}
	case *array.Dictionary:
		inner := stringAccessor(v.Dictionary())
		return func(i int) string {
			if v.IsNull(i) {
				return ""
			}
			return inner(v.GetValueIndex(i))
		}
	default:
		return func(int) string { return "" }
	}
}
