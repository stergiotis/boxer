package mappingplanview

import (
	"fmt"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/goplan"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/canonicaltypeedit"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/fsmview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/pager"
)

// FieldRow is one editable row of a [Model]. Its fields mirror the inputs
// mappingplan.PlanBuilder.AddField / AddUnderscoreField take, so the host's
// Recompute can turn a row straight into a builder call:
//
//   - Membership == ""  → a plain column (Section names id/ts/naturalKey/expiresAt).
//   - IsConst           → a `_`-field constant (AddUnderscoreField with ,const=).
//   - otherwise         → an lw:-tagged value field (AddField).
type FieldRow struct {
	// uid is a stable per-row identifier assigned once at AddRow. It scopes
	// the row's widget ids and keeps SendRespVal bindings stable across
	// add/remove: the Fields slice is reordered, but each *FieldRow keeps its
	// heap address and its uid.
	uid uint64

	GoField string // DTO field name (ignored for IsConst rows)

	// typeModel authors the field's value type as a leeway canonical
	// (ADR-0008): the bar accepts e.g. "u64" / "u64h" (array) / "u32s"
	// (roaring set); PlanBuilder derives the Go type + multiplicity from it.
	typeModel     *canonicaltypeedit.Model
	lastCanonical string // last canonical seen, to mark the model dirty on edit
	lastBarErr    string // last type-editor bar parse-error seen — unparseable input leaves Canonical() unchanged, so it is watched separately

	IsOption bool // option.Option[T] — presence, orthogonal to the value type

	Membership string // lw: first segment; "" ⇒ plain column
	Section    string // lw: second segment (plain column name when Membership == "")
	Column     string // lw: sub-column suffix after ':' (e.g. beginIncl / endExcl)

	Channel mappingplan.MembershipChannel // one of the four Cut-1 channels in v1
	Unit    bool                          // ,unit
	Explode bool                          // ,explode

	IsConst    bool   // declared on a `_` field as ,const=<value>
	ConstValue string // the constant value

	// IsTuple marks a dynamic-membership tuple row (ADR-0103): a
	// slice-of-struct DTO field mapping N attributes into ONE section, each
	// element carrying its own membership. The row's GoField / Section stay
	// meaningful (the outer field + its section); Membership is unused (the
	// membership is per-element data); TupleStructType names the element
	// struct; TupleElems are the element struct's fields. One tuple row is
	// one PlanBuilder call (AddTupleSliceField), so the per-field FSM chip
	// carries the whole tuple's verdict.
	IsTuple         bool
	TupleStructType string
	TupleElems      []*TupleElemRow

	// fsm is this row's per-field validity state machine (Empty → … → Valid /
	// Rejected / Conflicting / Blocked); fsmW is its tethered inspector chip,
	// lazily built on first render (the [fsmview.Widget] needs the frame's id
	// stack). state / reason are the latest derived verdict, refreshed by
	// [Model.SetBuildResult] and mirrored into fsm each frame.
	fsm    *fsmview.Machine[FieldState]
	fsmW   *fsmview.Widget[FieldState]
	state  FieldState
	reason string
}

// TupleElemRow is one field of a tuple row's element struct — either THE
// `@membership` field (IsMembership) carrying each attribute's membership
// value, or a value field mapping one sub-column of the tuple's section.
// Mirrors goplan.TupleElem the way FieldRow mirrors AddField inputs, so the
// host's Recompute hands the row straight to AddTupleSliceField.
type TupleElemRow struct {
	uid uint64 // stable per-element id (same uid space as FieldRow)

	GoField      string
	IsMembership bool
	Column       string // value fields: sub-column ("" targets "value")

	// Channel is the membership element's wire channel. ADR-0103 mandates an
	// explicit verbatim channel (`,verbatim` / `,highCardVerbatim`) — ref and
	// carrier channels cannot carry a per-element membership — so the picker
	// offers exactly the verbatim pair.
	Channel mappingplan.MembershipChannel
	// MembBytes picks the membership field's Go type: string (false) or
	// []byte (true) — the two shapes AddTupleSliceField accepts.
	MembBytes bool

	// typeModel authors a value element's canonical, exactly like a
	// FieldRow's value type. Unused for the membership element (its type is
	// string / []byte via MembBytes).
	typeModel     *canonicaltypeedit.Model
	lastCanonical string
	lastBarErr    string
}

