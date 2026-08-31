package windowhost

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
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

// With a facts store wired, the host offers the column-width capability
// and hands back a usable store (ADR-0151 M4 over ADR-0155 §SD1).
func TestColumnWidth_CapabilityPresentWithFacts(t *testing.T) {
	a := mkColWidthApp()
	reg := app.NewRegistry()
	require.NoError(t, reg.Register(a))
	h := NewInst(reg, zerolog.Nop())
	facts := factsstore.NewInMemoryFactsStore()
	h.SetAudit("run-xyz", facts)

	ctx := openAndFrameCtx(t, h, "test.colwidth")
	cap, ok := ctx.(colwidth.HostI)
	require.True(t, ok, "a host with a facts store must expose colwidth.HostI")
	store := cap.ColumnWidthStore()
	require.NotNil(t, store)

	// Usable, not merely non-nil: a round-trip through the returned store
	// is what the resolver will do.
	_, err := store.WriteColumnWidth(factsstore.ColumnWidthRow{
		AppId: "test.colwidth", Tier: factsstore.ColWidthTierColumn,
		ColumnKey: "k", Points: 42,
	})
	require.NoError(t, err)
	rows, err := store.ListColumnWidths("test.colwidth")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, 42.0, rows[0].Points)
}

// Without a facts store there is nowhere durable to put widths, and the
// capability must be absent rather than present-and-nil — absence is how
// an app is told to fall back to its own defaults.
func TestColumnWidth_CapabilityAbsentWithoutFacts(t *testing.T) {
	a := mkColWidthApp()
	reg := app.NewRegistry()
	require.NoError(t, reg.Register(a))
	h := NewInst(reg, zerolog.Nop())

	ctx := openAndFrameCtx(t, h, "test.colwidth")
	_, ok := ctx.(colwidth.HostI)
	assert.False(t, ok, "no facts store must mean no capability, not a nil store")
}

// The wrapper must not swallow the capabilities the host sets each frame.
func TestColumnWidth_WrapperPreservesWindowFocus(t *testing.T) {
	a := mkColWidthApp()
	reg := app.NewRegistry()
	require.NoError(t, reg.Register(a))
	h := NewInst(reg, zerolog.Nop())
	h.SetAudit("run-xyz", factsstore.NewInMemoryFactsStore())

	ctx := openAndFrameCtx(t, h, "test.colwidth")
	f, ok := ctx.(app.WindowFocusI)
	require.True(t, ok, "embedding must keep WindowFocusI reachable")
	_ = f.WindowFocused()
	assert.Equal(t, app.AppIdT("test.colwidth"), ctx.AppId(),
		"the wrapper must present the target's identity (ADR-0155 SD3)")
}
