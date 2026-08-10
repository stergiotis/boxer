package capinspector

// capschema.go renders the leeway schema of the table a capability's
// backend persists into — today only boxer.facts, behind the Facts cap
// — with the schemaview inspector widget (ADR-0075): a section
// navigator on the left, and the decoded canonical type / encoding
// hints / value semantics / membership spec of the selected node on the
// right. The rest of the detail page says which backend serves the cap
// and what it promises; this section says what the rows look like once
// they land.
//
// It is the *authored* schema — the mapping LoadRuntimeFactsMapping
// declares — not a live DESCRIBE TABLE. Nothing here connects to
// ClickHouse, so the section reads the same whether the effective
// backend is chstore or the in-memory fallback.

import (
	"strconv"
	"sync"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/schemaview"
)

// loadFactsTableDesc is CapFacts' CapSchema.Load: the boxer.facts
// TableDesc, built at most once per process and shared by every open
// inspector window. Memoised rather than built at init because the
// build runs the leeway describe pipeline (CBOR round-trip through the
// table marshaller) and most sessions never open the schema section —
// the same reason the runtime's own DDL path reads the generated
// factsschema/ddl package instead of building a TableDesc at startup.
// Sharing is safe: schemaview treats the TableDesc as read-only and
// mutates only its per-window Model.
var loadFactsTableDesc = sync.OnceValues(buildFactsTableDesc)

func buildFactsTableDesc() (tbl *common.TableDesc, err error) {
	manip, err := factsschema.GetSchemaInManipulator()
	if err != nil {
		err = eh.Errorf("unable to load the boxer.facts schema: %w", err)
		return
	}
	t, err := manip.BuildTableDesc()
	if err != nil {
		err = eh.Errorf("unable to build the boxer.facts table description: %w", err)
		return
	}
	tbl = &t
	return
}

// schemaScopeSalt namespaces the schemaview instance's scope key, which
// the widget turns into *absolute* ids for its tethered legend window
// and its pane probe (schemaview.legendScope, c.ProbeSeq) — ids that
// bypass the id stack and would therefore be identical in two open
// inspector windows. Folding the host's per-window salt in (the
// archgraph idiom, ADR-0026 §SD9) keeps each window's legend and probe
// its own.
const schemaScopeSalt uint64 = 0xbf58476d1ce4e5b9

// schemaScopePrefix derives the per-window half of the scope key, on
// first use rather than at construction: windowhost pushes its salt as
// an id scope around Frame, so a stack read in Mount is still empty.
// Derive() is non-zero by construction, so the empty string is a safe
// "not yet" sentinel.
func (inst *App) schemaScopePrefix() (prefix string) {
	if inst.schemaScope == "" {
		inst.schemaScope = "capschema-" + strconv.FormatUint(inst.ids.PrepareHighEntropy(schemaScopeSalt).Derive(), 36)
	}
	prefix = inst.schemaScope
	return
}

// renderCapSchema draws the storage-schema section at the foot of the
// detail page. A no-op for caps that declare no table.
func (inst *App) renderCapSchema(spec CapSpec) {
	if spec.Schema == nil {
		return
	}
	c.AddSpace(styletokens.PaddingOuter(inst.density))
	c.Separator().Horizontal().Send()
	c.AddSpace(styletokens.GapItems(inst.density))
	ch := c.CollapsingHeader(inst.ids.PrepareStr("hdr-schema-"+spec.Id),
		c.WidgetText().Text("Storage schema — "+spec.Schema.Qualified()).Keep())
	handle := ch.Handle()
	for range ch.KeepIter() {
		// Collapsed does not mean unemitted — per ADR-0012 the body still
		// runs once, and the inspector's legend is a top-level c.Window,
		// which a CollapsingHeader clips no more than any other floating
		// area. Without the advisory gate a pinned legend keeps floating
		// over a folded section, untethered. It also keeps the 620px-
		// floored dock (and the first TableDesc build) out of frames where
		// nobody asked for the schema.
		if c.IsBlockSkipped(handle) {
			continue
		}
		inst.renderSchemaInspector(spec)
	}
}

// renderSchemaInspector binds the model to this cap's table the first
// time the section renders — and again whenever the picker moves to
// another schema-carrying cap — then renders the inspector.
func (inst *App) renderSchemaInspector(spec CapSpec) {
	if inst.schemaCap != spec.Id {
		tbl, err := spec.Schema.Load()
		inst.schemaCap = spec.Id
		inst.schemaErr = err
		if inst.schemaModel == nil {
			inst.schemaModel = schemaview.NewModel(tbl)
		} else {
			inst.schemaModel.SetTable(tbl)
		}
	}
	if inst.schemaErr != nil {
		// A build failure is a bug in the schema definition, not a
		// runtime condition the operator can act on — say so plainly and
		// keep the rest of the page usable.
		for rt := range c.RichTextLabel("Schema unavailable: " + inst.schemaErr.Error()) {
			rt.Small().Weak()
		}
		return
	}
	// FillHost stays false: the detail body is a vertically-unbounded
	// ScrollArea (the gallery shape, not a dock-tab leaf), so the widget
	// must floor its own dock height instead of filling a rect nobody
	// bounded.
	schemaview.Render(schemaview.Input{
		Ids:      inst.ids,
		ScopeKey: inst.schemaScopePrefix() + "-" + spec.Id,
		Model:    inst.schemaModel,
	})
}
