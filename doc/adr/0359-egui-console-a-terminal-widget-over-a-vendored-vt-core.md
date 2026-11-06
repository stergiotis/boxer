---
type: adr
status: proposed
date: 2026-11-06
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0359: egui-console — a terminal widget over a vendored VT core

## Context

A terminal emulator as an imzero2 app needs three things: a pseudo-terminal
and the process behind it, a VT core that turns the program's output into a
screen, and a view that draws the screen and turns keys into the bytes the
program expects. A downstream application wants such a terminal with its
sessions audited: every byte in both directions recorded, attributed (typed,
pasted, or answered by the terminal itself) and replayable.

The process side belongs in Go, where the session, its policy and its record
live. The question is where the core runs. A Go core would put the screen
next to the recorder, but the whole grid would then cross the FFFI pipe every
frame, and Go would need raw key events, which imzero2 does not deliver: the
host turns command-C and command-X into `Copy` and `Cut` events, swallows
shell chords before any widget sees them, and moves focus on Tab and the
arrows. A core inside the Rust client sees egui's events itself, keeps the
grid off the pipe (only new output bytes cross), and builds for the browser
tab.

The candidates for that core were surveyed and measured in a downstream
design note; the measurements that bear on this decision are summarised
here. `fux-vt` (MIT, no runtime dependencies, no `unsafe`, about 14,000
lines of code) passes 80.7 % of xterm's esctest2 against a committed list of
expected failures, agrees with a panel of nine other engines (Ghostty's core,
Alacritty, libvterm, avt, wezterm, vt100, xterm.js, tmux, real xterm) on the
recorded sessions of real programs it was compared on, builds for
`wasm32-unknown-unknown`, gives the same screen however its input is
chunked, and carries key, paste, focus and mouse encoders. Its risks are
age (two months), one maintainer, and a Rust 1.95 floor.
`alacritty_terminal` is the fallback: mature, but without grapheme clusters,
with mode 2026 ended by the wall clock, and with its encoder in the
application rather than the library.

## Decision

We will add a `console` widget to imzero2: the VT core runs in the Rust
client, Go feeds it the program's output and receives everything the program
should be sent. The core is `fux-vt`, vendored from a reviewed upstream
commit under `rust/imzero2/vendor/fux-vt`.

### Subsidiary design decisions

