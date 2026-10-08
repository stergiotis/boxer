package lwread

import (
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/membership"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/stergiotis/boxer/public/semistructured/leeway/valueaspects"
)

var (
	tF64    = canonicaltypes.MachineNumericTypeAstNode{BaseType: canonicaltypes.BaseTypeMachineNumericFloat, Width: 64}
	tU64    = canonicaltypes.MachineNumericTypeAstNode{BaseType: canonicaltypes.BaseTypeMachineNumericUnsigned, Width: 64}
	tU32Set = canonicaltypes.MachineNumericTypeAstNode{BaseType: canonicaltypes.BaseTypeMachineNumericUnsigned, Width: 32, ScalarModifier: canonicaltypes.ScalarModifierSet}
	tStr    = canonicaltypes.StringAstNode{BaseType: canonicaltypes.BaseTypeStringUtf8}
	tStrArr = canonicaltypes.StringAstNode{BaseType: canonicaltypes.BaseTypeStringUtf8, ScalarModifier: canonicaltypes.ScalarModifierHomogenousArray}
	tBytes  = canonicaltypes.StringAstNode{BaseType: canonicaltypes.BaseTypeStringBytes}
)

func sn(s string) naming.StylableName { return naming.StylableName(s) }

type nameFormatter struct{}

func (nameFormatter) FormatRef(ref uint64) string {
	if ref == 5 {
		return "model"
	}
	return fmt.Sprintf("0x%x", ref)
}

// drive is one host record: a plain entity id with a natural key in bytes, a
// num section named by verbatim memberships, a ref-named attribute with
// parameters, a set and a list, a machine-readable-only column, and a
// label-only section.
func drive(s *Sink) {
	s.BeginBatch()
	s.BeginEntity()
	scalar := func(col string, ct canonicaltypes.PrimitiveAstNodeI, aspects valueaspects.AspectSet, idx int, text string) {
		s.BeginColumn(streamreadaccess.PhysicalColumnAddr{Index: idx}, sn(col), ct, aspects)
		s.BeginScalarValue()
		_, _ = s.WriteString(text)
		_ = s.EndScalarValue()
		s.EndColumn()
	}
	items := func(col string, ct canonicaltypes.PrimitiveAstNodeI, idx int, set bool, texts ...string) {
		s.BeginColumn(streamreadaccess.PhysicalColumnAddr{Index: idx}, sn(col), ct, valueaspects.EmptyAspectSet)
		if set {
			s.BeginSetValue(len(texts))
		} else {
			s.BeginHomogenousArrayValue(len(texts))
		}
		for i, t := range texts {
			s.BeginValueItem(i)
			_, _ = s.WriteString(t)
			s.EndValueItem()
		}
		if set {
			s.EndSetValue()
		} else {
			s.EndHomogenousArrayValue()
		}
		s.EndColumn()
	}
	s.BeginPlainSection(common.PlainItemTypeEntityId, []naming.StylableName{sn("id"), sn("naturalKey"), sn("blob")}, nil, 1)
	s.BeginPlainValue()
	scalar("id", tU64, valueaspects.EmptyAspectSet, 0, "7")
	scalar("naturalKey", tBytes, valueaspects.EmptyAspectSet, 1, base64.StdEncoding.EncodeToString([]byte("host-05")))
	scalar("blob", tBytes, valueaspects.EmptyAspectSet, 2, base64.StdEncoding.EncodeToString([]byte{0x00, 0xff, 0x10}))
	_ = s.EndPlainValue()
	_ = s.EndPlainSection()

	s.BeginTaggedSections()
	s.BeginSection(sn("num"), []naming.StylableName{sn("value"), sn("raw")}, nil, useaspects.EmptyAspectSet, 3)
	for i, n := range []string{"cpu", "mem"} {
		s.BeginTaggedValue()
		scalar("value", tF64, valueaspects.EmptyAspectSet, 3, fmt.Sprint(40+i))
		hidden, _ := valueaspects.EncodeAspects(valueaspects.AspectMachineReadable)
		scalar("raw", tStr, hidden, 4, "x")
		s.BeginTags(1)
		s.AddMembershipVerbatim(true, n)
		s.EndTags()
		_ = s.EndTaggedValue()
	}
	s.BeginTaggedValue()
	scalar("value", tF64, valueaspects.EmptyAspectSet, 3, "9")
	scalar("raw", tStr, valueaspects.EmptyAspectSet, 4, "y")
	s.BeginTags(2)
	s.AddMembershipMixedLowCardRefHighCardParam(5, "\x01")
	s.AddMembershipVerbatim(true, "unit")
	s.EndTags()
	_ = s.EndTaggedValue()
	_ = s.EndSection()

	s.BeginSection(sn("bins"), []naming.StylableName{sn("value")}, nil, useaspects.EmptyAspectSet, 2)
	s.BeginTaggedValue()
	items("value", tU32Set, 5, true, "10", "5", "1")
	s.BeginTags(1)
	s.AddMembershipVerbatim(true, "cpu")
	s.EndTags()
	_ = s.EndTaggedValue()
	s.BeginTaggedValue()
	items("value", tStrArr, 6, false, "a", "b", "c", "d")
	s.BeginTags(1)
	s.AddMembershipRef(true, 5)
	s.EndTags()
	_ = s.EndTaggedValue()
	_ = s.EndSection()

	allSec, _ := useaspects.EncodeAspects(useaspects.AspectSectionMembershipsAllSecondary)
	s.BeginSection(sn("flags"), nil, nil, allSec, 1)
	s.BeginTaggedValue()
	s.BeginTags(1)
	s.AddMembershipVerbatim(true, "urgent")
	s.EndTags()
	_ = s.EndTaggedValue()
	_ = s.EndSection()
	_ = s.EndTaggedSections()
	_ = s.EndEntity()
	_ = s.EndBatch()
}

