//go:build llm_generated_opus47

package widgets

import (
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/imzero2env"
)

// TestDriver writes PNGs directly to $IMZERO2_SCREENSHOT_DIR — the pre-ADR
// contract. During the C4–C18 migration this was $DIR/new/ so the legacy
// tour and the new driver could coexist; the transient subdir is gone
// with the C19 cutover.

const (
	testStageDefaultW      float32 = 1024
	testStageDefaultH      float32 = 600
	testDriverSettleFrames int     = 8
)

type testPhaseE uint8

const (
	testPhaseSettle  testPhaseE = 0
	testPhaseCapture testPhaseE = 1
	testPhaseAdvance testPhaseE = 2
)

type testDriverStateS struct {
	setupDone bool
	doneAll   bool
	demoIdx   int32
	phase     testPhaseE
	settleCnt int32
	active    []registry.Demo

	// ids is the single-instance tour's WidgetIdStack. Allocated on
	// setup; used for both Init (so pre-built widgets in each demo's
	// state struct bind to this stack) and the per-frame IdScope wrap
	// the seed scopes. Owned by the tour driver because there's no
	// host App that would supply one via MountCtx.Ids().
	ids *c.WidgetIdStack
	// demoState mirrors App.demoState for the single-instance tour:
	// keyed on Demo.Name, holds the state struct each stateful demo's
	// Init returned. Initialised at the same time as active (tour
	// setup), so RenderStateful sees a consistent state pointer for
	// the lifetime of the tour.
	demoState map[string]any
}

var testDriverState testDriverStateS