// ElemLWTag assembles the element's lw: tag for AddTupleSliceField
// (SplitTupleElemLW grammar): `@membership,<channel flag>` for the
// membership element, `<section>[:<column>]` for a value element. section is
// the tuple row's section (value-element tags repeat it, ADR-0103 D1).
func (e *TupleElemRow) ElemLWTag(section string) string {
	if e.IsMembership {
		if flag := e.Channel.String(); flag != "" {
			return "@membership," + flag
		}
		// No flag spelling (LowCardRef default) — emit the bare marker and let
		// the builder reject it with its explicit-verbatim-channel error.
		return "@membership"
	}
	if e.Column != "" {
		return section + ":" + e.Column
	}
	return section
}

// Shape returns the element's goplan.FieldShape: the authored canonical for
// a value element (nil when the type editor does not parse, so the builder
// rejects and the sequential build halts here — same contract as
// FieldRow.Shape), or the membership field's string / []byte scalar.
func (e *TupleElemRow) Shape() goplan.FieldShape {
	if e.IsMembership {
		goType := "string"
		if e.MembBytes {
			goType = "[]byte"
		}
		cn, err := goplan.ScalarCanonicalForGoType(goType)
		if err != nil {
			return goplan.FieldShape{}
		}
		return goplan.FieldShape{Canonical: cn}
	}
	if e.typeModel.BarError() != "" || !e.typeModel.Valid() {
		return goplan.FieldShape{}
	}
	return goplan.FieldShape{Canonical: e.typeModel.Node()}
}

// SetGoType seeds a value element's canonical from a Go source-type
// spelling — the same convenience FieldRow.SetGoType provides.
func (e *TupleElemRow) SetGoType(goType string) {
	cn, err := goplan.ScalarCanonicalForGoType(goType)
	if err != nil {
		return
	}
	e.SetCanonical(cn.String())
}

// SetCanonical seeds a value element's canonical directly (e.g. "u32h" for a
// container sub-column) — hosts seeding example tuples need it because a
// container type has no scalar Go spelling for SetGoType.
func (e *TupleElemRow) SetCanonical(ct string) {
	e.typeModel.SetCanonical(ct)
	e.lastCanonical = e.typeModel.Canonical()
}

// TupleElemSpecs converts the row's elements into the goplan.TupleElem list
// AddTupleSliceField takes, in declaration order.
func (r *FieldRow) TupleElemSpecs() []goplan.TupleElem {
	out := make([]goplan.TupleElem, 0, len(r.TupleElems))
	for _, e := range r.TupleElems {
		out = append(out, goplan.TupleElem{
			GoFieldName: e.GoField,
			LWTag:       e.ElemLWTag(r.Section),
			Shape:       e.Shape(),
		})
	}
	return out
}

// LWTag assembles the lw: struct-tag *value* this row represents — the string
// PlanBuilder parses via SplitLW. Plain columns produce ",<col>"; const rows
// produce "<memb>,<sec>,const=<value>"; value fields produce
// "<memb>,<sec>[:<col>][,unit][,explode][,<channel>]". The default channel
// (LowCardRef) contributes no flag, matching mappingplan.MembershipChannel.String.
// A tuple row's outer tag is the bare section name (SplitTupleOuterLW).
func (r *FieldRow) LWTag() string {
	if r.IsTuple {
		return r.Section
	}
	if r.Membership == "" {
		return "," + r.Section
	}
	var sb strings.Builder
	sb.WriteString(r.Membership)
	sb.WriteByte(',')
	sb.WriteString(r.Section)
	if r.Column != "" {
		sb.WriteByte(':')
		sb.WriteString(r.Column)
	}
	if r.IsConst {
		sb.WriteString(",const=")
		sb.WriteString(r.ConstValue)
		return sb.String()
	}
	if r.Unit {
		sb.WriteString(",unit")
	}
	if r.Explode {
		sb.WriteString(",explode")
	}
	if flag := r.Channel.String(); flag != "" {
		sb.WriteByte(',')
		sb.WriteString(flag)
	}
	return sb.String()
}

