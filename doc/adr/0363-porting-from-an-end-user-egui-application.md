---
type: adr
status: proposed
date: 2026-11-08
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0363: Porting from an end-user egui application

## Context

imzero2 is driven by Go through an opcode stream, and most of what it has
learned about egui it has learned from its own apps. Those apps are used by
the people who build them and by agents, so a class of failure surfaces late
or not at all: what a screen reader announces, what a compositor does to a
window it is not showing, what happens to a text selection when the list
under it scrolls, what a narrow window does to a header. An application
shipped to people who did not build it finds these failures through bug
reports, and the fixes accumulate in its code.

[zapfast](https://github.com/crmne/zapfast) is such an application: a
desktop messaging client written in Rust directly on eframe and egui, MIT
licensed, released for Linux, macOS and Windows. It and the imzero2 host
were both on egui 0.36.2 when this survey was made (2026-11-08, zapfast
commit `421d194fb45530ad0811e2b5d81be8f805c47748`). Both had patched winit
for the same Wayland stall, independently: ADR-0332 (proposed) and zapfast's
egui and winit forks. That overlap is why the comparison was made. The
survey read zapfast's rendering and pacing, text and selection, testing and
accessibility, and UI architecture and dependency handling, and checked each
finding against the imzero2 host, the Go widgets and the scene tooling.

Most of what it found does not transfer: the problems are a desktop client's
(window chrome, colour emoji, right-to-left shaping), or boxer already
solves them another way (headless capture with sidecars, profiling, context
menus opened from raw input). Seven groups do transfer. Each fills a gap
the survey confirmed in boxer on 2026-11-08:

- **Layout checks.** zapfast asserts geometry (controls inside the
  viewport, clear of the caption buttons, a minimum hit size, text not
  clipped) at several window sizes, themes and zoom levels. Boxer's scenes
  write every node's bounds beside each capture (`<name>.tree.jsonl`, the
  [imzero2-drive skill](../skills/imzero2-drive/SKILL.md)), and nothing reads
  them for invariants. A scene has a single `size:`.
