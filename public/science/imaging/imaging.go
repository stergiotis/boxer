package imaging

import (
	"math"
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// SampleKindE is the stored type of a plane's samples. The zero value is no
// kind: a plane of it holds nothing. The value of a kind is part of an
// image's digest, so kinds are only ever appended (ADR-0398 §SD1).
type SampleKindE uint8

const (
	SampleKindU8 SampleKindE = iota + 1
	SampleKindU16
	SampleKindF32
	SampleKindI16
	SampleKindI32
	SampleKindF64
)

// AllSampleKinds lists the kinds a plane can have.
var AllSampleKinds = []SampleKindE{SampleKindU8, SampleKindU16, SampleKindF32, SampleKindI16, SampleKindI32, SampleKindF64}

func (inst SampleKindE) String() string {
	switch inst {
	case SampleKindU8:
		return "u8"
	case SampleKindU16:
		return "u16"
	case SampleKindF32:
		return "f32"
	case SampleKindI16:
		return "i16"
	case SampleKindI32:
		return "i32"
	case SampleKindF64:
		return "f64"
	}
	return "none"
}

// IsInteger reports whether samples of this kind are integers, signed or
// unsigned. Whether a histogram of the kind is exact is [SampleKindE.Codes].
func (inst SampleKindE) IsInteger() bool {
	switch inst {
	case SampleKindU8, SampleKindU16, SampleKindI16, SampleKindI32:
		return true
	}
	return false
}

// IsFloat reports whether samples of this kind are floating point, in which
// NaN marks an invalid sample.
func (inst SampleKindE) IsFloat() bool {
	return inst == SampleKindF32 || inst == SampleKindF64
}

// IsSigned reports whether samples of this kind are signed integers.
func (inst SampleKindE) IsSigned() bool {
	return inst == SampleKindI16 || inst == SampleKindI32
}

// NominalMax is the largest stored sample of an unsigned integer kind, whose
// nominal range is [0, NominalMax]. Every other kind reports ok false: a
// float kind has no nominal range, and a signed kind's does not start at 0
// ([SampleKindE.NominalRange] covers both integer forms).
func (inst SampleKindE) NominalMax() (hi float64, ok bool) {
	switch inst {
	case SampleKindU8:
		return math.MaxUint8, true
	case SampleKindU16:
		return math.MaxUint16, true
	}
	return 0, false
}

// NominalRange is the smallest and largest stored sample an integer kind can
// hold; a float kind has no nominal range and reports ok false.
func (inst SampleKindE) NominalRange() (lo, hi float64, ok bool) {
	switch inst {
	case SampleKindU8:
		return 0, math.MaxUint8, true
	case SampleKindU16:
		return 0, math.MaxUint16, true
	case SampleKindI16:
		return math.MinInt16, math.MaxInt16, true
	case SampleKindI32:
		return math.MinInt32, math.MaxInt32, true
	}
	return 0, 0, false
}

// Codes is the smallest stored code and the number of codes of a kind that
// has at most 2^16 of them, so that a histogram has one bin per code and a
// display mapping is a lookup table indexed by stored − lo (ADR-0398 §SD1).
// Signed 32-bit and the float kinds report ok false: they are binned and
// windowed.
func (inst SampleKindE) Codes() (lo int64, n int, ok bool) {
	switch inst {
	case SampleKindU8:
		return 0, 1 << 8, true
	case SampleKindU16:
		return 0, 1 << 16, true
	case SampleKindI16:
		return math.MinInt16, 1 << 16, true
	}
	return 0, 0, false
}

// Bytes is the size of one sample of the kind; 0 for no kind.
func (inst SampleKindE) Bytes() int {
	switch inst {
	case SampleKindU8:
		return 1
	case SampleKindU16, SampleKindI16:
		return 2
	case SampleKindF32, SampleKindI32:
		return 4
	case SampleKindF64:
		return 8
	}
	return 0
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
	kind       SampleKindE
	hasInvalid bool
	width      int32
	height     int32
	u8         []uint8
	u16        []uint16
	f32        []float32
	i16        []int16
	i32        []int32
	f64        []float64
	transform  ValueTransform
	invalid    float64
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

// NewPlaneI16 makes a plane of signed 16-bit samples. It takes ownership of
// samples; the caller must not modify the slice afterwards.
func NewPlaneI16(width, height int, samples []int16) (plane Plane, err error) {
	if err = checkPlaneSize(width, height, len(samples)); err != nil {
		return
	}
	plane = Plane{kind: SampleKindI16, width: int32(width), height: int32(height), i16: samples, transform: IdentityTransform}
	return
}

// NewPlaneI32 makes a plane of signed 32-bit samples. It takes ownership of
// samples; the caller must not modify the slice afterwards.
func NewPlaneI32(width, height int, samples []int32) (plane Plane, err error) {
	if err = checkPlaneSize(width, height, len(samples)); err != nil {
		return
	}
	plane = Plane{kind: SampleKindI32, width: int32(width), height: int32(height), i32: samples, transform: IdentityTransform}
	return
}

// NewPlaneF64 makes a plane of 64-bit float samples, in which NaN marks an
// invalid sample. It takes ownership of samples; the caller must not modify
// the slice afterwards.
func NewPlaneF64(width, height int, samples []float64) (plane Plane, err error) {
	if err = checkPlaneSize(width, height, len(samples)); err != nil {
		return
	}
	plane = Plane{kind: SampleKindF64, width: int32(width), height: int32(height), f64: samples, transform: IdentityTransform}
	return
}

// WithInvalid returns the same samples with one stored value declared
// invalid: it marks "no measurement", as NaN does in a float plane
// (ADR-0398 §SD2). [Plane.StoredAt] and [Plane.ValueAt] report it as
// invalid and [Plane.StoredRange] skips it. A value the plane's kind cannot
// hold exactly is refused — outside the kind's range or fractional for an
// integer kind, not a float32 for [SampleKindF32] — and so are NaN, which
// is invalid already, and the infinities. The declaration is not part of an
// image's digest.
func (inst Plane) WithInvalid(stored float64) (plane Plane, err error) {
	if inst.IsZero() {
		return plane, eb.Build().Errorf("an empty plane has no samples to declare invalid")
	}
	if math.IsNaN(stored) || math.IsInf(stored, 0) {
		return plane, eb.Build().Float64("stored", stored).Stringer("kind", inst.kind).
			Errorf("the invalid value must be finite; NaN is invalid in a float plane already")
	}
	if lo, hi, isInt := inst.kind.NominalRange(); isInt {
		if stored != math.Trunc(stored) || stored < lo || stored > hi {
			return plane, eb.Build().Float64("stored", stored).Stringer("kind", inst.kind).
				Errorf("the invalid value is not a sample this integer kind can hold")
		}
	} else if inst.kind == SampleKindF32 && float64(float32(stored)) != stored {
		return plane, eb.Build().Float64("stored", stored).
			Errorf("the invalid value is not exactly a 32-bit float")
	}
	plane = inst
	plane.invalid, plane.hasInvalid = stored, true
	return
}

// Invalid returns the stored value declared invalid by [Plane.WithInvalid],
// and ok false when there is none.
func (inst Plane) Invalid() (stored float64, ok bool) {
	return inst.invalid, inst.hasInvalid
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

// I16 returns the samples of a [SampleKindI16] plane and nil for any other.
// The slice is the plane's own: it must not be modified.
func (inst Plane) I16() []int16 { return inst.i16 }

// I32 returns the samples of a [SampleKindI32] plane and nil for any other.
// The slice is the plane's own: it must not be modified.
func (inst Plane) I32() []int32 { return inst.i32 }

// F64 returns the samples of a [SampleKindF64] plane and nil for any other.
// The slice is the plane's own: it must not be modified.
func (inst Plane) F64() []float64 { return inst.f64 }

// Contains reports whether (col, row) is a pixel of the plane.
func (inst Plane) Contains(col, row int) bool {
	return col >= 0 && row >= 0 && col < int(inst.width) && row < int(inst.height)
}

// StoredAt returns the stored sample of pixel (col, row) as a float64, which
// holds every sample of every kind exactly. Outside the plane, for NaN in a
// float plane and for the value declared invalid ([Plane.WithInvalid]),
// valid is false.
func (inst Plane) StoredAt(col, row int) (stored float64, valid bool) {
	if !inst.Contains(col, row) {
		return 0, false
	}
	i := row*int(inst.width) + col
	switch inst.kind {
	case SampleKindU8:
		stored, valid = float64(inst.u8[i]), true
	case SampleKindU16:
		stored, valid = float64(inst.u16[i]), true
	case SampleKindF32:
		v := inst.f32[i]
		stored, valid = float64(v), v == v
	case SampleKindI16:
		stored, valid = float64(inst.i16[i]), true
	case SampleKindI32:
		stored, valid = float64(inst.i32[i]), true
	case SampleKindF64:
		stored = inst.f64[i]
		valid = stored == stored
	default:
		return 0, false
	}
	if inst.hasInvalid && stored == inst.invalid {
		valid = false
	}
	return
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
// invalid ones: NaN, and the value declared invalid. A plane with no valid
// sample reports ok false.
func (inst Plane) StoredRange() (lo, hi float64, ok bool) {
	switch inst.kind {
	case SampleKindU8:
		return intRange(inst.u8, inst.hasInvalid, inst.invalid)
	case SampleKindU16:
		return intRange(inst.u16, inst.hasInvalid, inst.invalid)
	case SampleKindI16:
		return intRange(inst.i16, inst.hasInvalid, inst.invalid)
	case SampleKindI32:
		return intRange(inst.i32, inst.hasInvalid, inst.invalid)
	case SampleKindF32:
		return floatRange(inst.f32, inst.hasInvalid, inst.invalid)
	case SampleKindF64:
		return floatRange(inst.f64, inst.hasInvalid, inst.invalid)
	}
	return 0, 0, false
}

type intSampleI interface {
	uint8 | uint16 | int16 | int32
}

type floatSampleI interface {
	float32 | float64
}

func intRange[T intSampleI](s []T, hasInvalid bool, invalid float64) (lo, hi float64, ok bool) {
	var a, b T
	if !hasInvalid {
		if len(s) == 0 {
			return 0, 0, false
		}
		a, b = s[0], s[0]
		for _, v := range s[1:] {
			a, b = min(a, v), max(b, v)
		}
		return float64(a), float64(b), true
	}
	// The declared value is one the kind holds exactly (WithInvalid), so the
	// conversion is exact.
	skip := T(invalid)
	for _, v := range s {
		if v == skip {
			continue
		}
		if !ok {
			a, b, ok = v, v, true
			continue
		}
		a, b = min(a, v), max(b, v)
	}
	return float64(a), float64(b), ok
}

func floatRange[T floatSampleI](s []T, hasInvalid bool, invalid float64) (lo, hi float64, ok bool) {
	a, b := T(math.Inf(1)), T(math.Inf(-1))
	skip := T(invalid)
	for _, v := range s {
		if v != v || (hasInvalid && v == skip) {
			continue
		}
		a, b = min(a, v), max(b, v)
		ok = true
	}
	return float64(a), float64(b), ok
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
	// DerivationRowsFlipped: the file stored the bottom row first and the
	// planes hold row 0 at the top (ADR-0373 §SD3, ADR-0398 §SD5).
	DerivationRowsFlipped
	// DerivationAxesReordered: the file's index order was converted to the
	// row-major order of the planes and of a stack's axes (ADR-0398 §SD5).
	DerivationAxesReordered
)

// AllDerivations lists the single derivations, for iterating a set.
var AllDerivations = []DerivationE{
	DerivationPaletteExpanded, DerivationDepthExpanded, DerivationColorKeyAlpha,
	DerivationYCbCrConverted, DerivationCMYKConverted, DerivationDefaultFrameOnly,
	DerivationRowsFlipped, DerivationAxesReordered,
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
	// Variant is the variant of the format, in the format's own word, where
	// one reader's name covers several ("bigtiff" of "tiff"); empty
	// otherwise (ADR-0398 §SD5).
	Variant string
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
	// SampleHash is the BLAKE3 digest of the samples: [Image.Hash] for an
	// image, [Stack.Hash] for a stack; zero until one is assembled.
	SampleHash [32]byte
	// Notes are sentences for a person about what a reader did or left
	// unapplied that the fields above do not say (ADR-0398 §SD5).
	Notes []string
}

func (inst Provenance) clone() Provenance {
	inst.Notes = slices.Clone(inst.Notes)
	return inst
}

// Image is one to four planes of one size and one kind with a colour model
// (ADR-0373 §SD1), optionally a label per plane and a placement (ADR-0398
// §SD3, §SD4). The zero value is an empty image.
type Image struct {
	model      ColorModelE
	planes     []Plane
	labels     []string
	placement  Placement
	provenance Provenance
}

// NewImage assembles an image. The planes must match the model in number and
// each other in size and kind, and declare the same invalid value or none
// ([Plane.WithInvalid]); alpha, where the model has it, is the last plane, is
// straight — not multiplied into the others — and declares no invalid value.
// The image keeps a copy of the planes slice, so the caller may reuse it; the
// samples are shared, and stay the planes' (see [NewPlaneU8]). The digest of
// the samples is computed here, once, and is what [Image.Hash] and the
// provenance's SampleHash report; a SampleHash the caller set is replaced.
func NewImage(model ColorModelE, planes []Plane, provenance Provenance) (img Image, err error) {
	if err = checkImagePlanes(model, planes); err != nil {
		return
	}
	digests := make([][32]byte, len(planes))
	for i, p := range planes {
		digests[i] = planeDigest(p)
	}
	img = assembleImage(model, slices.Clone(planes), digests, provenance.clone())
	return
}

func checkImagePlanes(model ColorModelE, planes []Plane) error {
	if model.PlaneCount() == 0 || len(planes) != model.PlaneCount() {
		return eb.Build().Stringer("model", model).Int("planes", len(planes)).
			Errorf("number of planes does not match the colour model")
	}
	first := planes[0]
	for i, p := range planes {
		if p.IsZero() || p.kind != first.kind || p.width != first.width || p.height != first.height {
			return eb.Build().Int("plane", i).Stringer("kind", p.kind).Stringer("firstKind", first.kind).
				Int("width", p.Width()).Int("height", p.Height()).
				Errorf("planes of an image must share one size and one sample kind")
		}
	}
	colour := planes
	if model.HasAlpha() {
		alpha := planes[len(planes)-1]
		if alpha.hasInvalid {
			return eb.Build().Float64("invalid", alpha.invalid).Errorf("an alpha plane declares no invalid value")
		}
		colour = planes[:len(planes)-1]
	}
	for i, p := range colour {
		if p.hasInvalid != first.hasInvalid || p.invalid != first.invalid {
			return eb.Build().Int("plane", i).Bool("declared", p.hasInvalid).Float64("invalid", p.invalid).
				Bool("firstDeclared", first.hasInvalid).Float64("firstInvalid", first.invalid).
				Errorf("planes of an image must declare the same invalid value, or none")
		}
	}
	return nil
}

// assembleImage makes an image of checked planes whose digests are known,
// so that an image taken from a stack is not hashed again.
func assembleImage(model ColorModelE, planes []Plane, digests [][32]byte, provenance Provenance) (img Image) {
	img = Image{model: model, planes: planes, provenance: provenance}
	first := planes[0]
	img.provenance.SampleHash = sumImage(model, first.kind, first.Width(), first.Height(), digests)
	return
}

func (inst Image) Model() ColorModelE { return inst.model }

// Provenance is where the samples came from. Its Notes are a copy.
func (inst Image) Provenance() Provenance { return inst.provenance.clone() }

// WithPlaneLabels returns the image with a label per plane, in storage order:
// what a readout names a plane by, such as the band of a stack it was taken
// from. The labels are copied.
func (inst Image) WithPlaneLabels(labels []string) (img Image, err error) {
	if len(labels) != len(inst.planes) {
		return img, eb.Build().Int("labels", len(labels)).Int("planes", len(inst.planes)).
			Errorf("an image needs one label per plane")
	}
	img = inst
	img.labels = slices.Clone(labels)
	return
}

// PlaneLabels returns the label of each plane in storage order: those set by
// [Image.WithPlaneLabels], or the colour model's plane names when none were.
// The slice is a copy.
func (inst Image) PlaneLabels() []string {
	if inst.labels != nil {
		return slices.Clone(inst.labels)
	}
	return slices.Clone(inst.model.PlaneNames())
}

// WithPlacement returns the image with a placement (ADR-0398 §SD4). The
// placement is stored as given, its Unapplied list copied; a caller that
// builds one checks it with [Placement.Validate]. It is not part of the
// digest.
func (inst Image) WithPlacement(p Placement) Image {
	inst.placement = p.clone()
	return inst
}

// Placement is where the image's pixels are; the zero placement when
// nothing says so. Its Unapplied list is a copy.
func (inst Image) Placement() Placement { return inst.placement.clone() }

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
// in storage order, integers and float bit patterns little-endian. Two
// images with the same digest hold the same samples whatever file they came
// from; value transforms, invalid values, plane labels, the placement and
// the provenance are not part of it. It was computed when the image was
// assembled, so asking costs nothing; it is the zero digest for the empty
// image.
func (inst Image) Hash() (sum [32]byte) { return inst.provenance.SampleHash }

// digest computes [Image.Hash] from the samples, through [Digest].
func (inst Image) digest() (sum [32]byte) {
	d := NewDigest(inst.model, inst.Kind(), inst.Width(), inst.Height())
	for i, p := range inst.planes {
		d.WritePlane(i, p)
	}
	return d.Sum()
}
