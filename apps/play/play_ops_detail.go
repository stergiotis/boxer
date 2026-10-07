package play

// play_ops_detail.go: get_detail, the Detail pane's reading of one row for
// an agent (ADR-0270, update of 2026-10-05). The pane draws through a card
// emitter that lives on the render goroutine; the read builds its own
// Driver from the result's column names (discoverCardRecipe, the same
// derivation the pane's CardDriver makes) and drives one row into a sink
// that keeps text, on the query's goroutine. Nothing the pane holds is
// touched, so the read neither needs the pane raised nor disturbs it.

import (
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/membership"
	"github.com/stergiotis/boxer/public/semistructured/leeway/membershiprole"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

const (
	opGetDetail  = "get_detail"
	detailPaneId = "detail"
)

// Bounds on get_detail.
const (
	detailOpsMaxBytes = 8 << 10
	detailOpsMaxValue = 300
)

// detailOpsView is what get_detail reads of the window besides the result.
type detailOpsView struct {
	// embedded is true when an embedder replaced the pane's body.
	embedded bool
}

// DetailArgs is get_detail's argument.
type DetailArgs struct {
	Row  *int64 `json:",omitzero" desc:"the row to read, 0 for the first; the selection when left out, as the Detail pane draws it"`
	Node string `json:",omitzero" desc:"the split node whose row to read; the node the Detail pane follows (selection_node, or its binding) when left out"`
}

// DetailValue is one named value of a row.
type DetailValue struct {
	Name  string `desc:"the value column's name; for a result that is not leeway-shaped, the column's short label"`
	Value string `desc:"the value as text, cut at 300 bytes; a list as [a, b, …]"`
}

// DetailAttribute is one attribute of a section: its values and, in a
// tagged section, the memberships that tag it.
type DetailAttribute struct {
	Values    []DetailValue `desc:"its values"`
	Primary   []string      `json:",omitzero" desc:"the memberships that name the attribute"`
	Secondary []string      `json:",omitzero" desc:"the memberships that annotate it"`
}

// DetailSection is one section of a row.
type DetailSection struct {
	Name       string            `desc:"the section"`
	Kind       string            `desc:"plain (one value per row), tagged (attributes carrying memberships) or, for a result that is not leeway-shaped, the column group the pane draws (pinned, relations, data, meta)"`
	CoGroup    string            `json:",omitzero" desc:"the co-section group the tagged section belongs to"`
	Attributes []DetailAttribute `json:",omitzero" desc:"its attributes, in the order the card draws them"`
	More       int               `json:",omitzero" desc:"attributes the byte bound left out"`
}

// DetailIdentity is a row's canonical identity, as the pane's identity strip
// shows it.
type DetailIdentity struct {
	Canonform string `desc:"the canonform digest, hex"`
	Canonwire string `desc:"the canonwire fingerprint, hex"`
	Canonical bool   `desc:"true when the canonical wire item passes the runtime's check"`
	Verdict   string `json:",omitzero" desc:"why it is not canonical"`
	WireBytes int    `desc:"the wire item's size in bytes"`
	Error     string `json:",omitzero" desc:"why no identity could be computed"`
}

// DetailTemporal is one temporal attribute of a row.
type DetailTemporal struct {
	Label   string `desc:"the attribute"`
	Section string `json:",omitzero" desc:"its leeway section"`
	Kind    string `desc:"instants or intervals"`
	Count   int    `desc:"how many instants or intervals"`
	From    string `json:",omitzero" desc:"the earliest, UTC"`
	To      string `json:",omitzero" desc:"the latest, UTC"`
	Summary string `desc:"the pane's legend line for it"`
}

// DetailReading is get_detail's result.
type DetailReading struct {
	ResultId         uint64           `desc:"the result the row is of"`
	Node             string           `desc:"the split node the result is of"`
	Row              int64            `desc:"the row read"`
	Rows             int64            `desc:"the result's row count"`
	Entity           string           `json:",omitzero" desc:"the entity type the row's tagged id names"`
	NaturalKey       string           `json:",omitzero" desc:"the row's natural key"`
	Leeway           bool             `desc:"true when the result is leeway-shaped and the sections are its own"`
	Sections         []DetailSection  `json:",omitzero" desc:"the row's sections in card order; for a result that is not leeway-shaped, its non-empty columns grouped as the pane groups them"`
	Identity         *DetailIdentity  `json:",omitzero" desc:"the row's canonical identity, for a leeway result"`
	Temporal         []DetailTemporal `json:",omitzero" desc:"the row's temporal attributes, as the pane's time strip draws them"`
	TemporalDropped  int              `json:",omitzero" desc:"marks the strip's density bound left out"`
	Embedded         bool             `json:",omitzero" desc:"true when the window replaced the Detail pane's body: the person sees the embedder's view, not this reading"`
	Truncated        bool             `json:",omitzero" desc:"true when the byte bound left attributes out (each section's more counts them)"`
	TruncationReason string           `json:",omitzero" desc:"which bound cut it"`
	Note             string           `json:",omitzero" desc:"what the reading leaves out"`
}

const detailOpsNote = "values are the driver's text, before the glosses the pane draws them through; typed components are not read"

func addDetailOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	// Untrusted: every value is the data's.
	appops.Query(s, app.OperationSpec{Name: opGetDetail, Version: 1,
		Summary: "read one row as the Detail pane does: its sections, each attribute's values and memberships, its canonical identity and its temporal attributes",
		Reads:   []string{opsResResult, opsResSignals, opsResPanes}, Agents: true, Untrusted: true,
		Follows: []string{"the Detail pane is unchanged; set_signal selection moves the row it draws"}},
		func(sn opsSnap, in DetailArgs) (DetailReading, error) {
			if !sn.mounted {
				return DetailReading{}, app.RefuseOperation("the window has not mounted")
			}
			return getDetail(&sn.results, sn.state.Signals, sn.detail, in)
		})
	addCanonicalOps(s)
}

