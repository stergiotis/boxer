---
name: UI stack
abbrev: IZ
synopsis: "a framed render pipeline over egui, with hosts for desktop, browser and headless"
level: 2
parent_ids:
  - "[[toolbelt/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - positioning:imzero2
  - adr:0049
  - adr:0062
  - adr:0154
  - adr:0248
  - adr:0278
  - pkg:public/thestack/imzero2
  - skill:imzero2
  - skill:imzero2-drive
  - howto:imzero2-in-the-browser
  - howto:imzero2-render-troubleshooting
  - trial:keelson-wasm-frame-cost
  - scene:jackstay
assessment:
  - dimension: readiness
    rating: 8
    basis: [positioning:imzero2, skill:imzero2, howto:imzero2-in-the-browser, trial:keelson-wasm-frame-cost]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [positioning:imzero2, skill:imzero2, howto:imzero2-in-the-browser]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [positioning:imzero2, skill:imzero2, howto:imzero2-in-the-browser]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Drawing an app's interface from Go through a frame-oriented pipeline into an egui host — on a desktop window, in a browser tab, headless with an accessibility tree a driver can read. The customer is someone who writes their interface in Go and wants it to run wherever the host runs, with a design system already applied.

Its competences: [[framed-render-pipeline]], [[widget-contracts]], [[design-system]], [[hosts]], [[remote-sessions]], [[headless-driver-and-scenes]], [[interaction-replay]], [[window-arrangement-and-captures]], [[keyboard-capture]], [[id-stack]].

# Mechanisms

Widgets emit into a frame the host renders; the IDL generates the dispatch on both sides; a deterministic id stack scopes every widget; the design system is policy as code; hosts differ in where the pixels go and the driver reads the tree the headless one exposes.

# Evidence

The positioning page, the imzero2 skill and the drive skill, the browser how-to, the render troubleshooting how-to, and the frame-cost trials.

# Boundaries

Not a web framework: the browser is one host among several, and a page is a frame, not a document.
