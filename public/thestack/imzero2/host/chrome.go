package imzhost

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/apps/capinspector"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/helphost"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/metricsoverlay"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/runtimestatus"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/videooutput"
	"github.com/stergiotis/boxer/public/thestack/imzero2/imzero2env"
)

// idleRepaintIntervalSecs is the steady-state repaint cadence requested by
// DecorateRenderer in interactive mode. egui overrides it with sooner
// repaints for input and animation (it keeps the earliest deadline), so it
// only bounds how often a fully idle window refreshes. Matches the imztop
// sampler's 1 s tick; the Rust side mirrors it (src/imzero2/app.rs's
// IDLE_REPAINT_INTERVAL). Apps that change without interaction faster than
// this can request a sooner repaint themselves.
const idleRepaintIntervalSecs = 1.0

// ChromeConfig configures the shared imzero2 host chrome wrapped around an
// inner renderer by DecorateRenderer. The zero value renders bare chrome
// (no extra menus, no status line, no video-output control, no F1 help).
type ChromeConfig struct {
	// ExtraMenus, when non-nil, is invoked inside the top MenuBar scope
	// after the built-in File / Layout menus (used by the dock host to
	// inject its "Apps" menu). May be nil.
	ExtraMenus func()
	// Status, when non-nil, renders the runtime status line in the bottom
	// panel. May be nil — the bottom row collapses to the metrics overlay
	// alone.
	Status *runtimestatus.Snapshot
	// Host, when non-nil, makes the status segments clickable (opening the
	// capinspector) and is required for HelpHost. May be nil.
	Host *windowhost.Inst
	// VideoOutput renders the ADR-0088 videooutput codec control (carousel:
	// true; elle: false).
	VideoOutput bool
	// HelpHost wires the F1 shortcut to open/focus the HelpHost (carousel:
	// true; elle: false). Requires Host != nil.
	HelpHost bool
}

