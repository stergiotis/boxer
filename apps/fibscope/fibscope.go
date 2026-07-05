// Package fibscope is a keelson app for learning the fibonacci-coded tagged-id
// scheme by manipulating it. A tagged id is one 64-bit word: the tag — the
// fibonacci code of (tag value − 1) — sits MSB-aligned in the high bits, the
// per-category body fills the rest. Drag the tag value, the body, or the raw
// id and the app paints the bit layout live, decodes every part, and shows the
// ClickHouse filter the tag folds into.
//
// The app is a pure front-end over the identity packages — it mints nothing and
// touches neither the bus nor the store. Everything on screen is a synchronous
// bit-op over a single uint64 recomputed each frame, so there is no worker, no
// mutex, and no reactive coalescing (unlike the terrainscope app it is modelled
// on). Two dock tabs: "Explore" (build an id and read it back) and "Trade-offs"
// (pick tag values by the id space each category needs).
//
// The scheme, its split contract, and the kill-reasons for the alternatives are
// ADR-0106; the end-to-end recipe (mint, split, query) is
// doc/howto/fibonacci-tagged-ids.md. This app re-argues neither.
//
// Lifecycle: Mount captures the logger; Frame renders inside the host-owned
// window; Unmount has nothing to release.
package fibscope

import (
	"fmt"
	"math"
	"strings"
	"sync/atomic"

	"github.com/dustin/go-humanize"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/identity/fibonacci"
	"github.com/stergiotis/boxer/public/identity/fibonaccicode"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/identity/identsql"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

const (
	stageW = float32(920) // width of the plot/table stages
	margin = float32(12)  // vertical breathing room around plots

	// defaultId is the howto's worked example: tag value 12 (code 101011,
	// width 6), body 1. A pleasant, non-trivial starting point.
	defaultId = uint64(12393906174523604993)

	// invalidId is a raw word with no adjacent 11 pair — carries no comma, so
	// it is not a tagged id. Wired to the "invalid" preset as a teaching case.
	invalidId = uint64(42)

	// Dock tab ids — stable across frames (they name entries in egui_dock's
	// persistent layout state).
	dockTabExplore  = uint64(1)
	dockTabTradeoff = uint64(2)

	// advisorMaxExpCeil is one below the advisor's hard ceiling: at 2^60 ids
	// the fitting tag would be narrower than the 2-bit minimum, so
	// SelectFittingTagValueRange rejects it. We clamp the slider below it so
	// the readout stays populated across the whole range.
	advisorMaxExpCeil = float64(uint64(1) << 60)

	sep = "  ·  " // separates inline metric chips in a row
)

// Region colours for the 64-bit strip. Distinct in both hue and lightness so
// the three regions read apart at a glance and the comma pops.
var (
	colTagCode = color.Hex(0x5b9dffff) // fibonacci code bits (blue)
	colComma   = color.Hex(0xffb454ff) // the trailing 11 comma (amber)
	colBody    = color.Hex(0x5fd39bff) // body bits (green)
	colInvalid = color.Hex(0x9aa0a6ff) // a word with no comma (neutral grey)
)

// Hover-help — each concept is explained at the widget that shows it, via
// c.HoverText, rather than in a wall of prose.
const (
	tipIntro     = "A tagged id packs a category (the tag) and a per-category counter (the body) into one 64-bit word. Drag the values below and watch the bits."
	tipTagValue  = "The category. Stored as the fibonacci code of (tag value − 1) in the high bits. Small values get short codes — give hot categories small tags."
	tipBody      = "The per-category counter, in the low bits. Body 0 is reserved, so counting starts at 1. Its ceiling shrinks as the tag gets wider."
	tipRaw       = "The whole id as one UInt64 — type a decimal or 0x-hex value to inspect how it splits, including words that are not valid tagged ids. Exact across the full 64-bit range."
	tipTagValRO  = "Decoded back from the bits. 0 means the word carries no comma and is not a tagged id."
	tipWidthRO   = "Full tag width in bits, including the trailing 11 comma. Frequency-adaptive: value 1 costs 2 bits and leaves 62 for the body; the largest uint32 tags cost 47."
	tipBodyRO    = "The body value, and the largest body this tag can hold: 2^(64−width) − 1."
	tipCode      = "The tag's fibonacci code: a Zeckendorf representation (no two adjacent 1s) closed by the 11 comma. Self-delimiting — the first 11 from the MSB is always the comma."
	tipZeck      = "Every positive integer is a unique sum of non-consecutive Fibonacci numbers (Zeckendorf's theorem). The encoder's two biases cancel, so this sum — the tag's code bits — is the tag value itself."
	tipRange     = "Ids of one tag form a contiguous UInt64 range, so a tag filter is a plain BETWEEN — sargable and index-prunable. Prefix-free codes mean different tags never overlap."
	tipIdRO      = "The composed 64-bit id, decimal and hex."
	tipInvalid   = "This word has no adjacent 11 pair, so it carries no comma and is not a well-formed tagged id — Split returns zeros rather than garbage."
	tipSelfDelim = "Self-delimiting: an id needs no out-of-band width to split. Scan from the MSB; the first 11 pair is the comma."
	tipMaxExp    = "How many distinct ids you expect in the busiest category. The advisor picks the widest tag (the most categories) that still leaves room for this many bodies."
	tipSQL       = "The constant-tag filter folds to a sargable range at expansion time (identsql.ExpandPass) — the same split the Go code does, golden-locked against ClickHouse."
	tipStats     = "Every code width: how many tag values it holds, and the largest body under it. Wider tag ⇒ more categories, fewer ids each."
	tipPlot      = "As the tag widens, body headroom (green) falls one bit per bit while tag capacity (blue) climbs. The amber line marks the advisor's pick."
	tipExhaust   = "Each rate column is the time to fill one tag's body space at that steady ingress rate — max ids ÷ rate, humanized. Narrow tags are effectively inexhaustible (giga-years); a wide tag at MHz rates fills in seconds."
)

// ingressRates are the typical steady id-creation rates the stats table
// projects a tag's exhaustion time against (Hz = ids per second).
var ingressRates = []struct {
	label string
	hz    float64
}{
	{"100Hz", 1e2},
	{"10kHz", 1e4},
	{"100kHz", 1e5},
	{"1MHz", 1e6},
	{"10MHz", 1e7},
}

// ids is the package-level WidgetIdStack. Each frame's render wraps the body in
// c.IdScope(ids.PrepareSeq(inst.seed)) so two open windows produce disjoint
// Go-side widget ids even though the stack is shared. (terrainscope pattern.)
var ids = c.NewWidgetIdStack()

// instanceCounter feeds per-instance seeds.
var instanceCounter atomic.Uint64

// App is the per-window fibscope instance. The only state is the id under
// inspection plus the advisor's magnitude input; everything else derives.
type App struct {
	seed   uint64
	logger zerolog.Logger

	// id is the single source of truth on the Explore tab — a TaggedId's raw
	// bits. Tag/body/raw edits all recompose it; an invalid word is kept
	// as-is so its split can be inspected. The raw-id field (c.U64Edit) keeps
	// its own exact text draft, so no draft state lives here.
	id uint64

	// maxExp backs the Trade-offs log slider (max expected ids per category).
	maxExp float64

	// SQL expansion cache, keyed on the tag value, so the nanopass runs only
	// when the tag changes rather than every frame.
	sqlTV   uint64
	sqlExpr string
	sqlErr  error
	sqlInit bool

	// statsScrolledWidth is the recommended width the stats table was last
	// scrolled to; re-scrolling only when it changes lets the user scroll the
	// table freely otherwise.
	statsScrolledWidth int
}

var _ app.AppI = (*App)(nil)

func newApp() (inst *App) {
	inst = &App{
		seed:               instanceCounter.Add(1),
		logger:             log.Logger,
		id:                 defaultId,
		maxExp:             1e6,
		statsScrolledWidth: -1,
	}
	return
}

func (inst *App) Manifest() (m app.Manifest) { m = manifest; return }

func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.logger = ctx.Log()
	return
}

