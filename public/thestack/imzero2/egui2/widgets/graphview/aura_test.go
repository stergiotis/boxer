package graphview

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

func TestAuraKernelPlateauAndRamp(t *testing.T) {
	p := AuraParams{Enabled: true}.withDefaults()
	k := kernelFor(30, p) // r' = 34, d0 = 17, R = 204
	require.InDelta(t, 17, k.d0, 1e-6)
	require.InDelta(t, 204, k.R, 1e-6)
	require.Equal(t, float32(1), k.at(0))
	require.Equal(t, float32(1), k.at(17))
	require.InDelta(t, 0.5, k.at(110.5), 1e-5)
	require.Equal(t, float32(0), k.at(204))
	require.Equal(t, float32(0), k.at(500))
	require.InDelta(t, 110.5, k.extent(0.5), 1e-4, "extent is where the ramp meets the draw limit")
	require.InDelta(t, 17, k.extent(1), 1e-4)
}

func TestAuraParamsDefaults(t *testing.T) {
	p := AuraParams{Enabled: true, DrawLimit: 2}.withDefaults()
	require.Equal(t, float32(8), p.CellSize)
	require.Equal(t, float32(6), p.Intensity)
	require.Equal(t, float32(1), p.DrawLimit, "clamped to 1")
	require.Equal(t, float32(4), p.Pad)
}

func TestAuraSetBuildsSortedIdsAndMembership(t *testing.T) {
	var g graph
	nodes := []NodeSpec{{Id: 1, Auras: []string{"z", "a"}}, {Id: 2, Auras: []string{"m"}}, {Id: 3}, {Id: 4, Auras: []string{"", "a"}}}
	g.reconcile(nodes, nil)
	var set auraSet
	require.True(t, set.build(nodes, &g))
	require.Equal(t, []string{"a", "m", "z"}, set.ids)
	require.ElementsMatch(t, []int32{2, 0}, set.members(g.slot[1]))
	require.Equal(t, []int32{1}, set.members(g.slot[2]))
	require.Empty(t, set.members(g.slot[3]))
	require.Equal(t, []int32{0}, set.members(g.slot[4]), "the empty id is ignored")
	require.False(t, set.build(nodes, &g), "same membership, no change")
	nodes[2].Auras = []string{"m"}
	require.True(t, set.build(nodes, &g))
	require.Equal(t, []int32{1}, set.members(g.slot[3]))
}

// setupField builds a graph of the given nodes (world units), a unit camera
// panned so world (0, 0) is canvas (200, 200), and the aura set.
func setupField(t *testing.T, nodes []NodeSpec) (g graph, set auraSet, cam camera) {
	t.Helper()
	g.reconcile(nodes, nil)
	for i := range nodes {
		s := g.slot[nodes[i].Id]
		g.x[s], g.y[s] = nodes[i].PinX, nodes[i].PinY
	}
	set.build(nodes, &g)
	cam = camera{zoom: 1, panX: 200, panY: 200}
	return
}

func TestAuraSingleNodeContourIsACircleAtTheExtent(t *testing.T) {
	nodes := []NodeSpec{{Id: 1, Radius: 30, Auras: []string{"A"}}}
	g, set, cam := setupField(t, nodes)
	p := AuraParams{Enabled: true, CellSize: 2, DrawLimit: 0.5}.withDefaults()
	var f auraField
	f.compute(&g, cam, &set, nil, 5, 400, 400, p)
	var out rings
	f.contours(0, &out)
	require.Equal(t, 1, out.count())
	xs, ys := out.ring(0)
	area := signedArea(xs, ys)
	require.Less(t, area, float32(0), "an outer ring winds negative")
	want := math.Pi * 110.5 * 110.5
	require.InDelta(t, want, -float64(area), want*0.03)
	var cx, cy float64
	for i := range xs {
		cx += float64(xs[i])
		cy += float64(ys[i])
	}
	require.InDelta(t, 200, cx/float64(len(xs)), 1)
	require.InDelta(t, 200, cy/float64(len(ys)), 1)
	for i := range xs {
		r := math.Hypot(float64(xs[i])-200, float64(ys[i])-200)
		require.InDelta(t, 110.5, r, 2.5, "every vertex lies near the iso-radius")
	}
}

