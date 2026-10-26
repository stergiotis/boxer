---
name: Exits and labels
abbrev: Ex
synopsis: "what may leave the cell, under which label"
level: 2
parent_ids:
  - "[[cell/capability]]"
domain: boxer-cell
catalog: boxer
realised_by:
  - adr:0312
  - adr:0317
  - adr:0311
  - adr:0307
  - pkg:public/keelson/runtime/httpegress
  - pkg:public/keelson/runtime/clipboardbroker
  - pkg:public/keelson/runtime/cell
  - env:BOXER_CELL_EGRESS_CLEARANCE
  - env:BOXER_CELL_EXPORT_CLEARANCE
assessment:
  - dimension: readiness
    rating: 3
    basis: [adr:0312, adr:0317, adr:0311, adr:0307, pkg:public/keelson/runtime/httpegress, pkg:public/keelson/runtime/clipboardbroker, pkg:public/keelson/runtime/cell, env:BOXER_CELL_EGRESS_CLEARANCE, env:BOXER_CELL_EXPORT_CLEARANCE]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [adr:0312, adr:0317, adr:0311]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: innovating
    basis: [adr:0312, adr:0317, adr:0311]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Anything that leaves the cell — a network call, an export, a clipboard copy, a route to a peer — passes an exit with a sink, an approval tier and a budget; records carry labels, integrity levels and sharing tiers that the exit checks.

Its competences: [[exits-and-budgets]], [[copy-by-reference]], [[labels-and-tiers]].

# Mechanisms

Exits and sinks; approval tiers and budgets; copy by reference through one choke point; labels and tiers on records; clearances from the environment.

# Evidence

The threat model and the exits decision; the cell's clearance variables.

# Boundaries

An exit bounds what the cell lets out; it does not inspect what a peer does with it.
