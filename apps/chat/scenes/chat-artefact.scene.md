---
type: reference
audience: contributor
status: draft
scene:
  launch: chat
  size: 1600x900
  env:
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-artefact.script.jsonl"
    BOXER_CHAT_ARTEFACT: "true"
    BOXER_CHAT_DRAFT: "Draft a plan for the artefact feature."
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — the artefact, under Ask first

ADR-0282 on the headless host, against the scripted model in
[chat-artefact.script.jsonl](./chat-artefact.script.jsonl). The person sets
*Changes* to Ask first, so each of the model's writes waits in the Artefact
panel as a diff.

- The first write is a draft whose link `[[#Risk]]` names no heading; the
  diff shows the chat's `chat_*` properties stamped into its frontmatter
  beside the model's own. It is accepted and becomes revision 1, and the
  write's result carries the lint finding.
- The second write fixes the link; accepted, it is revision 2.
- In Revisions, the person selects revision 1, sees its diff and reverts to
  it: revision 3, and the Lint tab reports the broken link again.

```jsonl trace
{"do":"note","text":"ADR-0282: one markdown document per conversation, edited by the model"}
{"do":"wait","valueContains":"→ scripted","role":"label"}
{"do":"wait","valueContains":"revision 0","role":"label","comment":"the Artefact panel, empty"}
{"do":"click","contains":"Settings","role":"button"}
{"do":"click","name":"Ask first","role":"radio_button","comment":"Changes: each write waits"}
{"do":"wait","valueContains":"may edit","role":"label","comment":"Apps is off: the scale is the artefact's"}
{"do":"click","contains":"Settings","role":"button","comment":"close Settings"}
{"do":"click","contains":"Send","role":"button","comment":"the seeded draft"}
{"do":"wait","valueContains":"Proposed change","role":"label","settleMs":500}
{"do":"wait","valueContains":"Drafting the plan · artefact_write","role":"label"}
{"do":"wait","valueContains":"+ chat_conversation: chat-","role":"label","comment":"the chat's properties, in the diff"}
{"do":"capture","text":"chat-artefact-proposal"}
{"do":"click","contains":"Accept","role":"button"}
{"do":"wait","valueContains":"Fixing the link · artefact_edit","role":"label","settleMs":500}
{"do":"click","contains":"Accept","role":"button"}
{"do":"wait","valueContains":"Drafted the plan in the artefact","role":"label","settleMs":1000}
{"do":"wait","valueContains":"revision 2 · 27 lines","role":"label"}
{"do":"wait","valueContains":"artefact_write · revision 1 · lines 1–27 · 1 finding","role":"label","comment":"the transcript's tool line"}
{"do":"capture","text":"chat-artefact-document"}
{"do":"click","name":"Revisions","role":"button"}
{"do":"click","contains":"r1 · Drafting the plan","role":"button"}
{"do":"wait","valueContains":"+ # Plan","role":"label","comment":"revision 1's diff against the empty document"}
{"do":"click","contains":"Revert to this","role":"button"}
{"do":"wait","contains":"r3 · reverted to r1","role":"button","settleMs":500}
{"do":"capture","text":"chat-artefact-revisions"}
{"do":"click","contains":"Lint","role":"button"}
{"do":"wait","valueContains":"ML001","role":"label"}
{"do":"capture","text":"chat-artefact-lint"}
```
