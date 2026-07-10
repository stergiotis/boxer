package play

import (
	"bytes"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/encodingaspects"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stretchr/testify/require"
)

// TestArrowToCanonical pins the arrow→canonical mapping structurally (against
// the concrete AST node, not against the function's own String()), including
// the two encodings that carry extra shape: dictionary → low-card flag, list →
// homogeneous-array scalar modifier.
func TestArrowToCanonical(t *testing.T) {
	dict := func(v arrow.DataType) arrow.DataType {
		return &arrow.DictionaryType{IndexType: arrow.PrimitiveTypes.Uint16, ValueType: v}
	}
	cases := []struct {
		name    string
		in      arrow.DataType
		want    canonicaltypes.PrimitiveAstNodeI
		lowCard bool
	}{
		{"int8", arrow.PrimitiveTypes.Int8, canonicaltypes.MachineNumericTypeAstNode{BaseType: canonicaltypes.BaseTypeMachineNumericSigned, Width: 8}, false},
		{"int32", arrow.PrimitiveTypes.Int32, canonicaltypes.MachineNumericTypeAstNode{BaseType: canonicaltypes.BaseTypeMachineNumericSigned, Width: 32}, false},
		{"int64", arrow.PrimitiveTypes.Int64, canonicaltypes.MachineNumericTypeAstNode{BaseType: canonicaltypes.BaseTypeMachineNumericSigned, Width: 64}, false},
		{"uint16", arrow.PrimitiveTypes.Uint16, canonicaltypes.MachineNumericTypeAstNode{BaseType: canonicaltypes.BaseTypeMachineNumericUnsigned, Width: 16}, false},
		{"uint64", arrow.PrimitiveTypes.Uint64, canonicaltypes.MachineNumericTypeAstNode{BaseType: canonicaltypes.BaseTypeMachineNumericUnsigned, Width: 64}, false},
		{"float32", arrow.PrimitiveTypes.Float32, canonicaltypes.MachineNumericTypeAstNode{BaseType: canonicaltypes.BaseTypeMachineNumericFloat, Width: 32}, false},
		{"float64", arrow.PrimitiveTypes.Float64, canonicaltypes.MachineNumericTypeAstNode{BaseType: canonicaltypes.BaseTypeMachineNumericFloat, Width: 64}, false},
		{"bool", arrow.FixedWidthTypes.Boolean, canonicaltypes.StringAstNode{BaseType: canonicaltypes.BaseTypeStringBool}, false},
		{"utf8", arrow.BinaryTypes.String, canonicaltypes.StringAstNode{BaseType: canonicaltypes.BaseTypeStringUtf8}, false},
		{"large_utf8", arrow.BinaryTypes.LargeString, canonicaltypes.StringAstNode{BaseType: canonicaltypes.BaseTypeStringUtf8}, false},
		{"binary", arrow.BinaryTypes.Binary, canonicaltypes.StringAstNode{BaseType: canonicaltypes.BaseTypeStringBytes}, false},
		{"fixed_binary16", &arrow.FixedSizeBinaryType{ByteWidth: 16}, canonicaltypes.StringAstNode{BaseType: canonicaltypes.BaseTypeStringBytes, WidthModifier: canonicaltypes.WidthModifierFixed, Width: 16}, false},
		{"ts_ms", &arrow.TimestampType{Unit: arrow.Millisecond}, canonicaltypes.TemporalTypeAstNode{BaseType: canonicaltypes.BaseTypeTemporalUtcDatetime, Width: 32}, false},
		{"ts_ns", &arrow.TimestampType{Unit: arrow.Nanosecond}, canonicaltypes.TemporalTypeAstNode{BaseType: canonicaltypes.BaseTypeTemporalUtcDatetime, Width: 64}, false},
		{"date32", arrow.FixedWidthTypes.Date32, canonicaltypes.TemporalTypeAstNode{BaseType: canonicaltypes.BaseTypeTemporalUtcDatetime, Width: 32}, false},
		// A dictionary is an encoding of its value type: same canonical type,
		// low-card flag set.
		{"lowcard_utf8", dict(arrow.BinaryTypes.String), canonicaltypes.StringAstNode{BaseType: canonicaltypes.BaseTypeStringUtf8}, true},
		// A list promotes its element to a homogeneous array.
		{"list_int32", arrow.ListOf(arrow.PrimitiveTypes.Int32), canonicaltypes.MachineNumericTypeAstNode{BaseType: canonicaltypes.BaseTypeMachineNumericSigned, Width: 32, ScalarModifier: canonicaltypes.ScalarModifierHomogenousArray}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, low := arrowToCanonical(tc.in)
			require.Equal(t, tc.want, got)
			require.Equal(t, tc.lowCard, low)
		})
	}
}

// TestArrowToCanonicalUnmapped: an unhandled physical type yields a nil node
// (rendered as an empty-type placeholder) rather than a panic or a wrong guess.
func TestArrowToCanonicalUnmapped(t *testing.T) {
	got, low := arrowToCanonical(&arrow.Decimal128Type{Precision: 18, Scale: 4})
	require.Nil(t, got)
	require.False(t, low)
}

