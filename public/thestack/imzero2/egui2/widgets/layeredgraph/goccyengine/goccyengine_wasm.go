//go:build js || wasip1

package goccyengine

import (
	"context"
	"errors"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
)

// ErrUnavailable is what every constructor returns under wasm: the engine
// embeds Graphviz through a wasm runtime of its own, which has no place
// inside a wasm module (ADR-0077 SD8). Callers already treat a failed
// Shared() as "no layered layout" and render without one.
var ErrUnavailable = errors.New("goccyengine: graphviz layout is unavailable under wasm")

// Engine is the same seam as natively; no instance is ever handed out.
type Engine struct{}

var _ layeredgraph.Engine = (*Engine)(nil)

// New fails with ErrUnavailable.
func New(_ context.Context) (*Engine, error) { return nil, ErrUnavailable }

// Shared fails with ErrUnavailable.
func Shared() (*Engine, error) { return nil, ErrUnavailable }

// Close is a no-op on an engine that was never built.
func (e *Engine) Close() error { return nil }

// Layout fails with ErrUnavailable.
func (e *Engine) Layout(_ context.Context, _ layeredgraph.GraphModel, _ layeredgraph.LayoutOpts) (*layeredgraph.Layout, error) {
	return nil, ErrUnavailable
}
