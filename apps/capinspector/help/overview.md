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

The view is read-only — switching the active backend happens at
process startup via the runtime configuration, not from here.

## Audit counters and sparklines

The bottom strip shows recent traffic per capability, sourced from
`boxer.facts`. A flat line is no recent activity; a busy
sparkline means the runtime is exercising the capability heavily.
Useful as a sanity check that the chosen backend is actually being
exercised.