func TestAuraAccumulationIsAComplementaryProduct(t *testing.T) {
	nodes := []NodeSpec{{Id: 1, Radius: 30, Auras: []string{"A"}, PinX: -50}, {Id: 2, Radius: 30, Auras: []string{"A"}, PinX: 50}}
	g, set, cam := setupField(t, nodes)
	p := AuraParams{Enabled: true, CellSize: 1}.withDefaults()
	var f auraField
	f.compute(&g, cam, &set, nil, 5, 400, 400, p)
	// Cell (200, 200) is sampled at its centre (200.5, 200.5): 50.5 px from
	// the node at canvas x 150 and 49.5 px from the one at 250, half a
	// pixel off their axis.
	k := kernelFor(30, p)
	f1 := k.at(float32(math.Hypot(50.5, 0.5)))
	f2 := k.at(float32(math.Hypot(49.5, 0.5)))
	got := f.vals[0][200*f.gw+200]
	require.InDelta(t, 1-(1-f1)*(1-f2), got, 1e-4)
	require.Greater(t, got, max(f1, f2), "two ramps accumulate past either alone")
	require.Less(t, got, f1+f2, "but not as a sum")
}

func TestAuraOwnershipIsArgmaxWithTiesToTheSmallerId(t *testing.T) {
	nodes := []NodeSpec{{Id: 1, Radius: 30, Auras: []string{"B"}, PinX: -50}, {Id: 2, Radius: 30, Auras: []string{"A"}, PinX: 50}}
	g, set, cam := setupField(t, nodes)
	p := AuraParams{Enabled: true, CellSize: 1, DrawLimit: 0.3}.withDefaults()
	var f auraField
	f.compute(&g, cam, &set, nil, 5, 400, 400, p)
	a, b := set.index["A"], set.index["B"]
	require.Equal(t, b, f.best[200*f.gw+180], "left of the middle the B node is nearer")
	require.Equal(t, a, f.best[200*f.gw+220])
	require.True(t, auraIso{&f, b}.inside(180, 200))
	require.False(t, auraIso{&f, a}.inside(180, 200), "a lost cell is outside its aura")
	// Overlap on: both own the middle.
	p.Overlap = true
	f.compute(&g, cam, &set, nil, 5, 400, 400, p)
	require.True(t, auraIso{&f, a}.inside(200, 200))
	require.True(t, auraIso{&f, b}.inside(200, 200))
	// Coincident nodes tie everywhere: the smaller id takes every cell.
	nodes[0].PinX, nodes[1].PinX = 0, 0
	g, set, cam = setupField(t, nodes)
	p.Overlap = false
	f.compute(&g, cam, &set, nil, 5, 400, 400, p)
	var outA, outB rings
	f.contours(a, &outA)
	f.contours(b, &outB)
	require.Equal(t, 1, outA.count())
	require.Equal(t, 0, outB.count())
}

func TestAuraHiddenContributesNothing(t *testing.T) {
	nodes := []NodeSpec{{Id: 1, Radius: 30, Auras: []string{"A", "B"}}}
	g, set, cam := setupField(t, nodes)
	p := AuraParams{Enabled: true, CellSize: 4}.withDefaults()
	var f auraField
	f.compute(&g, cam, &set, map[string]struct{}{"A": {}}, 5, 400, 400, p)
	require.True(t, f.box[set.index["A"]].empty())
	require.False(t, f.box[set.index["B"]].empty())
	var out rings
	f.contours(set.index["A"], &out)
	require.Equal(t, 0, out.count())
}

