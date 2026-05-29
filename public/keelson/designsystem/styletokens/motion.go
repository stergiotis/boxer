//go:build llm_generated_opus47

package styletokens

import (
	"sync/atomic"
	"time"
)

// Motion durations (ADR-0032 §SD5). Easing limited to what egui exposes.
//
// MIRROR INVARIANT: must equal Rust consts in
// src/rust/imzero2_egui/src/style/tokens/motion.rs.
const (
	// MotionQuickMs — state-change feedback (hover, focus, button-press).
	MotionQuickMs uint32 = 80
	// MotionStandardMs — default transitions (panel open/close, menu).
	MotionStandardMs uint32 = 160
	// MotionSlowMs — deliberate transitions (modal entrance, drawer slide).
	MotionSlowMs uint32 = 320
)

var motionEnabled atomic.Bool

func init() {
	motionEnabled.Store(true)
}

// SetMotionEnabled toggles motion at runtime. Set once at startup from the
// OS reduced-motion preference, or by the tour pipeline for conformance
// captures (ADR-0032 §SD5 last paragraph).
func SetMotionEnabled(enabled bool) {
	motionEnabled.Store(enabled)
}

// MotionEnabled returns the current motion-enabled flag.
func MotionEnabled() (b bool) {
	b = motionEnabled.Load()
	return
}

func durationOrZero(ms uint32) (d time.Duration) {
	if motionEnabled.Load() {
		d = time.Duration(ms) * time.Millisecond
	}
	return
}

// secondsOrZero converts a ladder value (ms) to the seconds form egui's
// animate_* API takes (durSecs float32). Honours the runtime
// motion-enabled flag so reduced-motion mode collapses to zero
// (instantaneous, matches MotionQuick / MotionStandard / MotionSlow).
func secondsOrZero(ms uint32) (s float32) {
	if motionEnabled.Load() {
		s = float32(ms) / 1000.0
	}
	return
}

// MotionQuick — 80 ms by default; zero if motion is disabled.
func MotionQuick() (d time.Duration) { d = durationOrZero(MotionQuickMs); return }

// MotionStandard — 160 ms by default; zero if motion is disabled.
func MotionStandard() (d time.Duration) { d = durationOrZero(MotionStandardMs); return }

// MotionSlow — 320 ms by default; zero if motion is disabled.
func MotionSlow() (d time.Duration) { d = durationOrZero(MotionSlowMs); return }

// MotionQuickSecs returns the MotionQuick duration as float32 seconds,
// the wire form the egui binding API expects
// (`AnimateBoolWithTimeBind(..., durSecs float32, ...)`). Zero if
// motion is disabled.
func MotionQuickSecs() (s float32) { s = secondsOrZero(MotionQuickMs); return }

// MotionStandardSecs — MotionStandard as float32 seconds; zero if motion is disabled.
func MotionStandardSecs() (s float32) { s = secondsOrZero(MotionStandardMs); return }

// MotionSlowSecs — MotionSlow as float32 seconds; zero if motion is disabled.
func MotionSlowSecs() (s float32) { s = secondsOrZero(MotionSlowMs); return }
