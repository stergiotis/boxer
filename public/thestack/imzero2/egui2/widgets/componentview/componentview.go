// Package componentview is an immediate-mode widget (ADR-0267), the typed
// per-component complement to the generic leewaywidgets.RecordCard
// (ADR-0075). Where RecordCard renders any leeway table structurally,
// this renders *recognised* components with bespoke widgets: each registered
// RendererI is an ECS "system that draws", matched to entities that carry its
// component. Render lays the detected components out as a collapsible
// single-record report — one foldable panel per component, the archetype made
// visible — and routes anything unrecognised to a generic fallback.
//
// Detection and typed decode live in this package too, as Binder (see
// componentview_detect.go): Bind ties a component kind to the leeway DTO that
// reads it, and Binder.Components decodes a row into the components it carries
// by asking each bound kind in turn. That is ADR-0075's detect-then-render,
// finally built on ADR-0146's read contract.
//
// Rendering does not depend on it — Render takes decoded Component values
// from anywhere, so a caller with its own decode path is unaffected. Earlier revisions of this doc claimed the package was free of
// leeway-codec dependencies; it no longer is, because the detection half had to
// live somewhere and the renderer↔component mapping is what this package owns.
package componentview

import (
	"errors"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// ComponentKindE names a recognised leeway component — a section or
// section-bundle treated as one logical thing. The seed kinds are the ecsdemo
// drone components; fact-components register their own.
type ComponentKindE string

const (
	KindIdentity ComponentKindE = "identity"
	KindBattery  ComponentKindE = "battery"
	KindTasked   ComponentKindE = "tasked"
)

// Component is one decoded component on an entity: its kind plus the typed value
// the kind's renderer understands (type-asserted by that renderer). A nil Value
// renders as present-but-empty.
type Component struct {
	Kind  ComponentKindE
	Value any
}

// IdentityVal, BatteryVal and TaskedVal are the decoded carriers the seed
// renderers expect. Tasked is tags-only for now — its time window (timeRange)
// is deferred together with the timeline widget (stage-2 defers timeRange).
type (
	IdentityVal struct{ Status string }
	BatteryVal  struct{ Charge uint64 }
	TaskedVal   struct{ Tags []string }
)

// ComponentInput is what a RendererI draws from: the decoded value, and the
// id space the report opened for its kind — every child id is relative under
// it, so a renderer prepares plain literals (`in.Ids.PrepareStr("dial")`)
// and opens no scope of its own.
type ComponentInput struct {
	// Ids is the report's widget id stack, already scoped to this component's
	// kind; ScopeKey names that scope (the kind).
	Ids      *c.WidgetIdStack
	ScopeKey string
	// Value is the component's decoded carrier, type-asserted by the renderer.
	Value any
}

// RendererI draws one component kind from its decoded value. Implementations
// type-assert ComponentInput.Value to their own carrier.
type RendererI interface {
	Kind() ComponentKindE
	Title() string
	Render(in ComponentInput)
}

// Registry holds the per-kind renderers in a stable registration order, which
// is also the report's rendering order.
type Registry struct {
	byKind map[ComponentKindE]RendererI
	order  []ComponentKindE
}

func NewRegistry() (inst *Registry) {
	return &Registry{byKind: make(map[ComponentKindE]RendererI, 8)}
}

// Register adds (or replaces) the renderer for its kind. First registration of a
// kind fixes its slot in the rendering order.
func (inst *Registry) Register(rend RendererI) {
	kind := rend.Kind()
	if _, ok := inst.byKind[kind]; !ok {
		inst.order = append(inst.order, kind)
	}
	inst.byKind[kind] = rend
}

func (inst *Registry) get(kind ComponentKindE) (rend RendererI, ok bool) {
	rend, ok = inst.byKind[kind]
	return
}

// Input is one entity's report.
type Input struct {
	// Ids is the host's widget id stack. Render opens its own IdScope under
	// it, so two reports in one frame need only differ in ScopeKey.
	Ids *c.WidgetIdStack
	// ScopeKey names this report within the host's id space; empty uses
	// "componentview".
	ScopeKey string
	// Registry supplies the renderers, in their registration order. Required.
	Registry *Registry
	// Components are the entity's decoded components.
	Components []Component
	// ShowAbsent renders registered-but-absent components as dimmed lines, so
	// the archetype is legible at a glance.
	ShowAbsent bool
	// DefaultOpen sets the initial expanded state of each component panel.
	DefaultOpen bool
	// Fallback renders a present component that no renderer claims — a
	// consumer wires this to the generic RecordCard. It is host-drawn
	// content in a slot the report positions; nil renders a short note.
	Fallback func(in ComponentInput, comp Component)
}

// Result is what one Render reports.
type Result struct {
	// Present is how many registered components the entity carried and
	// Unclaimed how many of its components no renderer claimed.
	Present, Unclaimed int
	// Err is set when Input.Registry is nil; the report draws the message in
	// place.
	Err error
}

// Render draws the report for one entity's decoded components: a foldable
// panel per registered component present on the entity, optionally a dimmed
// line per registered-but-absent component, and a generic fallback panel for
// any present component no renderer claims. Collapse state is keyed by
// component kind, so it survives clicking through records.
func Render(in Input) (res Result) {
	if in.Ids == nil {
		return
	}
	scopeKey := in.ScopeKey
	if scopeKey == "" {
		scopeKey = "componentview"
	}
	for range c.IdScope(in.Ids.PrepareStr(scopeKey)) {
		res = in.render()
	}
	return
}

func (in Input) render() (res Result) {
	if in.Registry == nil {
		res.Err = errors.New("componentview: Input.Registry is nil")
		for rt := range c.RichTextLabel(res.Err.Error()) {
			rt.Small().Weak()
		}
		return
	}
	ids := in.Ids
	present := make(map[ComponentKindE]any, len(in.Components))
	for _, comp := range in.Components {
		present[comp.Kind] = comp.Value
	}
	for range c.Vertical().KeepIter() {
		for _, kind := range in.Registry.order {
			rend := in.Registry.byKind[kind]
			val, isPresent := present[kind]
			for range c.IdScope(ids.PrepareStr(string(kind))) {
				switch {
				case isPresent:
					res.Present++
					for range c.CollapsingHeader(ids.PrepareStr("h"), c.WidgetText().Text(rend.Title()).Keep()).DefaultOpen(in.DefaultOpen).KeepIter() {
						rend.Render(ComponentInput{Ids: ids, ScopeKey: string(kind), Value: val})
					}
				case in.ShowAbsent:
					for rt := range c.RichTextLabel("▷ " + rend.Title() + "  ·  absent") {
						rt.Weak().Italics().Small()
					}
				}
			}
		}
		for range c.IdScope(ids.PrepareStr("unclaimed")) {
			for _, comp := range in.Components {
				if _, ok := in.Registry.get(comp.Kind); ok {
					continue
				}
				res.Unclaimed++
				for range c.IdScope(ids.PrepareStr(string(comp.Kind))) {
					for range c.CollapsingHeader(ids.PrepareStr("h"), c.WidgetText().Text(string(comp.Kind)+"  ·  generic").Keep()).DefaultOpen(in.DefaultOpen).KeepIter() {
						if in.Fallback != nil {
							in.Fallback(ComponentInput{Ids: ids, ScopeKey: string(comp.Kind), Value: comp.Value}, comp)
						} else {
							for rt := range c.RichTextLabel("rendered by the generic record card") {
								rt.Weak().Small()
							}
						}
					}
				}
			}
		}
	}
	return
}
