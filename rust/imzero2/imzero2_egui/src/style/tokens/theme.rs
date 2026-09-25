//! Colour theme (ADR-0258).
//!
//! Two themes: the IDS dark palette of ADR-0031, and `fresh`, a light one.
//! The choice is read once from `IMZERO2_THEME` and fixed for the life of
//! the process — the Go side resolves the same variable at package init
//! and overwrites its palette mirror, and widgets there may copy tokens at
//! init, so a runtime switch on this side alone would desynchronise the two.
//! The density re-apply (`setIdsDensity`) reads [`active`] so it keeps the
//! theme it started with.

use std::env;
use std::sync::atomic::{AtomicU8, Ordering};

/// The colour theme. Discriminants match the Go `styletokens.ThemeE`.
#[derive(Copy, Clone, Debug, Default, PartialEq, Eq, Hash)]
pub enum Theme {
    /// The IDS palette of ADR-0031 — the default.
    #[default]
    Dark = 0,
    /// The light theme of ADR-0258.
    Fresh = 1,
}

/// Parse an `IMZERO2_THEME` value. Case-insensitive; `fresh` selects the
/// light theme and anything else — including empty — is the IDS palette.
pub fn parse(value: &str) -> Theme {
    if value.trim().eq_ignore_ascii_case("fresh") {
        Theme::Fresh
    } else {
        Theme::Dark
    }
}

/// Read the theme from `IMZERO2_THEME`.
pub fn from_env() -> Theme {
    parse(&env::var("IMZERO2_THEME").unwrap_or_default())
}

const UNRESOLVED: u8 = u8::MAX;
static ACTIVE: AtomicU8 = AtomicU8::new(UNRESOLVED);

/// The theme this process runs with, resolved from the environment on the
/// first call and constant afterwards.
pub fn active() -> Theme {
    match ACTIVE.load(Ordering::Relaxed) {
        UNRESOLVED => {
            let t = from_env();
            // Another thread may have resolved it first; both read the same
            // environment, so either value is the same value.
            let _ =
                ACTIVE.compare_exchange(UNRESOLVED, t as u8, Ordering::Relaxed, Ordering::Relaxed);
            t
        }
        1 => Theme::Fresh,
        _ => Theme::Dark,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_is_case_insensitive_and_defaults_to_dark() {
        assert_eq!(parse("fresh"), Theme::Fresh);
        assert_eq!(parse(" FRESH "), Theme::Fresh);
        assert_eq!(parse(""), Theme::Dark);
        assert_eq!(parse("dark"), Theme::Dark);
        assert_eq!(parse("light"), Theme::Dark);
    }

    #[test]
    fn discriminants_match_the_go_mirror() {
        assert_eq!(Theme::Dark as u8, 0);
        assert_eq!(Theme::Fresh as u8, 1);
    }
}