// DecorateRenderer wraps an inner renderer in the shared host chrome:
// top PanelTop with the File / Layout menus + an optional ExtraMenus
// callback (used by the dock host to inject its "Apps" menu), bottom
// PanelBottom with a runtime status line + the metrics overlay.
// cc.ExtraMenus may be nil — the screenshot tour path uses nil since
// there is no WindowHost in tour mode. cc.Status may be nil — the
// bottom row collapses to the metrics overlay alone. cc.Host may be nil
// — the status segments stay non-clickable when no windowhost is
// available to open the capinspector.
func DecorateRenderer(inner func() error, cc ChromeConfig) func() error {
	ids := c.NewWidgetIdStack()
	_ = ids
	// Captured once at setup so the per-frame closure pays no per-call
	// env-read cost. ADR-0032 §SD2 — IDS spacing tokens at the active
	// density preset.
	density := styletokens.DensityFromEnv()
	// Screenshot/tour mode (IMZERO2_SCREENSHOT_DIR set). Drives two things,
	// captured once so the per-frame closure doesn't repeat the env read:
	//   1. Skips the bottom status panel — the metrics overlay (Go ms / Rust
	//      ms / vsync / network / fps) and the run_id chip in runtimestatus
	//      change byte-for-byte every frame, producing 30+ "modified" file
	//      diffs every tour rerun without any visual content change.
	//   2. Keeps requesting immediate repaints (continuous mode) so every
	//      pass renders for capture, rather than the interactive idle
	//      heartbeat below.
	screenshotMode := imzero2env.ScreenshotDir.Get() != ""
	// served: this process is a headless pixel-streaming carrier listening for
	// remote viewers (IMZERO2_HEADLESS_LISTEN set by imzero2-demo.service / the
	// nix module), as opposed to the desktop host or a screenshot tour. The
	// "viewport" is then a shared remote stream, not a per-viewer window, so the
	// File→Quit affordance below is omitted: it maps to ViewportCommand::Close,
	// which the headless host treats as a process shutdown — a single remote
	// click would take the demo down for everyone (ADR-0085). A remote viewer
	// closes their browser tab instead. Captured once; the env is read by the
	// Rust client too but the Go process inherits it.
	served := imzero2env.HeadlessListen.Get() != ""
	// Render cadence (IMZERO2_RENDER_CADENCE), captured once — the launch-time
	// default, and the fallback when no remote stream is live. Continuous
	// (default) paints at vsync rate; reactive drops to an idle heartbeat when
	// nothing is happening. The Rust client reads the same variable; both sides
	// must agree, since egui takes the soonest repaint deadline (an immediate
	// request on either side overrides the other's heartbeat). The headless
	// host's cadence is also runtime-switchable from the viewer, so the
	// per-frame closure prefers that live value (videoState.HostReactive) over
	// this flag when a stream is connected.
	reactive := imzero2env.RenderCadence.Get() == imzero2env.RenderCadenceReactive
	// ADR-0088: the remote-stream codec control. Persists the selected codec
	// across frames; renders only when a remote viewer has reported decode
	// capabilities (so it is invisible under the desktop host).
	videoState := &videooutput.State{}
	return func() error {
		// F1 global shortcut: open or focus HelpHost. The cached value
		// was drained from egui's input queue during StateManager.Sync
		// of the previous frame (consume_key already removed it), so
		// polling here is the one owner of this binding. Skipped in
		// tour mode (host == nil) since there's no windowhost to open
		// into.
		if cc.HelpHost && cc.Host != nil && c.CurrentApplicationState.StateManager.GetF1KeyPressed() {
			if _, openErr := cc.Host.Open(helphost.ManifestId); openErr != nil {
				log.Warn().Err(openErr).Msg("F1: helphost open failed")
			}
		}
		// Drive the next frame. Continuous mode (the default) and screenshot
		// capture always request an immediate repaint. Reactive mode requests
		// a slow idle heartbeat instead; egui still repaints immediately for
		// input and animation (it keeps the earliest deadline), so interaction
		// stays at vsync rate while a visible-but-idle window drops to a few
		// fps. The Rust side mirrors this (src/imzero2/app.rs).
		//
		// The headless host's cadence is runtime-switchable from the browser
		// viewer (ADR-0088/0024): prefer the host's live cadence over the
		// launch-time flag so a viewer-driven switch takes effect on the Go side
		// too. videoState carries the value the host reported on the previous
		// frame (refreshed by videooutput.ShowStatus below — a one-frame lag).
		// Without this the Go side stays pinned to the launch cadence, and the
		// immediate repaint defeats a runtime switch to reactive since egui
		// takes the soonest repaint deadline. Falls back to the launch-time flag
		// when no stream is live (desktop host, or before a viewer connects).
		reactiveNow := reactive
		if live, ok := videoState.HostReactive(); ok {
			reactiveNow = live
		}
		if screenshotMode || !reactiveNow {
			c.RequestRepaint()
		} else {
			c.RequestRepaintAfter(idleRepaintIntervalSecs)
		}
		c.CurrentApplicationState.StartServersideFrame()
		defer c.CurrentApplicationState.FinishServersideFrame()
		for range c.PanelTop(ids.PrepareStr("topPanel")).KeepIter() {
			for range c.MenuBar().KeepIter() {
				// File menu holds only Quit, which terminates the host — omit the
				// whole menu when served headlessly (see `served` above). Restore
				// the menu (not just the item) if File gains a safe entry.
				if !served {
					for range c.MenuButton(c.Atoms().Text("File").Keep()).KeepIter() {
						if c.Button(ids.PrepareStr("quit"), c.Atoms().Text("Quit").Keep()).SendResp().HasPrimaryClicked() {
							c.ContextSendViewPortCommandClose()
						}
					}
				}
				for range c.MenuButton(c.Atoms().Text("Layout").Keep()).KeepIter() {
					if c.Button(ids.PrepareStr("arrangeWindows"), c.Atoms().Text("Arrange Windows").Keep()).SendResp().HasPrimaryClicked() {
						c.MemoryResetAreas()
					}
					c.GuiZoomZoomMenuButtons()
				}
				if cc.ExtraMenus != nil {
					cc.ExtraMenus()
				}
				c.AddSpace(styletokens.GapSections(density))
			}
		}
		if !screenshotMode {
			for range c.PanelBottom(ids.PrepareStr("bottomPanel")).Resizable(false).KeepIter() {
				for range c.Horizontal().KeepIter() {
					c.AddSpace(styletokens.GapItems(density))
					if cc.Status != nil {
						var onClick runtimestatus.ClickHandler
						if cc.Host != nil {
							onClick = func(capId string) {
								// Push the selection first so newApp()
								// pops it during Open. FIFO queue —
								// rapid clicks open multiple inspector
								// windows, each tagged with the cap
								// that was clicked.
								capinspector.PushSelection(capinspector.CapId(capId))
								_, openErr := cc.Host.Open(capinspector.ManifestId)
								if openErr != nil {
									log.Warn().Err(openErr).Str("capId", capId).
										Msg("status-bar: capinspector open failed")
								}
							}
						}
						runtimestatus.RenderInline(cc.Status, onClick)
						// Visual separator before the metrics block; the
						// status segment is process-static info, metrics
						// is per-frame telemetry — the gap signals the
						// shift in cadence.
						c.AddSpace(styletokens.GapSections(density))
					}
					metricsoverlay.RenderInline(ids.PrepareStr("fps"))
					// ADR-0088: compact active-codec indicator; clicking it opens
					// the video-output settings dialog (ShowDialog, below).
					if cc.VideoOutput {
						videooutput.ShowStatus(ids, videoState)
					}
				}
			}
		}
		// ADR-0088: the video-output settings dialog floats over the app when
		// opened from the status-bar indicator (self-hides otherwise).
		if cc.VideoOutput {
			videooutput.ShowDialog(ids, videoState)
		}
		return inner()
	}
}
