---
name: Cell
abbrev: Cell
synopsis: what a person operating agents inside the cell gets
level: 1
parent_ids: []
domain: boxer-cell
catalog: boxer
realised_by:
  - adr:0269
  - adr:0307
  - adr:0310
  - pkg:public/keelson/runtime/cell
  - pkg:public/keelson/runtime/agent
  - app:agentconsole
  - app:chat
  - scene:agentconsole-approve
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0269, adr:0307, scene:agentconsole-approve]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [adr:0269, adr:0307, scene:agentconsole-approve]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: innovating
    basis: [adr:0269, adr:0307, scene:agentconsole-approve]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

What boxer does for one person who runs agents against their own data: the agents act through a command and query contract under a grant the person gives, every act is journaled before it happens and audited after, a moderator watches the loop, what leaves the cell passes one choke point under a label, and the person's own attention is a resource the system paces. The customer is the operator, not the app builder, which is why this is a root and not a branch of the toolbelt.

Its subsystems: [[operations/capability]], [[grants-and-consent/capability]], [[journal/capability]], [[moderation/capability]], [[exits-and-labels/capability]], [[evidence-and-audit/capability]], [[profiles-and-surfaces/capability]].

# Mechanisms

The harness profile composes the registry an agent sees; operations are the contract; grants and consent decide what proceeds; the journal and the trail record it; the moderator and the attention model pace it; exits and labels bound what leaves.

# Evidence

The threat model, the decisions numbered from the operations contract onward, and the agent-console and chat scenes.

# Boundaries

One person, their machine, their data: the cell is not a multi-tenant platform, and the threat model says so.
