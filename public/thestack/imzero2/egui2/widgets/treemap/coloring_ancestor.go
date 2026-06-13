package treemap

import (
	"math"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap/layout"
)

// AncestorHueColoring assigns each depth-1 child of root a distinct hue
// sampled evenly across the 360° spectrum, and shades all of that subtree
// by depth via an HSL lightness ramp from lightAtRoot (depth 0) to
// lightAtLeaf (deepest leaf in the tree). Useful for grouping by top-level
// container: every node under root.Children[i] reads as one color family,
// with subdirectory depth showing as shade.
//
// saturation must be in [0,1]. lightAtRoot and lightAtLeaf must be in [0,1]
// but no ordering is enforced — pass lightAtRoot > lightAtLeaf for the
// conventional "lighter near the root, darker near leaves" feel, or invert
// for the reverse.
//
// alpha is the alpha channel applied to every derived color (matches the
// 0xee convention used by the package's named palettes).
//
// Pointer-identity contract: this coloring memoizes per-node ancestor index
// from the root snapshot at construction time. If the caller transforms the
// tree afterward (e.g., via layout.CollapsePaths), the SAME transformed tree
// must be passed to both treemap.New and AncestorHueColoring; otherwise
// nodes from the un-transformed tree won't be found and Colors returns
// ok=false (fall-through in CompositeColoring).
//
// Panics if root is nil, root has no children, saturation is out of [0,1],
// or lightness values are out of [0,1].
func AncestorHueColoring(root *layout.Node, saturation, lightAtRoot, lightAtLeaf float64, alpha uint8) ColoringI {
	if root == nil {
		panic("treemap: AncestorHueColoring requires a non-nil root")
	}
	if len(root.Children) == 0 {
		panic("treemap: AncestorHueColoring requires root with at least one child")
	}
	if !(saturation >= 0 && saturation <= 1) {
		panic("treemap: AncestorHueColoring requires saturation in [0,1]")
	}
	if !(lightAtRoot >= 0 && lightAtRoot <= 1) || !(lightAtLeaf >= 0 && lightAtLeaf <= 1) {
		panic("treemap: AncestorHueColoring requires lightness values in [0,1]")
	}

	n := len(root.Children)
	hues := make([]float64, n)
	for i := range hues {
		hues[i] = 360.0 * float64(i) / float64(n)
	}

	inst := &ancestorHueColoring{
		ancestorIdx: make(map[*layout.Node]int),
		hues:        hues,
		saturation:  saturation,
		lightAtRoot: lightAtRoot,
		lightAtLeaf: lightAtLeaf,
		alpha:       alpha,
		cache:       make(map[ahcKey]CellColors),
	}

	// Depth here matches CellInfo.Depth: the rendered cells under root.Children
	// are at depth 0, their children at depth 1, etc. So we start the walk at 0.
	for i, child := range root.Children {
		inst.walk(child, i, 0)
	}
	return inst
}

type ahcKey struct{ ai, depth int }

type ancestorHueColoring struct {
	ancestorIdx map[*layout.Node]int
	hues        []float64
	saturation  float64
	lightAtRoot float64
	lightAtLeaf float64
	alpha       uint8
	maxDepth    int
	cache       map[ahcKey]CellColors
}

var _ ColoringI = (*ancestorHueColoring)(nil)

func (inst *ancestorHueColoring) walk(node *layout.Node, ancestorIdx, depth int) {
	inst.ancestorIdx[node] = ancestorIdx
	if depth > inst.maxDepth {
		inst.maxDepth = depth
	}
	for _, ch := range node.Children {
		inst.walk(ch, ancestorIdx, depth+1)
	}
}

func (inst *ancestorHueColoring) Colors(info CellInfo) (CellColors, bool) {
	ai, ok := inst.ancestorIdx[info.Node]
	if !ok {
		return CellColors{}, false
	}
	d := info.Depth
	if d < 0 {
		d = 0
	}
	if d > inst.maxDepth {
		d = inst.maxDepth
	}
	key := ahcKey{ai: ai, depth: d}
	if cached, hit := inst.cache[key]; hit {
		return cached, true
	}
	var t float64
	if inst.maxDepth > 0 {
		t = float64(d) / float64(inst.maxDepth)
	}
	light := inst.lightAtRoot + (inst.lightAtLeaf-inst.lightAtRoot)*t
	rgba := hslToRGBA(inst.hues[ai], inst.saturation, light, inst.alpha)
	cs := deriveCellColors(rgba)
	inst.cache[key] = cs
	return cs, true
}

// hslToRGBA converts HSL to a packed 0xRRGGBBAA value. h in degrees (any
// real number; reduced mod 360), s and l in [0,1]; out-of-range channels are
// clamped at the byte boundary.
//
// Standard formula from https://en.wikipedia.org/wiki/HSL_and_HSV.
func hslToRGBA(h, s, l float64, a uint8) uint32 {
	h = math.Mod(h, 360)
	if h < 0 {
		h += 360
	}
	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := l - c/2
	var r1, g1, b1 float64
	switch {
	case h < 60:
		r1, g1, b1 = c, x, 0
	case h < 120:
		r1, g1, b1 = x, c, 0
	case h < 180:
		r1, g1, b1 = 0, c, x
	case h < 240:
		r1, g1, b1 = 0, x, c
	case h < 300:
		r1, g1, b1 = x, 0, c
	default:
		r1, g1, b1 = c, 0, x
	}
	r := clampByte((r1 + m) * 255)
	g := clampByte((g1 + m) * 255)
	b := clampByte((b1 + m) * 255)
	return uint32(r)<<24 | uint32(g)<<16 | uint32(b)<<8 | uint32(a)
}

func clampByte(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(math.Round(v))
}
