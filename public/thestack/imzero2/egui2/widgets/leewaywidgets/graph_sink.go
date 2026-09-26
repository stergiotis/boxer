package leewaywidgets

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/membership"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

// GraphModel is a leeway batch projected onto a node-link graph: one node
// per entity, one edge per attribute of the linking section whose value names
// another entity of the batch (ADR-0257, proposed, §SD1).
//
// The linking section is found in the data rather than declared: of the
// tagged sections with a scalar string or integer value, the one whose values
// resolve to entities of the batch most often — by natural key or by id. A
// section marked with the linking use aspect is that section by schema, but
// the aspect cannot be minted per column in SQL, so resolution is what a
// scenario can rely on. An edge's kind is its attribute's first membership.
type GraphModel struct {
	Section string
	// Nodes are entity labels in batch order; Groups each node's first
	// membership in its entity's first section that is not the linking one,
	// empty when it has none.
	Nodes  []string
	Groups []string
	Edges  []GraphEdge
	// Unresolved counts values of the linking section that named no entity
	// of the batch — references out of the batch, or a section that only
	// looked like links.
	Unresolved int
}

// GraphEdge joins two nodes by index.
type GraphEdge struct {
	From, To int
	Kind     string
}

// Empty reports whether there is nothing to draw.
func (inst *GraphModel) Empty() bool { return len(inst.Nodes) == 0 }

// refScalar matches a scalar a reference can be: a string or an integer.
var refScalar = regexp.MustCompile(`^(s|y|[ui][0-9]+)$`)

type graphRef struct {
	from  int
	value string
	kind  string
}

// GraphSink collects a GraphModel from a batch driven through it.
type GraphSink struct {
	model    GraphModel
	renderer *membership.Renderer

	ids       []string
	label     string
	labelRank int
	id        string
	plainCol  string
	inPlain   bool
	node      int

	section    string
	refSection bool
	valueCol   int
	colIdx     int
	curIsValue bool
	text       strings.Builder
	value      string
	kind       string
	inTags     bool

	refs  map[string][]graphRef
	order []string
	// kinds holds each entity's first membership per section, in section
	// order, so a group can be chosen once the linking section is known.
	kinds [][]sectionKind
}

type sectionKind struct{ section, kind string }

var _ streamreadaccess.SinkI = (*GraphSink)(nil)
var _ streamreadaccess.MembershipSinkI = (*GraphSink)(nil)

// NewGraphSink returns an empty collector.
func NewGraphSink() *GraphSink {
	return &GraphSink{renderer: membership.DefaultRenderer(), refs: make(map[string][]graphRef, 4)}
}

// Model is what the last batch projected to.
func (inst *GraphSink) Model() *GraphModel { return &inst.model }

func (inst *GraphSink) BeginBatch() {
	inst.model = GraphModel{}
	inst.ids = inst.ids[:0]
	clear(inst.refs)
	inst.order = inst.order[:0]
	inst.kinds = inst.kinds[:0]
}

func (inst *GraphSink) EndBatch() (err error) {
	byKey := make(map[string]int, 2*len(inst.model.Nodes))
	for i, n := range inst.model.Nodes {
		byKey[n] = i
	}
	for i, id := range inst.ids {
		if id != "" {
			if _, taken := byKey[id]; !taken {
				byKey[id] = i
			}
		}
	}
	best, bestHits := "", 0
	for _, sec := range inst.order {
		hits := 0
		for _, r := range inst.refs[sec] {
			if _, ok := byKey[r.value]; ok {
				hits++
			}
		}
		if hits > bestHits {
			best, bestHits = sec, hits
		}
	}
	inst.model.Section = best
	for n, ks := range inst.kinds {
		for _, k := range ks {
			if k.section != best {
				inst.model.Groups[n] = k.kind
				break
			}
		}
	}
	for _, r := range inst.refs[best] {
		to, ok := byKey[r.value]
		if !ok {
			inst.model.Unresolved++
			continue
		}
		inst.model.Edges = append(inst.model.Edges, GraphEdge{From: r.from, To: to, Kind: r.kind})
	}
	return nil
}

func (inst *GraphSink) BeginEntity() {
	inst.label, inst.labelRank, inst.id = "", 0, ""
	inst.node = len(inst.model.Nodes)
	inst.model.Nodes = append(inst.model.Nodes, "")
	inst.model.Groups = append(inst.model.Groups, "")
	inst.ids = append(inst.ids, "")
	inst.kinds = append(inst.kinds, nil)
}

func (inst *GraphSink) EndEntity() (err error) {
	label := inst.label
	if label == "" {
		label = inst.id
	}
	if label == "" {
		label = "#" + strconv.Itoa(inst.node)
	}
	inst.model.Nodes[inst.node] = label
	inst.ids[inst.node] = inst.id
	return nil
}

func (inst *GraphSink) BeginPlainSection(_ common.PlainItemTypeE, _ []naming.StylableName, _ []canonicaltypes.PrimitiveAstNodeI, _ int) {
	inst.inPlain = true
}
func (inst *GraphSink) EndPlainSection() (err error) {
	inst.inPlain = false
	return nil
}
func (inst *GraphSink) BeginPlainValue()                 {}
func (inst *GraphSink) EndPlainValue() (err error)       { return nil }
func (inst *GraphSink) BeginTaggedSections()             {}
func (inst *GraphSink) EndTaggedSections() (err error)   { return nil }
func (inst *GraphSink) BeginCoSectionGroup(_ naming.Key) {}
func (inst *GraphSink) EndCoSectionGroup() (err error)   { return nil }

