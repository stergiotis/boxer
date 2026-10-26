---
name: Distribution and hosts
abbrev: Dist
synopsis: "reproducible builds, an air-gapped bundle, an appliance image"
level: 2
parent_ids:
  - "[[toolbelt/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0095
  - adr:0206
  - adr:0215
  - adr:0199
  - adr:0278
  - howto:airgapped-build
  - howto:release-signing
  - pkg:public/thestack/cmd/imzero2tab
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0215, howto:airgapped-build, howto:release-signing]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0215, howto:airgapped-build, howto:release-signing]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0215, howto:airgapped-build, howto:release-signing]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Getting boxer onto a machine: a byte-reproducible build, a bundle that installs with no network, an appliance image that boots into it, a signed release, and the tab host whose digest is checked. The customer is someone who deploys where the network is not trusted.

Its competences: [[reproducible-builds]], [[air-gapped-bundle]], [[appliance-image]], [[release-signing]], [[tab-host-digest]].

# Mechanisms

Two files hold every build flag; the bundle carries toolchain and modules; the appliance image is built from it; the release is signed; the tab host's digest is recorded.

# Evidence

The air-gapped build and release-signing how-tos.

# Boundaries

Not a package manager: one binary, one bundle, one image.
