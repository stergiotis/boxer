package goplan_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/goplan"
)

// Direct coverage of PlanBuilder — the shared per-field validation + assembly
// both front-ends feed (marshallgen via go/ast, marshallreflect via reflect).
// Previously it was exercised only transitively through the two front-ends;
// these tests pin the acceptance / rejection contract that is the whole point
// of the shared package ("the two front-ends cannot drift on what they accept").

// fld is one field fed to the builder: an `_` underscore field (kind / const)
// or a regular value/plain/carrier field (name + shape).
type fld struct {
	us          bool   // underscore field
	kind, plain string // underscore tag values
	lw          string
	name        string
	shape       goplan.FieldShape
}

func buildPlan(specs ...fld) (*mappingplan.Plan, error) {
	b := goplan.NewPlanBuilder("test.go", "demo", "MyDTO")
	for _, s := range specs {
		var err error
		if s.us {
			err = b.AddUnderscoreField(s.kind, s.plain, s.lw)
		} else {
			err = b.AddField(s.name, s.lw, s.shape)
		}
		if err != nil {
			return nil, err
		}
	}
	return b.Finish()
}

// scalarCanon mirrors the front-ends: it maps a scalar Go-type spelling to
// its leeway canonical, panicking on an unmapped type (tests pass only
// mapped spellings; the unsupported-plain-type case builds its canonical
// directly). PlanBuilder derives GoType / IsSlice / IsRoaring back from it.
func scalarCanon(goType string) canonicaltypes.PrimitiveAstNodeI {
	c, err := goplan.ScalarCanonicalForGoType(goType)
	if err != nil {
		panic(err)
	}
	return c
}

func shp(goType string) goplan.FieldShape {
	return goplan.FieldShape{Canonical: scalarCanon(goType)}
}
func sliceShp(elem string) goplan.FieldShape {
	return goplan.FieldShape{Canonical: canonicaltypes.PromoteScalarPrim(scalarCanon(elem), canonicaltypes.ScalarModifierHomogenousArray)}
}
func optionShp(goType string) goplan.FieldShape {
	return goplan.FieldShape{Canonical: scalarCanon(goType), IsOption: true}
}
func carrierShp(name string) goplan.FieldShape {
	return goplan.FieldShape{CarrierType: name}
}
func carrierSliceShp(name string) goplan.FieldShape {
	return goplan.FieldShape{CarrierType: name, CarrierIsSlice: true}
}

// Common building blocks.
var (
	kindUS = fld{us: true, kind: "my"}
	idCol  = fld{name: "Id", lw: ",id", shape: shp("uint64")}
)