// getDetail reads one row of the result the Detail pane follows, or of a
// node.
func getDetail(r *opsResults, signals []SignalState, v detailOpsView, in DetailArgs) (out DetailReading, err error) {
	pane := ""
	if in.Node == "" {
		pane = detailPaneId
	}
	lr, err := r.read(pane, in.Node)
	if err != nil {
		return
	}
	defer lr.release()
	if lr.rec == nil || lr.schema == nil {
		return out, app.RefuseOperation("node " + string(lr.node) + " holds no result")
	}
	row, err := opsRow(signals, in.Row, lr.numRows)
	if err != nil {
		return
	}
	out = DetailReading{ResultId: uint64(lr.id), Node: string(lr.node), Row: row, Rows: lr.numRows, Embedded: v.embedded, Note: detailOpsNote}
	out.Entity, out.NaturalKey = entityHeader(lr.rec, row)
	if out.Entity == "<not-a-tagged-id>" {
		out.Entity = ""
	}
	recipe, leeway := discoverCardRecipe(lr.schema)
	attrs, dropped := detectTemporalAttrs(lr.rec, lr.schema, row, recipe.classes)
	out.TemporalDropped = dropped
	for _, a := range attrs {
		t := DetailTemporal{Label: a.label, Section: a.section, Kind: "instants", Count: len(a.points), Summary: a.summary()}
		if a.kind == kindIntervals {
			t.Kind, t.Count = "intervals", len(a.spans)
		}
		if lo, hi, ok := a.extent(); ok {
			t.From, t.To = formatEpochMS(lo), formatEpochMS(hi)
		}
		out.Temporal = append(out.Temporal, t)
	}
	var sections []DetailSection
	if leeway {
		out.Leeway = true
		sink := &detailSink{renderer: membership.DefaultRenderer(), classifier: membershiprole.PathPrefixClassifier{}}
		slice := lr.rec.NewSlice(row, row+1)
		derr := recipe.driver.DriveRecordBatch(sink, slice)
		slice.Release()
		if derr != nil {
			return out, app.RefuseOperation("the row did not read as leeway: " + truncateRunes(derr.Error(), 300))
		}
		sections = sink.sections
		out.Identity = detailIdentity(recipe, lr.rec, row)
	} else {
		sections = adHocDetailSections(lr.rec, lr.schema, row)
	}
	out.Sections, out.Truncated = boundDetailSections(sections, detailOpsMaxBytes)
	if out.Truncated {
		out.TruncationReason = "the reading's byte bound (" + strconv.Itoa(detailOpsMaxBytes>>10) + " KiB of values)"
	}
	return out, nil
}

// detailIdentity computes the row's canonform digest and canonwire
// fingerprint as the pane's identity strip does.
func detailIdentity(recipe cardRecipe, rec arrow.RecordBatch, row int64) *DetailIdentity {
	comp, err := newIdentityComputer(recipe.table, recipe.ir, recipe.driver)
	if err != nil {
		return &DetailIdentity{Error: truncateRunes(err.Error(), 300)}
	}
	vals, err := comp.row(rec, row)
	if err != nil {
		return &DetailIdentity{Error: truncateRunes(err.Error(), 300)}
	}
	id := &DetailIdentity{Canonform: hex.EncodeToString(vals.canon[:]), Canonwire: hex.EncodeToString(vals.wire[:]),
		Canonical: vals.wireErr == nil, WireBytes: vals.wireLen}
	if vals.wireErr != nil {
		id.Verdict = truncateRunes(vals.wireErr.Error(), 300)
	}
	return id
}

