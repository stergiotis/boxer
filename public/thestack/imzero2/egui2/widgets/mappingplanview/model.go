package mappingplanview

import (
	"strings"

	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
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
	GoType  string // inner element source-form type: uint64 / string / []byte / [16]byte / time.Time / *roaring.Bitmap / ...

	IsOption  bool // option.Option[T]
	IsSlice   bool // []T element slice
	IsRoaring bool // *roaring.Bitmap

	Membership string // lw: first segment; "" ⇒ plain column
	Section    string // lw: second segment (plain column name when Membership == "")
	Column     string // lw: sub-column suffix after ':' (e.g. beginIncl / endExcl)

	Channel mappingplan.MembershipChannel // one of the four Cut-1 channels in v1
	Unit    bool                          // ,unit
	Explode bool                          // ,explode

	IsConst    bool   // declared on a `_` field as ,const=<value>
	ConstValue string // the constant value
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

// Shape returns the FieldShape this row's type bits describe, ready to hand to
// PlanBuilder.AddField. Carrier types are not modelled in v1, so CarrierType
// stays "".
func (r *FieldRow) Shape() mappingplan.FieldShape {
	return mappingplan.FieldShape{
		GoType:    r.GoType,
		IsOption:  r.IsOption,
		IsSlice:   r.IsSlice,
		IsRoaring: r.IsRoaring,
	}
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
	page    int    // current field-list page (pagination)
}

// NewModel returns an empty Model marked dirty so the first frame computes a
// preview. Seed it with AddRow.
func NewModel(kind, packageName, kindType string) *Model {
	return &Model{Kind: kind, PackageName: packageName, KindType: kindType, dirty: true}
}

// AddRow appends a fresh row with a stable uid and returns it for the caller
// to populate. Marks the model dirty.
func (m *Model) AddRow() *FieldRow {
	m.nextUID++
	r := &FieldRow{uid: m.nextUID}
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
