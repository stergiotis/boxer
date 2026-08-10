---
type: explanation
audience: operator
status: draft
title: Inspecting capabilities
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Inspecting capabilities

The Capability Inspector is keelson's introspection app for the
runtime's capability subjects (ADR-0026). It reads
`app.DefaultRegistry` in-process — no IPC, no bus — and renders one
view per registered capability.

## Picker

The horizontal row at the top is the capability picker. Each chip is
a registered capability id (`boxer.facts`, `runtime.bus`,
`runtime.fs`, `runtime.task`, …). Click a chip to switch the detail
view; the active chip carries the accent fill.

## Detail view

For the selected capability:

- **Active backend** — the implementation currently servicing the
  capability (e.g. `chstore` for `boxer.facts`, `inprocbus` for
  `runtime.bus`).
- **Schematic** — a small canvas showing producers and consumers,
  drawn with the IDS palette so callers stand out.
- **Prose** — a short description of what the capability promises
  and what guarantees the active backend honours.
- **Storage schema** — for a capability whose backend persists into a
  table, a collapsed section at the foot of the page: the leeway
  schema of that table as a section navigator beside a decoded
  property pane (canonical type, encoding hints, value semantics,
  membership spec). `boxer.facts` is the only such table today; the
  `?` in the navigator header opens the glyph legend.

The schema shown is the one the code declares, not a live
`DESCRIBE TABLE` — no database is contacted to render it, so it reads
the same when the facts backend has fallen back to the in-memory
store.

The view is read-only — switching the active backend happens at
process startup via the runtime configuration, not from here.

## Audit counters and sparklines

The bottom strip shows recent traffic per capability, sourced from
`boxer.facts`. A flat line is no recent activity; a busy
sparkline means the runtime is exercising the capability heavily.
Useful as a sanity check that the chosen backend is actually being
exercised.
