---
name: Evidence and audit
abbrev: Ev
synopsis: "one trail for what the models and agents did, tamper-evident"
level: 2
parent_ids:
  - "[[cell/capability]]"
domain: boxer-cell
catalog: boxer
realised_by:
  - adr:0277
  - adr:0314
  - adr:0327
  - adr:0330
  - pkg:public/keelson/runtime/trail
  - pkg:public/keelson/runtime/audit
  - pkg:public/storage/recordstore/evidence
  - scene:chat-trail
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0277, adr:0327, scene:chat-trail]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0277, adr:0327, scene:chat-trail]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0277, adr:0327, scene:chat-trail]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Everything a model or an agent did is on one audit trail, the trail's records are tamper-evident through the record stores' evidence profile, and one recorder is shared by every writer.

Its competences: [[one-audit-trail]], [[tamper-evident-records]], [[shared-recorder]].

# Mechanisms

The audit trail's components; the evidence profile — chain, vault, marks, seals, anchors; the shared recorder.

# Evidence

The trail scene, the audit decisions, and the evidence-profile decision.

# Boundaries

The trail records; it does not judge. What to do about a record is the person's or the moderator's.
