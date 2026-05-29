//go:build llm_generated_opus47

// Package registry is the single source of truth for the ImZero2 demo
// catalog. Each demo file calls Register in its init() with a Demo value
// carrying Name, Category, Render closure, and per-demo flags. Three hosts
// consume the registry independently: InteractiveDriver (human shell),
// TestDriver (deterministic screenshot capture), and Embed (drop a single
// demo into any host Ui scope — profiler, debug shell, etc.). See ADR-0008
// for design rationale and SKILLS.md §14 for screenshot infrastructure.
package registry

import (
	"reflect"
	"runtime"
	"sort"
	"sync"

	"github.com/rs/zerolog/log"
	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// WidgetsPkgPath is the Go import path of the demos' enclosing package. Kept
// for the capslock-check tool's package-mapping table and any future
// per-demo provenance work. Pre-C3 it was also used as the prefix for
// dual-registered DemoApp Ids in app.DefaultRegistry — that registration
// path is gone (M3 C3, 2026-05-12); demos are now reached only via the
// widgets Demo-gallery AppI.
const WidgetsPkgPath = "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/widgets"

// Stage-size budget for tour captures. Standard demos must fit within
// [StandardStageMaxW]×[StandardStageMaxH]; demos that genuinely need
// more vertical room opt into [DemoFlagNeedsLargeArea] which bumps the
// height ceiling to [LargeAreaStageMaxH]. Width is always capped at
// [StandardStageMaxW] — Wayland compositors often refuse to enlarge a
// window past the screen edge or default tile, so a width budget keeps
// captures comparable across desktops. The TestDriver clamps to these
// values at capture time; Register logs a warning so authors notice the
// drift. [imzero2env.ScreenshotSize] (IMZERO2_SCREENSHOT_SIZE=WxH) is
// an explicit user override that bypasses the budget.
const (
	StandardStageMaxW  float32 = 1200
	StandardStageMaxH  float32 = 600
	LargeAreaStageMaxH float32 = 1000
)

// DemoFlagsE is a bitmask of per-demo hints consumed by drivers.
type DemoFlagsE uint32

const (
	DemoFlagNone DemoFlagsE = 0
	// DemoFlagNeedsLargeArea opts a demo out of the standard
	// [StandardStageMaxH] height budget and into [LargeAreaStageMaxH]
	// (currently 1000 px). Width budget stays at [StandardStageMaxW]
	// regardless of this flag. Reserve for demos whose content (force
	// graph, treemap, walkers map, code views, multi-panel layouts)
	// truly needs the extra vertical room — most demos should fit
	// inside the standard budget.
	DemoFlagNeedsLargeArea DemoFlagsE = 1 << iota
	DemoFlagSkipInTour
	DemoFlagNeedsNetwork
	// DemoFlagNonDeterministic marks demos that render time- or
	// counter-dependent content (current time-of-day, frame counters,
	// live system metrics, randomised initial layouts, etc.). Their tour
	// captures drift byte-for-byte across runs even when nothing visual
	// has changed — clean diffs require freezing the underlying state.
	// The TestDriver skips these when IMZERO2_SCREENSHOT_DETERMINISTIC=1
	// is set so the tour run produces byte-stable output for CI / review
	// purposes; default mode includes them so reviewers still see them.
	DemoFlagNonDeterministic
)

// DemoKindE classifies whether a demo's value is primarily visual
// (UX showcase — what ImZero2 can render) or educational (DX example
// — how an API is used; the rendered output is hard to interpret without
// the calling code). Mixed when both apply.
type DemoKindE uint8

const (
	DemoKindUnspecified DemoKindE = 0
	DemoKindUX          DemoKindE = 1
	DemoKindDX          DemoKindE = 2
	DemoKindMixed       DemoKindE = 3
)

// String returns the human-readable label rendered in the per-demo intro
// chip. Empty for DemoKindUnspecified so the chip is suppressed.
func (inst DemoKindE) String() (s string) {
	switch inst {
	case DemoKindUX:
		s = "UX showcase"
	case DemoKindDX:
		s = "DX example"
	case DemoKindMixed:
		s = "mixed UX/DX"
	}
	return
}

// Demo is one catalog entry. Render (or RenderStateful, when Init is set)
// draws into the current Ui scope — no outer Window, no outer Frame, no
// scope chrome. The consuming driver owns wrapping.
//
// Description is a 1–2 sentence "what this demonstrates" rendered by drivers
// above the demo body. SourceFile/SourceLine are auto-resolved at Register
// from the Render closure (or set explicitly when Render delegates to a
// named function and the link should point there instead). Both feed the
// per-demo "view source on GitHub" link.
//
// State model: stateless demos set Render and leave Init/RenderStateful
// nil — their state lives in package-level vars shared across every
// gallery window. Stateful demos set Init + RenderStateful (Render must
// be nil). The driver calls Init once per app instance (at App.Mount /
// tour setup) and hands the returned value back to RenderStateful every
// frame, so widget singletons (filepicker.New, treemap.New, …) bind to
// the per-instance WidgetIdStack the host supplies via MountCtx.Ids()
// instead of a process-shared stack. Exactly one of (Render) or
// (Init+RenderStateful) must be set; Register drops Demos that violate
// this.
type Demo struct {
	Name     string
	Category string
	Title    string
	Stage    [2]float32
	Flags    DemoFlagsE
	Kind     DemoKindE
	Render   func(ids *c.WidgetIdStack)
	// Init builds the per-app-instance state struct for a stateful demo.
	// Called once per AppI Mount with the host-supplied WidgetIdStack —
	// pre-built widgets the state struct holds (e.g. filepicker.Inst,
	// treemap.Treemap, parsed markdown.Doc) bind to that stack so widget
	// ids cannot collide across open gallery windows. nil for stateless
	// demos (see Render). Demos that need the host BusI to mediate
	// capability publishes (chlocalbroker, fsbroker, …) set BusInit
	// instead — the gallery host invokes whichever is non-nil.
	Init func(ids *c.WidgetIdStack) (state any)
	// BusInit is the bus-aware variant of Init. The gallery host
	// captures BusI from MountContextI at Mount time and forwards it to
	// any demo that opts in here. Exactly one of Init or BusInit may
	// be set; setting both is a Register-time error.
	BusInit func(ids *c.WidgetIdStack, bus runtimeapp.BusI) (state any)
	// RenderStateful draws a stateful demo each frame. state is the
	// pointer Init returned for this Demo + this app instance; the demo
	// cast it back to its private state struct. nil for stateless demos.
	RenderStateful func(ids *c.WidgetIdStack, state any)
	Description    string
	// SourceFunc, when non-nil, is the function whose source location feeds
	// SourceFile/SourceLine. Use this when Render is a thin wrapper closure
	// that delegates to a named demo function — set SourceFunc to that named
	// function so the GitHub link and embedded source panel point at the
	// interesting body, not the wrapper. Must be a function value (any
	// signature) — non-funcs are ignored.
	SourceFunc any
	SourceFile string
	SourceLine int
}

var (
	registryMu sync.Mutex
	registered []Demo
	byName     map[string]int
)

// Register adds a Demo to the catalog. Idempotent on Name — a second
// registration under the same Name is logged and dropped (can happen if a
// demo package is imported transitively from multiple sites). Intended to
// be called from init() in each demo file.
func Register(d Demo) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if d.Name == "" {
		log.Warn().Msg("registry.Register: empty Name, dropping")
		return
	}
	if d.Init != nil && d.BusInit != nil {
		log.Warn().Str("name", d.Name).
			Msg("registry.Register: must set at most one of Init or BusInit, dropping")
		return
	}
	hasStateless := d.Render != nil
	hasStateful := (d.Init != nil || d.BusInit != nil) && d.RenderStateful != nil
	if hasStateless == hasStateful {
		log.Warn().Str("name", d.Name).
			Bool("hasRender", hasStateless).
			Bool("hasInit", d.Init != nil).
			Bool("hasBusInit", d.BusInit != nil).
			Bool("hasRenderStateful", d.RenderStateful != nil).
			Msg("registry.Register: must set exactly one of Render or ((Init|BusInit)+RenderStateful), dropping")
		return
	}
	if byName == nil {
		byName = make(map[string]int, 32)
	}
	if _, exists := byName[d.Name]; exists {
		log.Warn().Str("name", d.Name).Msg("registry.Register: duplicate Name, keeping first")
		return
	}
	maxH := StandardStageMaxH
	if d.Flags&DemoFlagNeedsLargeArea != 0 {
		maxH = LargeAreaStageMaxH
	}
	if d.Stage[0] > StandardStageMaxW {
		log.Warn().Str("name", d.Name).
			Float32("width", d.Stage[0]).
			Float32("budget", StandardStageMaxW).
			Msg("registry.Register: Stage width exceeds budget; TestDriver will clamp the capture rect")
	}
	if d.Stage[1] > maxH {
		log.Warn().Str("name", d.Name).
			Float32("height", d.Stage[1]).
			Float32("budget", maxH).
			Bool("needsLargeArea", d.Flags&DemoFlagNeedsLargeArea != 0).
			Msg("registry.Register: Stage height exceeds budget; TestDriver will clamp the capture rect")
	}
	if d.SourceFile == "" {
		var target reflect.Value
		if d.SourceFunc != nil {
			target = reflect.ValueOf(d.SourceFunc)
		}
		if !target.IsValid() || target.Kind() != reflect.Func {
			if d.Render != nil {
				target = reflect.ValueOf(d.Render)
			} else {
				target = reflect.ValueOf(d.RenderStateful)
			}
		}
		if target.IsValid() && target.Kind() == reflect.Func {
			if fn := runtime.FuncForPC(target.Pointer()); fn != nil {
				d.SourceFile, d.SourceLine = fn.FileLine(fn.Entry())
			}
		}
	}
	idx := sort.Search(len(registered), func(i int) bool { return registered[i].Name >= d.Name })
	registered = append(registered, Demo{})
	copy(registered[idx+1:], registered[idx:])
	registered[idx] = d
	for i := idx; i < len(registered); i++ {
		byName[registered[i].Name] = i
	}
	// ADR-0026 M3 C3 (2026-05-12): Demos are no longer dual-registered
	// into app.DefaultRegistry. They are now owned exclusively by the
	// widgets "Widget gallery" AppI, which renders them inline via Embed
	// inside its tile body. The DockHost's Apps menu lists only the
	// six top-level apps (including the gallery).
}

