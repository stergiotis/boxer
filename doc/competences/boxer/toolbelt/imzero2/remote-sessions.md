---
name: Remote sessions and viewers
level: 3
parent_ids:
  - "[[imzero2/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0082
  - adr:0086
  - adr:0088
  - adr:0242
  - pkg:public/thestack/imzero2/carrierclient
  - pkg:public/thestack/imzero2/videopipeline
  - howto:amd-hardware-video-encoding
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0086, howto:amd-hardware-video-encoding]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0086, howto:amd-hardware-video-encoding]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: innovating
    basis: [adr:0086, howto:amd-hardware-video-encoding]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A running app is viewed from elsewhere — a carrier client, a video pipeline, a viewer fixture — without the app knowing it is remote.
