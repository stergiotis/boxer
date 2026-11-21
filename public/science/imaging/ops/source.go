package ops

import (
	"context"
	"image"
	"regexp"
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
	"github.com/stergiotis/boxer/public/science/imaging/tiles"
)

// SourceI is an image an operation reads (ADR-0400 §SD2): its description,
// which the plan is made from, and its samples by region. A [*tiles.Store]
// is one — a decoded file or another operation's result — and [FromImage]
// makes one of an image in memory.
type SourceI interface {
	Width() int
	Height() int
	Model() imaging.ColorModelE
	Kind() imaging.SampleKindE
	// Transform is plane i's value transform.
	Transform(i int) imaging.ValueTransform
	// Invalid is the stored value the colour planes declare invalid, and ok
	// false when there is none.
	Invalid() (stored float64, ok bool)
	PlaneLabels() []string
	Placement() imaging.Placement
	// Provenance carries the digest of the samples as SampleHash once they
	// are all there.
	Provenance() imaging.Provenance
	// Read copies the samples of rect into an image whose planes declare
	// the invalid value and carry their value transforms.
	Read(ctx context.Context, rect image.Rectangle) (imaging.Image, error)
	// Wait returns once every sample can be read, or why none will be.
	Wait(ctx context.Context) error
}

var _ SourceI = (*tiles.Store)(nil)

// FromImage is an image in memory as a source. Its samples are read where
// they are; nothing is copied.
func FromImage(img imaging.Image) SourceI { return imageSource{img: img} }

type imageSource struct {
	img imaging.Image
}

func (inst imageSource) Width() int                 { return inst.img.Width() }
func (inst imageSource) Height() int                { return inst.img.Height() }
func (inst imageSource) Model() imaging.ColorModelE { return inst.img.Model() }
func (inst imageSource) Kind() imaging.SampleKindE  { return inst.img.Kind() }
func (inst imageSource) Transform(i int) imaging.ValueTransform {
	p := inst.img.Plane(i)
	if p.IsZero() {
		return imaging.IdentityTransform
	}
	return p.Transform()
}
func (inst imageSource) Invalid() (stored float64, ok bool) { return inst.img.Plane(0).Invalid() }
func (inst imageSource) PlaneLabels() []string              { return inst.img.PlaneLabels() }
func (inst imageSource) Placement() imaging.Placement       { return inst.img.Placement() }
func (inst imageSource) Provenance() imaging.Provenance     { return inst.img.Provenance() }
func (inst imageSource) Wait(context.Context) error         { return nil }

// Read copies rect of every plane into an image of its own.
func (inst imageSource) Read(_ context.Context, rect image.Rectangle) (img imaging.Image, err error) {
	if rect.Empty() || !rect.In(image.Rect(0, 0, inst.img.Width(), inst.img.Height())) {
		return img, eb.Build().Stringer("region", rect).Int("width", inst.img.Width()).Int("height", inst.img.Height()).
			Errorf("region is empty or not inside the image")
	}
	planes := make([]imaging.Plane, inst.img.PlaneCount())
	for i := range planes {
		if planes[i], err = cutPlane(inst.img.Plane(i), rect); err != nil {
			return img, err
		}
	}
	if img, err = imaging.NewImage(inst.img.Model(), planes, inst.img.Provenance()); err != nil {
		return img, err
	}
	return img.WithPlaneLabels(inst.img.PlaneLabels())
}

func cutPlane(p imaging.Plane, r image.Rectangle) (out imaging.Plane, err error) {
	w, h := r.Dx(), r.Dy()
	switch p.Kind() {
	case imaging.SampleKindU8:
		out, err = imaging.NewPlaneU8(w, h, cutSamples(p.U8(), p.Width(), r))
	case imaging.SampleKindU16:
		out, err = imaging.NewPlaneU16(w, h, cutSamples(p.U16(), p.Width(), r))
	case imaging.SampleKindI16:
		out, err = imaging.NewPlaneI16(w, h, cutSamples(p.I16(), p.Width(), r))
	case imaging.SampleKindI32:
		out, err = imaging.NewPlaneI32(w, h, cutSamples(p.I32(), p.Width(), r))
	case imaging.SampleKindF32:
		out, err = imaging.NewPlaneF32(w, h, cutSamples(p.F32(), p.Width(), r))
	case imaging.SampleKindF64:
		out, err = imaging.NewPlaneF64(w, h, cutSamples(p.F64(), p.Width(), r))
	default:
		return out, eb.Build().Stringer("kind", p.Kind()).Errorf("no plane of this sample kind to cut")
	}
	if err != nil {
		return
	}
	if inv, ok := p.Invalid(); ok {
		if out, err = out.WithInvalid(inv); err != nil {
			return
		}
	}
	return out.WithTransform(p.Transform())
}

