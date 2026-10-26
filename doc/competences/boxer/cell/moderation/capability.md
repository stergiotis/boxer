---
name: Moderation
abbrev: Mod
synopsis: a built-in loop that watches the agents and paces the person
level: 2
parent_ids:
  - "[[cell/capability]]"
domain: boxer-cell
catalog: boxer
realised_by:
  - adr:0302
  - adr:0323
  - adr:0300
  - pkg:public/keelson/runtime/moderator
  - pkg:public/keelson/runtime/attention
  - scene:chat-urgent
  - scene:chat-watch
  - env:BOXER_MODERATOR
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0323, scene:chat-urgent]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [adr:0323, scene:chat-urgent]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: innovating
    basis: [adr:0323, scene:chat-urgent]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A moderator watches the agent loop and escalates; attention levels pace what reaches the person; calls are metered and a ration's rules decide what proceeds.

Its competences: [[moderator-loop]], [[attention-levels]], [[metered-calls]].

# Mechanisms

The moderator loop; attention levels with flood control; metering and rules.

# Evidence

The urgent, watch and statistics scenes under the chat app.

# Boundaries

The moderator is a loop in the cell, not a service outside it; it escalates to the person and to no one else.
