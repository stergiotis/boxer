package play

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/kanban"
)

// play_kanban_panel.go is the ADR-0122 Kanban dock tab: a result set rendered as
// a board. The panel observes the active result on chMain, like Table and World,
// and optionally reads a lane inventory on chLanes (§SD6).
//
// The contract is named columns rather than detection (§SD1): `lane` and `title`
// are required, `subtitle` and up to three `dot_<label>` tallies optional.
// Nothing distinguishes a lane column from a title column but intent, so the
// pane asks for one `AS` per query instead of guessing between two strings.

const (
	kanbanLaneCol     = "lane"
	kanbanTitleCol    = "title"
	kanbanSubtitleCol = "subtitle"
	kanbanDotPrefix   = "dot_"
	// kanbanLanesNodeID is the CTE whose rows declare the board's lanes
	// (§SD6). It is a node of the user's own split graph, not a
	// panel-authored query: a board and its lane vocabulary are one thought,
	// and a second editor would be a second thing to keep in sync. A query
	// without it renders lanes off the rows, as before.
	kanbanLanesNodeID NodeID = "lanes"
	// kanbanTokenSep introduces a dot column's colour token —
	// `dot_cited@warning`. Not ':' (ADR-0116's splitHandle claims any
	// identifier with exactly one colon as a leeway `section:column` handle,
	// so the name would be rewritten before it ever reached this panel) and
	// not '_' (already this convention's own prefix separator, and leeway's
	// mangled-physical-name separator). ADR-0122 §SD2 records why '#' lost to
	// '@' despite the CSS mnemonic: '#' opens a ClickHouse line comment, so a
	// forgotten backtick silently yields a `dot_done` column plus a comment —
	// a plausible board in the wrong colour, with no diagnostic.
	kanbanTokenSep = "@"
	// kanbanMaxDots is the widget's cap: Card.Dots entries past the third are
	// dropped silently at render. The panel rejects instead — a board that
	// quietly omits a bucket is worse than one that says why it will not draw.
	kanbanMaxDots = 3
	// kanbanNoLane titles the lane an empty lane cell falls into; an empty
	// column header reads as a rendering fault rather than as a datum.
	kanbanNoLane = "(none)"
	// kanbanMaxCards bounds the fold. A board is a tens-to-hundreds instrument;
	// a million-card result would allocate and lay out every one of them. The
	// excess is dropped and counted in the status line rather than silently —
	// and rather than rejected, since a bounded look at a big table is a
	// reasonable thing to want on the way to a GROUP BY.
	kanbanMaxCards = 2000
)

// kanbanDotTokens is the `@token` colour vocabulary (ADR-0122 §SD2): the
// foreground semantic tones only.
//
// The *Subtle tones are deliberately absent. They are background fills
// (L≈0.2 — NeutralSubtle is 27/27/27) and land within a few points of the
// card's own NeutralBgSurface, so a dot painted in one is invisible. The
// vocabulary excludes them by construction rather than warning about them.
var kanbanDotTokens = map[string]styletokens.RGBA8{
	"success":  styletokens.SuccessDefault,
	"warning":  styletokens.WarningDefault,
	"error":    styletokens.ErrorDefault,
	"info":     styletokens.InfoDefault,
	"accent":   styletokens.AccentDefault,
	"neutral":  styletokens.NeutralDefault,
	"disabled": styletokens.NeutralTextDisabled,
}

// kanbanDotRamp colours dot columns carrying no `@token`, by position. It reads
// as progress — settled, in flight, neither — which is the shape a tally of
// buckets usually has. `@token` is the escape when it is not.
var kanbanDotRamp = [kanbanMaxDots]styletokens.RGBA8{
	styletokens.SuccessDefault,
	styletokens.WarningDefault,
	styletokens.NeutralTextDisabled,
}

func kanbanTokenColor(t styletokens.RGBA8) color.Color { return color.Hex(t.AsHex()) }

