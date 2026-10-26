---
name: Journal
abbrev: Jnl
synopsis: a run written down before each act
level: 2
parent_ids:
  - "[[cell/capability]]"
domain: boxer-cell
catalog: boxer
realised_by:
  - adr:0308
  - adr:0325
  - adr:0309
  - adr:0316
  - adr:0320
  - pkg:public/keelson/runtime/agentrun
  - pkg:public/keelson/runtime/inscribe
  - scene:chat-trail
  - scene:chat-inscribe
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0308, scene:chat-trail]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [adr:0308, scene:chat-trail]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: innovating
    basis: [adr:0308, scene:chat-trail]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A conversation outlives its process because a journal records each act before it happens; the context an agent sees is a projection written as SQL; compaction is an entry like any other; bodies live in a vault under per-entry keys.

Its competences: [[run-journal]], [[context-projection]], [[compaction-entries]], [[vault-bodies]].

# Mechanisms

The run journal and its store; context projection as SQL; compaction entries; the vault for bodies.

# Evidence

The chat trail and inscribe scenes, and the run-journal decisions.

# Boundaries

The journal is the agent's record; the audit trail is the system's. They share rows and differ in who writes.
