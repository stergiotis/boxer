---
name: SQL applets
abbrev: SA
synopsis: a document with a query is an app
level: 2
parent_ids:
  - "[[toolbelt/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0132
  - adr:0274
  - adr:0299
  - adr:0240
  - adr:0288
  - applet:docs-search
  - app:sqlappletcreator
  - howto:sqlapplet-authoring
  - howto:adhoc-datasets
  - applet:docs-search
  - scene:comp-browser
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0132, howto:sqlapplet-authoring, scene:comp-browser]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0132, howto:sqlapplet-authoring, scene:comp-browser]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [adr:0132, howto:sqlapplet-authoring, scene:comp-browser]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Publishing a query, its prose and its knobs as a small app a person launches by name — a book of them per corpus — and handing an app's computed table to SQL without storing it. The customer is someone who wants a lens over their data to be a markdown file rather than a build.

Its competences: [[sql-defined-applets]], [[agent-operable-applets]], [[applets-from-an-origin]], [[adhoc-datasets]].

# Mechanisms

A book is a directory of markdown applets with a front-matter stanza; minting registers each as an app under one id prefix; ad-hoc datasets publish an Arrow table to SQL under a sealed capability; the creator app writes a new applet.

# Evidence

The authoring how-to, the ad-hoc datasets how-to, and the applet books' corpus gates.

# Boundaries

A page the positioning README defers: today a clause of play's positioning, with a customer of its own once the agent-operable windows land.