// Shape returns the FieldShape this row describes, ready to hand to
// PlanBuilder.AddField. The value type is authored canonically (typeModel).
// When the type is not currently usable — the formula bar does not parse
// ([canonicaltypeedit.Model.BarError]), or the parsed type fails IsValid —
// Shape yields a nil Canonical so AddField rejects the field and the sequential
// build halts here: the field then reads incomplete and every field after it
// blocked, rather than the build silently proceeding on the last type that
// happened to parse. Carrier types are not modelled in v1, so CarrierType
// stays "".
func (r *FieldRow) Shape() goplan.FieldShape {
	if !r.IsConst && (r.typeModel.BarError() != "" || !r.typeModel.Valid()) {
		return goplan.FieldShape{IsOption: r.IsOption}
	}
	return goplan.FieldShape{
		Canonical: r.typeModel.Node(),
		IsOption:  r.IsOption,
	}
}

// SetGoType seeds the row's value type from a Go source-type spelling — a
// convenience for examples and the default new-row type that mirror a Go DTO;
// the editor itself authors the canonical directly. An unmapped spelling leaves
// the type empty (the editor then shows it invalid).
func (r *FieldRow) SetGoType(goType string) {
	cn, err := goplan.ScalarCanonicalForGoType(goType)
	if err != nil {
		return
	}
	r.typeModel.SetCanonical(cn.String())
	r.lastCanonical = r.typeModel.Canonical()
}

// Model is the editable state of the playground: the plan identity, the
// ordered field rows, and the most recent preview the host computed.
type Model struct {
	Kind        string // entity kind from the `_` field's kind: tag
	PackageName string // DTO package (header cosmetics in the preview)
	KindType    string // DTO struct type name

	Fields []*FieldRow

	nextUID uint64

	// panes are the generated output artifacts shown as dock tabs, set by the
	// host's Recompute via SetOutputs; ErrText/Valid carry the verdict.
	ErrText string // PlanBuilder / emit error when !Valid
	Valid   bool
	panes   []outputPane

	dirty   bool   // an edit (or the initial seed) needs a Recompute
	viewBuf string // stable backing string for the read-only error TextEdit

	// pager paginates the field list — the shared widget extracted from
	// apps/play, configured for a short list: a small fixed page (cards don't
	// virtualise, so a page must fit the editor pane), no page-size combo,
	// "fields" unit.
	pager *pager.Pager

	// planFSM / planFSMW are the plan-level compile-pipeline state machine and
	// its tethered inspector chip (shown beside the verdict); planFSMW is built
	// lazily on first render. queryable is the host's read-back-availability
	// signal (the dql SQL artefacts in the demo) feeding PlanQueryable vs
	// PlanSchemaMismatch.
	planFSM   *fsmview.Machine[PlanState]
	planFSMW  *fsmview.Widget[PlanState]
	queryable bool
}

// NewModel returns an empty Model marked dirty so the first frame computes a
// preview. Seed it with AddRow.
func NewModel(kind, packageName, kindType string) *Model {
	return &Model{
		Kind: kind, PackageName: packageName, KindType: kindType, dirty: true,
		pager:   pager.New(c.NewWidgetIdStack(), 3).WithUnit("fields").WithPageSizeCombo(false),
		planFSM: newPlanFSM(),
	}
}

// AddRow appends a fresh row with a stable uid and returns it for the caller
// to populate. Marks the model dirty.
func (m *Model) AddRow() *FieldRow {
	m.nextUID++
	r := &FieldRow{uid: m.nextUID, typeModel: canonicaltypeedit.NewModel(), fsm: newFieldFSM()}
	r.SetGoType("uint64") // sensible default value type
	m.Fields = append(m.Fields, r)
	m.dirty = true
	return r
}

// removeByUID drops the row with the given uid, marking the model dirty.
func (m *Model) removeByUID(uid uint64) {
	for i, r := range m.Fields {
		if r.uid == uid {
			m.Fields = append(m.Fields[:i], m.Fields[i+1:]...)
			m.dirty = true
			return
		}
	}
}