func read(t *testing.T) *Model {
	t.Helper()
	s := NewSink(Options{Renderer: membership.NewRenderer(nameFormatter{}, nil, nil), MaxItems: 3})
	drive(s)
	m := s.Model()
	require.Len(t, m.Records, 1)
	return m
}

func byName(r *Record, name string) *Attribute {
	for i := range r.Attributes {
		if r.Attributes[i].Name == name {
			return &r.Attributes[i]
		}
	}
	return nil
}

func TestAttributesAreNamedByTheirFirstMembership(t *testing.T) {
	r := &read(t).Records[0]
	cpu := byName(r, "cpu")
	require.NotNil(t, cpu, "a name without a leading / names its attribute")
	assert.Equal(t, "num", cpu.Section)
	assert.Equal(t, []Item{{Text: "40", Raw: "40"}}, cpu.Values[0].Items)

	var withParams *Attribute
	for i := range r.Attributes {
		if a := &r.Attributes[i]; a.Named != nil && a.Named.Params != "" {
			withParams = a
		}
	}
	require.NotNil(t, withParams)
	assert.Equal(t, "model", withParams.Named.Name, "the renderer names the ref")
	assert.Contains(t, withParams.Name, "model[", "parameters are spelled into the name")
	require.Len(t, withParams.Labels, 1)
	assert.Equal(t, "unit", withParams.Labels[0].Text, "further memberships are labels")

	flags := byName(r, "flags")
	require.NotNil(t, flags, "a section of secondary memberships names its attribute by the section")
	assert.Nil(t, flags.Named)
	assert.Equal(t, "urgent", flags.Labels[0].Text)
}

