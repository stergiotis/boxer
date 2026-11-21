package imaging

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cube makes the planes of a stack over sizes, each plane w × h of kind,
// with plane f's samples all distinct from every other plane's.
func cube(t *testing.T, kind SampleKindE, w, h, count int) []Plane {
	t.Helper()
	planes := make([]Plane, count)
	for f := range planes {
		planes[f] = planeOfKind(t, kind, w, h, func(i int) float64 { return float64(f*100 + i) })
	}
	return planes
}

func dataOf(p Plane) unsafe.Pointer {
	switch p.Kind() {
	case SampleKindU8:
		return unsafe.Pointer(unsafe.SliceData(p.U8()))
	case SampleKindU16:
		return unsafe.Pointer(unsafe.SliceData(p.U16()))
	case SampleKindF32:
		return unsafe.Pointer(unsafe.SliceData(p.F32()))
	case SampleKindI16:
		return unsafe.Pointer(unsafe.SliceData(p.I16()))
	case SampleKindI32:
		return unsafe.Pointer(unsafe.SliceData(p.I32()))
	case SampleKindF64:
		return unsafe.Pointer(unsafe.SliceData(p.F64()))
	}
	return nil
}

func TestNewStackRefusesWhatItMust(t *testing.T) {
	planes := cube(t, SampleKindI16, 3, 2, 6)
	band := Axis{Name: "band", Role: AxisRoleBand, Size: 3}
	time := Axis{Name: "time", Role: AxisRoleSelector, Size: 2}
	other := planeOfKind(t, SampleKindI32, 3, 2, func(int) float64 { return 0 })
	wide := planeOfKind(t, SampleKindI16, 4, 2, func(int) float64 { return 0 })
	withInvalid, err := planes[5].WithInvalid(-1)
	require.NoError(t, err)
	withOther, err := planes[5].WithInvalid(-2)
	require.NoError(t, err)
	allInvalid := make([]Plane, 6)
	for i, p := range planes {
		allInvalid[i], err = p.WithInvalid(-1)
		require.NoError(t, err)
	}
	replace := func(i int, p Plane) []Plane {
		out := append([]Plane(nil), planes...)
		out[i] = p
		return out
	}
	twoInvalid := append([]Plane(nil), allInvalid...)
	twoInvalid[5] = withOther

	refused := []struct {
		name   string
		axes   []Axis
		planes []Plane
		model  ColorModelE
		place  Placement
	}{
		{"too few planes", []Axis{time, band}, planes[:5], 0, Placement{}},
		{"too many planes", []Axis{band}, planes, 0, Placement{}},
		{"no axes but many planes", nil, planes, 0, Placement{}},
		{"no planes", nil, nil, 0, Placement{}},
		{"a plane of another kind", []Axis{time, band}, replace(3, other), 0, Placement{}},
		{"a plane of another size", []Axis{time, band}, replace(3, wide), 0, Placement{}},
		{"an empty plane", []Axis{time, band}, replace(0, Plane{}), 0, Placement{}},
		{"an invalid value on one plane", []Axis{time, band}, replace(5, withInvalid), 0, Placement{}},
		{"two invalid values", []Axis{time, band}, twoInvalid, 0, Placement{}},
		{"two band axes", []Axis{{Name: "a", Role: AxisRoleBand, Size: 2}, band}, planes, 0, Placement{}},
		{"an unnamed axis", []Axis{{Role: AxisRoleSelector, Size: 2}, band}, planes, 0, Placement{}},
		{"a repeated name", []Axis{{Name: "band", Role: AxisRoleSelector, Size: 2}, band}, planes, 0, Placement{}},
		{"an axis without a role", []Axis{{Name: "t", Size: 2}, band}, planes, 0, Placement{}},
		{"an axis of size zero", []Axis{{Name: "t", Role: AxisRoleSelector}, band}, nil, 0, Placement{}},
		{"coordinates of the wrong length", []Axis{{Name: "t", Role: AxisRoleSelector, Size: 2, Coords: []float64{1}}, band}, planes, 0, Placement{}},
		{"labels of the wrong length", []Axis{time, {Name: "band", Role: AxisRoleBand, Size: 3, Labels: []string{"a", "b"}}}, planes, 0, Placement{}},
		{"a model that is not the band axis", []Axis{time, band}, planes, ColorModelRGBA, Placement{}},
		{"a three-plane model without a band axis", []Axis{{Name: "t", Role: AxisRoleSelector, Size: 6}}, planes, ColorModelRGB, Placement{}},
		{"a model with alpha and an invalid value",
			[]Axis{{Name: "t", Role: AxisRoleSelector, Size: 3}, {Name: "ga", Role: AxisRoleBand, Size: 2}}, allInvalid, ColorModelGrayAlpha, Placement{}},
		{"a placement that does not validate", []Axis{time, band}, planes, 0,
			Placement{Frame: Frame{Kind: FrameKindPhysical}}},
		{"sizes whose product overflows", []Axis{{Name: "a", Role: AxisRoleSelector, Size: 1 << 62}, {Name: "b", Role: AxisRoleSelector, Size: 1 << 62}}, planes, 0, Placement{}},
	}
	for _, r := range refused {
		_, err := NewStack(r.axes, r.planes, r.model, r.place, Provenance{})
		assert.Error(t, err, r.name)
	}

	accepted := []struct {
		name   string
		axes   []Axis
		planes []Plane
		model  ColorModelE
	}{
		{"selector and band", []Axis{time, band}, planes, 0},
		{"a band axis that is RGB", []Axis{time, band}, planes, ColorModelRGB},
		{"one invalid value throughout", []Axis{time, band}, allInvalid, ColorModelRGB},
		{"no axes, one plane, grey", nil, planes[:1], ColorModelGray},
		{"no axes, one plane, no model", nil, planes[:1], 0},
		{"a selector alone with a grey model", []Axis{{Name: "z", Role: AxisRoleSelector, Size: 6}}, planes, ColorModelGray},
	}
	for _, a := range accepted {
		s, err := NewStack(a.axes, a.planes, a.model, Placement{}, Provenance{})
		assert.NoError(t, err, a.name)
		assert.Equal(t, len(a.planes), s.PlaneCount(), a.name)
	}

	// Value transforms may differ per plane.
	scaled, err := planes[2].WithTransform(ValueTransform{Scale: 0.5, Offset: 3})
	require.NoError(t, err)
	_, err = NewStack([]Axis{time, band}, replace(2, scaled), 0, Placement{}, Provenance{})
	assert.NoError(t, err)
}

