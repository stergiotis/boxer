package regions

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/observability/eh/eb/ebtest"
	"github.com/stergiotis/boxer/public/science/imaging/samples"
)

var update = flag.Bool("update", false, "rewrite the golden region documents")

// goldenRegions is one region per shape kind, in each of the four frames.
func goldenRegions(t *testing.T) map[string]Region {
	anchor, err := NewAnchor(AxisPosition{Axis: "time", Position: 2}, AxisPosition{Axis: "time", Position: 0})
	require.NoError(t, err)
	return map[string]Region{
		"point": {Id: "Vh3kT0", Label: "star", Note: "brightest in the field", Origin: "person",
			Frame: Frame{Kind: FrameKindCelestial, Code: "ICRS"}, Shape: mustShape(NewPoint(Point{X: 150.0125, Y: 2.21}))},
		"line": {Id: "transect-1", Label: "transect", Origin: "regions test",
			Frame: PixelFrame(), Shape: mustShape(NewLine(Point{X: 0.5, Y: 10}, Point{X: 120.25, Y: 3}))},
		"rectangle": {Id: "plot-7", Label: "plot 7", Note: "rotated with the field", Origin: "person",
			Frame: Frame{Kind: FrameKindReferenceSystem, Code: "EPSG:32632"}, Anchor: anchor,
			Shape: mustShape(NewRectangle(Point{X: 500_400, Y: 5_199_600}, 400, 200, -12.5))},
		"ellipse": {Id: "cell-3", Label: "cell", Origin: "segmenter v2",
			Frame: Frame{Kind: FrameKindPhysical, Units: [2]string{"um", "um"}},
			Shape: mustShape(NewEllipse(Point{X: 12.3, Y: -4.06}, 3.5, 2.25, 0.1))},
		"polygon": {Id: "basin", Origin: "person",
			Frame: PixelFrame(), Shape: mustShape(NewPolygon([]Point{{70, 160}, {150, 170}, {140, 250}, {60, 230}}))},
	}
}

// Each shape kind has a golden document: written exactly so, and read back
// as the region it was written from.
func TestTheGoldenDocuments(t *testing.T) {
	for kind, r := range goldenRegions(t) {
		data, err := MarshalRegion(r)
		require.NoError(t, err)
		path := filepath.Join("testdata", kind+".json")
		if *update {
			require.NoError(t, os.WriteFile(path, append(data, '\n'), 0o644))
		}
		golden, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, strings.TrimSuffix(string(golden), "\n"), string(data), kind)
		back, err := UnmarshalRegion(golden)
		require.NoError(t, err)
		assert.Equal(t, r, back, kind)
	}
}

func drawRegion(t *rapid.T, id string) Region {
	frames := []Frame{PixelFrame(), {Kind: FrameKindPhysical, Units: [2]string{"mm", "mm"}},
		{Kind: FrameKindReferenceSystem, Code: "EPSG:2056"}, {Kind: FrameKindCelestial, Code: "FK5"}}
	r := Region{Id: id, Label: rapid.String().Draw(t, "label"), Note: rapid.String().Draw(t, "note"),
		Origin: rapid.String().Draw(t, "origin"), Frame: rapid.SampledFrom(frames).Draw(t, "frame")}
	num := func(label string) float64 { return rapid.Float64Range(-89, 89).Draw(t, label) }
	size := func(label string) float64 { return rapid.Float64Range(1e-9, 1e6).Draw(t, label) }
	var err error
	switch rapid.SampledFrom(AllShapeKinds).Draw(t, "kind") {
	case ShapeKindPoint:
		r.Shape, err = NewPoint(Point{X: num("x"), Y: num("y")})
	case ShapeKindLine:
		r.Shape, err = NewLine(Point{X: num("x0"), Y: num("y0")}, Point{X: num("x1"), Y: num("y1")})
	case ShapeKindRectangle:
		r.Shape, err = NewRectangle(Point{X: num("x"), Y: num("y")}, size("w"), size("h"), rapid.Float64().Draw(t, "angle"))
	case ShapeKindEllipse:
		r.Shape, err = NewEllipse(Point{X: num("x"), Y: num("y")}, size("rx"), size("ry"), rapid.Float64Range(-1e300, 1e300).Draw(t, "angle"))
	case ShapeKindPolygon:
		n := rapid.IntRange(3, 9).Draw(t, "n")
		vs := make([]Point, n)
		for i := range vs {
			vs[i] = Point{X: num("x"), Y: num("y")}
		}
		r.Shape, err = NewPolygon(vs)
	}
	if err != nil {
		t.Skip("no area")
	}
	var positions []AxisPosition
	for i := range rapid.IntRange(0, 3).Draw(t, "anchor") {
		positions = append(positions, AxisPosition{Axis: rapid.SampledFrom([]string{"time", "depth", "z"}).Draw(t, "axis"), Position: i})
	}
	if r.Anchor, err = NewAnchor(positions...); err != nil {
		t.Skip("repeated position")
	}
	return r
}

