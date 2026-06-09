package mappingplanview

import (
	"fmt"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
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

	// fsm is this row's per-field validity state machine (Empty → … → Valid /
	// Rejected / Conflicting / Blocked); fsmW is its tethered inspector chip,
	// lazily built on first render (the [fsmview.Widget] needs the frame's id
	// stack). state / reason are the latest derived verdict, refreshed by
	// [Model.SetBuildResult] and mirrored into fsm each frame.
	fsm    *fsmview.Machine[FieldState]
	fsmW   *fsmview.Widget[FieldState]
	state  FieldState
	reason string

	// seedOpen requests the inspector window open the first frame the chip is
	// built — a convenience for demos / screenshot tours. Set via
	// [FieldRow.OpenInspector].
	seedOpen bool
}

// LWTag assembles the lw: struct-tag *value* this row represents — the string
// PlanBuilder parses via SplitLW. Plain columns produce ",<col>"; const rows
// produce "<memb>,<sec>,const=<value>"; value fields produce
// "<memb>,<sec>[:<col>][,unit][,explode][,<channel>]". The default channel
// (LowCardRef) contributes no flag, matching mappingplan.MembershipChannel.String.
func (r *FieldRow) LWTag() string {
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
// PlanBuilder.AddField. The value type is authored canonically (typeModel), so
// Shape just forwards its node; an invalid/empty editor yields a nil Canonical,
// which AddField rejects with a clear error the host surfaces. Carrier types
// are not modelled in v1, so CarrierType stays "".
func (r *FieldRow) Shape() mappingplan.FieldShape {
	return mappingplan.FieldShape{
		Canonical: r.typeModel.Node(),
		IsOption:  r.IsOption,
	}
}

// SetGoType seeds the row's value type from a Go source-type spelling — a
// convenience for examples and the default new-row type that mirror a Go DTO;
// the editor itself authors the canonical directly. An unmapped spelling leaves
// the type empty (the editor then shows it invalid).
func (r *FieldRow) SetGoType(goType string) {
	cn, err := mappingplan.ScalarCanonicalForGoType(goType)
	if err != nil {
		return
	}
	r.typeModel.SetCanonical(cn.String())
	r.lastCanonical = r.typeModel.Canonical()
}

// OpenInspector requests this field's tethered validity inspector open on the
// first frame it renders — a convenience for demos and screenshot tours. The
// user can close it afterwards like any inspector.
func (r *FieldRow) OpenInspector() { r.seedOpen = true }

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
}

// NewModel returns an empty Model marked dirty so the first frame computes a
// preview. Seed it with AddRow.
func NewModel(kind, packageName, kindType string) *Model {
	return &Model{
		Kind: kind, PackageName: packageName, KindType: kindType, dirty: true,
		pager: pager.New(c.NewWidgetIdStack(), 3).WithUnit("fields").WithPageSizeCombo(false),
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
	return row.GoField == "" && row.Membership == "" && row.Section == "" && row.ConstValue == ""
}

// rowIncompleteReason returns why a non-empty row is not yet buildable on its
// own, or "" when it is ready to hand to the builder. These mirror prerequisites
// PlanBuilder would reject, surfaced per-field and earlier so the chip reads
// "incomplete: <why>" instead of the field going Blocked behind a builder error.
func rowIncompleteReason(row *FieldRow) string {
	if _, err := mappingplan.SplitLW(row.LWTag()); err != nil {
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