func TestAStackIndexesRowMajorAndCopiesWhatItIsGiven(t *testing.T) {
	const w, h = 3, 2
	planes := cube(t, SampleKindF64, w, h, 2*3*4)
	axes := []Axis{
		{Name: "time", Role: AxisRoleSelector, Size: 2, Unit: "s", Coords: []float64{0, 60}},
		{Name: "band", Role: AxisRoleBand, Size: 3, Labels: []string{"B4", "B3", "B2"}},
		{Name: "depth", Role: AxisRoleSelector, Size: 4},
	}
	prov := Provenance{SourceName: "x.tif", Notes: []string{"a note"}}
	place := Placement{Affine: [6]float64{0, 1, 0, 0, 0, 1}, Frame: Frame{Kind: FrameKindPhysical, Units: [2]string{"um", "um"}},
		Unapplied: []string{"u"}}
	s, err := NewStack(axes, planes, 0, place, prov)
	require.NoError(t, err)

	assert.Equal(t, w, s.Width())
	assert.Equal(t, h, s.Height())
	assert.Equal(t, SampleKindF64, s.Kind())
	assert.Equal(t, 24, s.PlaneCount())
	assert.Equal(t, int64(24*w*h*8), s.Bytes())
	ba, ok := s.BandAxis()
	assert.True(t, ok)
	assert.Equal(t, 1, ba)
	assert.Equal(t, ColorModelE(0), s.Model())

	for f := range 24 {
		idx := s.Index(f)
		require.Len(t, idx, 3)
		assert.Equal(t, f, (idx[0]*3+idx[1])*4+idx[2], "the last axis varies fastest")
		back, err := s.Flat(idx)
		require.NoError(t, err)
		assert.Equal(t, f, back)
		p, err := s.Plane(idx)
		require.NoError(t, err)
		assert.Equal(t, dataOf(planes[f]), dataOf(p), "the stack shares the samples")
	}
	assert.Nil(t, s.Index(24))
	assert.Nil(t, s.Index(-1))
	_, err = s.Flat([]int{0, 3, 0})
	assert.Error(t, err)
	_, err = s.Flat([]int{0, 0})
	assert.Error(t, err)
	_, err = s.Plane([]int{2, 0, 0})
	assert.Error(t, err)

	assert.Equal(t, "B3", s.Label(1, 1))
	assert.Equal(t, "2", s.Label(2, 2), "an axis without labels names a position by number")
	assert.Equal(t, "", s.Label(2, 4))
	assert.Equal(t, "", s.Label(3, 0))

	// What the stack was given is copied: changing it afterwards changes
	// nothing in the stack, and what it hands out is a copy as well.
	axes[0].Coords[1] = -1
	axes[1].Labels[0] = "changed"
	axes[0].Name = "changed"
	planes[0] = Plane{}
	prov.Notes[0] = "changed"
	place.Unapplied[0] = "changed"
	got := s.Axes()
	assert.Equal(t, "time", got[0].Name)
	assert.Equal(t, []float64{0, 60}, got[0].Coords)
	assert.Equal(t, "s", got[0].Unit)
	assert.Equal(t, "B4", s.Label(1, 0))
	got[1].Labels[0] = "changed"
	assert.Equal(t, "B4", s.Axes()[1].Labels[0])
	p0, err := s.Plane([]int{0, 0, 0})
	require.NoError(t, err)
	assert.False(t, p0.IsZero())
	assert.Equal(t, "a note", s.Provenance().Notes[0])
	assert.Equal(t, "u", s.Placement().Unapplied[0])
	assert.Equal(t, s.Hash(), s.Provenance().SampleHash)

	assert.True(t, Stack{}.IsZero())
	_, ok = Stack{}.BandAxis()
	assert.False(t, ok)
	assert.Equal(t, [32]byte{}, Stack{}.Hash())
}

