---
type: reference
audience: contributor
status: draft
scene:
  launch: chat
  size: 1600x1000
  env:
    BOXER_AGENT_TEST_GRANTS: "true"
    BOXER_AGENT_COORDINATORS: "chat"
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-artefact-export.script.jsonl"
    BOXER_CHAT_APPS: "true"
    BOXER_CHAT_ARTEFACT: "true"
    BOXER_CHAT_DRAFT: "Capture the demo app and write it up in the artefact."
  requires: [clickhouse, "exists:boxer.fssnap"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — the artefact exported to lading, and opened in mdedit by snapshot

ADR-0342 on the headless host, against the scripted model in
[chat-artefact-export.script.jsonl](./chat-artefact-export.script.jsonl),
and the host's publish service of ADR-0341. The model captures the
operations demo into the artefact's screenshots and writes a document that
embeds the capture.

- **Export** opens the confirmation: the revision, its two files, the 30
  days, who can read them, and that exporting also records the person's
  endorsement of the revision, since a model wrote it and nothing endorsed
  it. The person started it, so Export is the filled answer (ADR-0404 §SD4).
- Confirming exports; the status line names the retention.
- **Open in mdedit** asks again, as the artefact has a screenshot.
  Confirming exports once more and opens mdedit, which reads the document
  from that snapshot and shows the screenshot inline. Its bar offers
  **rclone**, which copies the command that mounts that snapshot.

The scene writes two snapshots into the configured lading store, under the
chat's mount tag; they expire with their 30-day class.

```jsonl trace
{"do":"note","text":"ADR-0342: the artefact exported to lading, and handed to mdedit by snapshot"}
{"do":"wait","contains":"Send","role":"button"}
{"do":"click","contains":"Send","role":"button"}
{"do":"wait","valueContains":"Wrote up the demo with its screenshot","role":"label","settleMs":500}
{"do":"click","contains":"Export","role":"button","comment":"the artefact's Export"}
{"do":"wait","valueContains":"Export this artefact?","role":"label"}
{"do":"wait","valueContains":"revision 2 · 2 files","role":"label"}
{"do":"wait","valueContains":"It is kept for 30 days","role":"label"}
{"do":"wait","valueContains":"exporting also records that you reviewed it","role":"label"}
{"do":"capture","text":"chat-artefact-export-ask","settleMs":500}
{"do":"click","name":"\ue182 Export","role":"button","comment":"the confirmation's Export, the filled answer: the person started it"}
{"do":"wait","valueContains":"exported revision 2 to the shared file store, kept until","role":"label","settleMs":500}
{"do":"click","contains":"Open in mdedit","role":"button","comment":"the document's Open in mdedit"}
{"do":"wait","contains":"Export and open","role":"button"}
{"do":"click","contains":"Export and open","role":"button"}
{"do":"wait","valueContains":"and opened it in mdedit","role":"label","settleMs":500}
{"do":"wait","valueContains":"loaded ","role":"label","settleMs":1500,"comment":"mdedit read the document from the snapshot"}
{"do":"click","contains":"rclone","role":"button","comment":"copy the command that mounts the snapshot"}
{"do":"wait","valueContains":"copied the mount command","role":"label","comment":"mdedit names what it copied"}
{"do":"capture","text":"chat-artefact-export-mdedit","settleMs":1500}
```
