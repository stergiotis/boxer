---
name: Hosts
level: 3
parent_ids:
  - "[[imzero2/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0024
  - adr:0154
  - adr:0205
  - adr:0206
  - adr:0243
  - adr:0263
  - adr:0278
  - pkg:public/thestack/imzero2/host
  - pkg:public/thestack/imzero2/browserhost
  - howto:imzero2-in-the-browser
  - howto:launch-apps-non-interactively
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0154, adr:0278, howto:imzero2-in-the-browser]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0154, adr:0278, howto:imzero2-in-the-browser]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [adr:0154, adr:0278, howto:imzero2-in-the-browser]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

The same frame renders on a desktop window, in a browser tab, headless with an accessibility tree, on a CPU rasteriser, on an appliance image and in native windows; the host decides where pixels go and nothing above it changes.