// A region, and a set of them, read back from its document exactly — every
// float64 to the bit.
func TestTheDocumentRoundTripsExactly(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		r := drawRegion(t, "a")
		data, err := MarshalRegion(r)
		require.NoError(t, err)
		back, err := UnmarshalRegion(data)
		require.NoError(t, err)
		require.Equal(t, r, back)
		set := []Region{r, drawRegion(t, "b")}
		data, err = MarshalRegions(set)
		require.NoError(t, err)
		backs, err := UnmarshalRegions(data)
		require.NoError(t, err)
		require.Equal(t, set, backs)
	})
	empty, err := MarshalRegions(nil)
	require.NoError(t, err)
	got, err := UnmarshalRegions(empty)
	require.NoError(t, err)
	assert.Empty(t, got)
}

// Reading is strict, and a refusal names what it refused.
func TestTheDocumentIsReadStrictly(t *testing.T) {
	ok := `{"version":1,"region":{"id":"a","label":"","note":"","origin":"","frame":{"kind":"pixel"},"anchor":[],"shape":{"kind":"point","at":[1,2]}}}`
	_, err := UnmarshalRegion([]byte(ok))
	require.NoError(t, err)
	for name, c := range map[string]struct {
		doc, key, want string
	}{
		"unknown shape kind":                {strings.Replace(ok, `"kind":"point"`, `"kind":"circle"`, 1), "kind", "circle"},
		"unknown frame kind":                {strings.Replace(ok, `"kind":"pixel"`, `"kind":"world"`, 1), "kind", "world"},
		"missing frame":                     {strings.Replace(ok, `"frame":{"kind":"pixel"},`, ``, 1), "member", "frame"},
		"missing shape":                     {strings.Replace(ok, `,"shape":{"kind":"point","at":[1,2]}`, ``, 1), "member", "shape"},
		"missing id":                        {strings.Replace(ok, `"id":"a",`, ``, 1), "member", "id"},
		"missing version":                   {strings.Replace(ok, `"version":1,`, ``, 1), "member", "version"},
		"another version":                   {strings.Replace(ok, `"version":1`, `"version":2`, 1), "version", "2"},
		"a member another kind has":         {strings.Replace(ok, `"at":[1,2]`, `"at":[1,2],"radii":[1,1]`, 1), "member", "radii"},
		"a missing member":                  {strings.Replace(ok, `"kind":"point","at":[1,2]`, `"kind":"line","from":[1,2]`, 1), "member", "to"},
		"code on a pixel frame":             {strings.Replace(ok, `"kind":"pixel"`, `"kind":"pixel","code":"EPSG:2056"`, 1), "member", "code"},
		"a reference system without a code": {strings.Replace(ok, `"kind":"pixel"`, `"kind":"reference-system"`, 1), "", ""},
		"three numbers for a point":         {strings.Replace(ok, `[1,2]`, `[1,2,3]`, 1), "member", "at"},
		"an anchor without a position":      {strings.Replace(ok, `"anchor":[]`, `"anchor":[{"axis":"time"}]`, 1), "member", "position"},
		"a negative position":               {strings.Replace(ok, `"anchor":[]`, `"anchor":[{"axis":"time","position":-1}]`, 1), "", ""},
		"a latitude past the pole":          {strings.Replace(strings.Replace(ok, `"kind":"pixel"`, `"kind":"celestial","code":"ICRS"`, 1), `[1,2]`, `[1,92]`, 1), "", ""},
		"an empty id":                       {strings.Replace(ok, `"id":"a"`, `"id":""`, 1), "", ""},
	} {
		_, err := UnmarshalRegion([]byte(c.doc))
		require.Error(t, err, name)
		if c.key != "" {
			assert.Equal(t, c.want, fmt.Sprint(ebtest.Fields(t, err)[c.key]), name)
		}
	}
	for name, doc := range map[string]string{
		"an unknown member":  strings.Replace(ok, `"label":""`, `"label":"","colour":"red"`, 1),
		"a duplicate member": strings.Replace(ok, `"label":""`, `"label":"","label":"x"`, 1),
		"not a number":       strings.Replace(ok, `[1,2]`, `[1,"2"]`, 1),
	} {
		_, err := UnmarshalRegion([]byte(doc))
		assert.Error(t, err, name)
	}
	set := `{"version":1,"regions":[` + ok[len(`{"version":1,"region":`):len(ok)-1] + `,` + ok[len(`{"version":1,"region":`):len(ok)-1] + `]}`
	_, err = UnmarshalRegions([]byte(set))
	require.Error(t, err)
	assert.Equal(t, "a", ebtest.Fields(t, err)["id"], "ids are unique in a set")
	_, err = UnmarshalRegions([]byte(`{"version":1}`))
	assert.Error(t, err)
	_, err = MarshalRegion(Region{Id: "x", Frame: PixelFrame()})
	assert.Error(t, err, "no shape")
	_, err = MarshalRegion(Region{Id: "x", Shape: mustShape(NewPoint(Point{}))})
	assert.Error(t, err, "no frame")
}

