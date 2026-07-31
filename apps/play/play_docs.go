package play

// The Docs pane's data half: turning the name under the caret into
// ClickHouse's own reference documentation.
//
// `system.documentation` is a table the server ships (ClickHouse 26.x): one
// row per documented entity — functions, aggregate functions, data types,
// table engines, table functions, formats, settings, system tables and a dozen
// more kinds — carrying the reference prose as Markdown. It is the same
// content published on the website, answered by the server actually being
// queried, which is why it is worth a live query rather than a vendored copy:
// the docs then match the server's version by construction.
//
// The query runs through the ordinary lane machinery (newNodeLane over
// clientExecutor), so it inherits endpoint routing, auth, the pre-execute pass
// registry and the Arrow decode — and, being a lane, it runs off the render
// thread and memoises. What this file adds on top is a debounce (the caret
// moves per keystroke; the server should not) and a small cache of parsed
// documents, because the lane holds exactly one result and a reader flipping
// between two names would otherwise re-query both every time.

import (
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/highlight"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

const (
	// docsQuiescence is how long the caret's entity must hold still before a
	// lookup ships. The caret moves per keystroke and per arrow key; the
	// server should see neither. Short enough that a deliberate pause on a
	// name feels immediate.
	docsQuiescence = 250 * time.Millisecond
	// docsProbeTimeout bounds one lookup. The table is a few thousand rows
	// held in memory server-side, so a slow answer means the endpoint is in
	// trouble, not that the query is hard.
	docsProbeTimeout = 10 * time.Second
	// docsCacheMax is how many looked-up names are kept. Documentation bodies
	// run from ~80 B to ~80 KB, so this is bounded by the largest plausible
	// working set rather than by memory pressure.
	docsCacheMax = 32
	// docsSiteBase is where the site-relative links in the corpus point.
	// ClickHouse writes them for its own documentation site (`/sql-reference/…`),
	// and left alone they render as hyperlinks that resolve to nothing.
	docsSiteBase = "https://clickhouse.com/docs"
)

// docsQuery is the lookup. It matches case-insensitively and then prefers the
// exact spelling, because ClickHouse's own naming is inconsistent about it —
// `count` is lower-case, `INET6_ATON` upper — and a reader who typed one
// casing of a case-insensitive function should still get the page.
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
const docsQuery = "SELECT name, toString(type) AS type, description, source " +
	"FROM system.documentation " +
	"WHERE lower(name) = lower({n:String}) " +
	"ORDER BY name = {n:String} DESC, type"

// docEntry is one `system.documentation` row, with its rendered form attached
// once something asks for it.
type docEntry struct {
	Name   string
	Kind   string // the `type` enum's text: "Function", "Data Type", …
	Body   string // Markdown, as the server stores it
	Source string // path in the ClickHouse tree, empty when unknown

	// doc is Body parsed, built on first render rather than on fetch: a name
	// with several kinds fetches every one of them, and `MergeTree`'s body
	// alone is ~80 KB of Markdown that a reader looking at `Dictionary` never
	// sees.
	doc *markdown.Doc
}

// rendered returns the parsed document, parsing on first use.
func (inst *docEntry) rendered() *markdown.Doc {
	if inst.doc == nil {
		inst.doc = markdown.Parse([]byte(absolutiseDocLinks(inst.Body)))
	}
	return inst.doc
}

// docsResult is what one name's lookup produced.
type docsResult struct {
	entries []docEntry
	// err is the lookup's failure, if any. A server too old to have the table
	// lands here — an honest "this endpoint cannot answer", not an empty page.
	err error
}

// docsDriver owns the lookup lane and the parsed-document cache.
//
// Render-thread-only. A nil driver (tests, a client-less session) makes every
// method a no-op and the pane says so, the same shape DiagnosticsDriver uses.
type docsDriver struct {
	lane *nodeLane

	// cache maps a lowercased name to its result; order is the LRU, most
	// recently used last.
	cache map[string]*docsResult
	order []string

	// pending is the name a lookup is in flight or armed for, "" when idle.
	pending string
	// armed/armedAt implement the debounce: armed is the name last seen under
	// the caret, armedAt when it first appeared there.
	armed   string
	armedAt time.Time

	// now is an injection point for tests; nil means time.Now.
	now func() time.Time
}

func newDocsDriver(client *Client) (inst *docsDriver) {
	inst = &docsDriver{cache: make(map[string]*docsResult, docsCacheMax)}
	if client != nil {
		inst.lane = newNodeLane(clientExecutor{client: client, opts: newExecOptions("docs")},
			memory.NewGoAllocator(), docsProbeTimeout)
	}
	return
}

// close tears down the lane (PlayApp.Close). Idempotent, nil-safe.
func (inst *docsDriver) close() {
	if inst != nil && inst.lane != nil {
		inst.lane.close()
	}
}

// cached answers from the cache alone, touching nothing else. It is the
// free half of the lookup: a caller walking several candidates uses it to
// skip the ones it already knows about without disturbing the debounce.
func (inst *docsDriver) cached(name string) (res *docsResult) {
	if inst == nil || name == "" {
		return
	}
	key := strings.ToLower(name)
	hit, ok := inst.cache[key]
	if !ok {
		return
	}
	inst.touch(key)
	return hit
}

// lookup returns what is known about `name`, driving the fetch as a side
// effect: arming the debounce, shipping the query once the name holds still,
// and draining a finished lane run into the cache.
//
// CALL IT AT MOST ONCE PER FRAME. The debounce is a single slot, so two calls
// naming different entities restart each other's timer and neither ever
// reaches quiescence — a caller with several candidates screens them with
// [docsDriver.cached] and lookups only the one it decides to pursue.
//
// A cached name answers immediately and arms nothing. loading is true while
// this name's own query is in flight — never while some other name's is, so a
// pane showing a cached page does not flicker into a spinner because the caret
// passed over something else.
func (inst *docsDriver) lookup(name string) (res *docsResult, loading bool) {
	if inst == nil || name == "" {
		return
	}
	if hit := inst.cached(name); hit != nil {
		return hit, false
	}
	if inst.lane == nil {
		return
	}
	key := strings.ToLower(name)
	if inst.now == nil {
		inst.now = time.Now
	}

	// Debounce: a name must hold still before it costs a round trip.
	if inst.armed != key {
		inst.armed = key
		inst.armedAt = inst.now()
		return nil, false
	}
	if inst.now().Sub(inst.armedAt) < docsQuiescence {
		return nil, false
	}

	node := compiledNode{SQL: docsQuery, Params: map[string]string{"n": name}}
	view := inst.lane.demand(node)
	defer func() {
		if view.rec != nil {
			view.rec.Release()
		}
	}()
	inst.pending = key
	if view.key != node.key() {
		// Nothing served for THIS name yet — either the first demand or a
		// stale last-good from the previous one.
		return nil, true
	}
	stored := &docsResult{err: view.err}
	if view.err == nil {
		stored.entries = decodeDocRows(view.rec)
	}
	inst.store(key, stored)
	inst.pending = ""
	return stored, false
}

// store puts a result in the cache, evicting the least recently used.
func (inst *docsDriver) store(key string, res *docsResult) {
	if _, exists := inst.cache[key]; !exists && len(inst.order) >= docsCacheMax {
		oldest := inst.order[0]
		inst.order = inst.order[1:]
		delete(inst.cache, oldest)
	}
	inst.cache[key] = res
	inst.touch(key)
}

// touch moves key to the most-recently-used end.
func (inst *docsDriver) touch(key string) {
	for i, k := range inst.order {
		if k == key {
			inst.order = append(inst.order[:i], inst.order[i+1:]...)
			break
		}
	}
	inst.order = append(inst.order, key)
}

// decodeDocRows lifts the four string columns out of the served record.
//
// `type` is an Enum8 server-side; the Arrow stream carries it as a dictionary
// or as a plain string depending on the server's encoding choice, so both are
// read through the same value accessor.
func decodeDocRows(rec arrow.RecordBatch) (out []docEntry) {
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
	out = make([]docEntry, 0, rec.NumRows())
	for i := 0; i < int(rec.NumRows()); i++ {
		out = append(out, docEntry{
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

// absolutiseDocLinks rewrites the corpus's site-relative link targets onto the
// public documentation site.
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
func absolutiseDocLinks(md string) string {
	if !strings.Contains(md, "](/") {
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
		b.WriteString(docsSiteBase)
	}
}

// docsCandidates is the ranked list of names to look up for one caret
// position: what the caret is on, then outwards through the calls enclosing
// it.
//
// The order encodes what a reader means by "what is this?". A caret on a name
// asks about that name. Failing that — a caret on a column, a comma, or a
// literal inside a call — the next most likely question is about the call it
// is an argument of, which is also what signature help would answer.
// Duplicates are dropped so `f(f(|))` does not ask twice.
func docsCandidates(e highlight.CaretEntity, ok bool) (out []string) {
	if !ok {
		return
	}
	seen := make(map[string]struct{}, 1+len(e.Enclosing))
	add := func(n string) {
		if n == "" {
			return
		}
		k := strings.ToLower(n)
		if _, dup := seen[k]; dup {
			return
		}
		seen[k] = struct{}{}
		out = append(out, n)
	}
	add(e.Name)
	for _, n := range e.Enclosing {
		add(n)
	}
	return
}

// docsLinkClaimed reports whether a link target is a ClickHouse documentation
// page, and so belongs in this pane rather than in a browser.
//
// NOT WIRED YET, and deliberately so. A widget emitted inside the markdown
// widget's inline paragraph flow never receives a click: driving the live pane
// shows its response flags stuck at Enabled|ClickedElsewhere, with Hovered
// never set even under an explicit hover, while an identical widget emitted at
// the segment level of the SAME document (the code-block action buttons) takes
// clicks normally. Ruled out along the way: the widget kind (Button and
// SelectableLabel behave the same), Frame(false), the atoms construction, the
// enclosing HorizontalWrapped versus Horizontal, and a wrapped sibling label
// overlapping it. That leaves egui hit-testing disagreeing with the rect
// accesskit reports for the inline flow — an imzero2-level question, below
// this pane.
//
// Wiring it before that is settled would trade working browser links for links
// that look live and do nothing, which is strictly worse. The routing itself
// (markdown.WithLinkRouter, docsLinkCandidates, followDocsLink) is built and
// table-tested, so adopting it is one call site once the seam works.
//
// It runs once per link per frame during layout, so it is a cheap syntactic
// test and never a lookup: whether the target names something this server
// documents is decided on the click, where a query is affordable. Claiming a
// page that turns out to be undocumented is recoverable — the pane says so and
// offers the original URL — whereas consulting the cache here would make a
// link's appearance depend on what happened to be cached, and links would
// change shape as the reader scrolled.
//
// Absolute URLs are claimed only for the documentation site; a link out to
// GitHub or an RFC is exactly the case that should still leave for a browser.
func docsLinkClaimed(url string) bool {
	switch {
	case url == "":
		return false
	case strings.HasPrefix(url, "#"):
		// A fragment alone addresses this same page. There is nothing to
		// navigate TO, and the widget cannot scroll to it from here.
		return false
	case strings.HasPrefix(url, docsSiteBase):
		return true
	case strings.HasPrefix(url, "http://"), strings.HasPrefix(url, "https://"):
		return false
	case strings.HasPrefix(url, "/"), strings.HasPrefix(url, "../"), strings.HasPrefix(url, "./"):
		// The corpus's own relative and root-relative forms.
		return true
	}
	return false
}

// docsLinkCandidates ranks the names a claimed link might be naming, best
// first, for the pane to try in order.
//
// The LABEL leads, because it is what the author wrote to name the thing:
// “[`UInt8`](/sql-reference/data-types/int-uint)“ points at a page covering
// a dozen types and only the label says which one. Measured over the corpus
// the label and the URL's last segment each resolve about 60% of links on
// their own, and they fail on different links — the page-per-family targets
// (`int-uint`, `special-data-types/expression`) are exactly where the label
// carries the answer.
//
// The fragment comes second: doc URLs point at a section of a page, and the
// section is usually the entity (`.../date-time-functions#tohour`).
func docsLinkCandidates(label string, url string) (out []string) {
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