func TestAnImageTakenFromAStackSharesItsPlanesAndItsHashIsNewImages(t *testing.T) {
	planes := cube(t, SampleKindU16, 4, 3, 2*5)
	for i := range planes {
		var err error
		planes[i], err = planes[i].WithInvalid(0)
		require.NoError(t, err)
	}
	axes := []Axis{
		{Name: "time", Role: AxisRoleSelector, Size: 2},
		{Name: "band", Role: AxisRoleBand, Size: 5, Labels: []string{"coastal", "blue", "green", "red", "nir"}},
	}
	place := Placement{Affine: [6]float64{2600000, 10, 0, 1200000, 0, -10},
		Frame: Frame{Kind: FrameKindReferenceSystem, Code: "EPSG:2056", AxisNames: [2]string{"E", "N"}, Units: [2]string{"m", "m"}}}
	s, err := NewStack(axes, planes, 0, place, Provenance{SourceName: "scene.tif"})
	require.NoError(t, err)

	rgb, err := s.Image([]int{1, 99}, []int{4, 3, 2})
	require.NoError(t, err, "the band axis's entry of at is ignored")
	assert.Equal(t, ColorModelRGB, rgb.Model())
	assert.Equal(t, []string{"nir", "red", "green"}, rgb.PlaneLabels())
	want := []Plane{planes[5+4], planes[5+3], planes[5+2]}
	for i := range 3 {
		assert.Equal(t, dataOf(want[i]), dataOf(rgb.Plane(i)), "plane %d is the stack's own", i)
	}
	direct, err := NewImage(ColorModelRGB, want, Provenance{})
	require.NoError(t, err)
	assert.Equal(t, direct.Hash(), rgb.Hash(), "the same hash as NewImage computes")
	assert.Equal(t, rgb.Hash(), rgb.digest(), "and as hashing the samples again")
	assert.Equal(t, rgb.Hash(), rgb.Provenance().SampleHash)
	assert.Equal(t, "scene.tif", rgb.Provenance().SourceName)
	assert.Equal(t, "EPSG:2056", rgb.Placement().Frame.Code)
	inv, ok := rgb.Plane(0).Invalid()
	assert.True(t, ok)
	assert.Equal(t, 0.0, inv)

	gray, err := s.Image([]int{0, 0}, []int{1})
	require.NoError(t, err)
	assert.Equal(t, ColorModelGray, gray.Model())
	assert.Equal(t, []string{"blue"}, gray.PlaneLabels())
	direct, err = NewImage(ColorModelGray, []Plane{planes[1]}, Provenance{})
	require.NoError(t, err)
	assert.Equal(t, direct.Hash(), gray.Hash())

	for _, bad := range []struct {
		at, bands []int
	}{
		{[]int{0, 0}, nil},
		{[]int{0, 0}, []int{1, 2}},
		{[]int{0, 0}, []int{1, 2, 3, 4}},
		{[]int{0, 0}, []int{5}},
		{[]int{2, 0}, []int{1}},
		{[]int{0}, []int{1}},
	} {
		_, err = s.Image(bad.at, bad.bands)
		assert.Error(t, err, "at %v bands %v", bad.at, bad.bands)
	}
	_, ok, err = s.AsStored([]int{0, 0})
	assert.False(t, ok, "a stack without a model has no image as stored")
	assert.NoError(t, err)

	// A stack without a band axis gives grey images and takes no bands.
	sel, err := NewStack([]Axis{{Name: "z", Role: AxisRoleSelector, Size: 10}}, planes, 0, Placement{}, Provenance{})
	require.NoError(t, err)
	g, err := sel.Image([]int{7}, nil)
	require.NoError(t, err)
	assert.Equal(t, dataOf(planes[7]), dataOf(g.Plane(0)))
	assert.Equal(t, []string{"gray"}, g.PlaneLabels())
	_, err = sel.Image([]int{7}, []int{0})
	assert.Error(t, err)
}