// adHocDetailSections groups a non-leeway row's non-empty columns as the
// pane's ad-hoc view does.
func adHocDetailSections(rec arrow.RecordBatch, schema *arrow.Schema, row int64) (out []DetailSection) {
	for _, g := range []struct{ section, heading string }{
		{sectionPlain, "pinned"}, {sectionForeignKey, "relations"}, {sectionData, "data"}, {sectionRare, "meta"}} {
		sec := DetailSection{Name: g.heading, Kind: g.heading}
		for i := 0; i < schema.NumFields(); i++ {
			name := schema.Field(i).Name
			if sectionForColumn(name) != g.section {
				continue
			}
			val := formatDisplayCell(rec.Column(i), row)
			if val == "" || val == "[len=0]" {
				continue
			}
			sec.Attributes = append(sec.Attributes, DetailAttribute{Values: []DetailValue{{Name: shortColumnLabel(name),
				Value: truncateBytes(strings.Clone(val), detailOpsMaxValue)}}})
		}
		if len(sec.Attributes) > 0 {
			out = append(out, sec)
		}
	}
	return
}

// boundDetailSections keeps attributes in card order until the byte budget
// is spent; each section counts the attributes it lost.
func boundDetailSections(in []DetailSection, budget int) (out []DetailSection, truncated bool) {
	used := 0
	for _, sec := range in {
		kept := sec
		kept.Attributes = nil
		for _, a := range sec.Attributes {
			cost := 16
			for _, v := range a.Values {
				cost += len(v.Name) + len(v.Value) + 8
			}
			for _, m := range a.Primary {
				cost += len(m) + 4
			}
			for _, m := range a.Secondary {
				cost += len(m) + 4
			}
			if truncated || used+cost > budget {
				truncated = true
				kept.More++
				continue
			}
			used += cost
			kept.Attributes = append(kept.Attributes, a)
		}
		out = append(out, kept)
	}
	return
}

// detailSink is a streamreadaccess sink keeping one entity's sections as
// text: plain sections, then tagged sections with each attribute's values
// and memberships split by role as the card's emitter splits them.
type detailSink struct {
	renderer   *membership.Renderer
	classifier membershiprole.ClassifierI

	sections []DetailSection
	cur      *DetailSection
	attr     *DetailAttribute
	coGroup  string
	ctx      membershiprole.SectionContext

	col     string
	items   []string
	text    strings.Builder
	inItems bool
	inValue bool
}

var (
	_ streamreadaccess.SinkI           = (*detailSink)(nil)
	_ streamreadaccess.MembershipSinkI = (*detailSink)(nil)
)

func (inst *detailSink) BeginBatch()              {}
func (inst *detailSink) EndBatch() error          { return nil }
func (inst *detailSink) BeginEntity()             {}
func (inst *detailSink) EndEntity() error         { return nil }
func (inst *detailSink) BeginTaggedSections()     {}
func (inst *detailSink) EndTaggedSections() error { return nil }

func (inst *detailSink) BeginPlainSection(itemType common.PlainItemTypeE, _ []naming.StylableName, _ []canonicaltypes.PrimitiveAstNodeI, _ int) {
	inst.sections = append(inst.sections, DetailSection{Name: itemType.String(), Kind: "plain"})
	inst.cur = &inst.sections[len(inst.sections)-1]
}
func (inst *detailSink) EndPlainSection() error { inst.cur = nil; return nil }
func (inst *detailSink) BeginPlainValue() {
	if inst.cur != nil {
		inst.cur.Attributes = append(inst.cur.Attributes, DetailAttribute{})
		inst.attr = &inst.cur.Attributes[len(inst.cur.Attributes)-1]
	}
}
func (inst *detailSink) EndPlainValue() error { inst.attr = nil; return nil }

func (inst *detailSink) BeginCoSectionGroup(name naming.Key) { inst.coGroup = name.String() }
func (inst *detailSink) EndCoSectionGroup() error            { inst.coGroup = ""; return nil }

