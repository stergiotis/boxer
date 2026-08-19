//! Conformance / branding screenshot mode (ADR-0030 §SD11).
//!
//! Two modes, switched by `IMZERO2_SCREENSHOT_MODE`:
//!
//! - **Conformance** (default): IDS-default fonts only. `IMZERO2_FONT_*`
//!   overrides are ignored with a one-line stderr warning. Output goes to
//!   `IMZERO2_SCREENSHOT_DIR`. This is what Tier 2 LLM grading and visual
//!   regression read.
//! - **Branding** (opt-in via `IMZERO2_SCREENSHOT_MODE=branding`):
//!   `IMZERO2_FONT_*` overrides honoured. Output goes to
//!   `IMZERO2_BRANDING_DIR`. Marketing materials come from here.
//!
//! The mode + output-directory split is the load-bearing invariant per
//! ADR-0030 §SD11 — it makes Tier 2 grading immune to a contributor
//! accidentally setting a font override env var.

use std::env;

/// Screenshot mode, resolved from `IMZERO2_SCREENSHOT_MODE`.
#[derive(Copy, Clone, Debug, Default, PartialEq, Eq)]
pub enum ScreenshotMode {
    /// Default — IDS-default fonts; overrides ignored.
    #[default]
    Conformance,
    /// Opt-in via env var — overrides honoured; output to branding dir.
    Branding,
}

/// Read the mode from `IMZERO2_SCREENSHOT_MODE`. Case-insensitive;
/// "branding" → Branding, anything else (including unset) → Conformance.
pub fn mode_from_env() -> ScreenshotMode {
    match env::var("IMZERO2_SCREENSHOT_MODE")
        .ok()
        .as_deref()
        .map(str::to_ascii_lowercase)
        .as_deref()
    {
        Some("branding") => ScreenshotMode::Branding,
        _ => ScreenshotMode::Conformance,
    }
}

/// Resolve the body-mono family name, honouring `IMZERO2_FONT_BODY_MONO`
/// only in branding mode. Conformance mode warns + falls back to the
/// IDS default.
///
/// Returns the family name to use with `egui::FontId::new(size, name)`.
pub fn body_mono_family() -> String {
    let m = mode_from_env();
    let override_val = env::var("IMZERO2_FONT_BODY_MONO").ok();

    match (m, override_val) {
        (ScreenshotMode::Branding, Some(family)) => family,
        (ScreenshotMode::Conformance, Some(family)) => {
            // Dropping the override silently would leave an operator wondering
            // why their font never appears, so the one-line warning is part of
            // the mode's contract (module docs above; ADR-0030 §SD11). This
            // crate depends on egui alone — there is no log/tracing facade here
            // to route it through — so stderr is the channel.
            #[expect(clippy::print_stderr)]
            {
                eprintln!(
                    "imzero2_egui: override IMZERO2_FONT_BODY_MONO={family} ignored \
                     in conformance mode (set IMZERO2_SCREENSHOT_MODE=branding)"
                );
            }
            super::typography::FAMILY_MONO.to_owned()
        }
        (_, None) => super::typography::FAMILY_MONO.to_owned(),
    }
}

#[cfg(test)]
mod tests {
    // Rust 2024 made env-var mutation unsafe, and these tests mutate
    // IMZERO2_*, so this module opts back in. ENV_LOCK below is what makes
    // that sound: it serialises the tests that touch the process-global env.
    #![allow(unsafe_code)]
    use super::*;
    use std::sync::Mutex;

    static ENV_LOCK: Mutex<()> = Mutex::new(());

    fn with_env<F: FnOnce()>(vars: &[(&str, Option<&str>)], f: F) {
        let _g = ENV_LOCK.lock().expect("ENV_LOCK poisoned by an earlier test panic");
        let saved: Vec<(String, Option<String>)> =
            vars.iter().map(|(k, _)| (k.to_string(), env::var(*k).ok())).collect();
        // SAFETY: setting env vars races with any concurrent reader in the
        // process. ENV_LOCK, held for the whole of with_env, is the only thing
        // in this crate that touches IMZERO2_*, so no other thread can observe
        // a torn value while this runs.
        unsafe {
            for (k, v) in vars {
                match v {
                    Some(val) => env::set_var(k, val),
                    None => env::remove_var(k),
                }
            }
        }
        f();
        // SAFETY: as above — still under ENV_LOCK, restoring what was saved.
        unsafe {
            for (k, v) in saved {
                match v {
                    Some(val) => env::set_var(&k, val),
                    None => env::remove_var(&k),
                }
            }
        }
    }

    #[test]
    fn default_mode_is_conformance() {
        with_env(&[("IMZERO2_SCREENSHOT_MODE", None)], || {
            assert_eq!(mode_from_env(), ScreenshotMode::Conformance);
        });
    }

    #[test]
    fn branding_mode_recognised_case_insensitively() {
        with_env(&[("IMZERO2_SCREENSHOT_MODE", Some("BRANDING"))], || {
            assert_eq!(mode_from_env(), ScreenshotMode::Branding);
        });
    }

    #[test]
    fn override_ignored_in_conformance_mode() {
        with_env(
            &[
                ("IMZERO2_SCREENSHOT_MODE", Some("conformance")),
                ("IMZERO2_FONT_BODY_MONO", Some("PragmataPro")),
            ],
            || {
                assert_eq!(body_mono_family(), super::super::typography::FAMILY_MONO);
            },
        );
    }

    #[test]
    fn override_honoured_in_branding_mode() {
        with_env(
            &[
                ("IMZERO2_SCREENSHOT_MODE", Some("branding")),
                ("IMZERO2_FONT_BODY_MONO", Some("PragmataPro")),
            ],
            || {
                assert_eq!(body_mono_family(), "PragmataPro");
            },
        );
    }
}