// All returns the registered demos sorted by Name. The returned slice
// aliases internal storage; callers must not mutate. Stable iteration order
// is the driver's contract for filename-generation and for nav UIs.
func All() (demos []Demo) {
	registryMu.Lock()
	defer registryMu.Unlock()
	demos = registered
	return
}

// ByName looks up a demo by its Name. Used by Embed.
func ByName(name string) (d Demo, ok bool) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if byName == nil {
		return
	}
	idx, exists := byName[name]
	if !exists {
		return
	}
	d = registered[idx]
	ok = true
	return
}

// Embed renders a single demo by Name into the current Ui scope. A profiler
// or debug shell can call this inside any Window/Frame/Panel. Unknown names
// are logged and no-op rather than panicking — the caller typically wires
// the name from config.
//
// state carries the per-app-instance state struct returned by the demo's
// Init at App.Mount, and is forwarded to RenderStateful when the demo
// opted into the stateful path. Stateless demos (Render-only) ignore the
// state argument — callers that don't track state pass nil and the
// legacy Render fires unchanged.
func Embed(ids *c.WidgetIdStack, name string, state any) {
	d, ok := ByName(name)
	if !ok {
		log.Warn().Str("name", name).Msg("registry.Embed: demo not registered")
		return
	}
	if d.RenderStateful != nil {
		d.RenderStateful(ids, state)
		return
	}
	d.Render(ids)
}
