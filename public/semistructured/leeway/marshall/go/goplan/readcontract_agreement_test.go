package goplan_test

// ADR-0146 D1 derives a section's arity partly from whether any sub-column is
// scalar-shaped. It cannot call goplan.ClassifyBegin — mappingplan is the
// layer below goplan — so it uses mappingplan.TaggedField.IsMulti() and
// documents that the two agree. This test is what keeps that true: if
// ClassifyBegin ever stops being "IsMulti ⇒ container, otherwise scalar", the
// contract's arity table silently drifts from the emitter's call shape.

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/goplan"
)

func TestClassifyBeginAgreesWithIsMulti(t *testing.T) {
	scalar := ctabb.U64
	array := canonicaltypes.PromoteScalarPrim(ctabb.U64, canonicaltypes.ScalarModifierHomogenousArray)
	set := canonicaltypes.PromoteScalarPrim(ctabb.U64, canonicaltypes.ScalarModifierSet)

	cases := []struct {
		name  string
		field mappingplan.TaggedField
	}{
		{"scalar", mappingplan.TaggedField{Canonical: scalar}},
		{"scalar unit", mappingplan.TaggedField{Canonical: scalar, Flags: mappingplan.FieldFlags{Unit: true}}},
		{"scalar option", mappingplan.TaggedField{Canonical: scalar, IsOption: true}},
		{"const", mappingplan.TaggedField{IsConst: true, ConstValue: "x"}},
		{"homogenous array", mappingplan.TaggedField{Canonical: array}},
		{"set", mappingplan.TaggedField{Canonical: set}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			isContainer := goplan.ClassifyBegin(tc.field) == goplan.ShapeContainer
			require.Equal(t, tc.field.IsMulti(), isContainer,
				"ClassifyBegin's container class must be exactly IsMulti() — "+
					"mappingplan.DeriveReadContract depends on it")
		})
	}
}
