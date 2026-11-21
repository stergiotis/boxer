package imaging

import (
	"encoding/binary"
	"slices"
	"strconv"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// AxisRoleE says how a stack's axis is looked at (ADR-0398 §SD3). The zero
// value is no role and is refused.
type AxisRoleE uint8

const (
	// AxisRoleBand is an axis whose positions are measured together and may
	// be shown together: one position as a grey image, three as a colour
	// composite. A stack has at most one.
	AxisRoleBand AxisRoleE = iota + 1
	// AxisRoleSelector is an axis of which a view shows one position: a
	// time, a depth, a focus step.
	AxisRoleSelector
)

// AllAxisRoles lists the roles an axis can have.
var AllAxisRoles = []AxisRoleE{AxisRoleBand, AxisRoleSelector}

func (inst AxisRoleE) String() string {
	switch inst {
	case AxisRoleBand:
		return "band"
	case AxisRoleSelector:
		return "selector"
	}
	return "none"
}

// Axis is one axis of a stack other than rows and columns.
type Axis struct {
	Name string
	Role AxisRoleE
	Size int
	// Unit is the unit of Coords; empty is unknown.
	Unit string
	// Coords is nil, or one coordinate per position: a wavelength, a time,
	// a depth.
	Coords []float64
	// Labels is nil, or one label per position: a band's name.
	Labels []string
}

func (inst Axis) clone() Axis {
	inst.Coords = slices.Clone(inst.Coords)
	inst.Labels = slices.Clone(inst.Labels)
	return inst
}

// Stack is planes of one size and one kind arranged along named axes other
// than rows and columns (ADR-0398 §SD3), from which an [Image] — one plane
// or three — is taken without copying samples. It is resident in memory.
// The zero value is an empty stack.
type Stack struct {
	axes       []Axis
	planes     []Plane
	digests    [][32]byte
	model      ColorModelE
	band       int
	placement  Placement
	provenance Provenance
}

// NewStack assembles a stack. The planes are in row-major order over the
// axes, the last axis varying fastest, and there are as many as the product
// of the axes' sizes — exactly one for no axes. They share one size, one
// kind, and one declared invalid value or none; their value transforms may
// differ. Axis names are non-empty and unique, every axis has a role and a
// positive size, at most one is a band axis, and Coords and Labels are nil
// or of the axis's size.
//
// model is zero, or the colour model the band axis is — its plane count is
// the band axis's size; a one-plane model also fits a stack without a band
// axis. A model with alpha is refused when the planes declare an invalid
// value, since an alpha plane declares none. A placement other than the
// zero one must pass [Placement.Validate].
//
// The stack copies the axes, their coordinates and labels, the planes slice
// and the provenance's notes; the samples are shared and stay the planes'.
// Each plane's digest is computed here, once, so neither [Stack.Hash] nor
// an image taken from the stack hashes the samples again.
func NewStack(axes []Axis, planes []Plane, model ColorModelE, placement Placement, provenance Provenance) (stack Stack, err error) {
	band := -1
	n := 1
	for i, a := range axes {
		switch {
		case a.Name == "":
			return stack, eb.Build().Int("axis", i).Errorf("an axis needs a name")
		case !slices.Contains(AllAxisRoles, a.Role):
			return stack, eb.Build().Str("axis", a.Name).Uint8("role", uint8(a.Role)).Errorf("an axis needs a role")
		case a.Size <= 0:
			return stack, eb.Build().Str("axis", a.Name).Int("size", a.Size).Errorf("an axis needs a positive size")
		case a.Coords != nil && len(a.Coords) != a.Size:
			return stack, eb.Build().Str("axis", a.Name).Int("size", a.Size).Int("coords", len(a.Coords)).
				Errorf("an axis has one coordinate per position, or none")
		case a.Labels != nil && len(a.Labels) != a.Size:
			return stack, eb.Build().Str("axis", a.Name).Int("size", a.Size).Int("labels", len(a.Labels)).
				Errorf("an axis has one label per position, or none")
		}
		for _, b := range axes[:i] {
			if b.Name == a.Name {
				return stack, eb.Build().Str("axis", a.Name).Errorf("axis names must be unique")
			}
		}
		if a.Role == AxisRoleBand {
			if band >= 0 {
				return stack, eb.Build().Str("axis", a.Name).Str("first", axes[band].Name).
					Errorf("a stack has at most one band axis")
			}
			band = i
		}
		// Stop multiplying once past the plane count, so the product cannot
		// overflow.
		if n > len(planes)/a.Size {
			n = len(planes) + 1
		} else {
			n *= a.Size
		}
	}
	if n != len(planes) || len(planes) == 0 {
		return stack, eb.Build().Int("planes", len(planes)).Int("axes", len(axes)).
			Errorf("the number of planes is not the product of the axes' sizes")
	}
	first := planes[0]
	for i, p := range planes {
		if p.IsZero() || p.kind != first.kind || p.width != first.width || p.height != first.height {
			return stack, eb.Build().Int("plane", i).Stringer("kind", p.kind).Stringer("firstKind", first.kind).
				Int("width", p.Width()).Int("height", p.Height()).
				Errorf("planes of a stack must share one size and one sample kind")
		}
		if p.hasInvalid != first.hasInvalid || p.invalid != first.invalid {
			return stack, eb.Build().Int("plane", i).Bool("declared", p.hasInvalid).Float64("invalid", p.invalid).
				Errorf("planes of a stack must declare the same invalid value, or none")
		}
	}
	if model != 0 {
		bands := 1
		if band >= 0 {
			bands = axes[band].Size
		}
		if model.PlaneCount() == 0 || model.PlaneCount() != bands {
			return stack, eb.Build().Stringer("model", model).Int("bands", bands).
				Errorf("the colour model does not match the band axis")
		}
		if model.HasAlpha() && first.hasInvalid {
			return stack, eb.Build().Stringer("model", model).Float64("invalid", first.invalid).
				Errorf("a stack whose model has alpha cannot declare an invalid value")
		}
	}
	if err = placement.Validate(); err != nil {
		return
	}
	stack = Stack{
		axes:       make([]Axis, len(axes)),
		planes:     slices.Clone(planes),
		digests:    make([][32]byte, len(planes)),
		model:      model,
		band:       band,
		placement:  placement.clone(),
		provenance: provenance.clone(),
	}
	for i, a := range axes {
		stack.axes[i] = a.clone()
	}
	for i, p := range planes {
		stack.digests[i] = planeDigest(p)
	}
	stack.provenance.SampleHash = stack.digest()
	return
}

// IsZero reports whether the stack holds nothing.
func (inst Stack) IsZero() bool { return len(inst.planes) == 0 }

// Width is the width of every plane; 0 for an empty stack.
func (inst Stack) Width() int {
	if len(inst.planes) == 0 {
		return 0
	}
	return inst.planes[0].Width()
}

// Height is the height of every plane; 0 for an empty stack.
func (inst Stack) Height() int {
	if len(inst.planes) == 0 {
		return 0
	}
	return inst.planes[0].Height()
}

// Kind is the sample kind of every plane.
func (inst Stack) Kind() SampleKindE {
	if len(inst.planes) == 0 {
		return 0
	}
	return inst.planes[0].kind
}

// Axes returns a copy of the axes, coordinates and labels included.
func (inst Stack) Axes() []Axis {
	out := make([]Axis, len(inst.axes))
	for i, a := range inst.axes {
		out[i] = a.clone()
	}
	return out
}

// PlaneCount is the number of planes: the product of the axes' sizes.
func (inst Stack) PlaneCount() int { return len(inst.planes) }

// Model is the colour model the band axis is, or zero when it is none.
func (inst Stack) Model() ColorModelE { return inst.model }

// Placement is where the pixels of every plane are. Its Unapplied list is a
// copy.
func (inst Stack) Placement() Placement { return inst.placement.clone() }

// Provenance is where the samples came from; its SampleHash is
// [Stack.Hash]. Its Notes are a copy.
func (inst Stack) Provenance() Provenance { return inst.provenance.clone() }

// Bytes is what the stack's samples occupy.
func (inst Stack) Bytes() int64 {
	return int64(len(inst.planes)) * int64(inst.Width()) * int64(inst.Height()) * int64(inst.Kind().Bytes())
}

// BandAxis is the index of the band axis, and ok false when the stack has
// none.
func (inst Stack) BandAxis() (axis int, ok bool) {
	if inst.IsZero() || inst.band < 0 {
		return 0, false
	}
	return inst.band, true
}

// Flat is the position in plane order of the plane at index, one position
// per axis.
func (inst Stack) Flat(index []int) (flat int, err error) {
	if len(index) != len(inst.axes) || inst.IsZero() {
		return 0, eb.Build().Int("index", len(index)).Int("axes", len(inst.axes)).
			Errorf("an index has one position per axis")
	}
	for i, a := range inst.axes {
		at := index[i]
		if at < 0 || at >= a.Size {
			return 0, eb.Build().Str("axis", a.Name).Int("position", at).Int("size", a.Size).
				Errorf("position is outside the axis")
		}
		flat = flat*a.Size + at
	}
	return
}

// Index is the position on each axis of the plane at flat, the inverse of
// [Stack.Flat]; nil for a flat position the stack does not have.
func (inst Stack) Index(flat int) []int {
	if flat < 0 || flat >= len(inst.planes) {
		return nil
	}
	index := make([]int, len(inst.axes))
	for i := len(inst.axes) - 1; i >= 0; i-- {
		s := inst.axes[i].Size
		index[i] = flat % s
		flat /= s
	}
	return index
}

// Plane returns the plane at index, one position per axis.
func (inst Stack) Plane(index []int) (plane Plane, err error) {
	flat, err := inst.Flat(index)
	if err != nil {
		return
	}
	return inst.planes[flat], nil
}

// Label names position pos of axis axis for a person: the axis's label for
// it, or the position number when the axis has no labels. It is empty for
// an axis or position the stack does not have.
func (inst Stack) Label(axis, pos int) string {
	if axis < 0 || axis >= len(inst.axes) || pos < 0 || pos >= inst.axes[axis].Size {
		return ""
	}
	if l := inst.axes[axis].Labels; l != nil {
		return l[pos]
	}
	return strconv.Itoa(pos)
}

// Hash is the BLAKE3 digest of the stack's samples: over a header — the
// kind, the size, each axis's size and role — and each plane's own digest
// in plane order, as [Image.Hash] takes them. Axis names, coordinates and
// labels, the model, value transforms, the invalid value, the placement and
// the provenance are not part of it. It is the zero digest for the empty
// stack.
func (inst Stack) Hash() [32]byte { return inst.provenance.SampleHash }

// stackTag begins a stack's digest header, so that it is never the header
// of an image, which begins with a colour model's byte.
const stackTag = "stack"

func (inst Stack) digest() [32]byte {
	head := make([]byte, 0, len(stackTag)+1+4+4+4+5*len(inst.axes))
	head = append(head, stackTag...)
	head = append(head, byte(inst.Kind()))
	head = binary.LittleEndian.AppendUint32(head, uint32(inst.Width()))
	head = binary.LittleEndian.AppendUint32(head, uint32(inst.Height()))
	head = binary.LittleEndian.AppendUint32(head, uint32(len(inst.axes)))
	for _, a := range inst.axes {
		head = binary.LittleEndian.AppendUint32(head, uint32(a.Size))
		head = append(head, byte(a.Role))
	}
	return sumOver(head, inst.digests)
}

// Image takes an image from the stack (ADR-0398 §SD3). at has one position
// per axis; the band axis's entry is ignored. bands is one or three
// positions on the band axis, giving a [ColorModelGray] or [ColorModelRGB]
// image, and is empty for a stack without a band axis, giving a grey image.
// The image shares the stack's planes and carries its placement and
// provenance; its plane labels are the band positions' labels
// ([Stack.Label]). Its digest is [Image.Hash] of those planes, derived from
// the digests the stack holds.
func (inst Stack) Image(at []int, bands []int) (img Image, err error) {
	var model ColorModelE
	if inst.band < 0 {
		if len(bands) != 0 {
			return img, eb.Build().Int("bands", len(bands)).Errorf("a stack without a band axis takes no band positions")
		}
		model = ColorModelGray
	} else {
		switch len(bands) {
		case 1:
			model = ColorModelGray
		case 3:
			model = ColorModelRGB
		default:
			return img, eb.Build().Int("bands", len(bands)).Errorf("an image takes one band position or three")
		}
	}
	return inst.take(model, at, bands, true)
}

// AsStored is the image as stored, for a stack whose band axis is a colour
// model: the image under that model from every band position in order,
// alpha included, at the selector positions of at (the band axis's entry
// ignored). A stack without a model reports ok false. The plane labels are
// the band axis's labels when it has them, otherwise the model's plane
// names.
func (inst Stack) AsStored(at []int) (img Image, ok bool, err error) {
	if inst.model == 0 {
		return img, false, nil
	}
	var bands []int
	labelled := false
	if inst.band >= 0 {
		bands = make([]int, inst.axes[inst.band].Size)
		for i := range bands {
			bands[i] = i
		}
		labelled = inst.axes[inst.band].Labels != nil
	}
	img, err = inst.take(inst.model, at, bands, labelled)
	return img, err == nil, err
}

func (inst Stack) take(model ColorModelE, at []int, bands []int, labelled bool) (img Image, err error) {
	if inst.IsZero() {
		return img, eb.Build().Errorf("an empty stack has no image")
	}
	if len(at) != len(inst.axes) {
		return img, eb.Build().Int("at", len(at)).Int("axes", len(inst.axes)).Errorf("an index has one position per axis")
	}
	index := slices.Clone(at)
	n := max(1, len(bands))
	planes, digests := make([]Plane, n), make([][32]byte, n)
	var labels []string
	if labelled && inst.band >= 0 {
		labels = make([]string, n)
	}
	for i := range n {
		if inst.band >= 0 {
			b := bands[i]
			if b < 0 || b >= inst.axes[inst.band].Size {
				return img, eb.Build().Str("axis", inst.axes[inst.band].Name).Int("position", b).
					Int("size", inst.axes[inst.band].Size).Errorf("band position is outside the band axis")
			}
			index[inst.band] = b
			if labels != nil {
				labels[i] = inst.Label(inst.band, b)
			}
		}
		flat, ferr := inst.Flat(index)
		if ferr != nil {
			return img, ferr
		}
		planes[i], digests[i] = inst.planes[flat], inst.digests[flat]
	}
	if err = checkImagePlanes(model, planes); err != nil {
		return
	}
	img = assembleImage(model, planes, digests, inst.provenance.clone())
	img.labels = labels
	img.placement = inst.placement.clone()
	return
}
