package ops

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/eh/eb/ebtest"
	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/samples"
)

func TestRegistryHoldsTheFiveOperators(t *testing.T) {
	var ids []string
	for op := range All() {
		ids = append(ids, op.Declaration().Id)
		require.NoError(t, checkDeclaration(op.Declaration()))
	}
	require.Equal(t, []string{"axis-stat", "expr", "gaussian", "median-filter", "standardize"}, ids)
	_, ok := Lookup("gaussian")
	require.True(t, ok)
	_, ok = Lookup("sharpen")
	require.False(t, ok)
	require.Error(t, Register(gaussianOperator{}), "an id is registered once")
}

func TestValidateFillsDefaultsAndCanonicalises(t *testing.T) {
	got, err := gaussianDeclaration.Validate(Params{"border": Text("nearest")})
	require.NoError(t, err)
	require.Equal(t, Params{"sigma": Number(1), "border": Text("nearest")}, got)

	got, err = exprDeclaration.Validate(Params{"expression": Text(" ( a-b )/(a+b)")})
	require.NoError(t, err)
	require.Equal(t, Params{"expression": Text("(a - b) / (a + b)")}, got)

	got, err = medianFilterDeclaration.Validate(Params{"radius": Number(2), "border": Text("invalid")})
	require.NoError(t, err)
	r, ok := got["radius"].Integer()
	require.True(t, ok)
	require.EqualValues(t, 2, r)
}

func TestValidateRefusesNamingTheParameter(t *testing.T) {
	for _, c := range []struct {
		decl   Declaration
		params Params
		param  string
	}{
		{gaussianDeclaration, Params{"sigma": Number(2)}, "border"},
		{gaussianDeclaration, Params{"border": Text("wrap")}, "border"},
		{gaussianDeclaration, Params{"border": Text("nearest"), "sigma": Number(0)}, "sigma"},
		{gaussianDeclaration, Params{"border": Text("nearest"), "sigma": Text("2")}, "sigma"},
		{gaussianDeclaration, Params{"border": Text("nearest"), "radius": Number(2)}, "radius"},
		{medianFilterDeclaration, Params{"border": Text("nearest"), "radius": Number(1.5)}, "radius"},
		{medianFilterDeclaration, Params{"border": Text("nearest"), "radius": Number(17)}, "radius"},
		{axisStatDeclaration, Params{}, "stat"},
		{axisStatDeclaration, Params{"stat": Text("mode")}, "stat"},
		{exprDeclaration, Params{"expression": Text("a +")}, "expression"},
		{exprDeclaration, Params{"expression": Number(1)}, "expression"},
	} {
		_, err := c.decl.Validate(c.params)
		require.ErrorIs(t, err, ErrRefused, "%s %v", c.decl.Id, c.params)
		require.Equal(t, c.param, ebtest.Fields(t, err)["param"], "%s %v", c.decl.Id, c.params)
	}
	_, err := exprDeclaration.Validate(Params{"expression": Text("a + * b")})
	var pe *ParseError
	require.ErrorAs(t, err, &pe, "a parse error reaches the caller with its column")
	require.Equal(t, 5, pe.Column)
	_, err = gaussianDeclaration.Validate(Params{"sigma": Number(2)})
	require.Equal(t, []any{"invalid", "nearest"}, ebtest.Fields(t, err)["choices"], "a missing border names the choices")
}

func TestHelpStatesEveryParameter(t *testing.T) {
	for op := range All() {
		d := op.Declaration()
		help := d.Help()
		require.True(t, strings.HasPrefix(help, d.Ref()+" — "), help)
		for _, p := range d.Params {
			require.Contains(t, help, "  "+p.Name+" (", help)
			for _, c := range p.Choices {
				require.Contains(t, help, c.Name+" — "+c.Doc)
			}
		}
	}
	require.Contains(t, gaussianDeclaration.Help(), "sigma (number, px, 0.1 to 64, default 1)")
	require.Contains(t, gaussianDeclaration.Help(), "border (choice, required)")
}

func refusal(t *testing.T, req Request) map[string]any {
	t.Helper()
	_, err := Run(context.Background(), req, Options{})
	require.ErrorIs(t, err, ErrRefused)
	_, perr := PlanOf(req)
	require.ErrorIs(t, perr, ErrRefused, "the plan refuses what the run refuses")
	return ebtest.Fields(t, err)
}