// An anchor applies at the stack positions whose position on each axis it
// names is one it names; an axis it does not name is any position.
func TestTheAnchorSaysWhereARegionApplies(t *testing.T) {
	stack, err := samples.StackByName(samples.StackBands)
	require.NoError(t, err)
	axes := stack.Axes()
	everywhere := Anchor{}
	applies, err := everywhere.AppliesAt(axes, []int{2, 3})
	require.NoError(t, err)
	assert.True(t, applies)
	a, err := NewAnchor(AxisPosition{Axis: "time", Position: 0}, AxisPosition{Axis: "time", Position: 2})
	require.NoError(t, err)
	for at, want := range map[int]bool{0: true, 1: false, 2: true} {
		applies, err = a.AppliesAt(axes, []int{at, 0})
		require.NoError(t, err)
		assert.Equal(t, want, applies, "time %d", at)
	}
	for name, p := range map[string]AxisPosition{
		"an axis the stack lacks": {Axis: "depth", Position: 0},
		"the band axis":           {Axis: "band", Position: 1},
		"past the axis":           {Axis: "time", Position: 3},
	} {
		b, err := NewAnchor(p)
		require.NoError(t, err)
		_, err = b.AppliesAt(axes, []int{0, 0})
		assert.Error(t, err, name)
	}
	_, err = a.AppliesAt(axes, []int{0})
	assert.Error(t, err)
	_, err = NewAnchor(AxisPosition{Axis: "time", Position: 1}, AxisPosition{Axis: "time", Position: 1})
	assert.Error(t, err)
	_, err = NewAnchor(AxisPosition{Position: 1})
	assert.Error(t, err)
	id, err := NewId()
	require.NoError(t, err)
	assert.NotEmpty(t, id)
}
