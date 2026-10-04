package capture

import (
	"image"
	"image/draw"
	"math"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// PhaseE orders obligation handlers: what is drawn, then what is added or
// degraded. Encoding follows them all and is not an obligation.
type PhaseE uint8

const (
	PhaseScope     PhaseE = 0
	PhaseTransform PhaseE = 1
)

// Frame is a pixel capture between rendering and encoding.
type Frame struct {
	Img *image.RGBA
	// PixelsPerPoint converts logical points, the unit of a crop, to pixels.
	PixelsPerPoint float32
}

// PixelHandlerI carries out one obligation on a pixel capture.
type PixelHandlerI interface {
	Phase() PhaseE
	// Check refuses an obligation the handler cannot carry out, before
	// anything is rendered.
	Check(o Obligation) error
	Apply(f *Frame, o Obligation) error
}

// SvgHandlerI carries out one obligation on an SVG capture.
type SvgHandlerI interface {
	Phase() PhaseE
	Check(o Obligation) error
	Apply(svg []byte, o Obligation) ([]byte, error)
}

// Registry holds the handlers, by obligation name and format. An obligation
// without a handler for the request's format denies the capture.
type Registry struct {
	pixel map[string]PixelHandlerI
	svg   map[string]SvgHandlerI
}

// NewRegistry returns a registry with the scope handlers.
func NewRegistry() (inst *Registry) {
	inst = &Registry{pixel: map[string]PixelHandlerI{}, svg: map[string]SvgHandlerI{}}
	inst.RegisterPixel(ObligationScope, scopePixels{})
	inst.RegisterSvg(ObligationScope, scopeSvg{})
	return
}

func (inst *Registry) RegisterPixel(name string, h PixelHandlerI) { inst.pixel[name] = h }
func (inst *Registry) RegisterSvg(name string, h SvgHandlerI)     { inst.svg[name] = h }

// scopePixels: the windows are chosen before rendering (the source draws
// only the scope's windows); the crop is applied after.
type scopePixels struct{}

func (inst scopePixels) Phase() PhaseE { return PhaseScope }

func (inst scopePixels) Check(o Obligation) error {
	if o.Scope == nil || len(o.Scope.Windows) == 0 {
		return eh.Errorf("a scope names at least one window")
	}
	return nil
}

func (inst scopePixels) Apply(f *Frame, o Obligation) error {
	if o.Scope.Crop == nil {
		return nil
	}
	ppp := float64(f.PixelsPerPoint)
	if ppp <= 0 {
		ppp = 1
	}
	c := *o.Scope.Crop
	r := image.Rect(
		int(math.Floor(float64(c.Min.X)*ppp)), int(math.Floor(float64(c.Min.Y)*ppp)),
		int(math.Ceil(float64(c.Max.X)*ppp)), int(math.Ceil(float64(c.Max.Y)*ppp)),
	).Intersect(f.Img.Bounds())
	if r.Empty() {
		return eh.Errorf("the crop lies outside the frame")
	}
	out := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(out, out.Bounds(), f.Img, r.Min, draw.Src)
	f.Img = out
	return nil
}

// scopeSvg: an SVG capture is one window's shapes, uncropped.
type scopeSvg struct{}

func (inst scopeSvg) Phase() PhaseE { return PhaseScope }

func (inst scopeSvg) Check(o Obligation) error {
	if o.Scope == nil || len(o.Scope.Windows) != 1 {
		return eh.Errorf("an SVG capture is of one window")
	}
	if o.Scope.Crop != nil {
		return eh.Errorf("an SVG capture is not cropped")
	}
	return nil
}

func (inst scopeSvg) Apply(svg []byte, o Obligation) ([]byte, error) { return svg, nil }
