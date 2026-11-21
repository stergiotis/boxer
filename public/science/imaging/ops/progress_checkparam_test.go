package ops

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/science/imaging/samples"
)

// Progress counts the rows of the first passes before the output's, so an
// operator that scans the image first shows progress while it scans; one
// that makes no first pass counts the output rows alone, as before.
func TestProgressCountsTheFirstPasses(t *testing.T) {
	img, err := samples.ByName(samples.NameElevation)
	require.NoError(t, err)
	h := int64(img.Height())
	for _, tc := range []struct {
		op     string
		params Params
		passes int64
	}{
		{"standardize", nil, 2},
		{"gaussian", Params{"sigma": Number(1), "border": Text("nearest")}, 0},
	} {
		var calls [][2]int64
		run(t, tc.op, tc.params, Options{BandRows: 64, Progress: func(done, total int64) {
			calls = append(calls, [2]int64{done, total})
		}}, in("elevation", img))
		require.NotEmpty(t, calls, tc.op)
		want := (tc.passes + 1) * h
		var last int64
		sawFirstPass := false
		for _, c := range calls {
			assert.Equal(t, want, c[1], "%s: one total throughout", tc.op)
			assert.GreaterOrEqual(t, c[0], last, "%s: never backwards", tc.op)
			last = c[0]
			sawFirstPass = sawFirstPass || c[0] <= tc.passes*h && tc.passes > 0
		}
		assert.Equal(t, want, last, "%s: ends at the total", tc.op)
		assert.Equal(t, tc.passes > 0, sawFirstPass, "%s: progress during the first passes", tc.op)
	}
}

// CheckParam judges one field as Validate judges the whole: the canonical
// value, the default for none, a refusal naming the parameter otherwise.
func TestCheckParamJudgesOneField(t *testing.T) {
	d := gaussianDeclaration
	v, err := d.CheckParam("sigma", Number(2.5))
	require.NoError(t, err)
	assert.Equal(t, Number(2.5), v)
	v, err = d.CheckParam("sigma", Value{})
	require.NoError(t, err)
	assert.Equal(t, Number(1), v, "none is the default")
	_, err = d.CheckParam("sigma", Number(100))
	require.ErrorIs(t, err, ErrRefused)
	_, err = d.CheckParam("sigma", Text("wide"))
	require.ErrorIs(t, err, ErrRefused)
	_, err = d.CheckParam("border", Value{})
	require.ErrorIs(t, err, ErrRefused, "the border is required and has no default")
	v, err = d.CheckParam("border", Text("nearest"))
	require.NoError(t, err)
	assert.Equal(t, Text("nearest"), v)
	_, err = d.CheckParam("radius", Number(1))
	require.ErrorIs(t, err, ErrRefused, "a parameter the operator does not declare")

	v, err = exprDeclaration.CheckParam("expression", Text("( a+b )*2"))
	require.NoError(t, err)
	assert.Equal(t, Text("(a + b) * 2"), v, "an expression in its canonical spelling")
	_, err = exprDeclaration.CheckParam("expression", Text("a +"))
	require.ErrorIs(t, err, ErrRefused)

	p, _ := axisStatDeclaration.Param("stat")
	assert.Equal(t, []string{"mean", "median", "min", "max", "stddev", "count"}, p.ChoiceNames())
	p, _ = gaussianDeclaration.Param("sigma")
	assert.Nil(t, p.ChoiceNames())

	assert.True(t, IsInputName("B4"))
	assert.False(t, IsInputName("min"), "a function's name")
	assert.False(t, IsInputName("4b"))
}

// An input taken from a stack is the plane at the positions given, with
// every axis named in its positions, and a run over two of them records
// where each came from.
func TestStackInputNamesItsPositions(t *testing.T) {
	st, err := samples.StackByName(samples.StackBands)
	require.NoError(t, err)
	nir, err := StackInput("nir", st, []int{1, 0}, 3)
	require.NoError(t, err)
	assert.Equal(t, []Position{{Axis: "time", Index: 1, Label: "day 16"}, {Axis: "band", Index: 3, Label: "B8"}}, nir.Positions)
	want, err := st.Image([]int{1, 0}, []int{3})
	require.NoError(t, err)
	assert.Equal(t, want.Hash(), [32]byte(nir.Source.Provenance().SampleHash))
	red, err := StackInput("red", st, []int{1, 2}, 2)
	require.NoError(t, err)
	assert.Equal(t, 2, red.Positions[1].Index, "the band axis's entry of at is not read")

	res := run(t, "expr", Params{"expression": Text("(nir - red) / (nir + red)")}, Options{}, nir, red)
	require.Len(t, res.Record.Inputs, 2)
	assert.Equal(t, nir.Positions, res.Record.Inputs[0].Positions)
	assert.Equal(t, "B4", res.Record.Inputs[1].Positions[1].Label)

	_, err = StackInput("x", st, []int{5, 0}, 0)
	require.Error(t, err, "a position outside its axis")
}