// AddElem appends a fresh element to a tuple row (with a stable uid from the
// model's shared counter) and returns it for the caller to populate. Marks
// the model dirty. The element defaults to a value field; flip IsMembership
// (and pick a verbatim Channel) for the membership element.
func (m *Model) AddElem(r *FieldRow) *TupleElemRow {
	m.nextUID++
	e := &TupleElemRow{
		uid:       m.nextUID,
		Channel:   mappingplan.MembershipChannelLowCardVerbatim,
		typeModel: canonicaltypeedit.NewModel(),
	}
	e.SetGoType("string") // sensible default element value type
	r.TupleElems = append(r.TupleElems, e)
	m.dirty = true
	return e
}

// removeElemByUID drops the element with the given uid from the tuple row,
// marking the model dirty.
func (m *Model) removeElemByUID(r *FieldRow, uid uint64) {
	for i, e := range r.TupleElems {
		if e.uid == uid {
			r.TupleElems = append(r.TupleElems[:i], r.TupleElems[i+1:]...)
			m.dirty = true
			return
		}
	}
}

// hasMembershipElem reports whether the tuple row already declares its
// `@membership` element — gating the editor's add-membership affordance
// (a tuple carries exactly one).
func (r *FieldRow) hasMembershipElem() bool {
	for _, e := range r.TupleElems {
		if e.IsMembership {
			return true
		}
	}
	return false
}

// OutputLang selects the codeview syntax highlighter for an output pane.
type OutputLang uint8

const (
	LangGo OutputLang = iota
	LangSQL
	LangJSON
)

// Output is one generated artifact the host hands the widget to show as a dock
// tab. TabID must be stable across frames — it keys the persistent dock layout.
// Adding a new output format (e.g. the dql SQL artefacts) is just another
// Output; the widget code is format-agnostic.
type Output struct {
	TabID  uint64
	Title  string
	Lang   OutputLang
	Source string
}

// outputPane pairs a host-declared Output with its built (highlighted) codeview
// job.
type outputPane struct {
	out Output
	job typed.RetainedFffiHolderTyped[c.CodeViewJobS]
}

// buildJob highlights src with the codeview highlighter for lang.
func buildJob(lang OutputLang, src string) typed.RetainedFffiHolderTyped[c.CodeViewJobS] {
	switch lang {
	case LangSQL:
		return codeview.BuildSql(src)
	case LangJSON:
		return codeview.BuildJson(src)
	default:
		return codeview.BuildGo(src)
	}
}

// SetOutputs records a successful recompute: the generated output panes and a
// valid verdict. The highlighted codeview job per pane is built here — the
// recompute is dirty-gated, so once per edit, not per frame, and c.CodeView
// splices each job's bytes into the frame (no retained-element accumulation).
// Called by the host's Recompute.
func (m *Model) SetOutputs(outs ...Output) {
	m.panes = m.panes[:0]
	for _, o := range outs {
		m.panes = append(m.panes, outputPane{out: o, job: buildJob(o.Lang, o.Source)})
	}
	m.ErrText = ""
	m.Valid = true
}

// SetInvalid records a failed recompute: the error text, no source, invalid
// verdict. Called by the host's Recompute.
func (m *Model) SetInvalid(err error) {
	m.panes = m.panes[:0]
	if err != nil {
		m.ErrText = err.Error()
	} else {
		m.ErrText = "invalid plan"
	}
	m.Valid = false
	m.queryable = false // a plan that doesn't build isn't queryable
}

// FieldState is one field's validity standing — the state space of its
// per-field [fsmview.Machine]. Empty / Incomplete are decided by the widget
// from the row alone; Valid / Rejected / Conflicting / Blocked come from the
// host's sequential build report ([BuildResult]). See [deriveState].
type FieldState uint8

