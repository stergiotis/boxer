// Package capinspector renders the capability detail / schematic that
// the carousel's status-bar segments open on click. One App per
// window; the carousel pushes a selectedCap onto an internal queue
// before opening so the next-allocated App captures the right
// initial selection.
//
// Phase 1 (this package): static descriptions per cap + a layered
// (Sugiyama) schematic of App → capability → backend, laid out by
// Graphviz dot in-process and painted through the layeredgraph widget
// (ADR-0069, see archgraph.go). The layout is cached (the cap registry
// is closed); only the per-frame colours — selected cap, effective
// backend, degraded cap — vary. Live audit activity renders as a
// companion sparkline strip beneath the schematic.
package capinspector

import (
	"embed"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

// capDocsFS holds the per-cap explanation files. One markdown file
// per CapId (caps/<id>.md). Parsed once at init and rendered
// in-place every frame.
//
//go:embed caps/*.md
var capDocsFS embed.FS

// capDocs is the parse-once / render-many cache of the per-cap
// markdown explanations. Populated at init from capDocsFS; a
// missing entry means the inspector falls back to spec.Description
// for that cap.
var capDocs = map[CapId]*markdown.Doc{}

func init() {
	for _, capId := range allCapIdsOrdered() {
		path := "caps/" + string(capId) + ".md"
		body, err := capDocsFS.ReadFile(path)
		if err != nil {
			log.Warn().Err(err).Str("path", path).Msg("capinspector: cap doc missing")
			continue
		}
		capDocs[capId] = markdown.Parse(body)
	}
}

// ids is the package-level WidgetIdStack. Frame wraps its body in
// IdScope(seed) so widget ids are disjoint across multiple inspector
// windows.
var ids = c.NewWidgetIdStack()

// instanceCounter feeds per-instance seeds. Stamps each newApp() with
// a unique uint64.
var instanceCounter atomic.Uint64

// pendingMu guards pendingSelections — the FIFO queue of cap ids the
// status-bar clicks push into before opening an inspector. Each
// newApp() pops the head so the window opened by click N gets the
// selection set at click N.
var (
	pendingMu         sync.Mutex
	pendingSelections []CapId
)

// PushSelection enqueues a capId for the next newApp() to consume.
// The carousel calls this immediately before host.Open(ManifestId) so
// the inspector window opens already pointing at the right cap. The
// queue is FIFO so two rapid clicks open two windows in the click
// order.
func PushSelection(capId CapId) {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	pendingSelections = append(pendingSelections, capId)
}

// popSelection returns the head of the queue, or "" when empty. An
// empty pop is the "user opened the inspector from the Apps menu
// without a prior status-bar click" case — Frame renders a cap
// picker instead of a detail page.
func popSelection() (capId CapId) {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	if len(pendingSelections) == 0 {
		return
	}
	capId = pendingSelections[0]
	pendingSelections = pendingSelections[1:]
	return
}

// App is the per-window inspector instance. selectedCap stays
// mutable so the in-window picker can switch caps without opening
// another window.
type App struct {
	seed        uint64
	selectedCap CapId
	// density resolves IDS spacing tokens at the active preset
	// (ADR-0032 §SD2); cached once at newApp.
	density styletokens.DensityE
}

var _ app.AppI = (*App)(nil)

func newApp() (inst *App) {
	inst = &App{
		seed:        instanceCounter.Add(1),
		selectedCap: popSelection(),
		density:     styletokens.DensityFromEnv(),
	}
	return
}

func (inst *App) Manifest() (m app.Manifest)                { m = manifest; return }
func (inst *App) Mount(ctx app.MountContextI) (err error)   { return }
func (inst *App) Unmount(ctx app.MountContextI) (err error) { return }

func (inst *App) Frame(ctx app.FrameContextI) (err error) {
	ids.Reset()
	for range c.IdScope(ids.PrepareSeq(inst.seed)) {
		inst.renderBody()
	}
	return
}

func (inst *App) renderBody() {
	for range c.PanelTopInside(ids.PrepareStr("hdr")).Resizable(false).KeepIter() {
		inst.renderPicker()
	}
	for range c.PanelCentralInside().KeepIter() {
		spec, ok := Registry[inst.selectedCap]
		if !ok {
			c.Label("Pick a capability above.").Send()
			return
		}
		inst.renderDetail(spec)
	}
}

// renderPicker draws a row of selectable buttons across the top of
// the inspector body. Clicking one swaps selectedCap without opening
// another window; the schematic and prose below update on the same
// frame.
func (inst *App) renderPicker() {
	for range c.Horizontal().KeepIter() {
		for _, capId := range allCapIdsOrdered() {
			spec := Registry[capId]
			active := capId == inst.selectedCap
			if c.SelectableLabel(ids.PrepareStr("pick-"+string(capId)), active, spec.Display).
				SendResp().HasPrimaryClicked() {
				inst.selectedCap = capId
			}
		}
	}
}

func (inst *App) renderDetail(spec CapSpec) {
	// AutoShrink(false, false) is load-bearing — with default
	// horizontal auto-shrink the ScrollArea fits its width to its
	// widest child, which is usually our PaintCanvas. The canvas
	// then captures avail.W = its own previous width, paints at
	// avail.W - chromeW, the ScrollArea shrinks again, repeat until
	// the canvas is stuck small. Pinning the ScrollArea to its
	// parent's full width breaks that feedback loop so the canvas
	// always reads the panel's actual available width.
	for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
		heading(spec.Display)
		// SubjectFamily and Backend are syntactic strings (NATS pattern
		// + Go import path); render the value half in monospace so the
		// dots / braces / slashes don't get eaten by the proportional
		// font's kerning.
		c.LabelAtoms(c.Atoms().
			BeginRichText("Subject family: ").End().
			BeginRichText(spec.SubjectFamily).Monospace().End().
			Keep()).Send()
		c.LabelAtoms(c.Atoms().
			BeginRichText("Backend: ").End().
			BeginRichText(spec.Backend).Monospace().End().
			Keep()).Send()
		c.AddSpace(styletokens.PaddingOuter(inst.density))
		heading("Wiring")
		inst.renderGraph(spec)
		c.AddSpace(styletokens.GapItems(inst.density))
		inst.renderLiveConsumers(spec)
		c.AddSpace(styletokens.PaddingOuter(inst.density))
		c.Separator().Horizontal().Send()
		c.AddSpace(styletokens.GapItems(inst.density))
		inst.renderCapDoc(spec)
	}
}

