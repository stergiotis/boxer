package marshallreflect_test

import (
	"testing"

	"github.com/RoaringBitmap/roaring"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/functional/option"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
)

// These tests exercise the reflect front-end (classifyReflectType +
// buildPlan) feeding the shared goplan.PlanBuilder. The validation
// RULES are covered exhaustively by marshallgen's parse_test.go against
// the go/ast front-end; the cases here confirm the reflect front-end
// classifies each Go shape identically and routes rejections through the
// same shared validator. Before validation was factored into marshallgen,
// the reflect plan-build path had no rejection-path coverage at all.

func fieldByName(t *testing.T, plan *mappingplan.Plan, name string) mappingplan.TaggedField {
	t.Helper()
	for _, f := range plan.Fields {
		if f.GoFieldName == name {
			return f
		}
	}
	t.Fatalf("field %q not found in plan", name)
	return mappingplan.TaggedField{}
}

type shapeMix struct {
	_ struct{} `kind:"shapeMix"`

	Id     uint64                `lw:",id"`
	NK     []byte                `lw:",naturalKey"`
	Scalar string                `lw:"scalar,symbol"`
	Opt    option.Option[int64]  `lw:"opt,i64Array,unit"`
	Slice  []string              `lw:"slice,stringArray"`
	Bits   *roaring.Bitmap       `lw:"bits,u32Array"`
	Blob   option.Option[[]byte] `lw:"blob,blobArray,unit"`
	Fixed  [16]byte              `lw:"fixed,blob"`
}

// TestPlanFor_ClassifiesShapes confirms the reflect classifier maps each
// supported Go shape to the same FieldShape the AST classifier produces.
func TestPlanFor_ClassifiesShapes(t *testing.T) {
	plan, err := marshallreflect.PlanFor[shapeMix]()
	require.NoError(t, err)
	require.Len(t, plan.PlainCols, 2)
	require.Len(t, plan.Fields, 6)

	require.True(t, fieldByName(t, plan, "Opt").IsOption)
	require.Equal(t, "int64", fieldByName(t, plan, "Opt").GoType())
	require.True(t, fieldByName(t, plan, "Slice").IsSlice())
	require.True(t, fieldByName(t, plan, "Bits").IsRoaring())

	blob := fieldByName(t, plan, "Blob")
	require.True(t, blob.IsOption)
	require.Equal(t, "[]byte", blob.GoType(), "Option[[]byte] is the scalar-blob lane")

	require.Equal(t, "[16]byte", fieldByName(t, plan, "Fixed").GoType())
}

// --- Rejections routed through the shared validator. ---

type explodeScalar struct {
	_  struct{} `kind:"explodeScalar"`
	Id uint64   `lw:",id"`
	S  string   `lw:"s,symbol,explode"`
}

func TestPlanFor_RejectsExplodeRemoved(t *testing.T) {
	// ADR-0113 D1 cull: the flag errors at the shared grammar with the
	// nested replacement named.
	_, err := marshallreflect.PlanFor[explodeScalar]()
	require.ErrorContains(t, err, "removed (ADR-0113 D1)")
}

type dupMemb struct {
	_  struct{} `kind:"dupMemb"`
	Id uint64   `lw:",id"`
	A  string   `lw:"x,symbol"`
	B  string   `lw:"x,symbol"`
}

func TestPlanFor_RejectsDuplicateMembership(t *testing.T) {
	_, err := marshallreflect.PlanFor[dupMemb]()
	require.ErrorContains(t, err, "appears on two DTO fields")
}

type mixedChannels struct {
	_  struct{} `kind:"mixedChannels"`
	Id uint64   `lw:",id"`
	A  string   `lw:"a,symbol,verbatim"`
	B  string   `lw:"b,symbol"`
}

func TestPlanFor_RejectsMixedChannels(t *testing.T) {
	_, err := marshallreflect.PlanFor[mixedChannels]()
	require.ErrorContains(t, err, "mixes membership channels")
}

type constNonUnderscore struct {
	_  struct{} `kind:"constNonUnderscore"`
	Id uint64   `lw:",id"`
	S  string   `lw:"s,symbol,const=foo"`
}

func TestPlanFor_RejectsConstOnNonUnderscore(t *testing.T) {
	_, err := marshallreflect.PlanFor[constNonUnderscore]()
	require.ErrorContains(t, err, "only valid on `_` blank-identifier fields")
}

type noId struct {
	_  struct{} `kind:"noId"`
	Ts int64    `lw:",ts"`
}

func TestPlanFor_RejectsMissingId(t *testing.T) {
	_, err := marshallreflect.PlanFor[noId]()
	require.ErrorContains(t, err, "missing required plain column `id`")
}

// --- Rejections from the reflect-specific shape classifier. ---

type optOfSlice struct {
	_  struct{}                `kind:"optOfSlice"`
	Id uint64                  `lw:",id"`
	X  option.Option[[]string] `lw:"x,stringArray"`
}

func TestPlanFor_RejectsOptionOfSlice(t *testing.T) {
	_, err := marshallreflect.PlanFor[optOfSlice]()
	require.ErrorContains(t, err, "Option[[]T] is forbidden")
}

type sliceOfOpt struct {
	_  struct{}                `kind:"sliceOfOpt"`
	Id uint64                  `lw:",id"`
	X  []option.Option[string] `lw:"x,stringArray"`
}

func TestPlanFor_RejectsSliceOfOption(t *testing.T) {
	_, err := marshallreflect.PlanFor[sliceOfOpt]()
	require.ErrorContains(t, err, "[]option.Option[T] is forbidden")
}

type nonRoaringPtr struct {
	_  struct{} `kind:"nonRoaringPtr"`
	Id uint64   `lw:",id"`
	X  *int64   `lw:"x,i64Array"`
}

func TestPlanFor_RejectsNonRoaringPointer(t *testing.T) {
	_, err := marshallreflect.PlanFor[nonRoaringPtr]()
	require.ErrorContains(t, err, "pointer types forbidden except *roaring.Bitmap")
}