const (
	// StateEmpty is the zero value so a fresh row's machine starts here.
	StateEmpty       FieldState = iota // nothing authored yet
	StateIncomplete                    // partially authored, not buildable on its own
	StateValid                         // accepted by PlanBuilder in the full sequence
	StateRejected                      // rejected for this field's own shape / tag
	StateConflicting                   // rejected for clashing with another field
	StateBlocked                       // an earlier field stopped the build before this one
)

// fieldStateOrder pins the level-2 table / graph display order to the natural
// validity progression rather than iota-by-accident.
var fieldStateOrder = []FieldState{
	StateEmpty, StateIncomplete, StateValid, StateRejected, StateConflicting, StateBlocked,
}

// label is the chip / graph label for a state (lowercase, matching the editor's
// terse aesthetic). The [fsmview.WithLabel] hook.
func (s FieldState) label() string {
	switch s {
	case StateEmpty:
		return "empty"
	case StateIncomplete:
		return "incomplete"
	case StateValid:
		return "valid"
	case StateRejected:
		return "rejected"
	case StateConflicting:
		return "conflict"
	case StateBlocked:
		return "blocked"
	}
	return "?"
}

// tone maps a state to the level-1 badge tone. Conflict takes the accent role
// rather than error-red so it reads distinctly from a plain Rejected; Empty and
// Blocked stay neutral. The [fsmview.Widget.BadgeTone] hook.
func (s FieldState) tone() badge.ToneE {
	switch s {
	case StateValid:
		return badge.ToneSuccess
	case StateIncomplete:
		return badge.ToneWarning
	case StateRejected:
		return badge.ToneError
	case StateConflicting:
		return badge.TonePrimary
	default: // StateEmpty, StateBlocked
		return badge.ToneNeutral
	}
}

// stateColor is the [fsmview.WithStateColor] hook for the level-2 graph / table:
// the field's current state lights in its severity colour, every other node sits
// muted, so the graph reads as "here is where this field is" against the lattice.
func stateColor(s FieldState, isCurrent bool) styletokens.RGBA8 {
	if !isCurrent {
		return styletokens.NeutralSubtle
	}
	switch s {
	case StateValid:
		return styletokens.SuccessDefault
	case StateIncomplete:
		return styletokens.WarningDefault
	case StateRejected:
		return styletokens.ErrorDefault
	case StateConflicting:
		return styletokens.AccentDefault
	default: // StateEmpty, StateBlocked
		return styletokens.NeutralTextSecondary
	}
}

// fieldFSMHistory caps each field machine's transition log — enough to read the
// recent edit story in the inspector's History tab without unbounded growth.
const fieldFSMHistory = 24

// newFieldFSM builds a fresh per-field validity machine starting at StateEmpty.
// The declared edges are the natural validity lattice the level-2 graph draws;
// edits are arbitrary, so the per-frame driver uses
// [fsmview.Machine.MirrorWithMetadata] (never errors, records the real path in
// history) for any jump the lattice does not declare.
func newFieldFSM() *fsmview.Machine[FieldState] {
	m := fsmview.NewMachine(StateEmpty, fieldFSMHistory,
		fsmview.WithLabel(FieldState.label),
		fsmview.WithStateOrder(fieldStateOrder),
		fsmview.WithStateColor(stateColor),
	)
	m.AddRule(StateEmpty, StateIncomplete, StateValid)
	m.AddRule(StateIncomplete, StateValid, StateRejected, StateConflicting)
	m.AddRule(StateValid, StateRejected, StateConflicting, StateBlocked)
	m.AddRule(StateRejected, StateValid, StateConflicting)
	m.AddRule(StateConflicting, StateValid, StateRejected)
	m.AddRule(StateBlocked, StateValid, StateRejected, StateConflicting)
	return m
}

// BuildResult is the host's report of one sequential PlanBuilder pass over the
// model's fields — the input each per-field machine derives its plan-standing
// from. The host owns the build (and the marshallgen / dql back-ends); the
// widget only turns this report into states, so no validation rules are
// reimplemented here.
type BuildResult struct {
	// FirstFailIdx is the index into Model.Fields of the first field AddField
	// rejected, or -1 when every AddField succeeded. PlanBuilder is fail-fast
	// and stateful: fields before it were accepted (→ Valid), the field at it is
	// Rejected / Conflicting, and fields after it were never reached (→ Blocked).
	FirstFailIdx int
	// FirstFailErr is the AddField error at FirstFailIdx (nil when FirstFailIdx
	// is -1). Its message classifies Rejected vs Conflicting and seeds the chip
	// reason + inspector History.
	FirstFailErr error
	// FinishErr is set when every AddField passed but Finish failed — a
	// plan-level cross-field rejection that does not pin to a single field, so
	// it is surfaced in the global verdict rather than per-field. nil otherwise.
	FinishErr error
}

