---
type: reference
audience: contributor
status: draft
scene:
  launch: chat
  size: 1600x1000
  env:
    BOXER_AGENT_COORDINATORS: "chat"
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-send-image.script.jsonl"
    BOXER_CHAT_ARTEFACT: "true"
    BOXER_CHAT_PIXELS: "ask"
    BOXER_CHAT_SAMPLE_IMAGE: "true"
    BOXER_CHAT_DRAFT: "What does this image show?"
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — the person sends an image with a message

ADR-0385 on the headless host, against the scripted model in
[chat-send-image.script.jsonl](./chat-send-image.script.jsonl), with Pixels
at Ask each time. The artefact's set holds one image, seeded as a file the
person added would be: the host's file dialog starts in the home directory
and takes no typed path, so a scene cannot reach a fixture through it. The
Images tab shows the image under the name the chat gave it and the name of
the file it came from. Attaching it puts its name above the composer;
sending shows what went with the message under it, and the bar says that
the conversation read untrusted content.

The scripted model answers from the conversation's position, not from what
it was sent, so this scene shows the window's side. That the request
carries the image, and the next turn's a placeholder, is checked in
`apps/chat`'s tests.

```jsonl trace
{"do":"note","text":"ADR-0385: an image of the artefact's set goes with the person's next message, as the Pixels setting allows"}
{"do":"wait","contains":"Send","role":"button"}
{"do":"click","contains":"Images (1)","role":"button"}
{"do":"wait","valueContains":"upload-1.png · 320×200","role":"label"}
{"do":"wait","valueContains":"a file you added: sample.png","role":"label"}
{"do":"click","contains":"Attach to next message","role":"button"}
{"do":"wait","valueContains":"with this message:","role":"label"}
{"do":"capture","text":"chat-send-image-chosen","settleMs":500}
{"do":"click","contains":"Send","role":"button"}
{"do":"wait","valueContains":"Four bands of colour","role":"label","settleMs":500}
{"do":"wait","valueContains":"sent with this message: upload-1.png","role":"label"}
{"do":"wait","valueContains":"read untrusted content","role":"label","comment":"what an image shows is content from outside the person's own words"}
{"do":"capture","text":"chat-send-image","settleMs":500}
```
