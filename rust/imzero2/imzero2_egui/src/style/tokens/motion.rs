//! Motion durations (ADR-0032 §SD5).
//!
//! Three duration tokens. Easing limited to what egui exposes via
//! `Context::animate_bool` and `emath::easing` — no Bezier or spring lib.
//!
//! OS reduced-motion preference is respected via `MOTION_ENABLED`. When
//! disabled, all duration accessors return `Duration::ZERO`. The OS
//! detection (Linux DBus/GSettings, macOS CoreFoundation, Windows
//! `SystemParametersInfo`) lands in M3 of ADR-0032; for now `MOTION_ENABLED`
//! defaults to `true` and can be flipped by `set_motion_enabled` (called by
//! the screenshot tour to disable for conformance captures).

use std::sync::atomic::{AtomicBool, Ordering};
use std::time::Duration;

/// State change feedback (hover, focus, button-press).
pub const QUICK_MS: u32 = 80;
/// Default transitions (panel open/close, menu expand, DragValue finalise).
pub const STANDARD_MS: u32 = 160;
/// Deliberate transitions (modal entrance, drawer slide, page-level state).
pub const SLOW_MS: u32 = 320;

static MOTION_ENABLED: AtomicBool = AtomicBool::new(true);

/// Disable / enable motion at runtime. Set once at startup from the OS
/// reduced-motion preference, or by the tour pipeline for conformance
/// captures (per ADR-0032 §SD5 last paragraph).
pub fn set_motion_enabled(enabled: bool) {
    MOTION_ENABLED.store(enabled, Ordering::Relaxed);
}

/// Snapshot of the current motion-enabled flag.
#[inline]
pub fn motion_enabled() -> bool {
    MOTION_ENABLED.load(Ordering::Relaxed)
}

#[inline]
fn duration_or_zero(ms: u32) -> Duration {
    if motion_enabled() {
        Duration::from_millis(ms as u64)
    } else {
        Duration::ZERO
    }
}

/// 80 ms by default; `Duration::ZERO` if motion is disabled.
#[inline]
pub fn quick() -> Duration {
    duration_or_zero(QUICK_MS)
}

/// 160 ms by default; `Duration::ZERO` if motion is disabled.
#[inline]
pub fn standard() -> Duration {
    duration_or_zero(STANDARD_MS)
}

/// 320 ms by default; `Duration::ZERO` if motion is disabled.
#[inline]
pub fn slow() -> Duration {
    duration_or_zero(SLOW_MS)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn defaults_match_adr_0032_sd5() {
        assert_eq!(QUICK_MS, 80);
        assert_eq!(STANDARD_MS, 160);
        assert_eq!(SLOW_MS, 320);
    }

    #[test]
    fn motion_enabled_returns_full_duration() {
        set_motion_enabled(true);
        assert_eq!(quick(), Duration::from_millis(80));
        assert_eq!(standard(), Duration::from_millis(160));
        assert_eq!(slow(), Duration::from_millis(320));
    }

    #[test]
    fn motion_disabled_returns_zero() {
        set_motion_enabled(false);
        assert_eq!(quick(), Duration::ZERO);
        assert_eq!(standard(), Duration::ZERO);
        assert_eq!(slow(), Duration::ZERO);
        // Restore for subsequent tests in the same process.
        set_motion_enabled(true);
    }
}