// kanbanTokenNames lists the vocabulary for a reject message, sorted so the
// text is stable across runs (map order is not).
func kanbanTokenNames() string {
	names := make([]string, 0, len(kanbanDotTokens))
	for k := range kanbanDotTokens {
		names = append(names, k)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// kanbanDotSpec is one resolved `dot_*` column.
type kanbanDotSpec struct {
	col   int
	name  string // the physical column name, for the legend tooltip
	label string
	color color.Color
}

// kanbanClaim is the panel's channel claim: the resolved columns plus the row
// to highlight (-1 ⇒ nothing selected), read from the selection signal.
type kanbanClaim struct {
	laneCol  int
	titleCol int
	subCol   int // -1 when absent
	dots     []kanbanDotSpec
	selRow   int64
}

// KanbanDriver owns the Kanban tab state: the board model, its build cache, and
// the lane for the optional lanes node (§SD6).
type KanbanDriver struct {
	ids   *c.WidgetIdStack
	model *kanban.Model

	// lanesLane runs the `lanes` CTE when the board query carries one. It is a
	// node of the user's split graph demanded on its own lane, so a lane
	// vocabulary that is expensive to compute does not block the board.
	lanesLane *nodeLane
	// lanesLoading / lanesErr mirror the lane's last demand for the status
	// line — a lanes query that fails must say so rather than silently
	// reverting the board to row-derived lanes.
	lanesLoading bool
	lanesErr     error

	// Fold cache key: the result identity (executed timestamp — the same
	// freshness token the pager and the World pane use) + the schema the claim
	// was resolved from, + the declared lanes, which are an input to the fold
	// and can change without the result changing. Caching here is not only
	// about the per-frame allocation: kanban.Model owns the widget's
	// selection, so a Model rebuilt every frame would clear the highlight
	// every frame.
	forExecuted time.Time
	forSchema   *arrow.Schema
	forLanes    string

	// truncated counts cards dropped by kanbanMaxCards, for the status line.
	truncated int64

	// pendingExecuted is stashed by renderKanbanTab before dispatch — the
	// PanelI Render signature carries no result metadata (the World pane's
	// noteExecuted handoff).
	pendingExecuted time.Time
}

// NewKanbanDriver builds the driver. client may be nil (tests, an unwired
// host): the lanes lane is then absent and the board reads lanes off the rows.
func NewKanbanDriver(ids *c.WidgetIdStack, client *Client) (inst *KanbanDriver) {
	inst = &KanbanDriver{ids: ids}
	if client != nil {
		inst.lanesLane = newNodeLane(clientExecutor{client: client, opts: newExecOptions("kanban-lanes")},
			memory.NewGoAllocator(), 0)
	}
	return
}

// noteExecuted hands the driver the active result's freshness token before
// dispatch; the fold keys its cache on it.
func (inst *KanbanDriver) noteExecuted(t time.Time) { inst.pendingExecuted = t }

// kanbanPanel is the PanelI face. Acceptance is schema-only and cheap — it runs
// every frame — because the contract is a question about column names, which
// the schema answers on its own.
type kanbanPanel struct {
	driver *KanbanDriver
}

func (inst kanbanPanel) ID() PanelID { return "kanban" }

func (inst kanbanPanel) Channels() []ChannelSpec {
	return []ChannelSpec{
		{ID: chMain, Required: true, Label: "cards"},
		{ID: chLanes, Required: false, Label: "lanes"},
	}
}

func (inst kanbanPanel) AcceptForChannel(ch ChannelID, schema *arrow.Schema, sig SignalEnvI) (claim ChannelClaim, reason string) {
	if schema == nil {
		reason = "Run a query to see the board."
		if ch == chLanes {
			reason = "no lanes result"
		}
		return
	}
	if ch == chLanes {
		// The lanes node only has to name lanes; the claim is that column.
		for ci, f := range schema.Fields() {
			if f.Name == kanbanLaneCol {
				claim = ci
				return
			}
		}
		reason = "the `lanes` CTE needs a `lane` column"
		return
	}
	k, reason := resolveKanbanColumns(schema)
	if reason != "" {
		return
	}
	k.selRow, _ = readSelection(sig)
	claim = k
	return
}

func (inst kanbanPanel) Render(filled map[ChannelID]ChannelResult, emit SignalEmitterI) {
	main := filled[chMain]
	k, ok := main.Claim.(kanbanClaim)
	if !ok {
		return
	}
	var declared []string
	if l, filledLanes := filled[chLanes]; filledLanes {
		if col, isCol := l.Claim.(int); isCol {
			declared = kanbanDeclaredLanes(l.Rec, col)
		}
	}
	inst.driver.render(main.Rec, main.Rec.Schema(), k, declared, emit)
}

// kanbanDeclaredLanes reads the lanes node's rows in order, de-duplicated. An
// empty cell is a lane too — it is where a card with no lane value lands, and
// declaring it is the only way to show that lane empty.
func kanbanDeclaredLanes(rec arrow.RecordBatch, col int) (out []string) {
	if rec == nil || col < 0 || col >= int(rec.NumCols()) {
		return
	}
	seen := make(map[string]struct{}, rec.NumRows())
	for row := range rec.NumRows() {
		v := formatCell(rec, col, row)
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return
}

// resolveKanbanColumns applies the §SD1 contract to a schema. Pure and
// schema-only: every reject carries the reason the pane paints in place.
//
// lane / title / subtitle are read through formatCell, which is total over
// Arrow types, so they carry no type requirement — `SELECT num AS lane` is a
// board with numbered lanes, and rejecting it would buy nothing. Dot columns
// are different: they are counts, and a fractional or textual tally is not a
// weaker board but a meaningless one.
func resolveKanbanColumns(schema *arrow.Schema) (k kanbanClaim, reason string) {
	k = kanbanClaim{laneCol: -1, titleCol: -1, subCol: -1, selRow: -1}
	var dotCols []int
	for ci, f := range schema.Fields() {
		switch {
		case f.Name == kanbanLaneCol:
			k.laneCol = ci
		case f.Name == kanbanTitleCol:
			k.titleCol = ci
		case f.Name == kanbanSubtitleCol:
			k.subCol = ci
		case strings.HasPrefix(f.Name, kanbanDotPrefix):
			dotCols = append(dotCols, ci)
		}
	}
	if k.laneCol < 0 || k.titleCol < 0 {
		return k, kanbanContractHint(k)
	}
	if len(dotCols) > kanbanMaxDots {
		names := make([]string, 0, len(dotCols))
		for _, ci := range dotCols {
			names = append(names, schema.Field(ci).Name)
		}
		return k, fmt.Sprintf("The board paints at most %d dot kinds; this result has %d (%s). Drop or combine some.",
			kanbanMaxDots, len(dotCols), strings.Join(names, ", "))
	}
	for pos, ci := range dotCols {
		spec, r := resolveKanbanDot(schema.Field(ci), ci, pos)
		if r != "" {
			return k, r
		}
		k.dots = append(k.dots, spec)
	}
	return k, ""
}

// kanbanContractHint is the reject shown when the required columns are absent —
// the pane's one teaching moment, so it names both the contract and a query
// that satisfies it.
func kanbanContractHint(k kanbanClaim) string {
	var missing []string
	if k.laneCol < 0 {
		missing = append(missing, "`"+kanbanLaneCol+"`")
	}
	if k.titleCol < 0 {
		missing = append(missing, "`"+kanbanTitleCol+"`")
	}
	return fmt.Sprintf("The board needs a %s column. Name them in the query — e.g. SELECT status AS lane, name AS title FROM t — "+
		"and optionally add `subtitle` and up to %d `dot_<label>` integer tallies (`dot_open@warning`).",
		strings.Join(missing, " and a "), kanbanMaxDots)
}

// resolveKanbanDot resolves one `dot_*` column to its legend entry. pos is the
// column's ordinal among the dot columns, which selects the ramp colour when
// the name carries no `@token`; callers guarantee pos < kanbanMaxDots.
func resolveKanbanDot(f arrow.Field, ci, pos int) (spec kanbanDotSpec, reason string) {
	if !isKanbanCountType(f.Type) {
		return spec, fmt.Sprintf("Dot column `%s` must be an integer tally; it is %s. countIf(...) yields one.", f.Name, f.Type)
	}
	label, token, hasToken := parseKanbanDot(f.Name)
	if label == "" {
		return spec, fmt.Sprintf("Dot column `%s` carries no label — name it `%s<label>`.", f.Name, kanbanDotPrefix)
	}
	spec = kanbanDotSpec{col: ci, name: f.Name, label: label}
	if !hasToken {
		spec.color = kanbanTokenColor(kanbanDotRamp[pos])
		return spec, ""
	}
	t, known := kanbanDotTokens[token]
	if !known {
		return spec, fmt.Sprintf("Dot column `%s` names an unknown colour %q. Known tokens: %s.", f.Name, kanbanTokenSep+token, kanbanTokenNames())
	}
	spec.color = kanbanTokenColor(t)
	return spec, ""
}

// parseKanbanDot splits `dot_<label>` / `dot_<label>@<token>`. hasToken
// distinguishes a missing token from an empty one: `dot_done@` is a typo, not a
// request for the positional ramp.
func parseKanbanDot(name string) (label, token string, hasToken bool) {
	rest := strings.TrimPrefix(name, kanbanDotPrefix)
	label, token, hasToken = strings.Cut(rest, kanbanTokenSep)
	return
}

// isKanbanCountType reports the types a dot tally may carry. Integers only:
// ClickHouse's countIf/count yield UInt64, and a float tally would render a
// fraction of a dot.
func isKanbanCountType(dt arrow.DataType) bool {
	switch dt.ID() {
	case arrow.INT8, arrow.INT16, arrow.INT32, arrow.INT64,
		arrow.UINT8, arrow.UINT16, arrow.UINT32, arrow.UINT64:
		return true
	}
	return false
}

// render folds the result into a board (cached), draws it, and carries the
// selection both ways.
func (inst *KanbanDriver) render(rec arrow.RecordBatch, schema *arrow.Schema, k kanbanClaim, declared []string, emit SignalEmitterI) {
	inst.rebuild(rec, schema, k, declared)
	m := inst.model
	if m == nil || len(m.Cards) == 0 {
		for rt := range c.RichTextLabel("The query returned no rows, so the board has no cards.") {
			rt.Small().Weak()
		}
		return
	}
	// Follow the shared selection before drawing. Without this the board would
	// keep painting its own last click while the rest of the dock had moved on
	// — showing a selection nothing else agrees with.
	if k.selRow >= 0 && k.selRow < int64(len(m.Cards)) {
		m.SetSelected(uint64(k.selRow + 1))
	} else {
		m.SetSelected(0)
	}

	dens := styletokens.DensityFromEnv()
	c.Label(inst.statusLine()).Send()
	c.AddSpace(styletokens.GapInline(dens))
	kanban.RenderLegend(m.DotLegend) // no-op when the board carries no dots
	c.AddSpace(styletokens.GapItems(dens))
	kanban.Render(kanban.Input{
		Ids:      inst.ids,
		ScopeKey: "play-kanban",
		Model:    m,
		FillHost: true,
		// Read-only: a result is a query output, so there is nowhere to write a
		// dragged card back to. DrainMoves stays unused.
		ReadOnly: true,
	})

	// Publish a click. Card ids are row+1, so the row is recoverable; comparing
	// against the claim's row means a selection that merely echoes the signal
	// back does not re-emit.
	if sel := m.Selected(); sel != 0 {
		if row := int64(sel) - 1; row != k.selRow {
			emit.Emit(signalSelection, row)
		}
	}
}

// rebuild folds the result into a kanban model, keyed on (executed, schema,
// declared lanes).
//
// Lanes come from the declared inventory first, in its order, and then from the
// rows — so a declared lane no card sits in still renders, empty, and a lane the
// inventory does not name is appended rather than dropped: a vocabulary word the
// data has outgrown shows up on the board instead of vanishing from it. With no
// inventory the rows are the only source and
// first-seen row order decides the layout, so the query's ORDER BY controls it
// with no second mechanism.
func (inst *KanbanDriver) rebuild(rec arrow.RecordBatch, schema *arrow.Schema, k kanbanClaim, declared []string) {
	lanesKey := strings.Join(declared, "\x00")
	if inst.model != nil && schema == inst.forSchema &&
		inst.pendingExecuted.Equal(inst.forExecuted) && lanesKey == inst.forLanes {
		return
	}
	inst.forSchema = schema
	inst.forExecuted = inst.pendingExecuted
	inst.forLanes = lanesKey

	rows := rec.NumRows()
	inst.truncated = 0
	if rows > kanbanMaxCards {
		inst.truncated = rows - kanbanMaxCards
		rows = kanbanMaxCards
	}

	laneOf := make(map[string]uint64, 8)
	var cols []kanban.Column
	addLane := func(name string) (id uint64) {
		id = uint64(len(cols) + 1)
		title := name
		if title == "" {
			title = kanbanNoLane
		}
		cols = append(cols, kanban.Column{ID: id, Title: title})
		laneOf[name] = id
		return
	}
	for _, name := range declared {
		if _, dup := laneOf[name]; !dup {
			addLane(name)
		}
	}
	cards := make([]kanban.Card, 0, rows)
	for row := range rows {
		lane := formatCell(rec, k.laneCol, row)
		id, seen := laneOf[lane]
		if !seen {
			id = addLane(lane)
		}
		card := kanban.Card{
			// Position, not any column's value. Card ids must be unique (the
			// widget scopes each card's widget ids by its id) and non-zero (it
			// reads a zero ParentID as "no parent"). A result set has no
			// guaranteed unique key, and the tempting one is usually not unique
			// either — two rows can share the id a query selected as "the" id.
			// Position always is.
			ID:       uint64(row + 1),
			ColumnID: id,
			Title:    formatCell(rec, k.titleCol, row),
			Dots:     kanbanCardDots(rec, k.dots, row),
		}
		if k.subCol >= 0 {
			card.Subtitle = formatCell(rec, k.subCol, row)
		}
		cards = append(cards, card)
	}
	m := kanban.NewModel(cols, cards)
	m.DotLegend = kanbanLegend(k.dots)
	inst.model = m
}

// kanbanCardDots tallies one card's dots. A zero or negative count carries no
// dot at all, so a card with nothing to report stays clean.
func kanbanCardDots(rec arrow.RecordBatch, specs []kanbanDotSpec, row int64) (dots []kanban.DotTally) {
	for i, ds := range specs {
		n, ok := numericCellValue(rec.Column(ds.col), row)
		if !ok || n <= 0 {
			continue
		}
		dots = append(dots, kanban.DotTally{ID: uint64(i + 1), Count: int(n)})
	}
	return
}

// kanbanLegend names the dot vocabulary. Ids are the spec's position, matching
// what kanbanCardDots tallies against — an id absent from the legend is skipped
// silently by the widget, so the two must be built from the same slice.
func kanbanLegend(specs []kanbanDotSpec) (out []kanban.DotKind) {
	for i, ds := range specs {
		out = append(out, kanban.DotKind{
			ID:      uint64(i + 1),
			Color:   ds.color,
			Label:   ds.label,
			Tooltip: "column " + ds.name,
		})
	}
	return
}

func (inst *KanbanDriver) statusLine() string {
	if inst.model == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d cards · %d lanes", len(inst.model.Cards), len(inst.model.Columns))
	if inst.truncated > 0 {
		fmt.Fprintf(&b, " · %d more rows not shown (the board caps at %d — add a LIMIT or GROUP BY)",
			inst.truncated, kanbanMaxCards)
	}
	// A lanes node that failed must not read as "there were no declared
	// lanes": the board silently falls back to row-derived lanes, which looks
	// like a working board with lanes missing.
	switch {
	case inst.lanesErr != nil:
		fmt.Fprintf(&b, " · lanes query failed: %v", inst.lanesErr)
	case inst.lanesLoading:
		b.WriteString(" · lanes…")
	}
	return b.String()
}
