package play

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/imagedecode"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
	"github.com/stergiotis/boxer/public/thestack/utfsafe"
)

// play_detail_rich.go is ADR-0123: content-typed detail cells. A result column
// named `<label>@<mime>` renders its cell as that media type in the ad-hoc
// (non-leeway) Detail pane, instead of as the truncated one-line label every
// other column gets:
//
//	SELECT body AS `notes@text/markdown`, thumb AS `shot@image/png` FROM t
//
// Declared, never sniffed. A String column whose text happens to open with '#'
// is not thereby markdown, and a pane that guesses is a pane that is
// confidently wrong on somebody's data.

// richSep introduces a column's content-type declaration. It is ADR-0122
// §SD2's separator, reused with its reasoning intact: measured against
// clickhouse-local, '@' parses backtick-quoted and raises a syntax error
// unquoted, so a forgotten backtick fails at once — where '#' would silently
// open a line comment and leave a plausible column behind. ':' is unavailable
// for a different reason: ADR-0116's splitHandle claims any identifier with
// exactly one colon as a leeway `section:column` handle. A media type contains
// no colon, so a declared name rides through that resolver untouched.
const richSep = "@"

// The declared vocabulary (ADR-0123 §SD3). Closed: an open one has no way to
// be wrong out loud.
const (
	mimeMarkdown = "text/markdown"
	mimePlain    = "text/plain"
	mimeJSON     = "application/json"
	mimeSQL      = "application/sql"
	mimeGo       = "text/x-go"
	mimePNG      = "image/png"
	mimeJPEG     = "image/jpeg"
	mimeGIF      = "image/gif"
)

// richMaxTextBytes bounds a text cell. Past it the cell falls back to the
// ordinary truncated label with the reason attached: the renderers that would
// otherwise take over — a full markdown parse, a syntax highlight, an
// unwrapped label — all scale with the source, and a pane that locks up on a
// row is worse than one that declines it in writing.
const richMaxTextBytes = 1 << 20

// richImageMaxW / richImageMaxH bound an image's *rendered* box, not its
// decode (that is richMaxImagePixels). The box is clamped to the native size
// before use, because FitAspectMaxE scales up as readily as down and a 16×16
// favicon blown up to fill the pane is not a detail view.
const (
	richImageMaxW = 640
	richImageMaxH = 480
)

// richMaxImagePixels bounds the decode. imagedecode checks it against the
// header before allocating, which is the whole point: a 30000×30000 PNG is a
// ~40 KB file and a ~3.6 GB decode.
const richMaxImagePixels = imagedecode.DefaultMaxPixels

// richMarkdownFeatures is the obsidian feature set for a database cell.
//
// Wikilinks and embeds are dropped from the package default: there is no vault
// behind a cell, so NoopResolver would resolve them to `/page` URLs that go
// nowhere. Frontmatter is dropped because a cell is not a note — a leading
// `---` is content here, not metadata.
//
// Tables and math are absent for a different reason: the markdown widget's
// renderer drops both even though the parser recognises them (see its package
// doc). Enabling FeatureGFM still buys strikethrough, task lists and
// footnotes; a GFM *table* in a declared cell renders as nothing. That is an
// upstream gap, recorded in ADR-0123 §SD3 rather than worked around here.
const richMarkdownFeatures = obsidian.FeatureGFM |
	obsidian.FeatureCallout |
	obsidian.FeatureHighlight |
	obsidian.FeatureComment

// richKindE is the renderer a declared media type resolves to.
type richKindE uint8

const (
	richKindNone richKindE = iota
	richKindMarkdown
	richKindPlain
	richKindJSON
	richKindSQL
	richKindGo
	richKindImage
)

// richDecl is a parsed `<label>@<mime>` column name.
//
// A reason means the column declared *something* — the token after '@' carried
// a slash — that this pane cannot render. The cell then falls back to the
// ordinary truncated label with the reason shown beside it. That is the point
// of the slash gate (§SD2): a typo like `notes@text/markdwn` must not quietly
// render as plain text, but `user@example.com` must not be nagged about.
type richDecl struct {
	label  string
	mime   string
	kind   richKindE
	reason string
}