func TestPlanBuilder_Accept(t *testing.T) {
	cases := []struct {
		name  string
		specs []fld
		check func(t *testing.T, p *mappingplan.Plan)
	}{
		{
			name:  "minimal id-only",
			specs: []fld{kindUS, idCol},
			check: func(t *testing.T, p *mappingplan.Plan) {
				require.Equal(t, "my", p.KindName)
				require.Len(t, p.PlainCols, 1)
			},
		},
		{
			name: "all four plain roles",
			specs: []fld{kindUS, idCol,
				{name: "Nk", lw: ",naturalKey", shape: shp("[]byte")},
				{name: "Ts", lw: ",ts", shape: shp("time.Time")},
				{name: "Exp", lw: ",expiresAt", shape: shp("time.Time")},
			},
			check: func(t *testing.T, p *mappingplan.Plan) { require.Len(t, p.PlainCols, 4) },
		},
		{
			name:  "tagged scalar",
			specs: []fld{kindUS, idCol, {name: "V", lw: "v,sym", shape: shp("string")}},
			check: func(t *testing.T, p *mappingplan.Plan) { require.Len(t, p.Fields, 1) },
		},
		{
			name:  "tagged slice (allowlist elem)",
			specs: []fld{kindUS, idCol, {name: "V", lw: "v,arr", shape: sliceShp("uint32")}},
		},
		{
			name: "multi-sub-column shares one membership",
			specs: []fld{kindUS, idCol,
				{name: "B", lw: "rng,u32Range:beginIncl", shape: shp("uint32")},
				{name: "E", lw: "rng,u32Range:endExcl", shape: shp("uint32")},
			},
		},
		{
			name:  "const on underscore",
			specs: []fld{kindUS, idCol, {us: true, lw: "appId,sym,const=x"}},
			check: func(t *testing.T, p *mappingplan.Plan) {
				require.Len(t, p.Fields, 1)
				require.True(t, p.Fields[0].IsConst)
			},
		},
		{
			name: "const shares a section (not membership) with a value field",
			specs: []fld{kindUS, idCol,
				{us: true, lw: "constMemb,sym,const=x"},
				{name: "V", lw: "valMemb,sym", shape: shp("string")},
			},
		},
		{
			name: "carrier pair (mixedLowCardRef)",
			specs: []fld{kindUS, idCol,
				{name: "V", lw: "m,sec,mixedLowCardRef", shape: shp("uint32")},
				{name: "C", lw: "m,sec,mixedLowCardRef", shape: carrierShp("MixedLowCardRef")},
			},
			check: func(t *testing.T, p *mappingplan.Plan) {
				require.Equal(t, "C", p.Fields[0].CarrierField)
				require.Equal(t, "MixedLowCardRef", p.Fields[0].CarrierType)
			},
		},
		{
			name: "parametrized carrier pair",
			specs: []fld{kindUS, idCol,
				{name: "V", lw: "m,sec,lowCardRefParametrized", shape: shp("uint32")},
				{name: "C", lw: "m,sec,lowCardRefParametrized", shape: carrierShp("Parametrized")},
			},
		},
		{
			name: "carrier container value pairs a scalar carrier (ADR-0008 OQ#4)",
			specs: []fld{kindUS, idCol,
				{name: "V", lw: "m,sec,mixedLowCardRef", shape: sliceShp("uint32")},
				{name: "C", lw: "m,sec,mixedLowCardRef", shape: carrierShp("MixedLowCardRef")},
			},
			check: func(t *testing.T, p *mappingplan.Plan) { require.False(t, p.Fields[0].CarrierIsSlice) },
		},
		{
			name: "carrier exploded value pairs a slice carrier (ADR-0008 OQ#4)",
			specs: []fld{kindUS, idCol,
				{name: "V", lw: "m,sec,explode,mixedLowCardRef", shape: sliceShp("uint32")},
				{name: "C", lw: "m,sec,mixedLowCardRef", shape: carrierSliceShp("MixedLowCardRef")},
			},
			check: func(t *testing.T, p *mappingplan.Plan) { require.True(t, p.Fields[0].CarrierIsSlice) },
		},
		{
			name: "carrier Option value pairs a scalar carrier (ADR-0008 OQ#4)",
			specs: []fld{kindUS, idCol,
				{name: "V", lw: "m,sec,mixedLowCardRef", shape: optionShp("uint32")},
				{name: "C", lw: "m,sec,mixedLowCardRef", shape: carrierShp("MixedLowCardRef")},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := buildPlan(tc.specs...)
			require.NoError(t, err)
			require.NotNil(t, p)
			if tc.check != nil {
				tc.check(t, p)
			}
		})
	}
}

