---
type: explanation
audience: end-user
status: draft
title: Reading the help corpus
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Reading the help corpus

The Help app is keelson's inline reader for documentation that ships
*inside* an app's binary. There is no separate docs site to keep in
sync — every app's `help/` directory is embedded into the same binary
that runs it, indexed at startup, and surfaced here.

## Layout

The window has two panes:

- **Left nav** lists every app that registered help docs. Click an
  app row to expand it; click a doc row to read it. The `[type]`
  suffix is the doc's Diátaxis quadrant (`explanation`, `how-to`,
  `reference`, `tutorial`).
- **Central reader** renders the selected doc. Headings, code blocks,
  callouts, and inline images all work; wikilinks between docs render
  as clickable links (cross-app jumps land in a follow-up round).

## What you won't find here

- API references. Those live in `pkgsite` — the Go doc comments on
  exported symbols are authoritative.
- ADRs. Architecture decision records live under `doc/adr/` and are
  reviewed before they flip from `proposed` to `accepted`. They are
  developer-facing artefacts and aren't indexed by this reader.
- A search box. The library indexes titles + headings only; full-text
  search is deferred.
