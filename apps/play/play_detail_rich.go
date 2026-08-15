package play

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/dustin/go-humanize"
	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/imagedecode"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
	"github.com/stergiotis/boxer/public/thestack/utfsafe"
)

// play_detail_rich.go is the block-face side of ADR-0186 (née ADR-0123's
// content-typed detail cells). A result column named `<label>@<media type>`
// resolves against the gloss catalog (public/hmi/gloss — the parser, the
// gate and every inline face live there); the content family's block faces —
// the markdown widget, the code view, the decoded image — are bound here, in
// the ad-hoc Detail pane:
//
//	SELECT body AS `notes@text/markdown`, thumb AS `shot@image/png` FROM t
//
// Declared, never sniffed. A String column whose text happens to open with '#'
// is not thereby markdown, and a pane that guesses is a pane that is
// confidently wrong on somebody's data.

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
// FeatureGFM is on and buys what it says: tables, strikethrough and task
// lists all render in a declared cell. Footnotes are the one GFM construct
// still missing, and they are missing everywhere: the footnote extension is
// wired to no feature flag at all, so `[^1]` stays literal prose.
//
// Math is absent: obsidian.FeatureMath is declared, reserved and consulted by
// nothing, so setting it would change neither the parse nor the render.
const richMarkdownFeatures = obsidian.FeatureGFM |
	obsidian.FeatureCallout |
	obsidian.FeatureHighlight |
	obsidian.FeatureComment

// glossCatalog is the catalog every declaration and rule resolves against.
// Lazily the default set, so a bare &PlayApp{} in a unit test resolves like
// the wired app does; a host that wants more registers before first use.
func (inst *PlayApp) glossCatalog() *gloss.Catalog {
	if inst.glosses == nil {
		inst.glosses = gloss.Default()
	}
	return inst.glosses
}

// mediaTypeOnly strips a compact token's parameters: the block-face binding
// keys on the type alone.
func mediaTypeOnly(token string) string {
	if i := strings.IndexByte(token, ';'); i >= 0 {
		return token[:i]
	}
	return token
}

// hasBlockFace reports whether this pane binds a block face to the media
// type. Everything else — the `gloss/*` family — shows its inline face,
// wrapped, under the caption.
func hasBlockFace(mediaType string) bool {
	switch mediaType {
	case gloss.MediaTypeMarkdown, gloss.MediaTypePlain, gloss.MediaTypeJSON,
		gloss.MediaTypeSQL, gloss.MediaTypeGo,
		gloss.MediaTypePNG, gloss.MediaTypeJPEG, gloss.MediaTypeGIF:
		return true
	}
	return false
}

// cellRaw returns a cell's undecorated content as a string: the bytes of a
// string or binary value, else the plain rendering, so a nonsense-but-
// harmless `SELECT 42 AS x@text/plain` renders the number rather than
// nothing.
//
// It exists because formatCell must never touch a declared cell: formatCell
// hex-encodes Binary, so reading a one-megabyte PNG through it costs two
// megabytes of string — and the section loop calls it on every column merely
// to test the empty-skip.
//
// The string aliases the Arrow buffer for the string and binary types (see
// gloss.ArrowCell.Raw), so it is valid for the record's lifetime and MUST NOT
// be retained past the frame. The cache stores what it derives, never this.
func cellRaw(rec arrow.RecordBatch, col int, row int64) (raw string, ok bool) {
	cell := gloss.ArrowCell{Arr: rec.Column(col), Row: int(row)}
	if cell.IsNull() {
		return "", false
	}
	if raw, ok = cell.Raw(); ok {
		return raw, true
	}
	return cell.Text(), true
}

