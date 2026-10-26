---
name: Grants and consent
abbrev: Gr
synopsis: "what the person allowed, asked at the right time"
level: 2
parent_ids:
  - "[[cell/capability]]"
domain: boxer-cell
catalog: boxer
realised_by:
  - adr:0324
  - adr:0328
  - adr:0280
  - adr:0298
  - pkg:public/keelson/runtime/agent
  - app:agentconsole
  - scene:agentconsole-approve
  - scene:chat-afk-grant-expiry
  - env:BOXER_AGENT_UNATTENDED
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0324, adr:0280, scene:agentconsole-approve]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [adr:0324, adr:0280, scene:agentconsole-approve]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: innovating
    basis: [adr:0324, adr:0280, scene:agentconsole-approve]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

An agent proceeds only under a grant; what needs a decision queues for the person, status says what is open, a ceiling scored on a ladder bounds what a conversation's model may do, and an unattended mode proceeds within stated bounds when the person is away.

Its competences: [[decisions-queue]], [[status-and-what-opens]], [[ceiling-on-a-ladder]], [[unattended-mode]].

# Mechanisms

A decisions queue; a status surface; the ladder; the unattended bounds and the away-idle attention states.

# Evidence

The approval and grant-expiry scenes under the agent console and the chat app.

# Boundaries

A grant is the person's: nothing here grants on their behalf, and the unattended mode is bounded by what they set before leaving.
