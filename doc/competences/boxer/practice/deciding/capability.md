---
name: Deciding
abbrev: Dec
synopsis: "decision records, surveys, the QOC form"
level: 2
parent_ids:
  - "[[practice/capability]]"
domain: boxer-practice
catalog: boxer
realised_by:
  - adr:0001
  - adr:0092
  - adr:0119
  - howto:adr-overview
  - applet:adr-timeline
  - table:keelson/adr
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0001, howto:adr-overview]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0001, howto:adr-overview]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0001, howto:adr-overview]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A decision is a record with a status, a context, a design space and consequences; a non-trivial one is preceded by a background survey; a proposed record is a living snapshot and an accepted one changes only by dated entries.

Its competences: [[decision-records]], [[background-surveys]].

# Mechanisms

The ADR template and its tiers; the QOC form; the background-work directory; the clean-room bar for surveys.

# Evidence

The ADR corpus served as tables, and the ADR-overview how-to.

# Boundaries

A decision records why; the tree carries what. A record that transcribes the code goes false in weeks.