func cutSamples[T any](s []T, w int, r image.Rectangle) []T {
	out := make([]T, 0, r.Dx()*r.Dy())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		out = append(out, s[y*w+r.Min.X:y*w+r.Max.X]...)
	}
	return out
}

// Position is one position an input was taken from on an axis of a stack
// (ADR-0400 §SD2), as the caller describes it: the axis's name, the index
// and the position's label.
type Position struct {
	Axis  string `json:"axis"`
	Index int    `json:"index"`
	Label string `json:"label,omitzero"`
}

// Input is a named input of an operation.
type Input struct {
	// Name is how the operation and its record call the input: an
	// identifier — a letter or underscore, then letters, digits and
	// underscores — that is no function of the expression grammar.
	Name   string
	Source SourceI
	// Positions describe where on a stack's axes the input was taken from;
	// they go into the record beside its digest and change nothing else.
	Positions []Position
	// Record is the record of the operation whose result Source is. When
	// nil, a store's record is looked up with [RecordOf].
	Record *Record
}

var inputNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)

// IsInputName reports whether name can name an input: an identifier of at
// most 64 characters that is no function of the expression grammar.
func IsInputName(name string) bool {
	return inputNamePattern.MatchString(name) && !slices.Contains(ExpressionFunctions(), name)
}

// StackInput is an input taken from a stack (ADR-0400 §SD2): the plane at
// the positions at gives on the selector axes and at band on the band axis
// — not read for a stack without one, and at's own entry for the band axis
// is not read either — as a single-plane image that shares the stack's
// samples and its placement. Its Positions name every axis it was taken
// at, by name, index and label, so the record says where on the stack the
// input came from.
func StackInput(name string, st imaging.Stack, at []int, band int) (in Input, err error) {
	b, hasBand := st.BandAxis()
	var bands []int
	if hasBand {
		bands = []int{band}
	}
	img, err := st.Image(at, bands)
	if err != nil {
		return in, eb.Build().Str("input", name).Errorf("take the input from the stack: %w", err)
	}
	axes := st.Axes()
	positions := make([]Position, 0, len(axes))
	for i, a := range axes {
		pos := at[i]
		if hasBand && i == b {
			pos = band
		}
		positions = append(positions, Position{Axis: a.Name, Index: pos, Label: st.Label(i, pos)})
	}
	return Input{Name: name, Source: FromImage(img), Positions: positions}, nil
}

// InputDesc is what an operator's plan is made from: an input's
// description, without its samples (ADR-0400 §SD3).
type InputDesc struct {
	Name          string
	Width, Height int
	Model         imaging.ColorModelE
	Kind          imaging.SampleKindE
	// Transforms are the planes' value transforms, in storage order.
	Transforms []imaging.ValueTransform
	// HasInvalid declares Invalid, a stored value, invalid in the colour
	// planes.
	Invalid     float64
	HasInvalid  bool
	PlaneLabels []string
	Placement   imaging.Placement
	// Digest is the digest of the samples; zero while a store is not
	// finished.
	Digest    Digest
	Positions []Position
}

// PlaneCount is the number of planes.
func (inst InputDesc) PlaneCount() int { return inst.Model.PlaneCount() }

func describe(in Input) (d InputDesc) {
	s := in.Source
	d = InputDesc{Name: in.Name, Width: s.Width(), Height: s.Height(), Model: s.Model(), Kind: s.Kind(),
		PlaneLabels: s.PlaneLabels(), Placement: s.Placement(), Digest: Digest(s.Provenance().SampleHash),
		Positions: slices.Clone(in.Positions)}
	d.Invalid, d.HasInvalid = s.Invalid()
	d.Transforms = make([]imaging.ValueTransform, d.Model.PlaneCount())
	for i := range d.Transforms {
		d.Transforms[i] = s.Transform(i)
	}
	return
}
