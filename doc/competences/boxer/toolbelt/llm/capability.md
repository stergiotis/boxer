---
name: Model inference
abbrev: LLM
synopsis: "a model call as a capability, with retained conversations"
level: 2
parent_ids:
  - "[[toolbelt/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0254
  - adr:0264
  - adr:0265
  - adr:0286
  - adr:0300
  - pkg:public/llm/openaichat
  - pkg:public/keelson/runtime/llm
  - app:chat
  - scene:chat-trail
  - env:BOXER_LLM_ENDPOINT
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0254, app:chat, scene:chat-trail]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: product
    basis: [adr:0254, app:chat, scene:chat-trail]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [adr:0254, app:chat, scene:chat-trail]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Calling a language model as a capability the runtime grants, retaining the conversation as a trail a person can read back, streaming the answer, and metering what each call cost. The customer is someone who wants a model in their app under the same audit and grant rules as every other effect.

Its competences: [[inference-as-a-capability]], [[retained-conversations]], [[streamed-answers]], [[metering-and-moderator-rules]].

# Mechanisms

One completion client; a capability per call; conversations retained as trail rows; the chat app over them; moderator rules on what a call may do.

# Evidence

The chat app's scenes; the inference decision and the chat decision.

# Boundaries

A page the positioning README defers; not a model host and not a prompt library.