func TestPlanBuilder_Reject(t *testing.T) {
	cases := []struct {
		name   string
		specs  []fld
		substr string
	}{
		{"missing kind", []fld{idCol}, "missing the `_` entity-level field"},
		{"no plain cols", []fld{kindUS, {name: "V", lw: "v,sym", shape: shp("string")}}, "declares no plain columns"},
		{"missing id plain", []fld{kindUS, {name: "Ts", lw: ",ts", shape: shp("time.Time")}}, "missing required plain column `id`"},
		// A u128 scalar has no Go builtin; the canonical->Go derivation now
		// rejects it at the codegen layer (review B-2/D-5) before goplan's own
		// plain-column-shape check runs.
		{"plain unsupported type", []fld{kindUS, {name: "Id", lw: ",id", shape: goplan.FieldShape{Canonical: canonicaltypes.MachineNumericTypeAstNode{BaseType: canonicaltypes.BaseTypeMachineNumericUnsigned, Width: 128}}}}, "not implemented"},
		{"plain unknown column", []fld{kindUS, idCol, {name: "X", lw: ",bogus", shape: shp("uint64")}}, "unknown plain column"},
		{"plain carries a flag", []fld{kindUS, idCol, {name: "X", lw: ",ts,unit", shape: shp("time.Time")}}, "plain field cannot carry"},
		{"plain non-scalar", []fld{kindUS, {name: "Id", lw: ",id", shape: sliceShp("uint64")}}, "plain field must be a scalar"},
		{"duplicate plain column", []fld{kindUS, idCol, {name: "Id2", lw: ",id", shape: shp("uint64")}}, "plain column declared on two"},
		{"slice elem not supported", []fld{kindUS, idCol, {name: "V", lw: "v,arr", shape: sliceShp("time.Time")}}, "slice element type not yet supported"},
		{"duplicate membership+column", []fld{kindUS, idCol, {name: "A", lw: "m,sym", shape: shp("string")}, {name: "B", lw: "m,sym", shape: shp("string")}}, "membership+column appears on two"},
		{"explode on scalar", []fld{kindUS, idCol, {name: "V", lw: "v,sym,explode", shape: shp("string")}}, "requires a multi-element"},
		{"unit on multi without explode", []fld{kindUS, idCol, {name: "V", lw: "v,arr,unit", shape: sliceShp("uint32")}}, "requires `explode`"},
		{"const on non-underscore field", []fld{kindUS, idCol, {name: "V", lw: "v,sym,const=x", shape: shp("string")}}, "only valid on `_`"},
		{"section mixes channels", []fld{kindUS, idCol, {name: "A", lw: "a,sym", shape: shp("string")}, {name: "B", lw: "b,sym,verbatim", shape: shp("string")}}, "section mixes membership channels"},
		{"const+value share ref membership", []fld{kindUS, idCol, {us: true, lw: "m,secA,const=x"}, {name: "V", lw: "m,secB", shape: shp("string")}}, "kindXxx symbols would collide"},

		// `_` underscore-field grammar.
		{"two kind tags", []fld{kindUS, {us: true, kind: "other"}, idCol}, "only one entity-level kind name"},
		{"retired plain map", []fld{kindUS, idCol, {us: true, plain: "id=Id"}}, "`plain:` map is retired"},
		{"bare underscore lw without const", []fld{kindUS, idCol, {us: true, lw: "m,sec"}}, "must declare `,const="},
		{"const without section", []fld{kindUS, idCol, {us: true, lw: "m,,const=x"}}, "requires a section name"},
		{"const targeting sub-column", []fld{kindUS, idCol, {us: true, lw: "m,sec:col,const=x"}}, "cannot target a sub-column"},
		{"const with explode", []fld{kindUS, idCol, {us: true, lw: "m,sec,explode,const=x"}}, "cannot combine with `explode`"},

		// Carrier (mixed / parametrized) pairing.
		{"carrier value with sub-column", []fld{kindUS, idCol, {name: "V", lw: "m,sec:col,mixedLowCardRef", shape: shp("uint32")}}, "cannot target a sub-column"},
		{"carrier value roaring forbidden", []fld{kindUS, idCol, {name: "V", lw: "m,sec,mixedLowCardRef", shape: goplan.FieldShape{Canonical: canonicaltypes.PromoteScalarPrim(goplan.RoaringElemCanonical(), canonicaltypes.ScalarModifierSet)}}, {name: "C", lw: "m,sec,mixedLowCardRef", shape: carrierShp("MixedLowCardRef")}}, "cannot be a roaring bitmap"},
		{"exploded value needs slice carrier", []fld{kindUS, idCol, {name: "V", lw: "m,sec,explode,mixedLowCardRef", shape: sliceShp("uint32")}, {name: "C", lw: "m,sec,mixedLowCardRef", shape: carrierShp("MixedLowCardRef")}}, "carrier multiplicity must match"},
		{"container value needs scalar carrier", []fld{kindUS, idCol, {name: "V", lw: "m,sec,mixedLowCardRef", shape: sliceShp("uint32")}, {name: "C", lw: "m,sec,mixedLowCardRef", shape: carrierSliceShp("MixedLowCardRef")}}, "carrier multiplicity must match"},
		{"value carrier-channel without carrier", []fld{kindUS, idCol, {name: "V", lw: "m,sec,mixedLowCardRef", shape: shp("uint32")}}, "needs a sibling carrier"},
		{"carrier without value sibling", []fld{kindUS, idCol, {name: "C", lw: "m,sec,mixedLowCardRef", shape: carrierShp("MixedLowCardRef")}}, "has no value sibling"},
		{"carrier type mismatches channel", []fld{kindUS, idCol, {name: "V", lw: "m,sec,mixedLowCardRef", shape: shp("uint32")}, {name: "C", lw: "m,sec,mixedLowCardRef", shape: carrierShp("Parametrized")}}, "carrier type does not match"},
		{
			"carrier section with two memberships",
			[]fld{kindUS, idCol,
				{name: "V", lw: "m,sec,mixedLowCardRef", shape: shp("uint32")},
				{name: "C", lw: "m,sec,mixedLowCardRef", shape: carrierShp("MixedLowCardRef")},
				{name: "V2", lw: "m2,sec,mixedLowCardRef", shape: shp("uint32")},
				{name: "C2", lw: "m2,sec,mixedLowCardRef", shape: carrierShp("MixedLowCardRef")},
			},
			"only one membership",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := buildPlan(tc.specs...)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.substr)
		})
	}
}