func (inst *detailSink) BeginSection(name naming.StylableName, _ []naming.StylableName, _ []canonicaltypes.PrimitiveAstNodeI, aspects useaspects.AspectSet, _ int) {
	inst.sections = append(inst.sections, DetailSection{Name: name.String(), Kind: "tagged", CoGroup: inst.coGroup})
	inst.cur = &inst.sections[len(inst.sections)-1]
	inst.ctx = membershiprole.SectionContext{Name: name, UseAspects: aspects}
}
func (inst *detailSink) EndSection() error { inst.cur = nil; return nil }
func (inst *detailSink) BeginTaggedValue() {
	if inst.cur != nil {
		inst.cur.Attributes = append(inst.cur.Attributes, DetailAttribute{})
		inst.attr = &inst.cur.Attributes[len(inst.cur.Attributes)-1]
	}
}
func (inst *detailSink) EndTaggedValue() error { inst.attr = nil; return nil }

func (inst *detailSink) BeginColumn(_ streamreadaccess.PhysicalColumnAddr, name naming.StylableName, _ canonicaltypes.PrimitiveAstNodeI, _ valueaspects.AspectSet) {
	inst.col = name.String()
	inst.items = inst.items[:0]
	inst.text.Reset()
	inst.inItems = false
}
func (inst *detailSink) EndColumn() {
	if inst.attr == nil {
		return
	}
	val := inst.text.String()
	if inst.inItems {
		val = "[" + strings.Join(inst.items, ", ") + "]"
	}
	inst.attr.Values = append(inst.attr.Values, DetailValue{Name: inst.col, Value: truncateBytes(val, detailOpsMaxValue)})
}

func (inst *detailSink) BeginScalarValue()             { inst.text.Reset(); inst.inValue = true }
func (inst *detailSink) EndScalarValue() error         { inst.inValue = false; return nil }
func (inst *detailSink) BeginHomogenousArrayValue(int) { inst.inItems = true }
func (inst *detailSink) EndHomogenousArrayValue()      {}
func (inst *detailSink) BeginSetValue(int)             { inst.inItems = true }
func (inst *detailSink) EndSetValue()                  {}
func (inst *detailSink) BeginValueItem(int)            { inst.text.Reset(); inst.inValue = true }
func (inst *detailSink) EndValueItem() {
	inst.inValue = false
	// Items past what any value can show are not kept.
	if len(inst.items) < 64 {
		inst.items = append(inst.items, inst.text.String())
	}
}

func (inst *detailSink) Write(p []byte) (int, error) {
	if inst.text.Len() < detailOpsMaxValue+8 {
		inst.text.Write(p)
	}
	return len(p), nil
}
func (inst *detailSink) WriteString(s string) (int, error) {
	if inst.text.Len() < detailOpsMaxValue+8 {
		inst.text.WriteString(s)
	}
	return len(s), nil
}

func (inst *detailSink) BeginTags(int) {}
func (inst *detailSink) EndTags()      {}

func (inst *detailSink) AddMembershipRef(lowCard bool, ref uint64) {
	inst.addMembership(membership.MembershipValue{Kind: membership.IdentityRef, LowCard: lowCard, Ref: ref})
}
func (inst *detailSink) AddMembershipVerbatim(lowCard bool, verbatim string) {
	inst.addMembership(membership.MembershipValue{Kind: membership.IdentityVerbatim, LowCard: lowCard, Verbatim: verbatim})
}
func (inst *detailSink) AddMembershipRefParametrized(lowCard bool, ref uint64, params string) {
	inst.addMembership(membership.MembershipValue{Kind: membership.IdentityPerRowBlob, LowCard: lowCard, Ref: ref, Params: params})
}
func (inst *detailSink) AddMembershipMixedLowCardRefHighCardParam(ref uint64, params string) {
	inst.addMembership(membership.MembershipValue{Kind: membership.IdentityPerRowId, Ref: ref, Params: params})
}
func (inst *detailSink) AddMembershipMixedLowCardVerbatimHighCardParam(verbatim string, params string) {
	inst.addMembership(membership.MembershipValue{Kind: membership.IdentityPerRowName, Verbatim: verbatim, Params: params})
}

func (inst *detailSink) addMembership(mv membership.MembershipValue) {
	if inst.attr == nil || membership.IsPlaceholder(mv) {
		return
	}
	label := inst.renderer.Render(mv)
	if mv.Params != "" {
		label += " (" + inst.renderer.RenderParams(mv.Params) + ")"
	}
	label = truncateBytes(label, detailOpsMaxValue)
	role, _ := inst.classifier.Classify(inst.ctx, mv)
	if role == membershiprole.MembershipRoleSecondary {
		inst.attr.Secondary = append(inst.attr.Secondary, label)
		return
	}
	inst.attr.Primary = append(inst.attr.Primary, label)
}