// richEntry is one rendered-once artifact. Exactly one of doc / job / pixels
// is live, selected by the declaration's media type — unless reason is set,
// in which case none are and the cell falls back to a truncated label.
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
// Every block face here needs this, and the interning in codeview is not it:
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
func (inst *richCellCache) entryFor(col int, d gloss.Declaration, raw string) *richEntry {
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
func buildRichEntry(d gloss.Declaration, raw string) *richEntry {
	e := &richEntry{}
	if d.Reason != "" {
		e.reason = d.Reason
		return e
	}
	mt := mediaTypeOnly(d.MediaType)
	isImage := mt == gloss.MediaTypePNG || mt == gloss.MediaTypeJPEG || mt == gloss.MediaTypeGIF
	if !isImage && len(raw) > richMaxTextBytes {
		e.reason = fmt.Sprintf("%s is over the %s inline limit",
			humanize.IBytes(uint64(len(raw))), humanize.IBytes(richMaxTextBytes))
		return e
	}
	switch mt {
	case gloss.MediaTypeMarkdown:
		e.doc = markdown.Parse([]byte(raw), markdown.WithFeatures(richMarkdownFeatures))
	case gloss.MediaTypePlain:
		// EnsureUTF8 for the same reason formatCell does it: a ClickHouse
		// String is byte-arbitrary, and shipping invalid UTF-8 through
		// c.Label breaks the FFFI wire mid-frame.
		e.text = utfsafe.EnsureUTF8(raw)
	case gloss.MediaTypeJSON:
		e.job = codeview.BuildJson(richIndentJSON(raw))
		e.hasJob = true
	case gloss.MediaTypeSQL:
		e.job = codeview.BuildSql(utfsafe.EnsureUTF8(raw))
		e.hasJob = true
	case gloss.MediaTypeGo:
		e.job = codeview.BuildGo(utfsafe.EnsureUTF8(raw))
		e.hasJob = true
	case gloss.MediaTypePNG, gloss.MediaTypeJPEG, gloss.MediaTypeGIF:
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
//
// Three bodies: the reason (declared, cannot be honoured — the plain first
// line plus why), a block face when this pane binds one to the media type,
// else the gloss's inline face, wrapped.
func (inst *PlayApp) renderRichCell(col int, d gloss.Declaration, cell gloss.ArrowCell) {
	raw, ok := cell.Raw()
	if !ok {
		raw = cell.Text()
	}
	reason := d.Reason
	if reason == "" {
		if ok, why := d.Instance.Accepts(cell.Kind()); !ok {
			reason = why
		}
	}
	for range c.Vertical().KeepIter() {
		for range c.Horizontal().KeepIter() {
			for rt := range c.RichTextLabel(d.Label) {
				rt.Weak()
			}
			for rt := range c.RichTextLabel(d.MediaType) {
				rt.Small().Weak()
			}
		}
		if reason != "" {
			// The declared render is unavailable: show the cell as it would
			// have looked without the declaration, and say why.
			c.Label(gloss.FirstLine(raw)).Truncate().Send()
			for rt := range c.RichTextLabel(reason) {
				rt.Small().Weak()
			}
			return
		}
		if !hasBlockFace(mediaTypeOnly(d.MediaType)) {
			face := d.Instance.Inline(cell)
			if col, toned := toneColor(face.Tone); toned {
				for rt := range c.RichTextLabelColored(col, color.Transparent, face.Text) {
					rt.Monospace()
				}
			} else {
				c.Label(face.Text).Wrap().Send()
			}
			return
		}
		e := inst.richCells.entryFor(col, d, raw)
		if e.reason != "" {
			c.Label(gloss.FirstLine(raw)).Truncate().Send()
			for rt := range c.RichTextLabel(e.reason) {
				rt.Small().Weak()
			}
			return
		}
		inst.richCells.renderBody(col, d, e)
	}
}

// renderBody draws the artifact itself.
func (inst *richCellCache) renderBody(col int, d gloss.Declaration, e *richEntry) {
	switch mediaTypeOnly(d.MediaType) {
	case gloss.MediaTypeMarkdown:
		if e.doc == nil {
			return
		}
		// Doc.Render derives its embedded widgets' ids from PrepareSeq(0), 1,
		// … in document order and does NOT open its own scope, so two docs
		// under one parent would collide. Scope per column.
		for range c.IdScope(inst.ids.PrepareStr("play-detail-md-" + strconv.Itoa(col))) {
			e.doc.Render(inst.ids)
		}
	case gloss.MediaTypePlain:
		c.Label(e.text).Wrap().Send()
	case gloss.MediaTypeJSON, gloss.MediaTypeSQL, gloss.MediaTypeGo:
		if !e.hasJob {
			return
		}
		c.CodeView(inst.ids.PrepareStr("play-detail-code-"+strconv.Itoa(col)), e.job).Send()
	case gloss.MediaTypePNG, gloss.MediaTypeJPEG, gloss.MediaTypeGIF:
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
	// note at paintMap).
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

// firstLineOf is the fallback rendering for text that could not be rendered
// as declared, and the one-line form other panes give an error message:
// gloss.FirstLine, kept under its play name for the callers here.
func firstLineOf(raw string) string {
	return gloss.FirstLine(raw)
}
