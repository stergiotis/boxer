// Package lazypane gates a host-skippable region — a dock-tab body, or any
// subtree a parent block can cull — on whether the host actually rendered it
// last frame, so the Go side can skip building content that would only be
// discarded.
//
// # Why
//
// Go runs every dock-tab body every frame into a detached buffer, but the
// host interprets only the active tab of each dock leaf; the other buffers
// are dropped uninterpreted (the imzero2 skill's "Lost Sends" pitfall
// describes the correctness half of that seam). The cost half is that the
// body lambda still executes and its opcodes still cross the FFI. For heavy
// bodies (rasters, tables, force layouts) that is the dominant per-frame
// cost of an app whose tabs are mostly hidden.
//
// # Mechanism
//
// A Pane emits a captureUiRect probe as the first opcode of the region every
// frame. The probe is interpreted only when a live Ui is in scope — an
// inactive tab's buffer is discarded, a culled block drains with no Ui — so
// its presence in last frame's r21 report (StateManager.GetUiRect) is a
// one-frame-lagged "this region reached the screen". Skip reads that signal:
// when the region was not rendered, it emits a small loading placeholder and
// tells the caller to skip the heavy body.
//
//	pane := lazypane.New("myapp-dock-tab-results", "Results")
//	for range dock.Tab(tabID, title) {
//		if pane.Skip() {
//			continue
//		}
//		renderHeavyBody()
//	}
//
// # The one-frame contract (and why a placeholder, not a gate)
//
// The signal is advisory and one frame stale, like every feedback register
// (ADR-0012 removed the structural previous-frame gate precisely because
// stale gating flickers). A revealed tab therefore shows the placeholder for
// exactly one frame — a deliberate loading state in a fixed-geometry pane,
// not an empty flash — and the body lands on the next frame. Send-once
// protocols under the region (content-versioned textures, delta streams)
// re-arm through the starved-texture report exactly as they do after an
// idle-LRU eviction; JustRevealed additionally lets a body re-arm eagerly.
//
// HoldFrames extends the placeholder by N frames for taste. It DELAYS the
// body (and with it any texture re-ship the body would trigger) — it does
// not overlap warm-up. Leave it 0 unless a sub-frame flash proves
// objectionable in a specific spot.
//
// # When not to use
//
//   - Cheap bodies: a probe + placeholder swap buys nothing over just
//     emitting a few labels; gate only bodies that measurably cost.
//   - Regions whose body tethers floating c.Window children that must stay
//     visible while the host region is hidden: skipping the body hides them.
//   - Bodies that deliver one-shot ops (an insert-at-cursor): pair the
//     delivery with DockAreaFluid.ActivateTab and keep the pending op queued
//     until the body actually runs (the play snippet-insert pattern).
package lazypane