- **SD1 — Go owns every byte in both directions.** The widget never talks to
  a process. Go feeds output bytes in; the widget returns, as rows tagged by
  kind, the bytes for the program: `key` (typed), `paste`, `focus` (focus
  reports), `reply` (answers the core produced to the program's queries). A
  reply is output the terminal made, not something a person typed, and the
  tag keeps a recorder able to say so. The screen is a projection of the fed
  bytes; a replay rebuilds it by feeding them again.

- **SD2 — Feeding is offset-addressed and acknowledged.** Each frame Go sends
  the bytes from the widget's last acknowledged offset with that offset; the
  widget applies the part past what it has, ignores the overlap, and reports
  its new total. Go keeps unacknowledged bytes until the report arrives. A
  body in a hidden dock tab is discarded by the host (the "lost sends"
  pitfall); offset and acknowledgement make the feed survive that without a
  receipt protocol of its own. A gap (the widget's state was evicted) is
  reported, not papered over.

- **SD3 — Results come back through one register.** One fetcher returns rows
  of (widget id, kind, payload): the input kinds of SD1, `size` (columns and
  rows, sent when they change, for `TIOCSWINSZ`), `ack` (SD2), `title`,
  `bell`, `clipboard` (an OSC 52 set request, for policy to decide),
  `notification`, `command started` and `command ended` with its status (OSC
  133, for a recorder to segment a session by command), and `gap`. One
  register keeps the order between kinds within a frame, which a recorder
  needs; the StateManager groups it per widget, order kept.

- **SD4 — The widget takes keyboard focus and keeps the keys.** A click
  focuses it; while focused it locks focus against Tab, the arrows and
  Escape, and consumes the text, key and paste events it encodes, so
  nothing behind it reacts. Encoding follows the mode the program set
  (cursor keys, keypad, modifyOtherKeys, the kitty keyboard protocol,
  bracketed paste), which is the core's encoder's job, not the widget's.
  Host shell chords are drained before any widget, so they never reach a
  console; that is the host's precedence and this decision keeps it.

- **SD5 — The size follows the widget's rect.** The widget takes the room
  Go gives it in points (or what the parent has left), computes columns and
  rows from the monospace cell, resizes the core and reports the size (SD3).
  Go does not choose columns and rows. A grid is capped at 1,000 by 1,000
  cells, since an unbounded parent offers more room than any screen holds.

- **SD6 — The core sits behind a small seam.** `console.rs` talks to the
  core through a handful of operations: feed, resize, read a row's cells,
  read the cursor and modes, encode a key or a paste. Switching to the
  fallback core stays inside the widget; the Go API does not change.

- **SD7 — The vendored core is pinned to a commit and trimmed.** Upstream
  releases reach boxer only when someone merges one deliberately.
  `VENDORING.md` records the commit and what was cut. Features the widget
  does not use are removed: sixel and picture placement, and the decoder of
  a client terminal's raw input, which `fux` needs as a multiplexer and the
  widget does not, since it receives egui's events. Changes to the vendored
  tree are checked with upstream's own differential harness
  (`fux-vt/compare`) before they are merged.

- **SD8 — boxer's declared Rust floor rises to 1.95.** `fux-vt` uses
  language features from 1.95; patching them out would be a fork for no
  gain. The pinned toolchain (1.96.1) already satisfies it.

- **SD9 — One accessibility node; the rest deferred.** The widget is one
  accessibility node named `terminal` whose value is the visible screen's
  text, built only when the host asks for the tree; headless scenes type
  into it and read the screen through it. Deferred: selection and copy,
  mouse reporting, viewing the scrollback, OSC 8 links, a fetch of the
  screen for Go, rows as nodes of their own, the host-held frames of mode
  2026, and a viewer-only mode for the browser tab. Each is additive on
  SD3's register and the widget's state.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| egui2 IDL | added: `console` and `consoleRelease` builder nodes, `fetchR29Console` fetcher | regenerated bindings, `enums_out.rs`, the dispatch region of `interpreter.rs`, `IDL_FINGERPRINT`, the API reference |
| `StateManager` | added: console rows split per widget, with getters | `Sync` issue and collect order; scripted-input reset |
| `widgets/console` | added: a semi-retained widget (`New`, `Write`, `Render`, `RenderFill`, `Close`) holding output until acknowledged | the gallery demo and its scene |
| `rust/imzero2/Cargo.toml` | `rust-version` 1.94 → 1.95; path dependency on the vendored core | `Cargo.lock`; the browser host digest |
| `THIRD_PARTY_NOTICES.md` | added: fux-vt (MIT) | — |

## Alternatives

- **A Go core, the grid crossing FFFI.** The screen would sit next to the
  recorder, but every frame would carry the grid, Go would need a raw key
  lane the host does not provide, and neither Go core surveyed has a modern
  key encoder and grapheme clusters together.
- **libghostty-vt as wasm in wazero, or through cgo.** cgo is excluded by
  the CGO-free build; the wasm build brings Zig into the build and an API
  its authors call unstable.
- **`alacritty_terminal`, trimmed.** Kept as the fallback behind SD6:
  grapheme clusters are a structural change to its cell, and its own tests
  are a thin net for it.
- **`fux-vt` from crates.io.** The published release lacks the fix that lets
  it build for 32-bit targets and the browser, and trails the repository;
  vendoring a reviewed commit is what makes upstream changes deliberate.

## Consequences

### Positive

- A terminal costs the pipe only the bytes it shows, and the screen is
  rebuilt from the same bytes a recorder keeps.
- The key encoding (modifyOtherKeys, the kitty protocol, bracketed paste)
  is one implementation, conformance-tested upstream, not a table in Go.

### Negative

- About 14,000 lines of third-party Rust become boxer's to review and
  maintain, minus what SD7 cuts.
- Every client binary grows by the core.
- A program in the terminal can make the widget reply on its own (device
  attributes, mode reports, cursor position); Go sees those replies but
  does not choose them.

### Neutral

- The screen state lives in the client, so Go reads it only through a
  fetch, one frame late (deferred, SD9).

## Updates

### 2026-11-06 — §SD9, §SD3: the screen comes back as a row

Go reads the screen, which SD9 deferred, through SD3's register rather
than a fetcher of its own. After the screen changes (bytes fed, a resize),
a console adds a row of kind 25 (`screen`): the stream offset it shows,
the grid's size, the cursor and the screen's text, as the accessibility
node carries it. It sends at most one every 100 ms; while a change waits,
it asks egui for a repaint when the interval ends. `ConsoleRow.Screen`
decodes the row, and `Console.Screen` keeps the last one for a reader
that is not a person, such as an agent's operation or a test. Because
there is no request, nothing in the IDL changes, and a console that no one
reads still pays for one text copy per change, ten a second at most.
Selection, the mouse, scrollback, links and rows as nodes stay deferred.