// renderCapDoc draws the per-cap markdown explanation embedded in
// caps/<id>.md. Falls back to the inline CapSpec.Description when no
// file is registered for that cap. The doc is parse-once / render-
// many — `capDocs` caches the parsed shape.
func (inst *App) renderCapDoc(spec CapSpec) {
	doc, ok := capDocs[spec.Id]
	if !ok {
		c.Label(spec.Description).Send()
		return
	}
	for range c.IdScope(ids.PrepareStr("doc-" + string(spec.Id))) {
		doc.Render(ids)
	}
}

// diagramCapLabel returns the short label the painter writes inside a
// cap box. Different from spec.Display (which is the longer prose
// label rendered above the diagram) because the box has ~140px of
// usable width and Display strings overflow.
func diagramCapLabel(capId CapId) (s string) {
	switch capId {
	case CapRun:
		s = "Run identity"
	case CapFacts:
		s = "Audit + state"
	case CapBus:
		s = "Subject router"
	case CapFs:
		s = "fs.* Powerbox"
	case CapPersist:
		s = "Persist state"
	case CapTask:
		s = "Background task"
	}
	return
}

// renderLiveConsumers is a one-line footnote naming the apps in the
// current registry that exercise this cap. Kept compact on purpose
// — the inspector is about the cap-broker contract; the live
// consumer list is informational context, not the focus. Apps with
// no per-app filter (run, facts) get an explanatory line instead.
func (inst *App) renderLiveConsumers(spec CapSpec) {
	if spec.AppFilter == nil {
		c.Label("This capability is a runtime-level service; every app inherits it implicitly.").Send()
		return
	}
	apps := matchedApps(spec)
	if len(apps) == 0 {
		c.Label("Live consumers: (none — no registered app declares this cap yet).").Send()
		return
	}
	names := make([]string, 0, len(apps))
	for _, m := range apps {
		names = append(names, shortAppName(m))
	}
	c.Label("Live consumers in this registry: " + joinCommaSpace(names)).Send()
}

// joinCommaSpace joins a slice with ", " — strings.Join would do this
// too but keeping it inline saves a stdlib import for a one-call use.
func joinCommaSpace(parts []string) (s string) {
	for i, p := range parts {
		if i > 0 {
			s += ", "
		}
		s += p
	}
	return
}

// matchedApps returns every manifest in app.DefaultRegistry whose
// Caps include at least one filter matching the cap, or whose
// HostInjected pattern would fire. Sorted by AppId so the render
// order is stable across frames.
func matchedApps(spec CapSpec) (out []app.Manifest) {
	all := app.DefaultRegistry.AllManifests()
	for _, m := range all {
		hit := false
		if spec.AppFilter != nil {
			for _, f := range m.Caps {
				if spec.AppFilter(f) {
					hit = true
					break
				}
			}
		}
		if !hit && spec.HostInjected != nil {
			if spec.HostInjected(m) != "" {
				hit = true
			}
		}
		if hit {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Id < out[j].Id })
	return
}

// shortAppName returns the last "/"-separated segment of an AppId so
// node labels stay readable. "github.com/.../play" → "play".
func shortAppName(m app.Manifest) (s string) {
	id := string(m.Id)
	if i := lastSlash(id); i >= 0 {
		s = id[i+1:]
		return
	}
	s = id
	return
}

// heading emits a heading-styled label without depending on a
// top-level c.Heading factory — the bindings expose Heading() on
// RichTextScope but not as a widget shortcut, so this wrapper
// keeps the renderer code readable.
func heading(text string) {
	c.LabelAtoms(c.Atoms().BeginRichText(text).Heading().End().Keep()).Send()
}

func lastSlash(s string) (idx int) {
	idx = -1
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			idx = i
		}
	}
	return
}
