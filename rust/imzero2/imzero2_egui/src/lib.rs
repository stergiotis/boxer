//! ImZero2 Design System (IDS) — egui::Style overlay.
//!
//! See `doc/adr/0029-imzero2-design-system-and-policy-as-code.md` for the
//! framework, and `doc/adr/0030`–`0034` for the foundations sub-ADRs.
//!
//! The crate exposes a single `apply` entry plus the underlying token modules
//! (`style::tokens::{density, spacing, rounding, stroke, motion, ...}`).
//! No widget forks — IDS lives entirely as a layer over `egui::Visuals`,
//! `egui::Spacing`, and `egui::Style`.

pub mod style;