// TestInferTableDescPlainOpaque: every result column becomes a plain opaque
// value column, there are no tagged sections, and the low-card hint lands only
// on the dictionary column.
func TestInferTableDescPlainOpaque(t *testing.T) {
	schema := schemaWith(
		arrow.Field{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		arrow.Field{Name: "name", Type: arrow.BinaryTypes.String},
		arrow.Field{Name: "kind", Type: &arrow.DictionaryType{IndexType: arrow.PrimitiveTypes.Uint16, ValueType: arrow.BinaryTypes.String}},
	)
	td := inferOpaqueTableDesc(schema.Fields())
	require.NotNil(t, td)
	require.Empty(t, td.TaggedValuesSections)
	require.Len(t, td.PlainValuesNames, 3)
	require.Len(t, td.PlainValuesTypes, 3)
	require.Len(t, td.PlainValuesItemTypes, 3)
	require.Len(t, td.PlainValuesEncodingHints, 3)
	require.Len(t, td.PlainValuesValueSemantics, 3)

	for i, it := range td.PlainValuesItemTypes {
		require.Equalf(t, common.PlainItemTypeOpaque, it, "column %d should be opaque", i)
	}
	require.True(t, td.PlainValuesEncodingHints[0].IsEmptySet(), "id has no encoding hint")
	require.True(t, td.PlainValuesEncodingHints[1].IsEmptySet(), "name has no encoding hint")
	require.True(t, td.PlainValuesEncodingHints[2].Contains(encodingaspects.AspectInterRecordLowCardinality), "kind is low-cardinality")
}

// TestInferTableDescVerbatimNames: non-leeway result column names come from
// arbitrary SQL and are shown verbatim, including forms that aren't valid
// StylableNames.
func TestInferTableDescVerbatimNames(t *testing.T) {
	td := inferOpaqueTableDesc(schemaWith(
		arrow.Field{Name: "count()", Type: arrow.PrimitiveTypes.Uint64},
		arrow.Field{Name: "a + b", Type: arrow.PrimitiveTypes.Float64},
	).Fields())
	require.Equal(t, "count()", td.PlainValuesNames[0].String())
	require.Equal(t, "a + b", td.PlainValuesNames[1].String())
}

// TestInferOpaqueTableDescEmpty: no fields yields an empty (non-nil) TableDesc.
func TestInferOpaqueTableDescEmpty(t *testing.T) {
	td := inferOpaqueTableDesc(nil)
	require.NotNil(t, td)
	require.Empty(t, td.PlainValuesNames)
	require.Empty(t, td.TaggedValuesSections)
}

// TestCardDriverReconstructsLeewaySchema proves the faithful path: a leeway-
// shaped result's physical column names reconstruct the authored TableDesc —
// tagged sections and all — through the app's single derivation (CardDriver),
// which the Schema pane consumes instead of re-running discovery. A non-leeway
// schema reconstructs to nothing, so the pane falls back to the opaque inference.
func TestCardDriverReconstructsLeewaySchema(t *testing.T) {
	schema := anchor.CreateSchemaTestTable()
	cards := NewCardDriver(c.NewWidgetIdStack(), memory.NewGoAllocator())
	require.True(t, cards.EnsureFor(schema), "anchor schema must be leeway-shaped")

	td := cards.TableDesc()
	require.NotNil(t, td)
	require.NotEmpty(t, td.TaggedValuesSections, "reconstruction recovers tagged sections from the column names")

	require.False(t, cards.EnsureFor(schemaWith(strField("count()"))))
	require.Nil(t, cards.TableDesc())
}

// TestCardDriverCachesNonLeewaySchema locks in the per-frame anti-spam
// contract. EnsureFor runs every frame from the Detail tab, and a non-leeway
// result (an aggregation, a join, arbitrary SQL) never yields a driver — so it
// must be probed, and its fallback logged, at most once per schema pointer, not
// once per frame. Regression guard against re-adding a `driver != nil` clause
// to EnsureFor's pointer-identity cache, which would re-run discovery and
// re-log on every frame.
func TestCardDriverCachesNonLeewaySchema(t *testing.T) {
	prev := log.Logger
	var buf bytes.Buffer
	log.Logger = zerolog.New(&buf).Level(zerolog.DebugLevel)
	defer func() { log.Logger = prev }()

	cards := NewCardDriver(c.NewWidgetIdStack(), memory.NewGoAllocator())
	// One stable schema pointer, as rec.Schema() hands back per result.
	schema := schemaWith(strField("count()"))

	const frames = 5
	for range frames {
		require.False(t, cards.EnsureFor(schema), "a non-leeway schema is never card-usable")
	}
	require.Nil(t, cards.TableDesc())

	got := strings.Count(buf.String(), "not leeway-shaped")
	require.Equalf(t, 1, got,
		"the non-leeway fallback must log once per schema, not once per frame (got %d over %d frames)", got, frames)
}

// TestSchemaPanelContract: the panel declares one required main channel and
// gates only on schema presence.
func TestSchemaPanelContract(t *testing.T) {
	var p PanelI = schemaPanel{}
	require.Equal(t, PanelID("schema"), p.ID())
	require.Equal(t, []ChannelSpec{{ID: chMain, Required: true, Label: "schema"}}, p.Channels())

	_, reason := p.AcceptForChannel(chMain, nil, playSignals{})
	require.NotEmpty(t, reason)

	claim, reason := p.AcceptForChannel(chMain, schemaWith(strField("c")), playSignals{})
	require.Empty(t, reason)
	require.Nil(t, claim)
}
