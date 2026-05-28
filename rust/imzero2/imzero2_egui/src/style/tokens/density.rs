//! Density preset (ADR-0029 §SD3, ADR-0032 §SD1).
//!
//! Three modes: Tight / Standard / Roomy. The active preset is set once at
//! app startup. Mixed-density screens are a Tier 2 finding (rule V4); the
//! preset is per-app, fleet-wide.

use std::env;

/// IDS density preset. Original names per ADR-0029 §SD3 (deliberately not
/// "Compact / Regular / Spacious" or "dense / regular / comfortable").
#[derive(Copy, Clone, Debug, PartialEq, Eq, Hash)]
pub enum Density {
    Tight = 0,
    Standard = 1,
    Roomy = 2,
}

impl Default for Density {
    fn default() -> Self {
        Density::Standard
    }
}

/// Read the active density from `IMZERO2_DENSITY` env var.
///
/// Recognised values: `tight` / `standard` / `roomy` (case-insensitive).
/// Anything else (including absent) returns `Density::Standard`.
///
/// Per-user config file support (`$XDG_CONFIG_HOME/imzero2/density.toml`,
/// per ADR-0032 §SD1) lands later; the env var is the M0 surface.
pub fn from_env() -> Density {
    match env::var("IMZERO2_DENSITY")
        .ok()
        .as_deref()
        .map(str::to_ascii_lowercase)
        .as_deref()
    {
        Some("tight") => Density::Tight,
        Some("roomy") => Density::Roomy,
        _ => Density::Standard,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn default_is_standard() {
        assert_eq!(Density::default(), Density::Standard);
    }

    #[test]
    fn enum_indices_match_table_columns() {
        // ADR-0032 §SD2 table is indexed by (Px[i], density). The PX_TABLE
        // accessor casts `Density as usize` — the discriminants must agree.
        assert_eq!(Density::Tight as usize, 0);
        assert_eq!(Density::Standard as usize, 1);
        assert_eq!(Density::Roomy as usize, 2);
    }
}
