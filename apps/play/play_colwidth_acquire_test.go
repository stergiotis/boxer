package play

import (
	"reflect"
	"testing"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/colwidth"
)

// frameCtxWithStore is a frame context carrying the ADR-0151 capability, the
// shape windowhost wraps a real window's context in. Declared here rather than
// imported so this package's test does not depend on the host.
type frameCtxWithStore struct {
	*app.StaticFrameContext
	store colwidth.StoreI
}

func (inst *frameCtxWithStore) ColumnWidthStore() (store colwidth.StoreI) {
	store = inst.store
	return
}

func plainFrameCtx(id app.AppIdT) (ctx *app.StaticFrameContext) {
	mc := app.NewStaticMountContext(id, zerolog.Nop(), nil, nil, nil)
	ctx = app.NewStaticFrameContext(mc, nil)
	return
}

func storeFrameCtx(id app.AppIdT) (ctx *frameCtxWithStore) {
	ctx = &frameCtxWithStore{
		StaticFrameContext: plainFrameCtx(id),
		store:              factsstore.NewInMemoryFactsStore(),
	}
	return
}

// A host that offers the capability gets a resolver. This is the whole of what
// the engine needs from the frame context to persist widths, and it was
// reachable only from the launcher until the acquisition moved onto PlayApp
// itself (ADR-0151 Update 2026-08-16) — every other host of the engine held a
// context and rendered without it.
func TestEnsureColWidthResAcquiresFromCapability(t *testing.T) {
	inst := &PlayApp{}
	inst.ensureColWidthRes(storeFrameCtx("test/app"))
	if inst.colWidthRes == nil {
		t.Fatal("a context carrying colwidth.HostI must yield a resolver")
	}
}

// Absence is not an error (colwidth.HostI's contract): a host with no facts
// store leaves widths on the call site's defaults and every affordance keeps
// working. A panic or an error here would push the check to every re-host.
func TestEnsureColWidthResWithoutCapabilityIsNotAnError(t *testing.T) {
	inst := &PlayApp{}
	inst.ensureColWidthRes(plainFrameCtx("test/app"))
	if inst.colWidthRes != nil {
		t.Fatal("a context without the capability must not yield a resolver")
	}
}

// Acquisition is once-only, so the per-frame call is free after the first
// frame. The latch is deliberately independent of the outcome: a host that
// did not offer the capability on frame one is not asked again.
func TestEnsureColWidthResAcquiresOnce(t *testing.T) {
	inst := &PlayApp{}
	inst.ensureColWidthRes(plainFrameCtx("test/app"))
	inst.ensureColWidthRes(storeFrameCtx("test/app"))
	if inst.colWidthRes != nil {
		t.Fatal("acquisition must latch on the first frame, not retry")
	}

	other := &PlayApp{}
	first := storeFrameCtx("test/app")
	other.ensureColWidthRes(first)
	acquired := other.colWidthRes
	other.ensureColWidthRes(storeFrameCtx("test/app"))
	if other.colWidthRes != acquired {
		t.Fatal("a second frame must not replace the acquired resolver")
	}
}

// The render pass stays unexported, and that is the mechanism rather than a
// style choice: while it was exported, all three re-hosts of the engine called
// it directly and silently lost both per-frame capabilities — the column-width
// store and the window-focus gate. Frame being the only exported way in is
// what makes the next capability added there reach every host for free.
func TestFrameIsTheOnlyExportedRenderEntryPoint(t *testing.T) {
	ty := reflect.TypeFor[*PlayApp]()
	if _, ok := ty.MethodByName("Frame"); !ok {
		t.Fatal("PlayApp.Frame must stay exported: it is how a re-host renders")
	}
	if _, ok := ty.MethodByName("Render"); ok {
		t.Fatal("the render pass must stay unexported, or a re-host can route around the frame context")
	}
}
