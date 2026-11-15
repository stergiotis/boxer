package imaging

import (
	"math"
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// SampleKindE is the stored type of a plane's samples. The zero value is no
// kind: a plane of it holds nothing.
type SampleKindE uint8

const (
	SampleKindU8 SampleKindE = iota + 1
	SampleKindU16
	SampleKindF32
)

// AllSampleKinds lists the kinds a plane can have.
var AllSampleKinds = []SampleKindE{SampleKindU8, SampleKindU16, SampleKindF32}

func (inst SampleKindE) String() string {
	switch inst {
	case SampleKindU8:
		return "u8"
	case SampleKindU16:
		return "u16"
	case SampleKindF32:
		return "f32"
	}
	return "none"
}

// IsInteger reports whether samples of this kind are unsigned integers.
func (inst SampleKindE) IsInteger() bool {
	return inst == SampleKindU8 || inst == SampleKindU16
}

// NominalMax is the largest stored sample an integer kind can hold; a float
// kind has no nominal range and reports ok false.
func (inst SampleKindE) NominalMax() (hi float64, ok bool) {
	switch inst {
	case SampleKindU8:
		return math.MaxUint8, true
	case SampleKindU16:
		return math.MaxUint16, true
	}
	return 0, false
}

// ValueTransform says what a stored sample stands for:
// value = stored·Scale + Offset. The zero value is not the identity; use
// [IdentityTransform].
type ValueTransform struct {
	Scale, Offset float64
}

// IdentityTransform is the transform of a format whose samples are the
// values.
var IdentityTransform = ValueTransform{Scale: 1}

// Apply returns the value a stored sample stands for.
func (inst ValueTransform) Apply(stored float64) float64 {
	return stored*inst.Scale + inst.Offset
}

// IsIdentity reports whether stored samples are the values.
func (inst ValueTransform) IsIdentity() bool {
	return inst.Scale == 1 && inst.Offset == 0
}

// Plane is a width × height grid of samples of one kind, row-major. The zero
// value is an empty plane of no kind. A plane is immutable by contract: see
// the package documentation.
type Plane struct {
	kind      SampleKindE
	width     int32
	height    int32
	u8        []uint8
	u16       []uint16
	f32       []float32
	transform ValueTransform
}

func checkPlaneSize(width, height, n int) error {
	if width <= 0 || height <= 0 || n != width*height {
		return eb.Build().Int("width", width).Int("height", height).Int("samples", n).
			Errorf("sample count does not match a plane of positive size")
	}
	if width > math.MaxInt32 || height > math.MaxInt32 {
		return eb.Build().Int("width", width).Int("height", height).Errorf("plane side exceeds 2^31-1")
	}
	return nil
}

// NewPlaneU8 makes a plane of 8-bit samples. It takes ownership of samples;
// the caller must not modify the slice afterwards.
func NewPlaneU8(width, height int, samples []uint8) (plane Plane, err error) {
	if err = checkPlaneSize(width, height, len(samples)); err != nil {
		return
	}
	plane = Plane{kind: SampleKindU8, width: int32(width), height: int32(height), u8: samples, transform: IdentityTransform}
	return
}

// NewPlaneU16 makes a plane of 16-bit samples. It takes ownership of
// samples; the caller must not modify the slice afterwards.
func NewPlaneU16(width, height int, samples []uint16) (plane Plane, err error) {
	if err = checkPlaneSize(width, height, len(samples)); err != nil {
		return
	}
	plane = Plane{kind: SampleKindU16, width: int32(width), height: int32(height), u16: samples, transform: IdentityTransform}
	return
}

// NewPlaneF32 makes a plane of 32-bit float samples, in which NaN marks an
// invalid sample. It takes ownership of samples; the caller must not modify
// the slice afterwards.
func NewPlaneF32(width, height int, samples []float32) (plane Plane, err error) {
	if err = checkPlaneSize(width, height, len(samples)); err != nil {
		return
	}
	plane = Plane{kind: SampleKindF32, width: int32(width), height: int32(height), f32: samples, transform: IdentityTransform}
	return
}

// WithTransform returns the same samples under another value transform. A
// scale of zero is refused: every sample would stand for the same value.
func (inst Plane) WithTransform(t ValueTransform) (plane Plane, err error) {
	if t.Scale == 0 || math.IsNaN(t.Scale) || math.IsInf(t.Scale, 0) || math.IsNaN(t.Offset) || math.IsInf(t.Offset, 0) {
		return plane, eb.Build().Float64("scale", t.Scale).Float64("offset", t.Offset).
			Errorf("value transform needs a finite, non-zero scale and a finite offset")
	}
	plane = inst
	plane.transform = t
	return
}

func (inst Plane) Kind() SampleKindE         { return inst.kind }
func (inst Plane) Width() int                { return int(inst.width) }
func (inst Plane) Height() int               { return int(inst.height) }
func (inst Plane) Transform() ValueTransform { return inst.transform }

// IsZero reports whether the plane holds nothing.
func (inst Plane) IsZero() bool { return inst.kind == 0 }

// U8 returns the samples of a [SampleKindU8] plane and nil for any other. The
// slice is the plane's own: it must not be modified.
func (inst Plane) U8() []uint8 { return inst.u8 }

// U16 returns the samples of a [SampleKindU16] plane and nil for any other.
// The slice is the plane's own: it must not be modified.
func (inst Plane) U16() []uint16 { return inst.u16 }

// F32 returns the samples of a [SampleKindF32] plane and nil for any other.
// The slice is the plane's own: it must not be modified.
func (inst Plane) F32() []float32 { return inst.f32 }

// Contains reports whether (col, row) is a pixel of the plane.
func (inst Plane) Contains(col, row int) bool {
	return col >= 0 && row >= 0 && col < int(inst.width) && row < int(inst.height)
}

// StoredAt returns the stored sample of pixel (col, row) as a float64, which
// holds every sample of every kind exactly. Outside the plane, and for an
// invalid sample of a float plane, valid is false.
func (inst Plane) StoredAt(col, row int) (stored float64, valid bool) {
	if !inst.Contains(col, row) {
		return 0, false
	}
	i := row*int(inst.width) + col
	switch inst.kind {
	case SampleKindU8:
		return float64(inst.u8[i]), true
	case SampleKindU16:
		return float64(inst.u16[i]), true
	case SampleKindF32:
		v := inst.f32[i]
		return float64(v), v == v
	}
	return 0, false
}

// ValueAt returns what the sample of pixel (col, row) stands for: the stored
// sample through the plane's value transform.
func (inst Plane) ValueAt(col, row int) (value float64, valid bool) {
	stored, valid := inst.StoredAt(col, row)
	if !valid {
		return 0, false
	}
	return inst.transform.Apply(stored), true
}

// StoredRange returns the smallest and largest stored sample, skipping
// invalid ones. A plane with no valid sample reports ok false.
func (inst Plane) StoredRange() (lo, hi float64, ok bool) {
	switch inst.kind {
	case SampleKindU8:
		a, b := uint8(math.MaxUint8), uint8(0)
		for _, v := range inst.u8 {
			a, b = min(a, v), max(b, v)
		}
		return float64(a), float64(b), len(inst.u8) > 0
	case SampleKindU16:
		a, b := uint16(math.MaxUint16), uint16(0)
		for _, v := range inst.u16 {
			a, b = min(a, v), max(b, v)
		}
		return float64(a), float64(b), len(inst.u16) > 0
	case SampleKindF32:
		a, b := float32(math.Inf(1)), float32(math.Inf(-1))
		for _, v := range inst.f32 {
			if v != v {
				continue
			}
			a, b = min(a, v), max(b, v)
			ok = true
		}
		return float64(a), float64(b), ok
	}
	return 0, 0, false
}

// ColorModelE says what an image's planes are. The zero value is no model.
type ColorModelE uint8

const (
	ColorModelGray ColorModelE = iota + 1
	ColorModelGrayAlpha
	ColorModelRGB
	ColorModelRGBA
)

// AllColorModels lists the models an image can have.
var AllColorModels = []ColorModelE{ColorModelGray, ColorModelGrayAlpha, ColorModelRGB, ColorModelRGBA}

var colorModelPlanes = [...][]string{
	ColorModelGray:      {"gray"},
	ColorModelGrayAlpha: {"gray", "alpha"},
	ColorModelRGB:       {"red", "green", "blue"},
	ColorModelRGBA:      {"red", "green", "blue", "alpha"},
}

func (inst ColorModelE) String() string {
	switch inst {
	case ColorModelGray:
		return "gray"
	case ColorModelGrayAlpha:
		return "gray+alpha"
	case ColorModelRGB:
		return "rgb"
	case ColorModelRGBA:
		return "rgba"
	}
	return "none"
}

// PlaneNames returns the name of each plane of the model, in storage order.
func (inst ColorModelE) PlaneNames() []string {
	if int(inst) >= len(colorModelPlanes) {
		return nil
	}
	return colorModelPlanes[inst]
}

// PlaneCount is the number of planes an image of this model has.
func (inst ColorModelE) PlaneCount() int { return len(inst.PlaneNames()) }

// HasAlpha reports whether the last plane is alpha.
func (inst ColorModelE) HasAlpha() bool {
	return inst == ColorModelGrayAlpha || inst == ColorModelRGBA
}

// IsColor reports whether the model has red, green and blue planes, so that
// showing the image as stored is a composite and not a single plane.
func (inst ColorModelE) IsColor() bool {
	return inst == ColorModelRGB || inst == ColorModelRGBA
}

// DerivationE records what a reader did to a file's samples on the way to
// planes, beyond decoding them. It is a set: values combine with |.
type DerivationE uint8

const (
	// DerivationPaletteExpanded: the file stored indices into a palette and
	// the planes hold the palette's colours.
	DerivationPaletteExpanded DerivationE = 1 << iota
	// DerivationDepthExpanded: the file stored fewer than 8 bits per sample
	// and the decoder scaled them to the 8-bit range.
	DerivationDepthExpanded
	// DerivationColorKeyAlpha: the file named one colour as transparent and
	// the alpha plane was made from it.
	DerivationColorKeyAlpha
	// DerivationYCbCrConverted: the file stored luma and chroma and the
	// planes hold their conversion to red, green and blue.
	DerivationYCbCrConverted
	// DerivationCMYKConverted: the file stored ink amounts and the planes
	// hold a conversion to red, green and blue made without a profile.
	DerivationCMYKConverted
	// DerivationDefaultFrameOnly: the file holds an animation and the planes
	// are its default image.
	DerivationDefaultFrameOnly
)

// AllDerivations lists the single derivations, for iterating a set.
var AllDerivations = []DerivationE{
	DerivationPaletteExpanded, DerivationDepthExpanded, DerivationColorKeyAlpha,
	DerivationYCbCrConverted, DerivationCMYKConverted, DerivationDefaultFrameOnly,
}

// Has reports whether every derivation of d is in the set.
func (inst DerivationE) Has(d DerivationE) bool { return inst&d == d }

// Provenance is where an image's samples came from and what was done to
// them. A field the source did not determine is left at its zero value, which
// reads as unknown; nothing here is guessed.
type Provenance struct {
	// SourceName names the source for a person: a file name, not a path.
	SourceName string
	// Format is the container the samples were read from, e.g. "png".
	Format string
	// Decoder names what decoded them.
	Decoder string
	// StoredBitDepth is the bits per sample as the file stored them; 0 when
	// unknown.
	StoredBitDepth uint8
	// StoredColorType is the file's own name for its layout, e.g. "palette".
	StoredColorType string
	// Lossy reports that the format's compression does not preserve samples.
	Lossy bool
	// Derivations is what the reader did beyond decoding.
	Derivations DerivationE
	// ColorDeclaration is what the file said about its colour encoding, in
	// the file's terms; empty when it said nothing. It is recorded and not
	// applied (ADR-0373 §SD5).
	ColorDeclaration string
	// SampleHash is the BLAKE3 digest of the planes as [Image.Hash] defines
	// it; zero until an image is assembled.
	SampleHash [32]byte
}

// Image is one to four planes of one size and one kind with a colour model
// (ADR-0373 §SD1). The zero value is an empty image.
type Image struct {
	model      ColorModelE
	planes     []Plane
	provenance Provenance
}

// NewImage assembles an image. The planes must match the model in number and
// each other in size and kind; alpha, where the model has it, is the last
// plane and is straight — not multiplied into the others. The image keeps a
// copy of the planes slice, so the caller may reuse it; the samples are
// shared, and stay the planes' (see [NewPlaneU8]). The digest of the samples
// is computed here, once, and is what [Image.Hash] and the provenance's
// SampleHash report; a SampleHash the caller set is replaced.
func NewImage(model ColorModelE, planes []Plane, provenance Provenance) (img Image, err error) {
	if model.PlaneCount() == 0 || len(planes) != model.PlaneCount() {
		return img, eb.Build().Stringer("model", model).Int("planes", len(planes)).
			Errorf("number of planes does not match the colour model")
	}
	first := planes[0]
	for i, p := range planes {
		if p.IsZero() || p.kind != first.kind || p.width != first.width || p.height != first.height {
			return img, eb.Build().Int("plane", i).Stringer("kind", p.kind).Stringer("firstKind", first.kind).
				Int("width", p.Width()).Int("height", p.Height()).
				Errorf("planes of an image must share one size and one sample kind")
		}
	}
	img = Image{model: model, planes: slices.Clone(planes), provenance: provenance}
	img.provenance.SampleHash = img.digest()
	return
}

func (inst Image) Model() ColorModelE     { return inst.model }
func (inst Image) Provenance() Provenance { return inst.provenance }

// IsZero reports whether the image holds nothing.
func (inst Image) IsZero() bool { return inst.model == 0 }

// Width is the width of every plane; 0 for an empty image.
func (inst Image) Width() int {
	if len(inst.planes) == 0 {
		return 0
	}
	return inst.planes[0].Width()
}

// Height is the height of every plane; 0 for an empty image.
func (inst Image) Height() int {
	if len(inst.planes) == 0 {
		return 0
	}
	return inst.planes[0].Height()
}

// Kind is the sample kind of every plane.
func (inst Image) Kind() SampleKindE {
	if len(inst.planes) == 0 {
		return 0
	}
	return inst.planes[0].kind
}

// PlaneCount is the number of planes.
func (inst Image) PlaneCount() int { return len(inst.planes) }

// Plane returns plane i in the model's storage order, and an empty plane for
// an index the image does not have.
func (inst Image) Plane(i int) Plane {
	if i < 0 || i >= len(inst.planes) {
		return Plane{}
	}
	return inst.planes[i]
}

// Alpha returns the alpha plane, and ok false for a model without one.
func (inst Image) Alpha() (plane Plane, ok bool) {
	if !inst.model.HasAlpha() {
		return Plane{}, false
	}
	return inst.planes[len(inst.planes)-1], true
}

// Hash is the BLAKE3 digest of the image's samples, as [Digest] defines it:
// the model, the kind, the size, and each plane's own digest of its samples
// in storage order, integers and float bit patterns little-endian. Two images with the same digest hold the same
// samples whatever file they came from; value transforms and provenance are
// not part of it. It was computed when the image was assembled, so asking
// costs nothing; it is the zero digest for the empty image.
func (inst Image) Hash() (sum [32]byte) { return inst.provenance.SampleHash }

// digest computes [Image.Hash] from the samples.
func (inst Image) digest() (sum [32]byte) {
	d := NewDigest(inst.model, inst.Kind(), inst.Width(), inst.Height())
	for i, p := range inst.planes {
		d.WritePlane(i, p)
	}
	return d.Sum()
}