// parseRichColumn resolves a column name against the §SD2 contract.
// declared=false means the name is an ordinary column and nothing about the
// pane's existing behaviour changes.
//
// The gate is the slash, NOT "mime.ParseMediaType succeeds" — that function
// does not require one. ParseMediaType("success") returns ("success", nil):
// no error. Gating on a clean parse would make ADR-0122's own
// `dot_done@success` resolve to a media type named "success", fail the
// vocabulary lookup, and paint an "unknown content type" diagnostic into the
// Detail pane of every board query.
func parseRichColumn(name string) (d richDecl, declared bool) {
	label, token, found := strings.Cut(name, richSep)
	if !found || !strings.Contains(token, "/") {
		return d, false
	}
	if label == "" {
		// `@text/markdown` — a declaration with nothing to call it. Show the
		// raw name rather than an empty caption.
		label = name
	}
	d.label = label
	mt, _, err := mime.ParseMediaType(token)
	if err != nil {
		d.mime = token
		d.reason = fmt.Sprintf("not a media type: %s", err)
		return d, true
	}
	d.mime = mt
	d.kind = richKindFor(mt)
	if d.kind == richKindNone {
		d.reason = fmt.Sprintf("unknown content type %q — known: %s", mt, richKnownTypes())
	}
	return d, true
}

// richKindFor maps a canonical media type to its renderer. mt is already
// lower-cased and parameter-free — mime.ParseMediaType did both, which is why
// `TEXT/Markdown` and `text/markdown; charset=utf-8` need no handling here.
func richKindFor(mt string) richKindE {
	switch mt {
	case mimeMarkdown:
		return richKindMarkdown
	case mimePlain:
		return richKindPlain
	case mimeJSON:
		return richKindJSON
	case mimeSQL:
		return richKindSQL
	case mimeGo:
		return richKindGo
	case mimePNG, mimeJPEG, mimeGIF:
		return richKindImage
	}
	return richKindNone
}

// richKnownTypes lists the vocabulary for a reject message. Written out rather
// than derived from a map so the order is the table's, not a map's.
func richKnownTypes() string {
	return strings.Join([]string{
		mimeMarkdown, mimePlain, mimeJSON, mimeSQL, mimeGo,
		mimePNG, mimeJPEG, mimeGIF,
	}, ", ")
}

// cellRaw returns a cell's undecorated content as a string.
//
// It exists because formatCell must never touch a declared cell: formatCell
// hex-encodes Binary (play_format.go:62), so reading a one-megabyte PNG
// through it costs two megabytes of string — and the section loop calls it on
// every column merely to test the empty-skip.
//
// The string aliases the Arrow buffer for the string and binary types
// (String.Value returns a substring; Binary.ValueString is documented
// zero-copy), so it is valid for the record's lifetime and MUST NOT be
// retained past the frame. The cache stores what it derives, never this.
//
// Anything that is not a string or binary column falls back to formatCell, so
// a nonsense-but-harmless `SELECT 42 AS ` + "`x@text/markdown`" + ` renders
// the number rather than nothing.
func cellRaw(rec arrow.RecordBatch, col int, row int64) (raw string, ok bool) {
	arr := rec.Column(col)
	if row < 0 || int(row) >= arr.Len() || arr.IsNull(int(row)) {
		return "", false
	}
	i := int(row)
	switch a := arr.(type) {
	case *array.String:
		return a.Value(i), true
	case *array.LargeString:
		return a.Value(i), true
	case *array.Binary:
		return a.ValueString(i), true
	case *array.LargeBinary:
		return a.ValueString(i), true
	case *array.FixedSizeBinary:
		return string(a.Value(i)), true
	default:
		return formatArrayElem(arr, row), true
	}
}

// richEntry is one rendered-once artifact. Exactly one of doc / job / pixels
// is live, selected by the declaration's kind — unless reason is set, in which
// case none are and the cell falls back to a truncated label.
type richEntry struct {
	doc      *markdown.Doc
	job      typed.RetainedFffiHolderTyped[c.CodeViewJobS]
	hasJob   bool
	pixels   []uint32
	widthPx  uint32
	heightPx uint32
	text     string
	reason   string
}