func TestAuraFitMarginCoversTheExtent(t *testing.T) {
	v := New(c.NewWidgetIdStack(), "t", Options{Auras: AuraParams{Enabled: true}})
	v.style = v.Opts.Style.withDefaults()
	nodes := []NodeSpec{{Id: 1, Radius: 10, Auras: []string{"A"}}, {Id: 2, Radius: 40}}
	v.g.reconcile(nodes, nil)
	v.auraSet.build(nodes, &v.g)
	ap := v.Opts.Auras.withDefaults()
	v.cam = camera{zoom: 2}
	// Only the member's radius counts: r' = 10·2 + 4 = 24 px, extent at 0.8
	// = 12 + 0.2·(144 − 12) = 38.4 px = 19.2 world; minus the 10 in bounds.
	require.InDelta(t, 9.2, v.auraFitMargin(ap), 1e-4)
	v.Opts.Auras.Enabled = false
	require.Zero(t, v.auraFitMargin(v.Opts.Auras.withDefaults()))
}

func TestAuraLegendToggleRoundTrips(t *testing.T) {
	v := New(c.NewWidgetIdStack(), "t", Options{})
	require.False(t, v.AuraHidden("A"))
	v.HideAura("A")
	require.True(t, v.AuraHidden("A"))
	require.Equal(t, uint32(1), v.hiddenVer)
	v.HideAura("A")
	require.Equal(t, uint32(1), v.hiddenVer, "no change, no version bump")
	v.ShowAura("A")
	require.False(t, v.AuraHidden("A"))
	require.Equal(t, uint32(2), v.hiddenVer)
}

func TestUpdateAurasReusesRingsUntilSomethingChanges(t *testing.T) {
	v := New(c.NewWidgetIdStack(), "t", Options{Auras: AuraParams{Enabled: true, CellSize: 4}})
	v.style = v.Opts.Style.withDefaults()
	nodes := []NodeSpec{{Id: 1, Radius: 20, Auras: []string{"A"}}, {Id: 2, Radius: 20, Auras: []string{"B"}}}
	v.g.reconcile(nodes, nil)
	v.g.x[v.g.slot[2]] = 300
	v.auraSet.build(nodes, &v.g)
	v.cam = camera{zoom: 1, panX: 100, panY: 200}
	ap := v.Opts.Auras.withDefaults()

	v.updateAuras(ap, 600, 400)
	require.Equal(t, []int32{0, 1}, v.auraOrder)
	require.Equal(t, 1, v.auraRings[0].count())
	key := v.auraKey
	first := v.auraRings[0].xs[:1]

	// Nothing changed: the same rings, not recomputed.
	v.auraRings[0].xs[0] = -1
	v.updateAuras(ap, 600, 400)
	require.Equal(t, key, v.auraKey)
	require.Equal(t, float32(-1), first[0], "rings untouched")

	// Drift below a quarter cell keeps them; past it recomputes.
	v.auraDrift = 0.5
	v.updateAuras(ap, 600, 400)
	require.Equal(t, float32(-1), v.auraRings[0].xs[0])
	v.auraDrift = 2
	v.updateAuras(ap, 600, 400)
	require.NotEqual(t, float32(-1), v.auraRings[0].xs[0])
	require.Zero(t, v.auraDrift)

	// Hiding an aura changes the key and empties its rings; the order keeps
	// every aura so the legend still lists it.
	v.HideAura("A")
	v.updateAuras(ap, 600, 400)
	require.NotEqual(t, key, v.auraKey)
	require.Equal(t, 0, v.auraRings[0].count())
	require.Equal(t, 1, v.auraRings[1].count())

	// Disabled: no order, and the key resets so re-enabling recomputes.
	v.updateAuras(AuraParams{}.withDefaults(), 600, 400)
	require.Empty(t, v.auraOrder)
	require.Equal(t, auraCacheKey{}, v.auraKey)
}

func TestApplyPinsReportsMoves(t *testing.T) {
	var g graph
	g.reconcile([]NodeSpec{{Id: 1, Pinned: true, PinX: 5, PinY: 6}, {Id: 2}}, nil)
	require.True(t, g.applyPins(-1), "the first application moves the node onto its pin")
	require.False(t, g.applyPins(-1), "already there")
	g.pinX[g.slot[1]] = 7
	require.True(t, g.applyPins(-1))
	require.False(t, g.applyPins(g.slot[1]), "the dragged node is left where it is")
}