- **Accessibility semantics.** zapfast keeps
  [two alternating live regions](https://github.com/crmne/zapfast/blob/421d194fb45530ad0811e2b5d81be8f805c47748/src/announcer.rs)
  so a repeated announcement is still spoken, makes a long list
  [one Tab stop with an active descendant](https://github.com/crmne/zapfast/blob/421d194fb45530ad0811e2b5d81be8f805c47748/src/ui/list_nav.rs),
  and [traces focus](https://github.com/crmne/zapfast/blob/421d194fb45530ad0811e2b5d81be8f805c47748/src/demo/focus_trace.rs)
  through key presses, failing when focus lands on the window root, when one
  key moves it more than once, or when the focused item changes node id. The
  imzero2 host sets no live region, list role or active descendant.
- **Hidden-window pacing.** zapfast's egui fork runs `App::logic` without a
  paint once a redraw is 250 ms overdue, and its winit fork reports the
  Wayland `suspended` state as `Occluded`. Both are submitted upstream
  (emilk/egui#8631, rust-windowing/winit#4709 and #4710). This is ADR-0332's
  O3, rejected there because it means carrying eframe. Boxer's bounded wait
  keeps painting a hidden window at up to 4 Hz.
- **Repaint on demand.** zapfast repaints only on an event or when an
  animation that is on screen is due its next frame, and stops the text
  cursor blinking after idle. ADR-0062 keeps imzero2 continuous by default
  because a visible monitor would throttle under the reactive heartbeat.
  The survey found no way for Go to wake the desktop host between passes.
- **Long transcripts.** zapfast caches each message's measured height,
  estimates the rest, lays out the rows within three viewports either side
  and stands the others in as space, and holds the reading position when history is
  prepended. Boxer's `chatview` draws every message in its window
  (`defaultWindow`, [plan.go](../../public/thestack/imzero2/egui2/widgets/chatview/plan.go)).
- **Selection across a scrolled list.** egui drops a selection once either
  end's text is not drawn in a frame. zapfast keeps off-screen ends
  registered with `visible = false`, widens each message's selection bounds
  to the list's width so a sweep crosses left- and right-aligned bubbles,
  and rewrites copied text in a plugin's `output_hook`
  ([`CopyAnnotator`](https://github.com/crmne/zapfast/blob/421d194fb45530ad0811e2b5d81be8f805c47748/src/transcript.rs)).
  Boxer's selectable labels
  ([linked.rs](../../rust/imzero2/imzero2_egui/src/style/linked.rs),
  [pill.rs](../../rust/imzero2/imzero2_egui/src/style/pill.rs)) take part
  only while visible.
- **Smaller items.** Texture caches that evict by frame count
  (`MAX_AGE_FRAMES` in [image.rs](../../rust/imzero2/src/imzero2/image.rs)
  and [scrolling_texture.rs](../../rust/imzero2/src/imzero2/scrolling_texture.rs)),
  so the age stretches with the cadence; Linux text coverage left at egui's
  dark-mode curve, which draws heavier than GTK; `ui.horizontal` reversing
  inside a right-aligned cell; key matching that ignores Shift and shifted
  symbols; no CI check on where dependencies come from; a shortcut table
  ([shellchords.go](../../public/thestack/imzero2/egui2/keycodes/shellchords.go))
  without descriptions.

The question this ADR answers is the general one: how boxer takes work from
an application of this kind, whose code is shaped by end users rather than
by a platform, and what it owes the source for it.

## Decision

We will port the seven groups above as boxer-shaped reimplementations,
tracked as milestones M1–M7. Every port follows the rules in SD1–SD5. This
ADR records the provenance and licence handling once for all of them. A
milestone that changes an accepted contract gets its own ADR.

### SD1 — Techniques cross, code is reimplemented

What is ported is a technique and the behaviour it produces, re-expressed in
boxer's split between the Rust host, the IDL, Go widgets and the scene
tooling. zapfast's code is written against its own application state
(`App`, `Account`, `Action`) and does not lift out. No zapfast file is
vendored, and boxer takes no dependency on zapfast, its `fastframe-*`
crates, or its egui and winit forks.

### SD2 — Notice is given once, and again where code is close

zapfast is MIT licensed, and its notice is reproduced in
[THIRD_PARTY_NOTICES.md §1.13](../../THIRD_PARTY_NOTICES.md), with this ADR
and the surveyed commit as the provenance. Where a ported function follows
zapfast's code closely enough to be a translation rather than a
reimplementation, the boxer file carries the copyright line and permission
notice in its header and is listed under §1.13. Doing so is the porting
author's call, made when the file is written, and the default when in doubt.
A technique taken from zapfast's documentation or a bug report carries
attribution in its comment or ADR, not the notice.

### SD3 — A port reproduces boxer's failure first

zapfast's fixes answer failures its users reported, on compositors, screen
readers and platforms that boxer does not routinely run. A port starts with
the failure shown in boxer, in a scene, a test or a recorded observation,
and lands with that evidence going green. A group that cannot be reproduced
in boxer is not ported. Its milestone is closed with the reason in an
`Updates` entry.

### SD4 — Upstream first for egui and winit

Where zapfast's fix lives in its egui or winit fork, boxer tracks the
upstream pull request and adopts the change when a release carries it. It
does not track the fork. The pull request is recorded as the drop condition
of the vendored copy it would replace.

### SD5 — What stays out, and why

- **Colour emoji** (a placeholder glyph with the picture painted over it,
  and copy restoring the emoji). It needs a font pipeline and a
  copy-restoration layer, with no boxer app asking for it.
- **Right-to-left and mixed-direction text.** This needs shaping changes
  inside epaint, which the Go side cannot supply. It is revisited if
  upstream egui takes them.
- **Window glass, a custom title bar, an app that outlives its window.**
  Desktop-client concerns; boxer's windows are egui windows on its own
  desktop.
- **Applying view actions after the frame.** Boxer's operations catalog
  already applies agent commands after the frame's write-back (ADR-0269
  §SD4).

### M1 — Layout invariants over scene captures ✓

A checker reads the sidecars of scene captures and fails on:

- an interactive node outside the window, on the horizontal axis;
- overlapping enabled siblings;
- an interactive node below a minimum hit size, 14 points, the smallest
  control egui itself draws;
- text egui cut off with an ellipsis.

The first three read the `.tree.jsonl` sidecar. The fourth reads the SVG
sidecar: a label's accessible text is its full text even when egui draws it
elided, so the tree never shows the cut. Tree bounds are not clipped by
scrolling, so a row scrolled below the fold cannot be told from overflow
on the vertical axis, and content under a fixed control or inside an egui
`Scene` can report a false overlap; the host exporting clipped rects would
close that gap.

A known exception is recorded in the scene, never in the checker. Scenes
also gain a matrix axis that runs the same trace at several sizes and, at
launch, in light and dark (ADR-0258 fixes the theme at launch).

Overlap is advisory: it is reported beside a scene's result and fails
nothing, because unclipped bounds make it report content scrolled under a
fixed control. Two nodes with the same rect are one control listed twice
and are not an overlap. The first run over the scenes that capture a tree
(2026-11-08) found the status bar's codec indicator past the right edge at
any width under about 1700 points, frameless glyph buttons 13 points wide,
and one-letter table headers 7.8 points wide; those are fixed. It also
found that an app whose control rows are laid out on one line (play,
mdedit) overflows a window narrower than the rows, and the window clips
them. Wrapping those rows did not hold: a wrapped row is laid out at the
width the window's content had, so it wraps past the window's edge. That
is open, and the two scenes that show it carry exceptions naming it.

### M2 — Accessibility semantics in the IDL ✓

The IDL gains three things:

- a role on accessible regions;
- a live-region opcode with polite and assertive slots, alternating between
  two nodes and marking a repeat so it is announced again;
- list semantics: a list role, the active descendant, and each row's
  position in the set.

`imzero2 drive` shows live-region text, so `wait` and `read` can key on an
announcement. A focus-trace check applies zapfast's three failure rules
to a key step that asks for it (`focusTrace`). The rules run over trees
sampled while the step settles, so a focus flicker shorter than the
sampling interval is not seen.

The role on accessible regions is not added: no painter-drawn widget in
boxer was found to need a role other than the region's. The playground's
settled query is the first announcement.

### M3 — Hidden-window pacing tracks upstream ✓

[vendor/winit/VENDORING.md](../../rust/imzero2/vendor/winit/VENDORING.md)
records emilk/egui#8631 and rust-windowing/winit#4709/#4710 as the
condition for dropping the copy. ADR-0332 (proposed) records that its O3
kill-reason weakens once those land. When a release carries them, the host
runs logic without painting while hidden, and consumes `Occluded` under
ADR-0062 §SD6.

### M4 — A wake path from Go, then repaint on demand ✓

Go gains a way to wake the desktop host outside the lock-stepped frame, so
data arriving between passes is drawn without a heartbeat. The channel is a
FIFO the host creates and names to its client in `IMZERO2_WAKE_FIFO`; the
client reads it on a thread of its own, without `unsafe`, and requests a
repaint per read. Where FIFOs do not exist (Windows) there is no wake and
the cadence is unchanged. Code anywhere in the Go process calls
`hostwake.Wake`, which never blocks and coalesces; the operations engine
calls it when it queues a command, so an agent's command no longer waits
for the next pass. Host-side motion
asks only for its next due frame and only while drawn, and the text cursor
stops blinking after idle. Whether repaint on demand becomes the default is
a successor to ADR-0062, decided on measurement once the wake path exists.

### M5 — Height-cached virtualization for transcripts ✓

`chatview` keeps a measured height per message key and an estimate for
rows not yet measured. It lays out only the rows near the viewport and
stands the rest in as space. The host pins egui to one pass per frame (see
`max_passes` in [apphost.rs](../../rust/imzero2/src/imzero2/apphost.rs)),
so zapfast's discard-and-redo is unavailable. A height change above the
viewport shows for one frame; the next frame takes the difference out of
the space above the first laid-out row, so the content is back in place,
and asks for the scroll, which lands the frame after. Rows not laid out
are not in the accessibility tree, so a scene reaches a far message by
scrolling to it. `uiScrollWithDelta` is the relative scroll op that would
replace the absolute correction `chatview` builds from `ScrollToCursor`.

### M6 — Selection survives scrolling ✓

Selectable host widgets stay registered with egui's selection while
off-screen and selected. An option widens a label's selection bounds to its
container, so sweeps cross aligned columns; egui needs it only for a drag
that ends outside the labels it passes. Neither helps a row a virtualized
list does not draw at all (M5).

Rewriting copied text in a host plugin's `output_hook` is not ported: no
boxer app copies across rows in a way that loses something, so under SD3
there is no failure to reproduce. The seam is the place for it when one
appears.

### M7 — Smaller items ✓

- Texture caches evict by wall-clock age or a byte budget, and a re-upload
  of the same size reuses its handle (`TextureHandle::set`).
- Linear text coverage on Linux.
- An explicit left-to-right option on the `Horizontal` opcode.
- An egui-pitfalls section in the imzero2 skill: key matching ignoring
  Shift, shifted symbols, `Align::Center` claiming full height, rows
  reversing in right-aligned containers.
- A CI check allowing only listed dependency sources and `[patch]` targets
  in the Rust manifests.
- Descriptions on shell chord rows, rendered in the help book as a page
  generated from the table and compared with it by a test.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| IDL (`definition/`) | added: `announce`, `accessibleList`, `accessibleListItem` (M2); `horizontalLeftToRight` (M7) | regenerated bindings and `interpreter.rs` dispatch; the browser host digest (`tabhost/browserhost.sum`) |
| IDL (`definition/`) | added: `uiScrollWithDelta` (M5); `selectionColumn` on `labelAtomsLinked` (M6) | as above |
| `TreeNode.flags` on the carrier wire | added: bit 16, a live region (M2) | `treemap.rs`, the Go `Flag*` constants, the drive tree's `live` flag |
| Drive trace step | added: `focusTrace` on `key` (M2) | the [imzero2-drive skill](../skills/imzero2-drive/SKILL.md) |
| Scene document | added: matrix axis; layout-check exceptions (M1) | scene runner, ADR-0248 |
| Go↔host wake (M4) | added: `IMZERO2_WAKE_FIFO`, `hostwake` | desktop host's wake thread; the application launcher; ADR-0062 successor |
| `chatview` state | reshaped: height cache (M5) | chat apps that persist `State` |
| Help corpus | added: the desktop-keys page, generated from `keycodes.ShellChords` (M7) | the drift test beside the table |
| Lint steps | added: `rust-dep-sources` (M7) | `scripts/ci/lint.sh` |
| THIRD_PARTY_NOTICES §1.13 | added | header notice in any file SD2 marks as a translation |

## Alternatives

- **Depend on zapfast's `fastframe-*` crates or its egui fork.** The crates
  are desktop-client infrastructure built for a family of sibling apps, and
  a forked egui would pin every member of imzero2's egui ring (egui_table,
  egui_dock, egui-wgpu) to another project's revision. The host also builds
  for the browser and headless, which they do not target.
- **Vendor the relevant zapfast files.** They depend on zapfast's
  application types throughout. Detaching them is the reimplementation SD1
  calls for, with a vendored copy left to keep in sync on top.
- **One ADR per group.** The groups share provenance, licence handling and
  the reproduce-first rule, and recording those seven times would let them
  drift. Groups that change an accepted contract (M4's default cadence) still
  get their own ADR.
- **Leave it to boxer's own bug reports.** Several of these failures (screen
  reader announcements, selection lost on scroll, focus landing on the root)
  are invisible to the people who build boxer's apps and to agents reading
  the tree, so they would not be reported.

## Consequences

### Positive

- The scene captures already written become a layout regression lane at no
  extra capture cost (M1).
- Announcements and list positions reach screen readers and `drive` from
  the same nodes, so an agent can wait on what a person would hear (M2).
- Long transcripts cost per frame what is on screen, not what is in the
  window (M5).

### Negative

- New IDL surface (M2, M7), each opcode a contract kept across egui bumps.
- A height cache and a one-frame-late correction are state a Go widget must
  keep right. A wrong estimate shows as a one-frame jump that zapfast, with
  multipass, does not have (M5).
- zapfast moves fast. A technique ported from a commit can be revised
  upstream, and boxer does not follow it after the port.

### Neutral

- The licence obligation is met by §1.13 whether or not any file ends up
  close enough to need a header.
- M3 may close with nothing ported if upstream declines the pull requests.
  ADR-0332's bounded wait then stands.

## Migration — Tier 1

Nothing to migrate. Every surface above is additive. `chatview` state
gains fields with zero values that mean "not measured".

## Verification plan — Tier 1

- **Lane.** M1 runs in the scene lane (`//go:build integration`) over every
  capture with a tree sidecar. M2: a Rust unit test reading
  `accesskit_update` for each new node kind, plus a scene that waits on an
  announcement and runs the focus trace. M5 and M6: Go unit tests on the
  height cache, and a scene that selects text, scrolls it off screen and
  copies. M7's dependency-source check runs in the default CI lane.
- **What would fail.** A layout invariant broken in any captured state. A
  live region, active descendant or position missing from the tree. A
  selection dropped after scrolling. A git source or `[patch]` entry
  outside the allowlist.
- **Gap.** Real screen-reader speech is not tested, only the AccessKit tree
  it reads. Hidden-window pacing (M3, M4) needs a compositor and is checked
  by ADR-0332's manual hidden-window procedure.

## Status

Proposed — awaiting review by @spx.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- zapfast, at the surveyed commit:
  <https://github.com/crmne/zapfast/tree/421d194fb45530ad0811e2b5d81be8f805c47748>
  (MIT; notice in [THIRD_PARTY_NOTICES.md §1.13](../../THIRD_PARTY_NOTICES.md)).
- Upstream pull requests tracked under M3: emilk/egui#8631,
  rust-windowing/winit#4709, rust-windowing/winit#4710.
- ADR-0062 (render cadence), ADR-0248 (scenes), ADR-0258 (theme at launch),
  ADR-0267 (Go widget contract), ADR-0269 (operations), ADR-0332 (proposed;
  hidden desktop host).