// SetBuildResult records the host's build report and refreshes every field's
// derived validity state + reason. Called by the host's Recompute alongside
// SetOutputs / SetInvalid; the per-frame render then mirrors each row's machine
// to its refreshed state.
func (m *Model) SetBuildResult(r BuildResult) {
	for i, row := range m.Fields {
		row.state, row.reason = deriveState(row, i, r)
	}
}

// stateRollup summarises the per-field states as "3 valid · 1 conflict · 2
// blocked", in the natural display order, omitting states with no fields. Empty
// when there are no fields. Feeds the editor's global verdict line.
func (m *Model) stateRollup() string {
	if len(m.Fields) == 0 {
		return ""
	}
	counts := make(map[FieldState]int, len(fieldStateOrder))
	for _, r := range m.Fields {
		counts[r.state]++
	}
	var sb strings.Builder
	for _, s := range fieldStateOrder {
		n := counts[s]
		if n == 0 {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteString(" · ")
		}
		fmt.Fprintf(&sb, "%d %s", n, s.label())
	}
	return sb.String()
}

// deriveState is the pure reconciliation at the heart of the per-field machine:
// widget-owned local readiness (Empty / Incomplete) takes precedence — it is the
// most actionable thing to tell the user — and otherwise the row's plan-standing
// comes from the build report. Pure and host-free, so it is unit tested directly.
func deriveState(row *FieldRow, idx int, r BuildResult) (FieldState, string) {
	if rowIsEmpty(row) {
		return StateEmpty, ""
	}
	if reason := rowIncompleteReason(row); reason != "" {
		return StateIncomplete, reason
	}
	switch {
	case r.FirstFailIdx < 0 || idx < r.FirstFailIdx:
		// Accepted by AddField. A plan-level Finish error (r.FinishErr) is not
		// pinned here — it shows in the global verdict — so the field is Valid.
		return StateValid, ""
	case idx == r.FirstFailIdx:
		reason := ""
		if r.FirstFailErr != nil {
			reason = firstLine(r.FirstFailErr.Error())
		}
		if classifyConflict(r.FirstFailErr) {
			return StateConflicting, reason
		}
		return StateRejected, reason
	default: // idx > r.FirstFailIdx: a fail-fast builder never reached this field
		return StateBlocked, blockedReason(r.FirstFailIdx)
	}
}

// rowIsEmpty reports whether a row carries no authored content yet — a fresh
// AddRow (whose only content is the seeded default value type) or one cleared
// back out. The default canonical alone does not count as content.
func rowIsEmpty(row *FieldRow) bool {
	if row.IsTuple {
		return row.GoField == "" && row.Section == "" && row.TupleStructType == "" && len(row.TupleElems) == 0
	}
	return row.GoField == "" && row.Membership == "" && row.Section == "" && row.ConstValue == ""
}

// rowIncompleteReason returns why a non-empty row is not yet buildable on its
// own, or "" when it is ready to hand to the builder. These mirror prerequisites
// PlanBuilder would reject, surfaced per-field and earlier so the chip reads
// "incomplete: <why>" instead of the field going Blocked behind a builder error.
// Only local *readiness* is decided here (missing names, unparsed types);
// structural rules (one membership element, at least one value element, …)
// stay with the genuine builder, whose rejection the chip then reports.
func rowIncompleteReason(row *FieldRow) string {
	if row.IsTuple {
		return tupleIncompleteReason(row)
	}
	if _, err := goplan.SplitLW(row.LWTag()); err != nil {
		return "lw tag does not parse"
	}
	if row.IsConst {
		if row.Membership == "" || row.Section == "" {
			return "const needs a membership and a section"
		}
		if row.ConstValue == "" {
			return "const needs a value"
		}
		return ""
	}
	if row.typeModel.BarError() != "" {
		return "value type does not parse"
	}
	if !row.typeModel.Valid() {
		return "value type is empty or invalid"
	}
	if row.Membership == "" {
		if row.Section == "" {
			return "plain column needs a name"
		}
		return ""
	}
	if row.Section == "" {
		return "tagged field needs a section"
	}
	if row.GoField == "" {
		return "value field needs a Go field name"
	}
	return ""
}