func TestValuesAreSpelledOnce(t *testing.T) {
	r := &read(t).Records[0]
	assert.Equal(t, "host-05", r.Label, "the natural key labels the record, its bytes read as text")
	blob := byName(r, "blob")
	require.NotNil(t, blob)
	assert.Equal(t, "0x00ff10", blob.Values[0].Items[0].Text, "bytes that are not text read as hex")

	bins := r.Attributes[0]
	for _, a := range r.Attributes {
		if a.Section == "bins" && a.Values[0].Shape == ShapeSet {
			bins = a
		}
	}
	var set []string
	for _, it := range bins.Values[0].Items {
		set = append(set, it.Text)
	}
	assert.Equal(t, []string{"1", "5", "10"}, set, "a set reads in value order")

	for _, a := range r.Attributes {
		if a.Section == "bins" && a.Values[0].Shape == ShapeList {
			assert.Len(t, a.Values[0].Items, 3)
			assert.Equal(t, 1, a.Values[0].More, "a list is cut at MaxItems, and says so")
		}
	}
	assert.Equal(t, 2, r.Hidden, "the machine-readable-only column is counted, not shown")
	assert.Equal(t, 1, r.More)
	cpu := byName(r, "cpu")
	require.NotNil(t, cpu)
	assert.Len(t, cpu.Values, 1)
	assert.Equal(t, 1, cpu.Hidden)
}

func TestQualifyAndHeader(t *testing.T) {
	m := read(t)
	m.Qualify()
	r := &m.Records[0]
	assert.NotNil(t, byName(r, "num·cpu"), "cpu names attributes in two sections")
	assert.NotNil(t, byName(r, "bins·cpu"))
	assert.NotNil(t, byName(r, "mem"), "a name one section gives stays bare")
	m.Qualify()
	assert.NotNil(t, byName(r, "num·cpu"), "idempotent")

	handles := map[string]string{}
	for _, s := range m.Header() {
		assert.Equal(t, 1, s.Records)
		handles[s.Name] = s.Handle
	}
	assert.Equal(t, "LW_GET('num', 'mem', 'col:value')", handles["mem"])
	assert.Equal(t, "LW_GET_LIST('bins', 'cpu')", handles["bins·cpu"], "a set is read as a list")
	assert.Equal(t, "LW_GET_LIST('bins', 5)", handles["model"], "a ref is read by its id")
	assert.Empty(t, handles["id"], "a plain column's handle is its column's")
}

// A handle names the section and column as the physical names spell them,
// which is how leeway.columns and the Table's headers print them; the
// styled names stay the attribute's.
func TestHandleSpellsPhysically(t *testing.T) {
	s := NewSink(Options{})
	s.BeginBatch()
	s.BeginEntity()
	s.BeginTaggedSections()
	s.BeginSection(sn("geo-point"), []naming.StylableName{sn("point-lat"), sn("point-lon")}, nil, useaspects.EmptyAspectSet, 1)
	s.BeginTaggedValue()
	for i, col := range []string{"point-lat", "point-lon"} {
		phys := []string{"tv:geoPoint:pointLat:val:f64:4A:::0::data", "tv:geoPoint:pointLon:val:f64:4A:::0::data"}[i]
		s.BeginColumn(streamreadaccess.PhysicalColumnAddr{Index: i, FullColumnName: phys}, sn(col), tF64, valueaspects.EmptyAspectSet)
		s.BeginScalarValue()
		_, _ = s.WriteString("47")
		_ = s.EndScalarValue()
		s.EndColumn()
	}
	s.BeginTags(1)
	s.AddMembershipVerbatim(true, "home")
	s.EndTags()
	_ = s.EndTaggedValue()
	_ = s.EndSection()
	_ = s.EndTaggedSections()
	_ = s.EndEntity()
	_ = s.EndBatch()
	m := s.Model()
	require.Len(t, m.Records, 1)
	a := byName(&m.Records[0], "home")
	require.NotNil(t, a)
	assert.Equal(t, "geo-point", a.Section)
	assert.Equal(t, "geoPoint", a.HandleSection)
	assert.Equal(t, "point-lat", a.Values[0].Column)
	assert.Equal(t, "pointLat", a.Values[0].HandleColumn)
	specs := m.Header()
	require.Len(t, specs, 1)
	assert.Equal(t, "LW_GET('geoPoint', 'home', 'col:pointLat')", specs[0].Handle)
}
