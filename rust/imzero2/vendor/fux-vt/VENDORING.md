---
type: reference
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Vendored 2026-11-06 for the console
> widget, [ADR-0359](../../../../doc/adr/0359-egui-console-a-terminal-widget-over-a-vendored-vt-core.md)
> (proposed).

# Vendored: `fux-vt`

The VT core of imzero2's console widget: a bounded terminal emulator (parser,
screen, history) with key, paste, focus and mouse encoders. Upstream:
<https://github.com/gold-silver-copper/fux>, the `fux-vt` crate of that
workspace.

- **Upstream commit**: `03f8d6ce5f8e9617a949def80729d1c09cf9491c`
  (2026-11-05), past the crates.io release `0.16.0`, whose version number
  the crate still carries. The release lacks the fix that lets the crate
  build for 32-bit targets and the browser (upstream `13442a8c`).
- **Licence**: MIT. The text is kept verbatim beside this file (`LICENSE`)
  and the crate is recorded in
  [THIRD_PARTY_NOTICES.md](../../../../THIRD_PARTY_NOTICES.md).
- **Taken**: `src/`, `Cargo.toml`, `LICENSE`, `README.md`, `THREATS.md`,
  `CONFORMANCE.md`, and `tests/data/GraphemeBreakTest.txt` (which a unit
  test includes). Upstream's integration tests, fuzz targets, table
  generators and differential harness (`compare/`) stay upstream: changes
  here are checked with that harness from an upstream checkout.

## Why vendored rather than a crates.io dependency

ADR-0359 SD7: the crate is young and has one maintainer, so upstream changes
reach boxer only when someone merges one deliberately, and the copy is
trimmed to what the widget uses.

## Deltas against upstream

Deletions and manifest changes only; no emulation logic was touched.

- **The raw-input decoder** (`src/keys/decode.rs`) and the byte queue it
  read from (`src/bytes.rs`): `fux` decodes a client terminal's input as a
  multiplexer; the widget receives egui's events instead. Helpers only the
  decoder used went with it (`numbered_key`, `Control::of` and `key`,
  `MouseButton::from_code`, `keys::mouse::event`, `NOT_BUTTON`,
  `Modifiers::from_bits`), and so did the mouse test that round-tripped
  reports through the decoder. `Modifiers::ALT` is kept for the tests only.
  The `ALL` constant the `closed!` macro writes allows dead code, as
  `Control` no longer reads its own.
- **`Cargo.toml`**: the workspace lint table written out (the crate is not
  a member of imzero2's workspace, as the other vendored crates are not),
  `publish = false`, the `fux-vt-peak` dev-dependency and the two
  integration tests that need it removed, the description adjusted.

## Not yet trimmed

Sixel decoding (`src/sixel.rs`) and picture placement
(`src/screen/pictures.rs`) are threaded through the parser, the screen, the
grid and the history, about 230 references; cutting them is a refactor of
its own, to be checked with upstream's differential harness. Until then they
stay, off by default: nothing reaches them unless `Feature::Pictures` is
enabled, and the widget does not enable it.

## Updating

Take the new commit's `fux-vt` tree with `git archive`, reapply the deltas
above, run the crate's unit tests, run upstream's `fux-vt/compare` panel
against the result, and record the commit here.