// tupleIncompleteReason is rowIncompleteReason's tuple arm: the local
// readiness prerequisites of a dynamic-membership tuple row (ADR-0103).
func tupleIncompleteReason(row *FieldRow) string {
	if row.GoField == "" {
		return "tuple field needs a Go field name"
	}
	if row.Section == "" {
		return "tuple needs a section"
	}
	if row.TupleStructType == "" {
		return "tuple needs an element struct name"
	}
	if len(row.TupleElems) == 0 {
		return "tuple needs elements (one @membership + value fields)"
	}
	for _, e := range row.TupleElems {
		if e.GoField == "" {
			if e.IsMembership {
				return "@membership element needs a Go field name"
			}
			return "a value element needs a Go field name"
		}
		if e.IsMembership {
			continue
		}
		if e.typeModel.BarError() != "" {
			return "element " + e.GoField + ": value type does not parse"
		}
		if !e.typeModel.Valid() {
			return "element " + e.GoField + ": value type is empty or invalid"
		}
	}
	return ""
}

// blockedReason explains a Blocked field: a fail-fast builder never reached it.
func blockedReason(failIdx int) string {
	return fmt.Sprintf("an earlier field (#%d) stopped the build", failIdx+1)
}

// crossFieldPhrases are substrings mappingplan's *cross-field* rejections carry
// — errors that fault a field for clashing with ANOTHER field rather than for
// its own shape / tag. A match promotes Rejected to Conflicting. Curated from
// mappingplan/build.go's error vocabulary; the conflict behaviour test builds
// real plans that trip each, so a reworded message fails that test instead of
// silently misclassifying.
var crossFieldPhrases = []string{
	"two DTO fields",                    // duplicate plain column / membership+column
	"two carrier fields share",          // duplicate carrier membership+section
	"section mixes membership channels", // a section's fields disagree on channel
	"share a ref-channel membership",    // const + value ref-symbol collision
	"may carry only one membership",     // carrier section with >1 membership
	"needs a sibling carrier field",     // mixed/parametrized value with no carrier
	"declare different channels",        // value / carrier channel mismatch
	"carrier multiplicity must match",   // value / carrier shape mismatch
	"has no value sibling",              // carrier with no value field
}