// RenderLoopHandlerTestDriver is the deterministic screenshot tour that
// activates when IMZERO2_SCREENSHOT_DIR is set. Writes one PNG per
// registered demo to $IMZERO2_SCREENSHOT_DIR. Flips the Rust-side
// animation_freeze flag at startup so animated widgets snap to their
// target pose for pixel-stable captures. The seed comes from
// SeededFuncApp — see [RenderLoopHandlerInteractive] for the
// multi-instance rationale; in tour mode the tour is single-instance
// in practice, but the seed scope guards against drift.
func RenderLoopHandlerTestDriver(seed uint64) (err error) {
	screenshotDir := imzero2env.ScreenshotDir.Get()
	if screenshotDir == "" {
		return
	}
	allowNet := imzero2env.AllowNetwork.Get() == "1"
	testDir := screenshotDir

	if !testDriverState.setupDone {
		err = os.MkdirAll(testDir, 0o755)
		if err != nil {
			log.Warn().Err(err).Str("dir", testDir).Msg("TestDriver: unable to create output dir")
			err = nil
		}
		c.SetAnimationFreeze(true)
		c.MemoryResetAreas()
		all := registry.All()
		skipNonDeterministic := imzero2env.ScreenshotDeterministic.Get() != ""
		testDriverState.active = make([]registry.Demo, 0, len(all))
		for _, d := range all {
			if d.Flags&registry.DemoFlagSkipInTour != 0 {
				log.Info().Str("name", d.Name).Msg("TestDriver: skip (SkipInTour)")
				continue
			}
			if d.Flags&registry.DemoFlagNeedsNetwork != 0 && !allowNet {
				log.Info().Str("name", d.Name).Msg("TestDriver: skip (NeedsNetwork; set IMZERO2_ALLOW_NETWORK=1)")
				continue
			}
			if d.Flags&registry.DemoFlagNonDeterministic != 0 && skipNonDeterministic {
				log.Info().Str("name", d.Name).Msg("TestDriver: skip (NonDeterministic; IMZERO2_SCREENSHOT_DETERMINISTIC set)")
				continue
			}
			testDriverState.active = append(testDriverState.active, d)
		}
		// Allocate the tour's WidgetIdStack + per-demo state map and
		// Init every demo that opted into the stateful path. Same
		// idea as App.Mount for the interactive driver, just on the
		// tour-owned stack since the tour is single-instance.
		testDriverState.ids = c.NewWidgetIdStack()
		testDriverState.demoState = make(map[string]any, len(testDriverState.active))
		for _, d := range testDriverState.active {
			switch {
			case d.BusInit != nil:
				// Tour runs with no host-supplied bus; BusInit demos
				// receive nil and are expected to degrade to picker-
				// only rendering (no capability publishes). Mirrors
				// App.Mount's BusInit branch in interactive_driver.go.
				testDriverState.demoState[d.Name] = d.BusInit(testDriverState.ids, nil)
			case d.Init != nil:
				testDriverState.demoState[d.Name] = d.Init(testDriverState.ids)
			}
		}
		log.Info().Int("total", len(all)).Int("active", len(testDriverState.active)).Str("dir", testDir).Msg("TestDriver: tour starting")
		testDriverState.setupDone = true
	}
	c.RequestRepaint()

	if testDriverState.doneAll {
		return
	}
	if int(testDriverState.demoIdx) >= len(testDriverState.active) {
		log.Info().Str("dir", testDir).Msg("TestDriver: tour complete")
		testDriverState.doneAll = true
		return
	}

	demo := testDriverState.active[testDriverState.demoIdx]
	stageW, stageH := demo.Stage[0], demo.Stage[1]
	if stageW == 0 {
		stageW = testStageDefaultW
	}
	if stageH == 0 {
		stageH = testStageDefaultH
	}
	// IMZERO2_SCREENSHOT_SIZE overrides every demo's Stage so the tour
	// produces uniformly-sized PNGs at the requested resolution. The
	// launch wrapper widens the eframe viewport in tandem so the rect
	// doesn't clip against the window. ADR-0008 SD5. The env override
	// bypasses the registry stage budget on the assumption the operator
	// knows their compositor will honour the request.
	if w, h, ok := imzero2env.ScreenshotSizeWH(); ok {
		stageW = float32(w)
		stageH = float32(h)
	} else {
		// Clamp to the registry stage budget. Protects PNG output from
		// per-demo Stage drift exceeding what typical Wayland compositors
		// will reliably allocate. NeedsLargeArea opts into a taller
		// envelope; width is capped unconditionally.
		maxH := registry.StandardStageMaxH
		if demo.Flags&registry.DemoFlagNeedsLargeArea != 0 {
			maxH = registry.LargeAreaStageMaxH
		}
		if stageW > registry.StandardStageMaxW {
			stageW = registry.StandardStageMaxW
		}
		if stageH > maxH {
			stageH = maxH
		}
	}

	// The host (carousel adaptToRenderer, ADR-0026 Amendment 2026-05-12)
	// wraps this Frame in a runtime-created c.Window, which provides a
	// real Ui scope — the previous PanelCentral bridge from u=None is no
	// longer needed. AllocateUiAtRect uses absolute viewport coordinates
	// regardless of the surrounding scope. The IdScope wraps the tour
	// body so widget ids derive from the per-instance seed (drift guard).
	// Single-instance by design — the host's per-window MountCtx.Ids()
	// is not consumed here because the tour owns its own stack into
	// which each demo's pre-built widget singletons are bound at Init.
	tourIds := testDriverState.ids
	tourIds.Reset()
	for range c.IdScope(tourIds.PrepareSeq(seed)) {
		for range c.AllocateUiAtRect(0, 0, stageW, stageH).KeepIter() {
			RenderDemoIntro(tourIds, &demo)
			// Dispatch to RenderStateful for migrated demos (state
			// pre-built in setup above) and to legacy Render for the
			// rest. The two paths cannot both be set per
			// [registry.Register]'s validation.
			if demo.RenderStateful != nil {
				demo.RenderStateful(tourIds, testDriverState.demoState[demo.Name])
			} else {
				demo.Render(tourIds)
			}
			RenderDemoOutro(tourIds, &demo)
		}
	}

	switch testDriverState.phase {
	case testPhaseSettle:
		testDriverState.settleCnt++
		if int(testDriverState.settleCnt) >= testDriverSettleFrames {
			testDriverState.phase = testPhaseCapture
		}
	case testPhaseCapture:
		path := filepath.Join(testDir, demo.Name+".png")
		c.RequestScreenshotRect(path, 0, 0, stageW, stageH)
		// Parallel SVG dump for the same frame — exercises the new
		// exportSvg FFFI opcode end-to-end through the same tour
		// machinery. Lands in /tmp/<screenshotDir>/<demo>.svg next to
		// the PNG so visual diffs are trivial.
		svgPath := filepath.Join(testDir, demo.Name+".svg")
		// Tour writes the self-contained (Tier 2) variant so the dumped
		// SVGs render pixel-faithfully without depending on a viewer's
		// installed fonts. To do tier-1 (lighter, externally-referenced)
		// comparisons, drop embedFonts to false here or use the painter
		// demo's "Export SVG" button which defaults to the light variant.
		// Only one ExportSvg call may be queued per pass — the IDL writes
		// into a single `pending` slot.
		// bgRgba=0x1e1e1eff matches the dark VIEWPORT_BG so tour SVGs
		// composite over the same baseline the egui PNG screenshot
		// captures. Pass 0 to omit the rect for transparent exports.
		c.ExportSvg(svgPath, true, 0x1e1e1eff)
		log.Info().Str("path", path).Str("svg", svgPath).Str("name", demo.Name).Msg("TestDriver: capture requested")
		testDriverState.phase = testPhaseAdvance
	case testPhaseAdvance:
		testDriverState.demoIdx++
		testDriverState.phase = testPhaseSettle
		testDriverState.settleCnt = 0
		c.MemoryResetAreas()
	}
	return
}