func TestRefusalsNameTheirReason(t *testing.T) {
	a := kindImage(t, imaging.SampleKindU8, 10, 8, 1)
	b := kindImage(t, imaging.SampleKindU8, 10, 9, 2)
	rgb := imageOf(t, imaging.ColorModelRGB, imaging.SampleKindU8, 10, 8, 3)
	rgba := imageOf(t, imaging.ColorModelRGBA, imaging.SampleKindU8, 10, 8, 4)
	flat, err := imaging.NewPlaneU16(4, 3, make([]uint16, 12))
	require.NoError(t, err)
	flatImg, err := imaging.NewImage(imaging.ColorModelGray, []imaging.Plane{flat}, imaging.Provenance{})
	require.NoError(t, err)

	f := refusal(t, Request{Operator: "expr", Params: Params{"expression": Text("a - b")}, Inputs: []Input{in("a", a), in("b", b)}})
	require.Equal(t, "b", f["input"])
	require.Equal(t, "a", f["firstInput"])

	f = refusal(t, Request{Operator: "gaussian", Params: Params{"sigma": Number(1)}, Inputs: []Input{in("img", a)}})
	require.Equal(t, "border", f["param"])

	f = refusal(t, Request{Operator: "gaussian", Params: Params{"border": Text("nearest"), "sigma": Number(-1)}, Inputs: []Input{in("img", a)}})
	require.Equal(t, "sigma", f["param"])

	f = refusal(t, Request{Operator: "expr", Params: Params{"expression": Text("c * 2")}, Inputs: []Input{in("c", rgb)}})
	require.Equal(t, "c", f["input"])

	f = refusal(t, Request{Operator: "gaussian", Params: Params{"border": Text("nearest")}, Inputs: []Input{in("pic", rgba)}})
	require.Equal(t, "pic", f["input"])

	// A zero standard deviation is found by the first pass: the plan
	// cannot know it.
	_, err = Run(context.Background(), Request{Operator: "standardize", Inputs: []Input{in("flat", flatImg)}}, Options{})
	require.ErrorIs(t, err, ErrRefused)
	require.Equal(t, "gray", ebtest.Fields(t, err)["label"])

	f = refusal(t, Request{Operator: "expr", Params: Params{"expression": Text("a * 2")}, Inputs: []Input{in("a", a), in("unused", a)}})
	require.Equal(t, "unused", f["input"])

	f = refusal(t, Request{Operator: "expr", Params: Params{"expression": Text("a * z")}, Inputs: []Input{in("a", a)}})
	require.Equal(t, "z", f["name"])

	f = refusal(t, Request{Operator: "axis-stat", Params: Params{"stat": Text("mean")}, Inputs: []Input{in("a", a), in("a", a)}})
	require.Equal(t, "a", f["input"])

	f = refusal(t, Request{Operator: "expr", Params: Params{"expression": Text("abs * 2")}, Inputs: []Input{in("abs", a)}})
	require.Equal(t, "expression", f["param"], "a function name is no name the parser reads")
	f = refusal(t, Request{Operator: "axis-stat", Params: Params{"stat": Text("max")}, Inputs: []Input{in("sqrt", a)}})
	require.Equal(t, "sqrt", f["input"])

	f = refusal(t, Request{Operator: "gaussian", Params: Params{"border": Text("nearest")}, Inputs: []Input{in("a", a), in("b", a)}})
	require.Equal(t, 2, toInt(f["inputs"]))

	f = refusal(t, Request{Operator: "sharpen", Inputs: []Input{in("a", a)}})
	require.Equal(t, "sharpen", f["operator"])
}

func toInt(v any) int {
	switch x := v.(type) {
	case int64:
		return int(x)
	case uint64:
		return int(x)
	case int:
		return x
	}
	return -1
}

func TestPlanStatesTheOutputBeforeReading(t *testing.T) {
	img, err := samples.ByName(samples.NameCounts)
	require.NoError(t, err)
	plan, err := PlanOf(Request{Operator: "gaussian", Params: Params{"sigma": Number(2), "border": Text("invalid")}, Inputs: []Input{in("counts", img)}})
	require.NoError(t, err)
	require.Equal(t, img.Width(), plan.Width)
	require.Equal(t, img.Height(), plan.Height)
	require.Equal(t, imaging.SampleKindF64, plan.Kind, "i32 is not exact in a float32")
	require.Equal(t, imaging.IdentityTransform, plan.Transform)
	require.Equal(t, 8, plan.Halo)
	require.Equal(t, BorderInvalid, plan.Border)
	require.Equal(t, []string{"gray"}, plan.PlaneLabels)

	field, err := samples.ByName(samples.NameField)
	require.NoError(t, err)
	plan, err = PlanOf(Request{Operator: "standardize", Inputs: []Input{in("field", field)}})
	require.NoError(t, err)
	require.Equal(t, imaging.SampleKindF32, plan.Kind)
	require.Equal(t, 2, plan.FirstPasses)
}

func TestPlacementIsKeptWhenSharedAndDroppedOtherwise(t *testing.T) {
	stack, err := samples.StackByName(samples.StackBands)
	require.NoError(t, err)
	b8, err := stack.Image([]int{0, 0}, []int{3})
	require.NoError(t, err)
	b4, err := stack.Image([]int{0, 0}, []int{2})
	require.NoError(t, err)
	plan, err := PlanOf(Request{Operator: "expr", Params: Params{"expression": Text("B8 - B4")}, Inputs: []Input{in("B8", b8), in("B4", b4)}})
	require.NoError(t, err)
	require.Equal(t, samples.BandsPlacement, plan.Placement)

	moved := samples.BandsPlacement
	moved.Affine[0] += 10
	plan, err = PlanOf(Request{Operator: "expr", Params: Params{"expression": Text("B8 - B4")},
		Inputs: []Input{in("B8", b8), in("B4", b4.WithPlacement(moved))}})
	require.NoError(t, err)
	require.True(t, plan.Placement.IsZero())
	require.True(t, slices.ContainsFunc(plan.Notes, func(n string) bool { return strings.Contains(n, "placed differently") }))
}
