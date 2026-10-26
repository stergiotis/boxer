---
name: "Gauge, spectrum and audio"
level: 3
parent_ids:
  - "[[widgets/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0068
  - adr:0091
  - adr:0208
  - pkg:public/thestack/imzero2/egui2/widgets/gauge
  - pkg:public/thestack/imzero2/egui2/widgets/spectrumdisplay
  - pkg:public/thestack/imzero2/egui2/widgets/waveform
  - app:tally
  - scene:tally-audio
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0208, app:tally, scene:tally-audio]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0208, app:tally, scene:tally-audio]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: innovating
    basis: [adr:0208, app:tally, scene:tally-audio]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A gauge for a scalar, a spectrum display and a waveform for audio, with peaks cached for long recordings.