// richCellCache memoises the artifacts for the columns of ONE row.
//
// Every renderer here needs this, and codeview's interning is not it:
// BuildRetained interns the *already-serialized* buffer
// (unique.Make(string(raw)), fffi2_typed_impl.go:170), so the highlighter and
// the buffer construction still run on every call. markdown.Parse builds a
// segment tree and exists to be hoisted; decoding a PNG per frame is not
// arguable.
//
// Keyed on (executed, row) — the Detail pane shows one row, so the working set
// is that row's columns and needs no LRU to stay bounded. `executed` is the
// same freshness token the pager, the World pane and KanbanDriver's fold key
// on; without it, re-running a query that returns different bytes at the same
// row index would show the old ones.
type richCellCache struct {
	ids     *c.WidgetIdStack
	tracker *c.ImageVersionTracker[string]

	forExecuted time.Time
	forRow      int64
	entries     map[int]*richEntry

	// pendingExecuted is stashed by renderDetailTab before dispatch — the
	// PanelI Render signature carries no result metadata (the World and Kanban
	// panes' noteExecuted handoff).
	pendingExecuted time.Time

	// generation bumps whenever the cache is dropped, and is the image
	// widget's contentVersion. The tracker keys by widget id, which is stable
	// per column across rows — so without a changing version, selecting a
	// second row would show the first row's texture.
	generation uint64
}

func newRichCellCache(ids *c.WidgetIdStack) *richCellCache {
	return &richCellCache{
		ids:     ids,
		tracker: c.NewImageVersionTracker[string](),
		forRow:  -1,
		entries: make(map[int]*richEntry, 4),
	}
}

// noteExecuted hands the cache the active result's freshness token before
// dispatch.
func (inst *richCellCache) noteExecuted(t time.Time) { inst.pendingExecuted = t }

// syncTo drops the cache when the row or the result changed.
//
// Nil-receiver-safe: renderDetailPane calls this before every Detail body,
// including the leeway card path that never reaches a declared cell, and the
// tests construct a bare &PlayApp{} for unrelated unit work. Matches the
// nil-guard RenderDefaultDetailContent already keeps on inst.cards.
func (inst *richCellCache) syncTo(row int64) {
	if inst == nil {
		return
	}
	if inst.forRow == row && inst.forExecuted.Equal(inst.pendingExecuted) {
		return
	}
	inst.forRow = row
	inst.forExecuted = inst.pendingExecuted
	clear(inst.entries)
	inst.generation++
}

// entryFor returns the artifact for one declared cell, building it on first
// use. raw is frame-lifetime (see cellRaw); everything retained here is
// derived from it, never it.
func (inst *richCellCache) entryFor(col int, d richDecl, raw string) *richEntry {
	if e, ok := inst.entries[col]; ok {
		return e
	}
	e := buildRichEntry(d, raw)
	inst.entries[col] = e
	return e
}

// buildRichEntry does the once-per-(result, row, column) work: parse, highlight
// or decode. A failure is not an error to log but a string to show — the cell
// falls back to the truncated label carrying the reason.
func buildRichEntry(d richDecl, raw string) *richEntry {
	e := &richEntry{}
	if d.reason != "" {
		e.reason = d.reason
		return e
	}
	if d.kind != richKindImage && len(raw) > richMaxTextBytes {
		e.reason = fmt.Sprintf("%d bytes is over the %d-byte inline limit", len(raw), richMaxTextBytes)
		return e
	}
	switch d.kind {
	case richKindMarkdown:
		e.doc = markdown.Parse([]byte(raw), markdown.WithFeatures(richMarkdownFeatures))
	case richKindPlain:
		// EnsureUTF8 for the same reason formatCell does it: a ClickHouse
		// String is byte-arbitrary, and shipping invalid UTF-8 through
		// c.Label breaks the FFFI wire mid-frame.
		e.text = utfsafe.EnsureUTF8(raw)
	case richKindJSON:
		e.job = codeview.BuildJson(richIndentJSON(raw))
		e.hasJob = true
	case richKindSQL:
		e.job = codeview.BuildSql(utfsafe.EnsureUTF8(raw))
		e.hasJob = true
	case richKindGo:
		e.job = codeview.BuildGo(utfsafe.EnsureUTF8(raw))
		e.hasJob = true
	case richKindImage:
		pixels, w, h, err := imagedecode.DecodeRGBA8([]byte(raw), richMaxImagePixels)
		if err != nil {
			e.reason = err.Error()
			return e
		}
		e.pixels, e.widthPx, e.heightPx = pixels, w, h
	}
	return e
}

