---
name: The competence corpus
level: 3
parent_ids:
  - "[[gov/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0168
  - adr:0337
  - pkg:public/gov/capmapcorpus
  - pkg:public/gov/capmapanchors
  - pkg:public/gov/capmapfacts
  - applet:comp-overview
  - applet:comp-lint
  - scene:comp-browser
  - table:keelson/competence
  - table:keelson/competenceclaim
  - env:BOXER_CAPMAP_VAULT
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0168, adr:0337, applet:comp-overview, scene:comp-browser]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0168, adr:0337, applet:comp-overview]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [adr:0168, adr:0337, applet:comp-overview]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

This catalog: competences anchored to the tree, assessed by claims with a basis, served as tables beside the evidence the tree carries for them.