func (inst *GraphSink) BeginSection(name naming.StylableName, _ []naming.StylableName, types []canonicaltypes.PrimitiveAstNodeI, _ useaspects.AspectSet, _ int) {
	inst.section, inst.refSection = string(name), false
	for i, ct := range types {
		if ct != nil && refScalar.MatchString(ct.String()) {
			inst.valueCol, inst.refSection = i, true
			if _, seen := inst.refs[inst.section]; !seen {
				inst.refs[inst.section] = nil
				inst.order = append(inst.order, inst.section)
			}
			return
		}
	}
}
func (inst *GraphSink) EndSection() (err error) {
	inst.section, inst.refSection = "", false
	return nil
}

func (inst *GraphSink) BeginTaggedValue() {
	inst.colIdx, inst.value, inst.kind = 0, "", ""
}

func (inst *GraphSink) EndTaggedValue() (err error) {
	if inst.refSection && inst.value != "" {
		inst.refs[inst.section] = append(inst.refs[inst.section], graphRef{from: inst.node, value: inst.value, kind: inst.kind})
	}
	// A node's group is its first membership outside the linking section —
	// what the entity is rather than what it points at — chosen at EndBatch.
	if inst.kind != "" {
		ks := inst.kinds[inst.node]
		if len(ks) == 0 || ks[len(ks)-1].section != inst.section {
			inst.kinds[inst.node] = append(ks, sectionKind{section: inst.section, kind: inst.kind})
		}
	}
	return nil
}

func (inst *GraphSink) BeginColumn(_ streamreadaccess.PhysicalColumnAddr, name naming.StylableName, _ canonicaltypes.PrimitiveAstNodeI, _ valueaspects.AspectSet) {
	inst.plainCol = string(name)
	inst.curIsValue = !inst.inPlain && inst.refSection && inst.colIdx == inst.valueCol
	inst.text.Reset()
}

func (inst *GraphSink) EndColumn() {
	text := strings.TrimSpace(inst.text.String())
	switch {
	case inst.inPlain:
		inst.offerPlain(inst.plainCol, text)
	case inst.curIsValue:
		inst.value = text
	}
	inst.colIdx++
}

// offerPlain keeps the entity's id and its best label, the chart's rule: a
// natural key over another key-like column.
func (inst *GraphSink) offerPlain(col string, text string) {
	if text == "" {
		return
	}
	lc := strings.ToLower(col)
	if lc == "id" {
		inst.id = text
		return
	}
	rank := 0
	switch {
	case strings.Contains(lc, "natural"):
		rank = 2
	case strings.Contains(lc, "name") || strings.Contains(lc, "key") || strings.Contains(lc, "label"):
		rank = 1
	}
	if rank > inst.labelRank {
		inst.label, inst.labelRank = text, rank
	}
}

func (inst *GraphSink) BeginScalarValue()                 { inst.text.Reset() }
func (inst *GraphSink) EndScalarValue() (err error)       { return nil }
func (inst *GraphSink) BeginHomogenousArrayValue(_ int)   {}
func (inst *GraphSink) EndHomogenousArrayValue()          {}
func (inst *GraphSink) BeginSetValue(_ int)               {}
func (inst *GraphSink) EndSetValue()                      {}
func (inst *GraphSink) BeginValueItem(_ int)              {}
func (inst *GraphSink) EndValueItem()                     {}
func (inst *GraphSink) Write(p []byte) (n int, err error) { return inst.WriteString(string(p)) }
func (inst *GraphSink) WriteString(s string) (n int, err error) {
	if !inst.inTags {
		inst.text.WriteString(s)
	}
	return len(s), nil
}

func (inst *GraphSink) BeginTags(_ int) { inst.inTags = true }
func (inst *GraphSink) EndTags()        { inst.inTags = false }

func (inst *GraphSink) tag(label string) {
	if inst.kind == "" {
		inst.kind = label
	}
}
func (inst *GraphSink) AddMembershipRef(_ bool, ref uint64) { inst.tag(inst.renderer.RenderRef(ref)) }
func (inst *GraphSink) AddMembershipVerbatim(_ bool, verbatim string) {
	inst.tag(inst.renderer.RenderVerbatim(verbatim))
}
func (inst *GraphSink) AddMembershipRefParametrized(_ bool, ref uint64, params string) {
	inst.tag(inst.renderer.RenderRef(ref) + "(" + inst.renderer.RenderParams(params) + ")")
}
func (inst *GraphSink) AddMembershipMixedLowCardRefHighCardParam(ref uint64, params string) {
	inst.tag(inst.renderer.RenderRef(ref) + "(" + inst.renderer.RenderParams(params) + ")")
}
func (inst *GraphSink) AddMembershipMixedLowCardVerbatimHighCardParam(verbatim string, params string) {
	inst.tag(inst.renderer.RenderVerbatim(verbatim) + "(" + inst.renderer.RenderParams(params) + ")")
}
