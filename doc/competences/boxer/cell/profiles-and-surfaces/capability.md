---
name: Profiles and surfaces
abbrev: Prf
synopsis: "what an agent sees of the machine, under one policy"
level: 2
parent_ids:
  - "[[cell/capability]]"
domain: boxer-cell
catalog: boxer
realised_by:
  - adr:0310
  - adr:0335
  - adr:0276
  - adr:0281
  - adr:0284
  - adr:0287
  - adr:0301
  - adr:0297
  - adr:0307
  - pkg:public/keelson/runtime/harness
  - pkg:public/keelson/runtime/capture
  - scene:chat-coordinator-window-tree
  - scene:agentconsole-capture-png
  - env:BOXER_CHAT_PROFILE
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0310, scene:chat-coordinator-window-tree]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [adr:0310, scene:chat-coordinator-window-tree]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: innovating
    basis: [adr:0310, scene:chat-coordinator-window-tree]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A harness profile composes the registry an agent sees; windows are what agents read and arrange; captures — screen, window, pixels — pass one policy point; an overlay shows the person what the agents are doing; the threat model says what all of this defends against.

Its competences: [[harness-profiles]], [[windows-for-agents]], [[captures-under-policy]], [[overlay]], [[threat-model]].

# Mechanisms

Profiles and registry composition; window trees; the capture policy; the overlay.

# Evidence

The coordinator scenes that read window trees and the capture scene under the agent console.

# Boundaries

The surfaces are the cell's own windows; the desktop beyond them is outside the policy.
