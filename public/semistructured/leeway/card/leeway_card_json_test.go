//go:build llm_generated_opus47

package card

import (
	"bytes"
	"encoding/json/jsontext"
	"strconv"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/membershiprole"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

// driveOneAttribute walks the SinkI calls for a single tagged attribute with
// one value column. Used by the unit tests below to build minimal entities
// without spinning up the full Driver / Arrow batch.
func driveOneAttribute(t *testing.T, sink streamreadaccess.SinkI, sectionName string, colName string, scalar string, addTags func()) {
	t.Helper()
	name := naming.MustBeValidStylableName(sectionName)
	col := naming.MustBeValidStylableName(colName)
	ct := canonicaltypes.StringAstNode{BaseType: canonicaltypes.BaseTypeStringUtf8}
	sink.BeginSection(name, []naming.StylableName{col}, []canonicaltypes.PrimitiveAstNodeI{ct}, useaspects.EmptyAspectSet, 1)
	sink.BeginTaggedValue()
	sink.BeginColumn(streamreadaccess.PhysicalColumnAddr{}, col, ct, valueaspects.EmptyAspectSet)
	sink.BeginScalarValue()
	_, _ = sink.WriteString(scalar)
	_ = sink.EndScalarValue()
	sink.EndColumn()
	sink.BeginTags(0)
	addTags()
	sink.EndTags()
	_ = sink.EndTaggedValue()
	_ = sink.EndSection()
}

func newTestEmitter(t *testing.T, ndjson bool) (sink *JsonCardEmitter, buf *bytes.Buffer) {
	t.Helper()
	buf = bytes.NewBuffer(nil)
	enc := jsontext.NewEncoder(buf)
	opts := []JsonCardEmitterOption{}
	if ndjson {
		opts = append(opts, WithNDJSON())
	}
	sink = NewJsonCardEmitter(enc, nil, opts...)
	return
}

func TestJsonCardEmitter_AliasingMultiPrimary(t *testing.T) {
	sink, buf := newTestEmitter(t, false)
	sink.BeginBatch()
	sink.BeginEntity()
	sink.BeginTaggedSections()
	driveOneAttribute(t, sink, "float64", "value", "19.99", func() {
		sink.AddMembershipVerbatim(true, "/price/current", "/price/current")
		sink.AddMembershipVerbatim(true, "/promo/flash_sale", "/promo/flash_sale")
		sink.AddMembershipVerbatim(true, "/stats/min", "/stats/min")
	})
	_ = sink.EndTaggedSections()
	_ = sink.EndEntity()
	_ = sink.EndBatch()

	out := buf.String()
	// Canonical key is lex-smallest primary path:
	if !strings.Contains(out, `"/price/current"`) {
		t.Fatalf("missing canonical key: %s", out)
	}
	if !strings.Contains(out, `"aliases"`) {
		t.Fatalf("missing aliases field: %s", out)
	}
	if !strings.Contains(out, `"/promo/flash_sale"`) || !strings.Contains(out, `"/stats/min"`) {
		t.Fatalf("missing alias entries: %s", out)
	}
}

func TestJsonCardEmitter_SecondaryLabels(t *testing.T) {
	sink, buf := newTestEmitter(t, false)
	sink.BeginBatch()
	sink.BeginEntity()
	sink.BeginTaggedSections()
	driveOneAttribute(t, sink, "null", "value", "", func() {
		sink.AddMembershipVerbatim(true, "/metrics/error", "/metrics/error")
		// Plain identifier verbatim → secondary under DefaultClassifier.
		sink.AddMembershipVerbatim(true, "errormsg", "errormsg")
	})
	_ = sink.EndTaggedSections()
	_ = sink.EndEntity()
	_ = sink.EndBatch()

	out := buf.String()
	if !strings.Contains(out, `"/metrics/error"`) {
		t.Fatalf("missing primary key: %s", out)
	}
	if !strings.Contains(out, `"labels"`) {
		t.Fatalf("missing labels field: %s", out)
	}
	if !strings.Contains(out, `"errormsg"`) {
		t.Fatalf("missing secondary label: %s", out)
	}
}

func TestJsonCardEmitter_NDJSONHeaderAndOneEntityPerLine(t *testing.T) {
	sink, buf := newTestEmitter(t, true)
	sink.BeginBatch()

	for _, val := range []string{"a", "b"} {
		sink.BeginEntity()
		sink.BeginTaggedSections()
		driveOneAttribute(t, sink, "string", "value", val, func() {
			sink.AddMembershipVerbatim(true, "/x", "/x")
		})
		_ = sink.EndTaggedSections()
		_ = sink.EndEntity()
	}
	_ = sink.EndBatch()

	out := strings.TrimRight(buf.String(), "\n")
	lines := strings.Split(out, "\n")
	if len(lines) < 3 {
		t.Fatalf("expected header + 2 entity lines, got %d: %q", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], `{"leewayCardData"`) {
		t.Fatalf("first line is not a header: %q", lines[0])
	}
	if !strings.Contains(lines[1], `"/x"`) {
		t.Fatalf("second line missing entity attribute: %q", lines[1])
	}
}

func TestJsonCardEmitter_TypeAwareScalars(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	enc := jsontext.NewEncoder(buf)
	sink := NewJsonCardEmitter(enc, nil)

	intCt := canonicaltypes.MachineNumericTypeAstNode{
		BaseType: canonicaltypes.BaseTypeMachineNumericSigned,
		Width:    64,
	}
	boolCt := canonicaltypes.StringAstNode{BaseType: canonicaltypes.BaseTypeStringBool}

	sink.BeginBatch()
	sink.BeginEntity()
	sink.BeginTaggedSections()

	// Integer column
	col := naming.MustBeValidStylableName("value")
	sink.BeginSection(naming.MustBeValidStylableName("int64"),
		[]naming.StylableName{col},
		[]canonicaltypes.PrimitiveAstNodeI{intCt}, useaspects.EmptyAspectSet, 1)
	sink.BeginTaggedValue()
	sink.BeginColumn(streamreadaccess.PhysicalColumnAddr{}, col, intCt, valueaspects.EmptyAspectSet)
	sink.BeginScalarValue()
	_, _ = sink.WriteString("42")
	_ = sink.EndScalarValue()
	sink.EndColumn()
	sink.BeginTags(0)
	sink.AddMembershipVerbatim(true, "/x", "/x")
	sink.EndTags()
	_ = sink.EndTaggedValue()
	_ = sink.EndSection()

	// Bool column
	sink.BeginSection(naming.MustBeValidStylableName("bool"),
		[]naming.StylableName{col},
		[]canonicaltypes.PrimitiveAstNodeI{boolCt}, useaspects.EmptyAspectSet, 1)
	sink.BeginTaggedValue()
	sink.BeginColumn(streamreadaccess.PhysicalColumnAddr{}, col, boolCt, valueaspects.EmptyAspectSet)
	sink.BeginScalarValue()
	_, _ = sink.WriteString("true")
	_ = sink.EndScalarValue()
	sink.EndColumn()
	sink.BeginTags(0)
	sink.AddMembershipVerbatim(true, "/y", "/y")
	sink.EndTags()
	_ = sink.EndTaggedValue()
	_ = sink.EndSection()

	_ = sink.EndTaggedSections()
	_ = sink.EndEntity()
	_ = sink.EndBatch()

	out := buf.String()
	// Integer should be emitted as JSON-native number, not a quoted string.
	if !strings.Contains(out, `"scalar":42`) {
		t.Fatalf("integer not emitted as JSON-native: %s", out)
	}
	if !strings.Contains(out, `"scalar":true`) {
		t.Fatalf("bool not emitted as JSON-native: %s", out)
	}
}

// renderTwo emits the same fixture through two independent emitters and
// returns both byte sequences for byte-equality comparison.
func renderTwo(t *testing.T, build func(sink *JsonCardEmitter)) (a, b []byte) {
	t.Helper()
	emit := func() []byte {
		sink, buf := newTestEmitter(t, false)
		build(sink)
		return append([]byte(nil), buf.Bytes()...)
	}
	return emit(), emit()
}

func TestJsonCardEmitter_Determinism(t *testing.T) {
	build := func(sink *JsonCardEmitter) {
		sink.BeginBatch()
		sink.BeginEntity()
		sink.BeginTaggedSections()
		driveOneAttribute(t, sink, "string", "value", "alpha", func() {
			sink.AddMembershipVerbatim(true, "/c", "/c")
			sink.AddMembershipVerbatim(true, "/a", "/a")
			sink.AddMembershipVerbatim(true, "/b", "/b")
			sink.AddMembershipVerbatim(true, "labelTwo", "labelTwo")
			sink.AddMembershipVerbatim(true, "labelOne", "labelOne")
		})
		_ = sink.EndTaggedSections()
		_ = sink.EndEntity()
		_ = sink.EndBatch()
	}
	a, b := renderTwo(t, build)
	if !bytes.Equal(a, b) {
		t.Fatalf("non-deterministic output:\n--- a ---\n%s\n--- b ---\n%s", a, b)
	}
	out := string(a)
	// Aliases sorted lex: ["/b","/c"]
	idxB := strings.Index(out, `"/b"`)
	idxC := strings.Index(out, `"/c"`)
	if idxB < 0 || idxC < 0 || idxB > idxC {
		t.Fatalf("aliases not sorted lex: %s", out)
	}
	// Labels sorted: labelOne before labelTwo
	if strings.Index(out, "labelOne") > strings.Index(out, "labelTwo") {
		t.Fatalf("labels not sorted lex: %s", out)
	}
}

// driveOneAttributeArray walks one tagged attribute whose single value column
// is a homogenous array of the given typed elements.
func driveOneAttributeArray(t *testing.T, sink streamreadaccess.SinkI, sectionName string, ct canonicaltypes.PrimitiveAstNodeI, items []string, addTags func()) {
	t.Helper()
	name := naming.MustBeValidStylableName(sectionName)
	col := naming.MustBeValidStylableName("value")
	sink.BeginSection(name, []naming.StylableName{col}, []canonicaltypes.PrimitiveAstNodeI{ct}, useaspects.EmptyAspectSet, 1)
	sink.BeginTaggedValue()
	sink.BeginColumn(streamreadaccess.PhysicalColumnAddr{}, col, ct, valueaspects.EmptyAspectSet)
	sink.BeginHomogenousArrayValue(len(items))
	for i, it := range items {
		sink.BeginValueItem(i)
		_, _ = sink.WriteString(it)
		sink.EndValueItem()
	}
	sink.EndHomogenousArrayValue()
	sink.EndColumn()
	sink.BeginTags(0)
	addTags()
	sink.EndTags()
	_ = sink.EndTaggedValue()
	_ = sink.EndSection()
}

func TestJsonCardEmitter_SetWrapper(t *testing.T) {
	sink, buf := newTestEmitter(t, false)
	col := naming.MustBeValidStylableName("value")
	setCt := canonicaltypes.StringAstNode{
		BaseType:       canonicaltypes.BaseTypeStringUtf8,
		ScalarModifier: canonicaltypes.ScalarModifierSet,
	}
	sink.BeginBatch()
	sink.BeginEntity()
	sink.BeginTaggedSections()
	sink.BeginSection(naming.MustBeValidStylableName("symbol"),
		[]naming.StylableName{col}, []canonicaltypes.PrimitiveAstNodeI{setCt}, useaspects.EmptyAspectSet, 1)
	sink.BeginTaggedValue()
	sink.BeginColumn(streamreadaccess.PhysicalColumnAddr{}, col, setCt, valueaspects.EmptyAspectSet)
	sink.BeginSetValue(2)
	sink.BeginValueItem(0)
	_, _ = sink.WriteString("z")
	sink.EndValueItem()
	sink.BeginValueItem(1)
	_, _ = sink.WriteString("a")
	sink.EndValueItem()
	sink.EndSetValue()
	sink.EndColumn()
	sink.BeginTags(0)
	sink.AddMembershipVerbatim(true, "/tags", "/tags")
	sink.EndTags()
	_ = sink.EndTaggedValue()
	_ = sink.EndSection()
	_ = sink.EndTaggedSections()
	_ = sink.EndEntity()
	_ = sink.EndBatch()

	out := buf.String()
	if !strings.Contains(out, `"set":["a","z"]`) && !strings.Contains(out, `"set":[ "a","z" ]`) {
		// Set canonicalisation sorts items in canonical bytes; "a" < "z".
		t.Fatalf("set wrapper not emitted in canonical order: %s", out)
	}
}

func TestJsonCardEmitter_HomogenousArrayItemsTyped(t *testing.T) {
	sink, buf := newTestEmitter(t, false)
	intArrayCt := canonicaltypes.MachineNumericTypeAstNode{
		BaseType:       canonicaltypes.BaseTypeMachineNumericUnsigned,
		Width:          32,
		ScalarModifier: canonicaltypes.ScalarModifierHomogenousArray,
	}
	sink.BeginBatch()
	sink.BeginEntity()
	sink.BeginTaggedSections()
	driveOneAttributeArray(t, sink, "u32array", intArrayCt, []string{"1", "2", "3"}, func() {
		sink.AddMembershipVerbatim(true, "/wordLength", "/wordLength")
	})
	_ = sink.EndTaggedSections()
	_ = sink.EndEntity()
	_ = sink.EndBatch()

	out := buf.String()
	if !strings.Contains(out, `"value":[1,2,3]`) {
		t.Fatalf("homogenous array items not JSON-native: %s", out)
	}
}

func TestJsonCardEmitter_NaNAndInf(t *testing.T) {
	sink, buf := newTestEmitter(t, false)
	floatCt := canonicaltypes.MachineNumericTypeAstNode{
		BaseType: canonicaltypes.BaseTypeMachineNumericFloat,
		Width:    64,
	}
	col := naming.MustBeValidStylableName("value")
	sink.BeginBatch()
	sink.BeginEntity()
	sink.BeginTaggedSections()

	for _, s := range []string{"NaN", "+Inf", "-Inf"} {
		sink.BeginSection(naming.MustBeValidStylableName("float64"),
			[]naming.StylableName{col}, []canonicaltypes.PrimitiveAstNodeI{floatCt}, useaspects.EmptyAspectSet, 1)
		sink.BeginTaggedValue()
		sink.BeginColumn(streamreadaccess.PhysicalColumnAddr{}, col, floatCt, valueaspects.EmptyAspectSet)
		sink.BeginScalarValue()
		_, _ = sink.WriteString(s)
		_ = sink.EndScalarValue()
		sink.EndColumn()
		sink.BeginTags(0)
		sink.AddMembershipVerbatim(true, "/x_"+s, "/x_"+s)
		sink.EndTags()
		_ = sink.EndTaggedValue()
		_ = sink.EndSection()
	}

	_ = sink.EndTaggedSections()
	_ = sink.EndEntity()
	_ = sink.EndBatch()

	out := buf.String()
	for _, want := range []string{`"NaN"`, `"+Inf"`, `"-Inf"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %s sentinel: %s", want, out)
		}
	}
}

func TestJsonCardEmitter_ParamTreatmentIndex(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	enc := jsontext.NewEncoder(buf)
	cls := indexClassifier{}
	sink := NewJsonCardEmitter(enc, nil, WithClassifier(cls))

	col := naming.MustBeValidStylableName("value")
	floatCt := canonicaltypes.MachineNumericTypeAstNode{
		BaseType: canonicaltypes.BaseTypeMachineNumericFloat,
		Width:    64,
	}

	sink.BeginBatch()
	sink.BeginEntity()
	sink.BeginTaggedSections()

	for i, val := range []string{"1.1", "1.2", "1.3"} {
		sink.BeginSection(naming.MustBeValidStylableName("float64"),
			[]naming.StylableName{col}, []canonicaltypes.PrimitiveAstNodeI{floatCt}, useaspects.EmptyAspectSet, 1)
		sink.BeginTaggedValue()
		sink.BeginColumn(streamreadaccess.PhysicalColumnAddr{}, col, floatCt, valueaspects.EmptyAspectSet)
		sink.BeginScalarValue()
		_, _ = sink.WriteString(val)
		_ = sink.EndScalarValue()
		sink.EndColumn()
		sink.BeginTags(0)
		sink.AddMembershipMixedLowCardVerbatimHighCardParam("/measurements/_", "/measurements/_", strconv.Itoa(i), strconv.Itoa(i))
		sink.EndTags()
		_ = sink.EndTaggedValue()
		_ = sink.EndSection()
	}

	_ = sink.EndTaggedSections()
	_ = sink.EndEntity()
	_ = sink.EndBatch()

	out := buf.String()
	if !strings.Contains(out, `"/measurements/_"`) {
		t.Fatalf("paramTreatmentIndex did not produce skeleton key: %s", out)
	}
	if !strings.Contains(out, `"indexed":[`) {
		t.Fatalf("indexed shape not emitted: %s", out)
	}
	for _, want := range []string{`"params":[0]`, `"params":[1]`, `"params":[2]`} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %s: %s", want, out)
		}
	}
}

// indexClassifier is a custom classifier that returns ParamTreatmentIndex for
// any parametrized membership; used to exercise the indexed projection path.
type indexClassifier struct{}

func (indexClassifier) Classify(sec membershiprole.SectionContext, mv membershiprole.MembershipValue) (role membershiprole.MembershipRoleE, pt membershiprole.ParamTreatmentE) {
	role = membershiprole.MembershipRolePrimary
	switch mv.Kind {
	case membershiprole.MembershipKindRefParametrized,
		membershiprole.MembershipKindMixedLowCardRefHighCardParam,
		membershiprole.MembershipKindMixedLowCardVerbatimHighCardParam:
		pt = membershiprole.ParamTreatmentIndex
	default:
		pt = membershiprole.ParamTreatmentNone
	}
	return
}

// Compile-time check that the emitter still satisfies SinkI even if the
// constructor signature changes.
var _ streamreadaccess.SinkI = (*JsonCardEmitter)(nil)
var _ = common.PlainItemTypeE(0)
var _ membershiprole.ClassifierI = membershiprole.DefaultClassifier{}