// TestPlanBuilder_MembershipColonNoFalseCollision pins the 2026-06-14 review
// fix: the (membership, sub-column) uniqueness key uses a NUL separator, not
// ":", so a colon inside a verbatim membership name cannot alias a
// membership+column pair. Here a scalar field with membership "a:b" and a
// multi-sub-column section whose membership is "a" with sub-column "b" are
// distinct, valid, emittable fields — but a ":" separator keyed both "a:b"
// and false-rejected the second as a duplicate. Verbatim is used so the
// colon label declares no kindXxx Go identifier.
func TestPlanBuilder_MembershipColonNoFalseCollision(t *testing.T) {
	p, err := buildPlan(
		kindUS, idCol,
		fld{name: "Alpha", lw: "a:b,sym,verbatim", shape: shp("string")},
		fld{name: "Beg", lw: "a,rng:b,verbatim", shape: shp("uint32")},
		fld{name: "End", lw: "a,rng:c,verbatim", shape: shp("uint32")},
	)
	require.NoError(t, err, "membership %q and membership %q+column %q must not collide", "a:b", "a", "b")
	require.Len(t, p.Fields, 3)
}

// TestPlanBuilder_RefMembershipMustBeIdentifier pins the identifier rule
// at the shared (both-front-end) level: EVERY ref-channel field's
// membership becomes kind<Upper(memb)> in the marshallgen core emit
// (membership-keyed so kind vars stay unique across kinds generated into
// one package — ADR-0100 stores), so a non-identifier name is rejected at
// plan-build, const and value fields alike. Verbatim memberships are wire
// labels, never identifiers, so they are unrestricted.
func TestPlanBuilder_RefMembershipMustBeIdentifier(t *testing.T) {
	// const ref membership with an illegal identifier char → rejected.
	for _, bad := range []string{"bad-name", "foo.bar", "foo bar", "a:b"} {
		_, err := buildPlan(kindUS, idCol,
			fld{us: true, lw: bad + ",symbol,const=v"})
		require.Errorf(t, err, "const ref membership %q must be rejected", bad)
		if err != nil {
			require.Contains(t, err.Error(), "Go identifier")
		}
	}

	// A value ref field's membership mints the same kind<Upper(memb)>
	// symbol, so it is held to the same rule.
	_, err := buildPlan(kindUS, idCol,
		fld{name: "App", lw: "my-app,symbol,highCardRef", shape: shp("string")})
	require.Error(t, err, "value ref membership must be a Go identifier too")

	// A verbatim const membership is a wire label, not an identifier — OK.
	_, err = buildPlan(kindUS, idCol,
		fld{us: true, lw: "any-label.v2,symbol,verbatim,const=v"})
	require.NoError(t, err, "verbatim membership names need not be identifiers")

	// A leading digit is fine — the name is only ever an identifier *suffix*
	// (kind<Name>), so "kind3d" is valid.
	_, err = buildPlan(kindUS, idCol,
		fld{us: true, lw: "3d,symbol,const=v"})
	require.NoError(t, err, "a leading digit is valid after the kind prefix")
}
