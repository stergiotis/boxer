package regions

import (
	"iter"
	"slices"

	gonanoid "github.com/matoous/go-nanoid/v2"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
)

// Region is a shape with what it means and apart from how it is drawn
// (ADR-0399 §SD5, §SD6): an identity, a label, a note, an origin, the frame
// its coordinates are in, an anchor naming the positions of a stack it
// belongs to, and the shape. Colour and stroke are not part of it.
type Region struct {
	// Id identifies the region among others; [NewId] makes one.
	Id string
	// Label names the region for a person; Note says more.
	Label, Note string
	// Origin says who or what made the region: a person, a program, an
	// import. It is free text.
	Origin string
	Frame  Frame
	Anchor Anchor
	Shape  Shape
}

// NewId is a fresh region id.
func NewId() (id string, err error) {
	id, err = gonanoid.New()
	if err != nil {
		return "", eb.Build().Errorf("make a region id: %w", err)
	}
	return id, nil
}

// Validate refuses a region without an id, without a frame or with an
// invalid one, without a shape, with an invalid anchor, and — in a
// celestial frame — with a latitude outside [−90°, 90°].
func (inst Region) Validate() error {
	if inst.Id == "" {
		return eb.Build().Errorf("region has no id")
	}
	if err := inst.Frame.Validate(); err != nil {
		return eb.Build().Str("region", inst.Id).Errorf("region frame: %w", err)
	}
	if inst.Shape.IsZero() {
		return eb.Build().Str("region", inst.Id).Errorf("region has no shape")
	}
	if err := inst.Anchor.Validate(); err != nil {
		return eb.Build().Str("region", inst.Id).Errorf("region anchor: %w", err)
	}
	if inst.Frame.Kind == FrameKindCelestial {
		for _, p := range inst.Shape.positions() {
			if p.Y < -90 || p.Y > 90 {
				return eb.Build().Str("region", inst.Id).Float64("latitude", p.Y).
					Errorf("a position on the sky has a latitude outside [-90, 90]")
			}
		}
	}
	return nil
}

// positions are the positions a shape's parameters name: the point, the
// line's ends, the centre, the vertices.
func (inst Shape) positions() []Point {
	switch inst.kind {
	case ShapeKindPoint, ShapeKindRectangle, ShapeKindEllipse:
		return []Point{inst.a}
	case ShapeKindLine:
		return []Point{inst.a, inst.b}
	case ShapeKindPolygon:
		return inst.vertices
	}
	return nil
}

// ToPixels is [ToPixels] of the region's shape in its frame.
func (inst Region) ToPixels(p imaging.Placement, tolerance float64) (px Shape, m Mapping, err error) {
	if err = inst.Validate(); err != nil {
		return
	}
	return ToPixels(inst.Shape, inst.Frame, p, tolerance)
}

// AxisPosition is one position on a named axis of a stack.
type AxisPosition struct {
	Axis     string
	Position int
}

// Anchor says which positions of a stack a region belongs to (ADR-0399
// §SD5): every position when it is empty; otherwise those whose position on
// each axis it names is one of the positions it names for that axis. It
// names axes by name, so it survives a stack being re-read. The zero value
// is the empty anchor.
type Anchor struct {
	positions []AxisPosition
}

// NewAnchor is the anchor of the given positions, which it copies: several
// positions on one axis are alternatives, positions on different axes all
// apply. An axis without a name, a negative position and a position named
// twice are refused.
func NewAnchor(positions ...AxisPosition) (a Anchor, err error) {
	if len(positions) == 0 {
		return Anchor{}, nil
	}
	a = Anchor{positions: slices.Clone(positions)}
	if err = a.Validate(); err != nil {
		return Anchor{}, err
	}
	return a, nil
}

// Validate refuses an axis without a name, a negative position and a
// position named twice.
func (inst Anchor) Validate() error {
	for i, p := range inst.positions {
		if p.Axis == "" || p.Position < 0 {
			return eb.Build().Int("entry", i).Str("axis", p.Axis).Int("position", p.Position).
				Errorf("an anchor names an axis and a position not below zero")
		}
		if slices.Contains(inst.positions[:i], p) {
			return eb.Build().Str("axis", p.Axis).Int("position", p.Position).Errorf("an anchor names a position twice")
		}
	}
	return nil
}

// IsEmpty reports whether the anchor names nothing: the region belongs to
// every position.
func (inst Anchor) IsEmpty() bool { return len(inst.positions) == 0 }

// All is the anchor's positions in the order given.
func (inst Anchor) All() iter.Seq[AxisPosition] {
	return func(yield func(AxisPosition) bool) {
		for _, p := range inst.positions {
			if !yield(p) {
				return
			}
		}
	}
}

// AppliesAt reports whether a region with this anchor belongs to the stack
// position at — one position per axis of axes, as [imaging.Stack.Image]
// takes it. An anchor naming an axis the stack does not have, a band axis,
// or a position outside its axis is refused, naming it: the region was made
// for another stack, and saying it does not apply would hide that. The band
// axis's entry of at is not read.
func (inst Anchor) AppliesAt(axes []imaging.Axis, at []int) (applies bool, err error) {
	if len(at) != len(axes) {
		return false, eb.Build().Int("at", len(at)).Int("axes", len(axes)).Errorf("a stack position has one position per axis")
	}
	applies = true
	for i, a := range axes {
		named, hit := false, false
		for _, p := range inst.positions {
			if p.Axis != a.Name {
				continue
			}
			if a.Role != imaging.AxisRoleSelector {
				return false, eb.Build().Str("axis", a.Name).Stringer("role", a.Role).Errorf("an anchor names selector axes only")
			}
			if p.Position >= a.Size {
				return false, eb.Build().Str("axis", a.Name).Int("position", p.Position).Int("size", a.Size).
					Errorf("an anchor names a position outside its axis")
			}
			named = true
			hit = hit || p.Position == at[i]
		}
		if named && !hit {
			applies = false
		}
	}
	for _, p := range inst.positions {
		if !slices.ContainsFunc(axes, func(a imaging.Axis) bool { return a.Name == p.Axis }) {
			return false, eb.Build().Str("axis", p.Axis).Errorf("an anchor names an axis the stack does not have")
		}
	}
	return applies, nil
}