func (inst *App) Unmount(ctx app.MountContextI) (err error) { return }

// Frame renders the app body. Wrapped in IdScope(seed) so per-instance widget
// ids stay disjoint across multiple open windows.
func (inst *App) Frame(ctx app.FrameContextI) (err error) {
	ids.Reset()
	for range c.IdScope(ids.PrepareSeq(inst.seed)) {
		inst.renderBody()
	}
	return
}

func (inst *App) renderBody() {
	for range c.PanelCentralInside().KeepIter() {
		for dock := range c.DockArea(ids.PrepareStr("fib-dock")) {
			dock.InitRoot(dockTabExplore, dockTabTradeoff)
			for range dock.Tab(dockTabExplore, "Explore") {
				for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
					inst.renderExplore()
				}
			}
			for range dock.Tab(dockTabTradeoff, "Trade-offs") {
				for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
					inst.renderTradeoff()
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Explore tab
// ---------------------------------------------------------------------------

// decoded is the full split of one id, computed once per render from inst.id.
type decoded struct {
	id       uint64
	valid    bool
	tag      identifier.IdTag
	tagValue identifier.TagValue
	tagWidth int
	body     identifier.UntaggedId
	maxBody  uint64
	bits     string // 64 chars, MSB first
}

// decode splits id into every part the readout needs. It is total: an invalid
// (comma-less) word comes back valid=false with zero tag/body, never garbage.
func decode(id uint64) (d decoded) {
	tid := identifier.TaggedId(id)
	d.id = id
	d.valid = tid.IsValid()
	d.tag, d.body = tid.Split()
	d.tagValue = d.tag.GetValue()
	d.tagWidth = tid.GetTagWidth()
	d.maxBody = uint64(d.tag.GetMaxPossibleIdIncl())
	d.bits = fmt.Sprintf("%064b", id)
	return
}

func (inst *App) renderExplore() {
	metric("A tagged id packs a category and a per-category counter into one 64-bit UInt64.", tipIntro)
	c.Separator().Send()
	inst.renderControls()

	// Re-decode after the controls so the strip and readout reflect this
	// frame's edits (the drag widgets themselves lag one frame, as usual for
	// immediate-mode).
	d := decode(inst.id)

	c.Separator().Send()
	inst.renderBitStrip(d)
	c.Separator().Send()
	inst.renderSplitReadout(d)
	c.Separator().Send()
	inst.renderSQL(d)
}

func (inst *App) renderControls() {
	d := decode(inst.id)
	for range c.Horizontal().KeepIter() {
		tv := uint64(d.tagValue)
		for range c.HoverText(tipTagValue).KeepIter() {
			if c.DragValueU64(ids.PrepareStr("tagval"), tv).Speed(1).Prefix("tag ").
				SendRespVal(&tv).HasChanged() {
				inst.setTagValue(tv)
			}
		}
		b := uint64(d.body)
		for range c.HoverText(tipBody).KeepIter() {
			if c.DragValueU64(ids.PrepareStr("body"), b).Speed(1).Prefix("body ").
				SendRespVal(&b).HasChanged() {
				inst.setBody(b)
			}
		}
	}
	for range c.Horizontal().KeepIter() {
		for range c.HoverText(tipRaw).KeepIter() {
			// c.U64Edit is exact across the whole uint64 range (a tagged id is
			// always > 2^53) and manages its own text draft — including
			// re-seeding when inst.id changes from a preset or a tag/body edit
			// (its "Stubborn Text" override). Decimal or 0x-hex both parse.
			_ = c.U64Edit(ids.PrepareStr("raw"), inst.id).
				DesiredWidth(320).HintText("id — decimal or 0x-hex").
				SendRespVal(&inst.id)
		}
		if c.Button(ids.PrepareStr("preset-ex"), c.Atoms().Text("example").Keep()).
			SendResp().HasPrimaryClicked() {
			inst.id = defaultId
		}
		if c.Button(ids.PrepareStr("preset-inv"), c.Atoms().Text("invalid").Keep()).
			SendResp().HasPrimaryClicked() {
			inst.id = invalidId
		}
	}
}

// setTagValue recomposes the id with tag value tv (clamped to the valid tag
// domain), preserving the body where it still fits under the new tag width.
func (inst *App) setTagValue(tv uint64) {
	if tv < 1 {
		tv = 1
	}
	if tv > uint64(identifier.MaxTagValue) {
		tv = uint64(identifier.MaxTagValue)
	}
	tag := identifier.TagValue(tv).GetTag()
	_, body := identifier.TaggedId(inst.id).Split()
	inst.id = composeId(tag, uint64(body))
}

// setBody recomposes the id with a new body under the current tag. An invalid
// id has no tag region to hold a body, so the edit is ignored there.
func (inst *App) setBody(b uint64) {
	tag, _ := identifier.TaggedId(inst.id).Split()
	if tag == 0 {
		return
	}
	inst.id = composeId(tag, b)
}

// composeId ORs a body under a tag, masking the body to the tag's body region
// first. We OR directly rather than call identifier.UntaggedId.AddTag (which
// panics on overlap, ADR-0106 SD1) so a UI drag can never crash the app.
func composeId(tag identifier.IdTag, body uint64) (id uint64) {
	return uint64(tag) | (body & uint64(tag.GetMaxPossibleIdIncl()))
}

// bitRun is one contiguous, single-colour segment of the 64-bit strip. emph
// underlines the run — used to mark the comma inline. A separate caret line
// below the strip cannot be relied on: the host renders the "monospace" style
// with the proportional main font by default (MONO_FONT unset), so a
// spaces-plus-caret line drifts out of column alignment with the digits, and
// the drift changes with the tag width.
type bitRun struct {
	text string
	col  color.Color
	emph bool
}

// bitRuns segments the MSB-first bit string into the tag code (minus comma),
// the 11 comma, and the body. A comma-less word is one neutral run — there is
// no tag to colour.
func bitRuns(bits string, valid bool, tagWidth int) (runs []bitRun) {
	if !valid || tagWidth < 2 || tagWidth > len(bits) {
		return []bitRun{{text: bits, col: colInvalid}}
	}
	w := tagWidth
	return []bitRun{
		{text: bits[:w-2], col: colTagCode}, // empty for the width-2 tag (value 1)
		{text: bits[w-2 : w], col: colComma, emph: true},
		{text: bits[w:], col: colBody},
	}
}

func (inst *App) renderBitStrip(d decoded) {
	// The comma is marked inline (amber + underline) rather than by a caret
	// line below — see bitRun.emph for why a separate line would not stay
	// column-aligned.
	a := c.Atoms()
	for _, r := range bitRuns(d.bits, d.valid, d.tagWidth) {
		if r.text == "" {
			continue
		}
		run := a.BeginRichTextColored(r.col, color.Transparent, r.text).Monospace()
		if r.emph {
			run = run.Underline()
		}
		a = run.End()
	}
	c.LabelAtoms(a.Keep()).Send()
	inst.renderBitLegend(d)
}

func (inst *App) renderBitLegend(d decoded) {
	// Every run is wrapped in BeginRichText[Colored]…End — the one-shot
	// RichTextColored/Text forms desync the atom sub-loop (the Rust
	// interpreter rejects a Text method inside a richTextColored scope).
	if !d.valid {
		c.LabelAtoms(c.Atoms().
			BeginRichTextColored(colInvalid, color.Transparent, "■ ").End().
			BeginRichText("no comma — inspecting raw bits").End().
			Keep()).Send()
		return
	}
	c.LabelAtoms(c.Atoms().
		BeginRichTextColored(colTagCode, color.Transparent, "■").End().
		BeginRichText(" fibonacci code   ").End().
		BeginRichTextColored(colComma, color.Transparent, "■").End().
		BeginRichText(" comma (11)   ").End().
		BeginRichTextColored(colBody, color.Transparent, "■").End().
		BeginRichText(" body").End().
		Keep()).Send()
}

func (inst *App) renderSplitReadout(d decoded) {
	if !d.valid {
		metric(fmt.Sprintf("not a tagged id — 0x%016x carries no comma", d.id), tipInvalid)
		metric("Scanning from the MSB, a tagged id must contain an adjacent 11 pair (the comma). This word has none.", tipSelfDelim)
		return
	}
	for range c.Horizontal().KeepIter() {
		metric(fmt.Sprintf("tag value %d", d.tagValue), tipTagValRO)
		c.Label(sep).Send()
		metric(fmt.Sprintf("tag width %d bits", d.tagWidth), tipWidthRO)
		c.Label(sep).Send()
		metric(fmt.Sprintf("body %d / %s max", uint64(d.body), humanSci(d.maxBody)), tipBodyRO)
	}
	for range c.Horizontal().KeepIter() {
		metric("fibonacci code "+d.bits[:d.tagWidth], tipCode)
		c.Label(sep).Send()
		metric("Zeckendorf "+zeckExplain(d.tagValue), tipZeck)
	}
	lo := uint64(d.tag)
	hi := lo | d.maxBody
	for range c.Horizontal().KeepIter() {
		metric(fmt.Sprintf("same-tag range %d … %d", lo, hi), tipRange)
		c.Label(sep).Send()
		metric(fmt.Sprintf("id %d = 0x%016x", d.id, d.id), tipIdRO)
	}
}

func (inst *App) renderSQL(d decoded) {
	if !d.valid {
		metric("SQL decode applies to valid tagged ids only.", tipSQL)
		return
	}
	tv := uint64(d.tagValue)
	if !inst.sqlInit || inst.sqlTV != tv {
		inst.sqlExpr, inst.sqlErr = identsql.ExpandPass.Run(
			fmt.Sprintf("SELECT LW_ID_HAS_TAG(id, %d) FROM t", tv))
		inst.sqlTV = tv
		inst.sqlInit = true
	}
	kvMono(fmt.Sprintf("filter LW_ID_HAS_TAG(id, %d)", tv), "", tipSQL)
	if inst.sqlErr != nil {
		metric("expansion error: "+inst.sqlErr.Error(), tipSQL)
		return
	}
	kvMono("expands to", inst.sqlExpr, tipSQL)
}

// ---------------------------------------------------------------------------
// Trade-offs tab
// ---------------------------------------------------------------------------

func (inst *App) renderTradeoff() {
	metric("Pick tag values by the id space each category needs: a narrow tag leaves a huge body but few categories; a wide tag is the reverse.", tipStats)
	c.Separator().Send()

	for range c.HoverText(tipMaxExp).KeepIter() {
		_ = c.SliderF64(ids.PrepareStr("maxexp"), inst.maxExp, 1, advisorMaxExpCeil).
			Logarithmic(true).Text("max ids / category").FixedDecimals(0).
			SendRespVal(&inst.maxExp)
	}

	maxExpU := clampMaxExp(inst.maxExp)
	lo, hiExcl, err := fibonacci.SelectFittingTagValueRange(maxExpU)
	recWidth := 0
	if err == nil {
		recWidth = lo.GetTag().GetTagWidth()
		nTags := uint64(hiExcl) - uint64(lo)
		maxBodies := uint64(1)<<(64-recWidth) - 1
		for range c.Horizontal().KeepIter() {
			metric(fmt.Sprintf("use tag width %d bits", recWidth), tipWidthRO)
			c.Label(sep).Send()
			metric(fmt.Sprintf("tag values %d … %d (%s categories)", lo, hiExcl-1, humanSci(nTags)), tipStats)
			c.Label(sep).Send()
			metric(fmt.Sprintf("up to %s ids / category", humanSci(maxBodies)), tipBodyRO)
		}
		metric("Give the hottest categories the smallest tag values in that range — they compress tightest.", tipTagValue)
	} else {
		metric("advisor: "+err.Error(), tipMaxExp)
	}

	c.Separator().Send()
	inst.renderTradeoffPlot(recWidth)
	c.Separator().Send()
	inst.renderStatsTable(recWidth)
}

func (inst *App) renderTradeoffPlot(recWidth int) {
	metric("ID space vs tag capacity across every code width", tipPlot)
	var xs, bodyBits, tagBits []float64
	for _, cl := range fibonacci.WidthClasses() {
		xs = append(xs, float64(cl.Width))
		bodyBits = append(bodyBits, float64(64-cl.Width)) // log2(max ids/tag) == 64−width
		tagBits = append(tagBits, math.Log2(float64(max(uint64(1), cl.TagValueCount))))
	}
	c.PlotLine("body headroom (log2 ids/tag)", xs, bodyBits).Color(colBody).Width(2).Send()
	c.PlotLine("tag capacity (log2 tag values)", xs, tagBits).Color(colTagCode).Width(2).Send()
	if recWidth >= 2 {
		c.PlotLine("your pick", []float64{float64(recWidth), float64(recWidth)}, []float64{0, 64}).
			Color(colComma).Width(1.5).Send()
	}
	c.AddSpace(margin)
	c.Plot(ids.PrepareStr("tradeoff-plot")).Width(stageW).Height(240).
		XAxisLabel("tag width (bits)").YAxisLabel("bits (log2)").
		Legend().AllowZoom(true).AllowDrag(true).AllowScroll(false).Send()
	c.AddSpace(margin)
}

func (inst *App) renderStatsTable(recWidth int) {
	metric("Time to exhaust one tag's id space at typical ingress rates (the rightmost columns):", tipExhaust)

	c.TableColumn().Exact(24).Send()    // recommended-row marker
	c.TableColumn().Initial(50).Send()  // tag bits
	c.TableColumn().Initial(165).Send() // tag value range
	c.TableColumn().Initial(78).Send()  // # tag values
	c.TableColumn().Initial(86).Send()  // max ids / tag
	c.TableColumn().Initial(72).Send()  // code overhead
	for range ingressRates[:len(ingressRates)-1] {
		c.TableColumn().Initial(70).Send() // per-rate exhaustion time
	}
	c.TableColumn().Remainder().Send() // last rate

	c.TableHeaderText("").Send()
	c.TableHeaderText("tag bits").Send()
	c.TableHeaderText("tag value range").Send()
	c.TableHeaderText("# tag values").Send()
	c.TableHeaderText("max ids / tag").Send()
	c.TableHeaderText("overhead").Send()
	for _, r := range ingressRates {
		c.TableHeaderText(r.label).Send()
	}

	classes := fibonacci.WidthClasses()
	markerRow := -1
	for i, cl := range classes {
		marker := ""
		if cl.Width == recWidth {
			marker = "►"
			markerRow = i
		}
		c.TableCellText(marker).Send()
		c.TableCellText(fmt.Sprintf("%d", cl.Width)).Send()
		c.TableCellText(fmt.Sprintf("%d … %d", cl.TagValueMinIncl, cl.TagValueMaxIncl)).Send()
		c.TableCellText(humanSci(cl.TagValueCount)).Send()
		c.TableCellText(humanSci(cl.MaxBodyIncl)).Send()
		c.TableCellText(fmt.Sprintf("%.2f×", cl.EncodingOverhead)).Send()
		for _, r := range ingressRates {
			c.TableCellText(humanizeExhaust(float64(cl.MaxBodyIncl) / r.hz)).Send()
		}
	}
	rows := len(classes)

	t := c.Table(ids.PrepareStr("stats"), 20, uint64(rows)).Striped(true).Vscroll(true).MaxScrollHeight(320)
	// Scroll the recommended row into view when it changes, but leave the user
	// free to scroll otherwise.
	if markerRow >= 0 && recWidth != inst.statsScrolledWidth {
		t = t.ScrollToRow(uint64(markerRow))
		inst.statsScrolledWidth = recWidth
	}
	t.Send()
}

// ---------------------------------------------------------------------------
// Pure helpers
// ---------------------------------------------------------------------------

// clampMaxExp maps the log-slider float into the advisor's valid domain
// [1, 2^60) so the readout never hits the "too large" rejection at the slider's
// top end.
func clampMaxExp(f float64) (n uint64) {
	if !(f >= 1) { // also catches NaN
		return 1
	}
	if f >= advisorMaxExpCeil {
		return uint64(advisorMaxExpCeil) - 1
	}
	return uint64(f)
}

// zeckExplain renders the tag value as its unique sum of non-consecutive
// Fibonacci numbers. The encoder's two biases cancel (it codes tagValue−1 but
// biases +1 internally), so the Zeckendorf sum of the tag's code bits is the
// tag value itself (ADR-0106; howto §LW_ID_*) — the code literally spells it
// out.
func zeckExplain(tagValue identifier.TagValue) (s string) {
	if tagValue == 0 {
		return "—"
	}
	z, _ := fibonaccicode.EncodeZeckendorf(uint64(tagValue))
	parts := make([]string, 0, 8)
	for i := 63; i >= 0; i-- {
		if z&(uint64(1)<<uint(i)) != 0 {
			parts = append(parts, fmt.Sprintf("%d", fibonaccicode.DecodeZeckendorfV(uint64(1)<<uint(i))))
		}
	}
	return fmt.Sprintf("%d = %s", tagValue, strings.Join(parts, " + "))
}

// humanSci renders a count compactly: small values exact, large ones in three
// significant figures.
func humanSci(n uint64) (s string) {
	if n < 100000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%.3g", float64(n))
}

// humanizeExhaust renders a time-to-exhaust (in seconds) compactly. The values
// span ~19 orders of magnitude (a wide tag at 10 MHz fills in milliseconds; a
// narrow tag at 100 Hz outlasts the age of the universe), so go-humanize SI
// formatting carries the two open-ended extremes — sub-minute as ms/s, and
// years as yr/kyr/Myr/Gyr (the geology-standard abbreviations) — with a plain
// minute/hour/day ladder in between.
func humanizeExhaust(secs float64) (s string) {
	const (
		minute = 60.0
		hour   = 60 * minute
		day    = 24 * hour
		year   = 365.25 * day
	)
	switch {
	case secs < minute:
		return humanize.SIWithDigits(secs, 1, "s")
	case secs < hour:
		return fmt.Sprintf("%.1f min", secs/minute)
	case secs < day:
		return fmt.Sprintf("%.1f h", secs/hour)
	case secs < year:
		return fmt.Sprintf("%.1f d", secs/day)
	default:
		return humanize.SIWithDigits(secs/year, 1, "yr")
	}
}

// metric renders a label carrying a hover tooltip — used for every displayed
// quantity so the reader can learn what each part means. (terrainscope idiom.)
func metric(text string, tip string) {
	for range c.HoverText(tip).KeepIter() {
		c.Label(text).Send()
	}
}

// kvMono renders "key: <monospace value>" with a hover tooltip. An empty value
// renders the key alone (used for the SQL macro line).
func kvMono(key string, val string, tip string) {
	for range c.HoverText(tip).KeepIter() {
		a := c.Atoms().BeginRichText(key).End()
		if val != "" {
			a = a.BeginRichText(": ").End().BeginRichText(val).Monospace().End()
		}
		c.LabelAtoms(a.Keep()).Send()
	}
}
