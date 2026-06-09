// Package componentview is the typed per-component complement to the generic
// leewaywidgets.Table2CardEmitter (ADR-0075). Where Table2CardEmitter renders
// any leeway table structurally, this renders *recognised* components with
// bespoke widgets: each registered RendererI is an ECS "system that draws",
// matched to entities that carry its component. A Dispatcher lays the detected
// components out as a collapsible single-record report — one foldable panel per
// component, the archetype made visible — and routes anything unrecognised to a
// generic fallback.
//
// Detection and typed decode happen upstream (leeway RA population counts +
// marshallreflect.Unmarshal); this package consumes the already-decoded
// Component values, so it stays free of leeway-codec dependencies.
package componentview

import (
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

// RendererI draws one component kind from its decoded value. Implementations
// type-assert value to their own carrier.
type RendererI interface {
	Kind() ComponentKindE
	Title() string
	Render(ids *c.WidgetIdStack, value any)
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

// Dispatcher renders one entity's components as a collapsible single-record
// report: a foldable panel per registered component present on the entity,
// optionally a dimmed line per registered-but-absent component (so the
// archetype is legible at a glance), and a generic fallback panel for any
// present component no renderer claims.
type Dispatcher struct {
	reg *Registry
	// ShowAbsent renders registered-but-absent components as dimmed lines.
	ShowAbsent bool
	// DefaultOpen sets the initial expanded state of each component panel.
	DefaultOpen bool
	// Fallback renders a present component that no renderer claims — a consumer
	// wires this to the generic Table2CardEmitter. Nil renders a short note.
	Fallback func(ids *c.WidgetIdStack, comp Component)
}

func NewDispatcher(reg *Registry) (inst *Dispatcher) {
	return &Dispatcher{reg: reg}
}

// RenderReport draws the report for one entity's decoded components. Collapse
// state is keyed by component kind, so it survives clicking through records.
func (inst *Dispatcher) RenderReport(ids *c.WidgetIdStack, comps []Component) {
	present := make(map[ComponentKindE]any, len(comps))
	for _, comp := range comps {
		present[comp.Kind] = comp.Value
	}
	for range c.Vertical().KeepIter() {
		for _, kind := range inst.reg.order {
			rend := inst.reg.byKind[kind]
			val, isPresent := present[kind]
			for range c.IdScope(ids.PrepareStr(string(kind))) {
				switch {
				case isPresent:
					for range c.CollapsingHeader(ids.PrepareStr("h"), c.WidgetText().Text(rend.Title()).Keep()).DefaultOpen(inst.DefaultOpen).KeepIter() {
						rend.Render(ids, val)
					}
				case inst.ShowAbsent:
					for rt := range c.RichTextLabel("▷ " + rend.Title() + "  ·  absent") {
						rt.Weak().Italics().Small()
					}
				}
			}
		}
		for _, comp := range comps {
			if _, ok := inst.reg.get(comp.Kind); ok {
				continue
			}
			for range c.IdScope(ids.PrepareStr("unclaimed-" + string(comp.Kind))) {
				for range c.CollapsingHeader(ids.PrepareStr("h"), c.WidgetText().Text(string(comp.Kind)+"  ·  generic").Keep()).DefaultOpen(inst.DefaultOpen).KeepIter() {
					if inst.Fallback != nil {
						inst.Fallback(ids, comp)
					} else {
						for rt := range c.RichTextLabel("rendered by the generic Table2CardEmitter fallback") {
							rt.Weak().Small()
						}
					}
				}
			}
		}
	}
}
