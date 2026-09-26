package leewaywidgets

import (
	"math"
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

// ChartModel is a leeway batch projected onto a chart: one category per
// entity, one series per membership of the charted section, one value per
// (series, entity). It is the projection a chart of a leeway batch is drawn
// from (ADR-0257, proposed, §SD1): the mapping, not the drawing.
//
// The charted section is the first tagged section, in batch order, with a
// numeric scalar value column; its first such column is the value. An
// attribute's series is its first membership, rendered as text; an attribute
// without one falls in a series named after the value column. A category is
// labelled by the entity's natural key when it has one, else by its id.
type ChartModel struct {
	Section   string
	ValueName string
	// Categories are entity labels in batch order.
	Categories []string
	// Series are membership labels in first-seen order.
	Series []string
	// Values is [series][category]; NaN where an entity has no attribute in
	// that series. A second attribute in the same series of the same entity
	// is counted in Collisions and does not overwrite the first.
	Values     [][]float64
	Collisions int
}

// Empty reports whether there is nothing to draw.
func (inst *ChartModel) Empty() bool {
	return len(inst.Categories) == 0 || len(inst.Series) == 0
}

// numericScalar matches a scalar number canonical type: u8…u64, i8…i64,
// f32, f64 — not an array (h) or a set (m).
var numericScalar = regexp.MustCompile(`^[uif][0-9]+$`)

// ChartSink collects a ChartModel from a batch driven through it.
type ChartSink struct {
	model    ChartModel
	renderer *membership.Renderer

	// Entity state.
	label      string
	labelRank  int
	plainCol   string
	cat        int
	inCharted  bool
	sectionSet bool

	// Attribute state.
	valueCol   int
	colIdx     int
	curCol     int
	curIsValue bool
	text       strings.Builder
	value      float64
	hasValue   bool
	series     string
	inTags     bool

	seriesIdx map[string]int
}

var _ streamreadaccess.SinkI = (*ChartSink)(nil)
var _ streamreadaccess.MembershipSinkI = (*ChartSink)(nil)

// NewChartSink returns an empty collector.
func NewChartSink() *ChartSink {
	return &ChartSink{renderer: membership.DefaultRenderer(), seriesIdx: make(map[string]int, 8)}
}

// Model is what the last batch projected to.
func (inst *ChartSink) Model() *ChartModel { return &inst.model }

func (inst *ChartSink) BeginBatch() {
	inst.model = ChartModel{}
	inst.sectionSet = false
	clear(inst.seriesIdx)
}
func (inst *ChartSink) EndBatch() (err error) { return nil }

func (inst *ChartSink) BeginEntity() {
	inst.label, inst.labelRank = "", 0
	inst.cat = len(inst.model.Categories)
	inst.model.Categories = append(inst.model.Categories, "")
	for s := range inst.model.Values {
		inst.model.Values[s] = append(inst.model.Values[s], math.NaN())
	}
}

func (inst *ChartSink) EndEntity() (err error) {
	label := inst.label
	if label == "" {
		label = "#" + strconv.Itoa(inst.cat)
	}
	inst.model.Categories[inst.cat] = label
	return nil
}

func (inst *ChartSink) BeginPlainSection(_ common.PlainItemTypeE, _ []naming.StylableName, _ []canonicaltypes.PrimitiveAstNodeI, _ int) {
}
func (inst *ChartSink) EndPlainSection() (err error) { return nil }
func (inst *ChartSink) BeginPlainValue()             {}
func (inst *ChartSink) EndPlainValue() (err error)   { return nil }
func (inst *ChartSink) BeginTaggedSections()         {}
func (inst *ChartSink) EndTaggedSections() (err error) {
	return nil
}
func (inst *ChartSink) BeginCoSectionGroup(_ naming.Key) {}
func (inst *ChartSink) EndCoSectionGroup() (err error)   { return nil }

func (inst *ChartSink) BeginSection(name naming.StylableName, _ []naming.StylableName, types []canonicaltypes.PrimitiveAstNodeI, _ useaspects.AspectSet, _ int) {
	inst.inCharted = false
	if inst.sectionSet {
		inst.inCharted = string(name) == inst.model.Section
		return
	}
	for i, ct := range types {
		if ct != nil && numericScalar.MatchString(ct.String()) {
			inst.model.Section = string(name)
			inst.valueCol = i
			inst.sectionSet, inst.inCharted = true, true
			return
		}
	}
}
func (inst *ChartSink) EndSection() (err error) {
	inst.inCharted = false
	return nil
}

func (inst *ChartSink) BeginTaggedValue() {
	inst.colIdx, inst.hasValue, inst.series = 0, false, ""
}

func (inst *ChartSink) EndTaggedValue() (err error) {
	if !inst.inCharted || !inst.hasValue {
		return nil
	}
	series := inst.series
	if series == "" {
		series = inst.model.ValueName
	}
	s, ok := inst.seriesIdx[series]
	if !ok {
		s = len(inst.model.Series)
		inst.seriesIdx[series] = s
		inst.model.Series = append(inst.model.Series, series)
		row := make([]float64, len(inst.model.Categories))
		for i := range row {
			row[i] = math.NaN()
		}
		inst.model.Values = append(inst.model.Values, row)
	}
	if !math.IsNaN(inst.model.Values[s][inst.cat]) {
		inst.model.Collisions++
		return nil
	}
	inst.model.Values[s][inst.cat] = inst.value
	return nil
}

func (inst *ChartSink) BeginColumn(_ streamreadaccess.PhysicalColumnAddr, name naming.StylableName, _ canonicaltypes.PrimitiveAstNodeI, _ valueaspects.AspectSet) {
	inst.plainCol = string(name)
	inst.curCol = inst.colIdx
	inst.curIsValue = inst.inCharted && inst.colIdx == inst.valueCol
	if inst.curIsValue && inst.model.ValueName == "" {
		inst.model.ValueName = string(name)
	}
	inst.text.Reset()
}

func (inst *ChartSink) EndColumn() {
	text := strings.TrimSpace(inst.text.String())
	switch {
	case inst.curIsValue:
		if v, err := strconv.ParseFloat(text, 64); err == nil {
			inst.value, inst.hasValue = v, true
		}
	case !inst.inCharted && inst.colIdx == inst.curCol:
		inst.offerLabel(inst.plainCol, text)
	}
	inst.colIdx++
}

// offerLabel keeps the best entity label seen: a natural key over any other
// key-like column over the id.
func (inst *ChartSink) offerLabel(col string, text string) {
	if text == "" {
		return
	}
	lc := strings.ToLower(col)
	rank := 1
	switch {
	case strings.Contains(lc, "natural"):
		rank = 3
	case strings.Contains(lc, "name") || strings.Contains(lc, "key") || strings.Contains(lc, "label"):
		rank = 2
	case lc != "id":
		return
	}
	if rank > inst.labelRank {
		inst.label, inst.labelRank = text, rank
	}
}

func (inst *ChartSink) BeginScalarValue()                 { inst.text.Reset() }
func (inst *ChartSink) EndScalarValue() (err error)       { return nil }
func (inst *ChartSink) BeginHomogenousArrayValue(_ int)   {}
func (inst *ChartSink) EndHomogenousArrayValue()          {}
func (inst *ChartSink) BeginSetValue(_ int)               {}
func (inst *ChartSink) EndSetValue()                      {}
func (inst *ChartSink) BeginValueItem(_ int)              {}
func (inst *ChartSink) EndValueItem()                     {}
func (inst *ChartSink) Write(p []byte) (n int, err error) { return inst.WriteString(string(p)) }
func (inst *ChartSink) WriteString(s string) (n int, err error) {
	if !inst.inTags {
		inst.text.WriteString(s)
	}
	return len(s), nil
}

func (inst *ChartSink) BeginTags(_ int) { inst.inTags = true }
func (inst *ChartSink) EndTags()        { inst.inTags = false }

func (inst *ChartSink) tag(label string) {
	if inst.series == "" {
		inst.series = label
	}
}
func (inst *ChartSink) AddMembershipRef(_ bool, ref uint64) { inst.tag(inst.renderer.RenderRef(ref)) }
func (inst *ChartSink) AddMembershipVerbatim(_ bool, verbatim string) {
	inst.tag(inst.renderer.RenderVerbatim(verbatim))
}
func (inst *ChartSink) AddMembershipRefParametrized(_ bool, ref uint64, params string) {
	inst.tag(inst.renderer.RenderRef(ref) + "(" + inst.renderer.RenderParams(params) + ")")
}
func (inst *ChartSink) AddMembershipMixedLowCardRefHighCardParam(ref uint64, params string) {
	inst.tag(inst.renderer.RenderRef(ref) + "(" + inst.renderer.RenderParams(params) + ")")
}
func (inst *ChartSink) AddMembershipMixedLowCardVerbatimHighCardParam(verbatim string, params string) {
	inst.tag(inst.renderer.RenderVerbatim(verbatim) + "(" + inst.renderer.RenderParams(params) + ")")
}