func TestAStackWhoseBandAxisIsAColourModelHasAnImageAsStored(t *testing.T) {
	planes := cube(t, SampleKindU8, 2, 2, 3*4)
	axes := []Axis{
		{Name: "page", Role: AxisRoleSelector, Size: 3},
		{Name: "sample", Role: AxisRoleBand, Size: 4},
	}
	s, err := NewStack(axes, planes, ColorModelRGBA, Placement{}, Provenance{})
	require.NoError(t, err)
	img, ok, err := s.AsStored([]int{2, 0})
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, ColorModelRGBA, img.Model())
	assert.Equal(t, []string{"red", "green", "blue", "alpha"}, img.PlaneLabels(), "an unlabelled band axis takes the model's names")
	for i := range 4 {
		assert.Equal(t, dataOf(planes[8+i]), dataOf(img.Plane(i)))
	}
	direct, err := NewImage(ColorModelRGBA, planes[8:12], Provenance{})
	require.NoError(t, err)
	assert.Equal(t, direct.Hash(), img.Hash())
	_, _, err = s.AsStored([]int{3, 0})
	assert.Error(t, err)

	// One plane, no axes, a grey model: the plane is the image as stored.
	one, err := NewStack(nil, planes[:1], ColorModelGray, Placement{}, Provenance{})
	require.NoError(t, err)
	img, ok, err = one.AsStored(nil)
	require.NoError(t, err)
	assert.True(t, ok)
	direct, err = NewImage(ColorModelGray, planes[:1], Provenance{})
	require.NoError(t, err)
	assert.Equal(t, direct.Hash(), img.Hash())
	_, ok = one.BandAxis()
	assert.False(t, ok)
}

func TestAStacksHashFollowsItsSamplesAndShape(t *testing.T) {
	planes := cube(t, SampleKindI32, 2, 2, 6)
	mk := func(axes []Axis, ps []Plane, model ColorModelE, name string) Stack {
		s, err := NewStack(axes, ps, model, Placement{}, Provenance{SourceName: name})
		require.NoError(t, err)
		return s
	}
	ab := []Axis{{Name: "t", Role: AxisRoleSelector, Size: 2}, {Name: "b", Role: AxisRoleBand, Size: 3}}
	base := mk(ab, planes, 0, "a")
	assert.NotEqual(t, [32]byte{}, base.Hash())
	renamed := []Axis{{Name: "time", Role: AxisRoleSelector, Size: 2, Labels: []string{"x", "y"}}, {Name: "bands", Role: AxisRoleBand, Size: 3}}
	assert.Equal(t, base.Hash(), mk(renamed, planes, ColorModelRGB, "b").Hash(), "names, labels, model and provenance are not hashed")
	ba := []Axis{{Name: "t", Role: AxisRoleSelector, Size: 3}, {Name: "b", Role: AxisRoleBand, Size: 2}}
	assert.NotEqual(t, base.Hash(), mk(ba, planes, 0, "a").Hash(), "the sizes are")
	sel := []Axis{{Name: "t", Role: AxisRoleSelector, Size: 2}, {Name: "b", Role: AxisRoleSelector, Size: 3}}
	assert.NotEqual(t, base.Hash(), mk(sel, planes, 0, "a").Hash(), "the roles are")
	swapped := append([]Plane{planes[1], planes[0]}, planes[2:]...)
	assert.NotEqual(t, base.Hash(), mk(ab, swapped, 0, "a").Hash(), "the plane order is")

	one := mk(nil, planes[:1], ColorModelGray, "")
	img, _, err := one.AsStored(nil)
	require.NoError(t, err)
	assert.NotEqual(t, img.Hash(), one.Hash(), "a stack's digest is not an image's")
}
