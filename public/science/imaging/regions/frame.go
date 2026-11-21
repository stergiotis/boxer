package regions

import (
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
)

// FrameKindE says what a region's coordinates are (ADR-0399 §SD2). The zero
// value is no frame: a region without one does not validate.
type FrameKindE uint8

const (
	// FrameKindPixel is the continuous pixel coordinates of ADR-0373 §SD3:
	// pixel (col, row) covers [col, col+1) × [row, row+1), row 0 at the top.
	// It needs no placement and fits every image.
	FrameKindPixel FrameKindE = iota + 1
	// FrameKindPhysical is a placement's physical scale, in its units
	// ([imaging.FrameKindPhysical]).
	FrameKindPhysical
	// FrameKindReferenceSystem is a coordinate reference system named by its
	// code ([imaging.FrameKindReferenceSystem]). Its coordinates are taken
	// as plane coordinates, whatever the system defines.
	FrameKindReferenceSystem
	// FrameKindCelestial is the sky in a celestial system named by its code,
	// longitude and latitude in degrees, as a gnomonic placement evaluates
	// it ([imaging.FrameKindCelestialGnomonic]). A line and a polygon's
	// edges are great-circle arcs, the shorter ones; a rectangle and an
	// ellipse are laid out in the plane tangent to the sky at their centre,
	// +x east and +y north, sizes in degrees of that plane.
	FrameKindCelestial
)

// AllFrameKinds lists the frames a region can be in.
var AllFrameKinds = []FrameKindE{FrameKindPixel, FrameKindPhysical, FrameKindReferenceSystem, FrameKindCelestial}

// String is the kind's name in a region document.
func (inst FrameKindE) String() string {
	switch inst {
	case FrameKindPixel:
		return "pixel"
	case FrameKindPhysical:
		return "physical"
	case FrameKindReferenceSystem:
		return "reference-system"
	case FrameKindCelestial:
		return "celestial"
	}
	return "none"
}

// Frame is the one frame a region's coordinates are in (ADR-0399 §SD2),
// named as a placement names it: a reference system or a celestial system by
// its code, a physical frame by its units.
type Frame struct {
	Kind FrameKindE
	// Code names a reference system or a celestial system as the placement
	// records it: "EPSG:2056", "ICRS". It is compared, not resolved, and is
	// empty for any other kind.
	Code string
	// Units are a physical frame's units for its two coordinates, as the
	// placement records them; empty for any other kind.
	Units [2]string
}

// PixelFrame is the frame of the continuous pixel coordinates.
func PixelFrame() Frame { return Frame{Kind: FrameKindPixel} }

// FrameOf is the frame a placement's coordinates are in, and ok false for
// the zero placement, which places nothing.
func FrameOf(p imaging.Placement) (f Frame, ok bool) {
	switch p.Frame.Kind {
	case imaging.FrameKindPhysical:
		return Frame{Kind: FrameKindPhysical, Units: p.Frame.Units}, true
	case imaging.FrameKindReferenceSystem:
		return Frame{Kind: FrameKindReferenceSystem, Code: p.Frame.Code}, true
	case imaging.FrameKindCelestialGnomonic:
		return Frame{Kind: FrameKindCelestial, Code: p.Frame.Code}, true
	}
	return f, false
}

// Validate refuses a frame of no kind or an unknown one; a reference system
// or celestial frame without a code; a physical frame without both units;
// and a code or units on a kind that has none.
func (inst Frame) Validate() error {
	if !slices.Contains(AllFrameKinds, inst.Kind) {
		return eb.Build().Uint8("kind", uint8(inst.Kind)).Errorf("region has no frame, or an unknown one")
	}
	hasUnits := inst.Units != [2]string{}
	switch inst.Kind {
	case FrameKindPixel:
		if inst.Code != "" || hasUnits {
			return eb.Build().Str("code", inst.Code).Strs("units", inst.Units[:]).Errorf("a pixel frame has no code and no units")
		}
	case FrameKindPhysical:
		if inst.Code != "" || inst.Units[0] == "" || inst.Units[1] == "" {
			return eb.Build().Str("code", inst.Code).Strs("units", inst.Units[:]).
				Errorf("a physical frame is named by its two units and has no code")
		}
	case FrameKindReferenceSystem, FrameKindCelestial:
		if inst.Code == "" || hasUnits {
			return eb.Build().Stringer("frame", inst.Kind).Str("code", inst.Code).Strs("units", inst.Units[:]).
				Errorf("a reference system or celestial frame is named by its code and has no units")
		}
	}
	return nil
}

// String names the frame for a person: "pixel", "reference-system EPSG:2056",
// "physical um/um".
func (inst Frame) String() string {
	switch inst.Kind {
	case FrameKindPhysical:
		return inst.Kind.String() + " " + inst.Units[0] + "/" + inst.Units[1]
	case FrameKindReferenceSystem, FrameKindCelestial:
		return inst.Kind.String() + " " + inst.Code
	}
	return inst.Kind.String()
}

// Matches reports whether a region in this frame can be brought to the
// pixels of an image under placement p (ADR-0399 §SD2): always for the
// pixel frame; otherwise when the placement's frame is of the same kind and
// names the same non-empty code (a reference system or the sky) or the same
// non-empty units (a physical frame). Two frames whose code is unknown are
// not known to be the same, and do not match.
func (inst Frame) Matches(p imaging.Placement) bool {
	if inst.Kind == FrameKindPixel {
		return true
	}
	f, ok := FrameOf(p)
	if !ok || f.Kind != inst.Kind {
		return false
	}
	switch inst.Kind {
	case FrameKindPhysical:
		return inst.Units[0] != "" && inst.Units[1] != "" && inst.Units == f.Units
	case FrameKindReferenceSystem, FrameKindCelestial:
		return inst.Code != "" && inst.Code == f.Code
	}
	return false
}
