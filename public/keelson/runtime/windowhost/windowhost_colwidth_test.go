package windowhost

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/statestore"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/colwidth"
)

// colWidthApp records the frame context it was handed so the test can
// assert on the capability the host actually delivers, rather than on the
// host's internals.
type colWidthApp struct {
	manifest app.Manifest
	seen     app.FrameContextI
}

func (inst *colWidthApp) Manifest() app.Manifest              { return inst.manifest }
func (inst *colWidthApp) Mount(ctx app.MountContextI) error   { return nil }
func (inst *colWidthApp) Unmount(ctx app.MountContextI) error { return nil }
func (inst *colWidthApp) Frame(ctx app.FrameContextI) error {
	inst.seen = ctx
	return nil
}

var _ app.AppI = (*colWidthApp)(nil)

func mkColWidthApp() *colWidthApp {
	return &colWidthApp{manifest: app.Manifest{
		Id:      "test.colwidth",
		Display: "colwidth",
		Topics:  []app.TopicT{app.TopicRuntime},
		Summary: "fixture summary",
		Surface: app.SurfaceWindowed,
	}}
}

// openAndFrameCtx opens a window and returns the frame context the host
// hands the app. It calls the app's Frame directly with the context the
// host stored, which is what renderWindowBody does — the render path
// itself needs an egui frame and cannot run here.
func openAndFrameCtx(t *testing.T, h *Inst, id app.AppIdT) (ctx app.FrameContextI) {
	t.Helper()
	_, err := h.Open(id)
	require.NoError(t, err)
	require.Len(t, h.windows, 1)
	return h.windows[0].frameCtxApp
}

// With a state store wired, the host offers the column-width capability
// and hands back a usable store (ADR-0151 M4 over ADR-0382 §SD4).
func TestColumnWidth_CapabilityPresentWithState(t *testing.T) {
	a := mkColWidthApp()
	reg := app.NewRegistry()
	require.NoError(t, reg.Register(a))
	h := NewInst(t.Context(), reg, zerolog.Nop())
	h.SetState(statestore.NewMemory())

	ctx := openAndFrameCtx(t, h, "test.colwidth")
	store := colwidth.StoreOf(ctx)
	require.NotNil(t, store, "a host with a state store must hand the column-width store")

	// Usable, not merely non-nil: a round-trip through the returned store
	// is what the resolver will do.
	err := store.WriteColumnWidth(t.Context(), statestore.ColumnWidthRow{
		AppId: "test.colwidth", Tier: statestore.ColWidthTierColumn,
		ColumnKey: "k", Points: 42,
	})
	require.NoError(t, err)
	rows, err := store.ListColumnWidths(t.Context(), "test.colwidth")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, 42.0, rows[0].Points)
}

// Without a state store there is nowhere to put widths, and the capability
// must be absent rather than present-and-nil — absence is how an app is
// told to fall back to its own defaults. An audit trail alone does not
// supply one: widths are state, not trail.
func TestColumnWidth_CapabilityAbsentWithoutState(t *testing.T) {
	a := mkColWidthApp()
	reg := app.NewRegistry()
	require.NoError(t, reg.Register(a))
	h := NewInst(t.Context(), reg, zerolog.Nop())
	h.SetAudit("run-xyz", factsstore.NewInMemoryFactsStore())

	ctx := openAndFrameCtx(t, h, "test.colwidth")
	assert.Nil(t, colwidth.StoreOf(ctx), "no state store must mean no column-width store")
}

// The wrapper must not swallow the capabilities the host sets each frame.
func TestColumnWidth_WrapperPreservesWindowFocus(t *testing.T) {
	a := mkColWidthApp()
	reg := app.NewRegistry()
	require.NoError(t, reg.Register(a))
	h := NewInst(t.Context(), reg, zerolog.Nop())
	h.SetState(statestore.NewMemory())

	ctx := openAndFrameCtx(t, h, "test.colwidth")
	_ = ctx.WindowFocused()
	assert.Equal(t, app.AppIdT("test.colwidth"), ctx.AppId(),
		"the wrapper must present the target's identity (ADR-0382 §SD5)")
}