// richIndentJSON pretty-prints a JSON cell, falling back to the source
// verbatim when it does not parse. A column declared application/json that
// holds something else is still worth highlighting as best it can — the
// highlighter degrades to plain spans on a parse error by design.
func richIndentJSON(raw string) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(raw), "", "  "); err != nil {
		return utfsafe.EnsureUTF8(raw)
	}
	return utfsafe.EnsureUTF8(buf.String())
}

// renderRichCell draws one declared cell: a caption line naming the column and
// its declared type, then the rendered body beneath it. Unlike an ordinary
// cell — a Horizontal of label and value — a document needs the full width, so
// the body sits under the caption rather than beside it.
func (inst *PlayApp) renderRichCell(col int, d richDecl, raw string) {
	e := inst.richCells.entryFor(col, d, raw)
	for range c.Vertical().KeepIter() {
		for range c.Horizontal().KeepIter() {
			for rt := range c.RichTextLabel(d.label) {
				rt.Weak()
			}
			for rt := range c.RichTextLabel(d.mime) {
				rt.Small().Weak()
			}
		}
		if e.reason != "" {
			// The declared render is unavailable: show the cell as it would
			// have looked without the declaration, and say why.
			c.Label(firstLineOf(raw)).Truncate().Send()
			for rt := range c.RichTextLabel(e.reason) {
				rt.Small().Weak()
			}
			return
		}
		inst.richCells.renderBody(col, d, e)
	}
}

// renderBody draws the artifact itself.
func (inst *richCellCache) renderBody(col int, d richDecl, e *richEntry) {
	switch d.kind {
	case richKindMarkdown:
		if e.doc == nil {
			return
		}
		// Doc.Render derives its embedded widgets' ids from PrepareSeq(0), 1,
		// … in document order and does NOT open its own scope, so two docs
		// under one parent would collide. Scope per column.
		for range c.IdScope(inst.ids.PrepareStr("play-detail-md-" + strconv.Itoa(col))) {
			e.doc.Render(inst.ids)
		}
	case richKindPlain:
		c.Label(e.text).Wrap().Send()
	case richKindJSON, richKindSQL, richKindGo:
		if !e.hasJob {
			return
		}
		c.CodeView(inst.ids.PrepareStr("play-detail-code-"+strconv.Itoa(col)), e.job).Send()
	case richKindImage:
		inst.renderImage(col, e)
	}
}

// renderImage draws a decoded cell image, bounded and version-tracked.
func (inst *richCellCache) renderImage(col int, e *richEntry) {
	if len(e.pixels) == 0 {
		return
	}
	key := "play-detail-img-" + strconv.Itoa(col)
	// Two separate PrepareStr creators: each is a single-use state machine, so
	// reusing one across Derive() and the Image call panics (the worldmap's
	// note at renderImage).
	imgId := inst.ids.PrepareStr(key).Derive()
	// PixelsToSendFor, not PixelsToSend: the Detail pane is a dock tab, whose
	// body renders every frame into a buffer the host only interprets when the
	// tab is active. A hidden tab's upload is discarded and the idle LRU can
	// evict the texture underneath it, so "sent" is not "received". The For
	// variant consults the host's starved-texture report and re-ships.
	pixels := inst.tracker.PixelsToSendFor(key, imgId, inst.generation, e.pixels)
	// Clamp the box to the native size: FitAspectMaxE scales up to fill the
	// box, and a favicon rendered 640 wide is not a detail view.
	boxW := min(e.widthPx, richImageMaxW)
	boxH := min(e.heightPx, richImageMaxH)
	c.Image(inst.ids.PrepareStr(key),
		e.widthPx, e.heightPx, inst.generation,
		uint8(c.FitAspectMaxE), boxW, boxH,
		uint8(c.FilterLinearE), c.TintNoneRgba, pixels).
		Send()
}

// firstLineOf is the fallback rendering for a declared cell that could not be
// rendered as declared: the first line, for a Truncate()d label. Cutting at
// the newline matters because Truncate() clips on width — a multi-megabyte
// blob would otherwise be shipped across the wire in full to draw forty
// visible characters.
func firstLineOf(raw string) string {
	if i := strings.IndexByte(raw, '\n'); i >= 0 {
		raw = raw[:i]
	}
	if len(raw) > 256 {
		raw = raw[:256]
	}
	return utfsafe.EnsureUTF8(raw)
}
