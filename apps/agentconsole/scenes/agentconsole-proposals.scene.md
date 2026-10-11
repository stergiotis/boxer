---
type: reference
audience: contributor
status: draft
scene:
  launch: agentconsole
  size: 1400x900
  env:
    BOXER_AGENT_COORDINATORS: agentconsole
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# agentconsole — undo, a suggestion and a confirmation

ADR-0269 §SD5 and §SD8 on the headless host, with the person approving. An
agent's change to the demo's note is undone from the window's badge, which
restores the note because nothing has changed it since; the undo is the
person's change, so it pauses the task until its next turn, which lists it.
In suggest mode a
write waits as a proposal on the badge until the person accepts it; one
that still expects the revision the undo moved ends as stale when accepted,
and one made after a fresh read applies. A
copy to the clipboard writes outside the app, so it waits for the person's
confirmation in its own dialog, in act mode as well.

```jsonl trace
{"do":"note","text":"ADR-0269 M3: undo per call, proposals in suggest mode, confirmation of a consequential command"}
{"do":"click","name":"Open operations demo","role":"button","settleMs":1500}
{"do":"click","name":"Request grant","role":"button","settleMs":800}
{"do":"wait","contains":"your turn","role":"button","comment":"the request waits in the person's queue (ADR-0324)"}
{"do":"click","contains":"your turn","role":"button"}
{"do":"click","contains":"Next","role":"button","comment":"its card, non-modal"}
{"do":"click","name":"Approve","role":"button"}
{"do":"wait","valueContains":"grant task-","role":"label"}
{"do":"click","name":"Call","role":"button","comment":"k1: get_state"}
{"do":"wait","valueContains":"k1 · get_state · completed","role":"label"}
{"do":"focus","id":17222585601186924707}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":17222585601186924707,"text":"set_note"}
{"do":"focus","id":7028873545952381604}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":7028873545952381604,"text":"{\"text\":\"from the agent\"}"}
{"do":"click","name":"Call","role":"button","comment":"k2: set_note in act mode"}
{"do":"wait","valueContains":"k2 · set_note · rendered","role":"label"}
{"do":"read","id":12013363430533946716,"pattern":"^(?P<changed>from the agent)$"}
{"do":"click","contains":"agent · act","role":"button","settleMs":500,"comment":"open the badge's menu"}
{"do":"click","name":"Undo","role":"button","settleMs":800,"comment":"undo k2 from its card"}
{"do":"read","id":12013363430533946716,"pattern":"^(?P<restored>A note both of you can edit\\.)$","comment":"nothing changed the note since, so undo restores it"}
{"do":"click","name":"suggest","settleMs":500,"comment":"the person lowers the task to suggest"}
{"do":"key","text":"Escape","settleMs":300}
{"do":"wait","contains":"agent · suggest · paused","role":"button","comment":"the person's undo changed what the task read, so the task is paused"}
{"do":"click","name":"Turn","role":"button","comment":"the coordinator's turn lists the undo and lifts the pause"}
{"do":"wait","valueContains":"person changed note in window 2 (undo set_note)","role":"label"}
{"do":"click","name":"Call","role":"button","comment":"k3: set_note again, without reading first: it expects the revision the undo moved"}
{"do":"wait","valueContains":"k3 · set_note · proposed","role":"label"}
{"do":"wait","contains":"proposals 1","role":"button","comment":"the badge says a proposal waits"}
{"do":"click","contains":"proposals 1","role":"button","settleMs":500}
{"do":"click","name":"Accept","role":"button","settleMs":500}
{"do":"key","text":"Escape","settleMs":300}
{"do":"wait","valueContains":"k3 · set_note · stale","role":"label","comment":"accepted too late: the note moved after it was proposed"}
{"do":"focus","id":17222585601186924707}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":17222585601186924707,"text":"get_state"}
{"do":"focus","id":7028873545952381604}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":7028873545952381604,"text":"{}"}
{"do":"click","name":"Call","role":"button","comment":"k4: read again, so the next write expects the current revision"}
{"do":"wait","valueContains":"k4 · get_state · completed","role":"label"}
{"do":"focus","id":17222585601186924707}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":17222585601186924707,"text":"set_note"}
{"do":"focus","id":7028873545952381604}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":7028873545952381604,"text":"{\"text\":\"suggested\"}"}
{"do":"click","name":"Call","role":"button","comment":"k5: in suggest mode the write waits as a proposal"}
{"do":"wait","valueContains":"k5 · set_note · proposed","role":"label"}
{"do":"wait","contains":"proposals 1","role":"button"}
{"do":"capture","text":"agentconsole-proposals-badge"}
{"do":"click","contains":"proposals 1","role":"button","settleMs":500}
{"do":"click","name":"Accept","role":"button","settleMs":500}
{"do":"key","text":"Escape","settleMs":300}
{"do":"wait","valueContains":"k5 · set_note · rendered","role":"label"}
{"do":"read","id":12013363430533946716,"pattern":"^(?P<accepted>suggested)$"}
{"do":"focus","id":17222585601186924707}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":17222585601186924707,"text":"copy_note"}
{"do":"focus","id":7028873545952381604}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":7028873545952381604,"text":"{}"}
{"do":"click","name":"Call","role":"button","comment":"k6: a copy to the clipboard writes outside the app"}
{"do":"wait","valueContains":"asks to copy the note to the clipboard","role":"label"}
{"do":"capture","text":"agentconsole-proposals-confirm"}
{"do":"click","name":"Confirm","role":"button"}
{"do":"wait","valueContains":"copied 1×","role":"label"}
{"do":"wait","valueContains":"k6 · copy_note · rendered","role":"label"}
```