// classifyConflict reports whether a builder rejection is a cross-field conflict
// (→ Conflicting) rather than a fault in the field itself (→ Rejected).
func classifyConflict(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, p := range crossFieldPhrases {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}

// PlanState is the whole plan's standing in the compile pipeline — the state
// space of the plan-level [fsmview.Machine] shown beside the verdict. It tracks
// how far the plan gets through build → marshal → query:
//
//   - Empty: no fields.
//   - Incomplete: a field isn't ready (Empty/Incomplete), so the plan can't build.
//   - Invalid: the build failed — a field is rejected/conflicting, or Finish
//     (cross-field) / emit failed; there is no usable Plan.
//   - SchemaMismatch: a valid Plan that emits a Go codec + Plan IR, but the SQL
//     read-back does not generate against the bound schema (Plan ⊄ schema,
//     ADR-0066) — built, but not queryable.
//   - Queryable: the whole pipeline succeeds — builds, emits, AND the read-back
//     generates.
//
// The intermediate build/emit micro-stages are not separate states: recompute
// runs the pipeline synchronously, so the plan settles directly into one of
// these terminal conditions each edit. SchemaMismatch vs Queryable needs a host
// signal (the host owns the read-back); see [Model.SetQueryable].
type PlanState uint8

const (
	PlanEmpty PlanState = iota
	PlanIncomplete
	PlanInvalid
	PlanSchemaMismatch
	PlanQueryable
)

// planStateOrder pins the level-2 table / graph order to the pipeline flow.
var planStateOrder = []PlanState{PlanEmpty, PlanIncomplete, PlanInvalid, PlanSchemaMismatch, PlanQueryable}

func (s PlanState) label() string {
	switch s {
	case PlanEmpty:
		return "empty"
	case PlanIncomplete:
		return "incomplete"
	case PlanInvalid:
		return "invalid"
	case PlanSchemaMismatch:
		return "schema-mismatch"
	case PlanQueryable:
		return "queryable"
	}
	return "?"
}

func (s PlanState) tone() badge.ToneE {
	switch s {
	case PlanQueryable:
		return badge.ToneSuccess
	case PlanIncomplete:
		return badge.ToneWarning
	case PlanInvalid:
		return badge.ToneError
	case PlanSchemaMismatch:
		return badge.ToneInfo // valid plan, just not queryable — informational, not an error
	default: // PlanEmpty
		return badge.ToneNeutral
	}
}

func planStateColor(s PlanState, isCurrent bool) styletokens.RGBA8 {
	if !isCurrent {
		return styletokens.NeutralSubtle
	}
	switch s {
	case PlanQueryable:
		return styletokens.SuccessDefault
	case PlanIncomplete:
		return styletokens.WarningDefault
	case PlanInvalid:
		return styletokens.ErrorDefault
	case PlanSchemaMismatch:
		return styletokens.InfoDefault
	default: // PlanEmpty
		return styletokens.NeutralTextSecondary
	}
}

// newPlanFSM builds the plan-level machine. The declared lattice is the
// pipeline's happy path (empty → incomplete → schema-mismatch → queryable) plus
// the failure branches; edits are arbitrary, so the per-frame driver mirrors
// (never errors) for any undeclared jump.
func newPlanFSM() *fsmview.Machine[PlanState] {
	m := fsmview.NewMachine(PlanEmpty, fieldFSMHistory,
		fsmview.WithLabel(PlanState.label),
		fsmview.WithStateOrder(planStateOrder),
		fsmview.WithStateColor(planStateColor),
	)
	m.AddRule(PlanEmpty, PlanIncomplete, PlanSchemaMismatch, PlanQueryable)
	m.AddRule(PlanIncomplete, PlanInvalid, PlanSchemaMismatch, PlanQueryable)
	m.AddRule(PlanInvalid, PlanIncomplete, PlanSchemaMismatch, PlanQueryable)
	m.AddRule(PlanSchemaMismatch, PlanQueryable, PlanInvalid, PlanIncomplete)
	m.AddRule(PlanQueryable, PlanSchemaMismatch, PlanInvalid, PlanIncomplete)
	return m
}

// SetQueryable records whether the host's read-back (the dql SQL artefacts in
// the demo) generated for the current plan — it refines a valid plan into
// PlanQueryable (true) vs PlanSchemaMismatch (false). The host's Recompute calls
// it on the success path; SetInvalid resets it. A host with no read-back stage
// simply never sets it, so its valid plans read SchemaMismatch.
func (m *Model) SetQueryable(b bool) { m.queryable = b }

// planState derives the plan-level pipeline state + a reason from the per-field
// states (set by SetBuildResult), the global verdict, and the queryable signal.
// Pure; driven into the plan machine each frame by renderVerdict.
func (m *Model) planState() (PlanState, string) {
	if len(m.Fields) == 0 {
		return PlanEmpty, ""
	}
	notReady := 0
	for _, r := range m.Fields {
		if r.state == StateEmpty || r.state == StateIncomplete {
			notReady++
		}
	}
	if notReady > 0 {
		return PlanIncomplete, fmt.Sprintf("%d field(s) not ready", notReady)
	}
	if !m.Valid {
		return PlanInvalid, firstLine(m.ErrText)
	}
	if m.queryable {
		return PlanQueryable, ""
	}
	return PlanSchemaMismatch, "valid plan; SQL read-back unavailable for the bound schema"
}
