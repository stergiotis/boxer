package ops

import (
	"github.com/stergiotis/boxer/public/science/imaging"
)

// OperatorI is an operator: its declaration, and a plan made from the
// inputs' descriptions and validated parameters, with the kernel that
// computes what the plan states.
type OperatorI interface {
	Declaration() Declaration
	// Plan states the output and the input each band needs (ADR-0400
	// §SD3), or refuses, wrapping [ErrRefused] and naming the reason. It
	// sees no sample. params are canonical ([Declaration.Validate]); the
	// inputs agree in size and their number is within the declaration's
	// bounds. Width, Height, Transform and Placement are the runner's and
	// are filled in after it returns.
	Plan(inputs []InputDesc, params Params) (Plan, KernelI, error)
}

// BorderE is what stands in for samples outside the image around a
// neighbourhood (ADR-0400 §SD6). The zero value is no border, for an
// operator that reads no neighbours.
type BorderE uint8

const (
	// BorderInvalid: samples outside the image do not exist, and every
	// output sample whose neighbourhood reaches outside it is invalid.
	BorderInvalid BorderE = iota + 1
	// BorderNearest: the nearest sample on the image's edge stands in.
	BorderNearest
)

// AllBorders lists the border rules.
var AllBorders = []BorderE{BorderInvalid, BorderNearest}

func (inst BorderE) String() string {
	switch inst {
	case BorderInvalid:
		return "invalid"
	case BorderNearest:
		return "nearest"
	}
	return "none"
}

// borderParam is the border parameter every neighbourhood operator takes:
// required, so that a request states what it means at the edge.
var borderParam = Param{
	Name: "border", Type: ParamTypeChoice, Required: true,
	Doc: "What stands in for samples outside the image around a neighbourhood.",
	Choices: []Choice{
		{Name: BorderInvalid.String(), Doc: "Samples outside the image do not exist: an output sample whose neighbourhood reaches outside the image is invalid."},
		{Name: BorderNearest.String(), Doc: "The nearest sample on the image's edge stands in for each sample outside it."},
	},
}

func parseBorder(s string) BorderE {
	for _, b := range AllBorders {
		if b.String() == s {
			return b
		}
	}
	return 0
}

// Plan is what an operation will make, stated before a sample is read
// (ADR-0400 §SD3). The output's samples are values (§SD4): a float kind
// under the identity transform, in which NaN is an invalid sample and no
// stored value is declared invalid (§SD5).
type Plan struct {
	Width, Height int
	Model         imaging.ColorModelE
	// Kind is [imaging.SampleKindF32] or [imaging.SampleKindF64].
	Kind        imaging.SampleKindE
	PlaneLabels []string
	// Transform is the identity.
	Transform imaging.ValueTransform
	// Placement is the inputs' placement when they all have the same one,
	// and the zero placement otherwise.
	Placement imaging.Placement
	// Halo is the rows and columns of input an output band needs on each
	// side; Border what stands in for those outside the image, given
	// whenever Halo is not zero.
	Halo   int
	Border BorderE
	// FirstPasses is the number of passes over the whole inputs the kernel
	// makes before any output (ADR-0400 §SD7).
	FirstPasses int
	// Notes are sentences for a person about what the plan decided; they go
	// into the record and the result's provenance.
	Notes []string
}

// Quantity is a number a first pass found, which went into the output and
// goes into the record (ADR-0400 §SD7). Plane is the output plane it
// belongs to, or −1 for one of the whole image.
type Quantity struct {
	Name  string  `json:"name"`
	Plane int     `json:"plane"`
	Value float64 `json:"value"`
}

// KernelI computes what a plan states. The runner calls it from one
// goroutine, except the workers' rows, which run concurrently on disjoint
// rows of one band.
type KernelI interface {
	// FirstPass is given every band of the inputs, top to bottom and without
	// a halo, once per pass the plan declares.
	FirstPass(pass int, b *Band) error
	// EndPass follows the last band of a pass; a refusal found by the pass
	// is returned here.
	EndPass(pass int) error
	// Quantities are the numbers the first passes found.
	Quantities() []Quantity
	// Worker makes the state one goroutine computes rows with.
	Worker() WorkerI
}

// WorkerI computes output rows.
type WorkerI interface {
	// Row computes output row y, an image row inside the band, into out:
	// one row of [Band.Width] values per output plane, NaN for an invalid
	// sample. A value that is not finite is written out as invalid, and so
	// is every sample the border rule makes invalid, whatever Row wrote.
	Row(b *Band, y int, out [][]float64)
}

// NoFirstPass is embedded by a kernel that makes no first pass.
type NoFirstPass struct{}

func (NoFirstPass) FirstPass(int, *Band) error { return nil }
func (NoFirstPass) EndPass(int) error          { return nil }
func (NoFirstPass) Quantities() []Quantity     { return nil }

// valueKind is the output kind of ADR-0400 §SD4: 32-bit float when every
// input kind is exact in a float32, 64-bit otherwise.
func valueKind(inputs []InputDesc) imaging.SampleKindE {
	for _, in := range inputs {
		switch in.Kind {
		case imaging.SampleKindU8, imaging.SampleKindU16, imaging.SampleKindI16, imaging.SampleKindF32:
		default:
			return imaging.SampleKindF64
		}
	}
	return imaging.SampleKindF32
}
