---
type: reference
audience: contributor
status: draft
scene:
  launch: chat
  size: 1400x900
  requires: [clickhouse]
  env:
    BOXER_AGENT_TEST_GRANTS: "true"
    BOXER_AGENT_COORDINATORS: "chat"
    BOXER_LLM_SCRIPT: "apps/chat/scenes/chat-play-query-keelson.script.jsonl"
    BOXER_CHAT_APPS: "true"
    BOXER_CHAT_CALL_WAIT: "3s"
    BOXER_CHAT_WAKES_MAX: "0"
    BOXER_CHAT_DRAFT: "List the host's tables in play."
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat — play's query tells a table of the host from an ad-hoc dataset alias

The model is the scripted one in
[chat-play-query-keelson.script.jsonl](./chat-play-query-keelson.script.jsonl).
It calls play's `query` twice. `keelson('tables')` names a table of the
host's introspection plane, which play asks the plane about rather than the
process-wide registry the host does not serve: the run starts at once, with
no alias to bind. `keelson('nothing_here')` names an alias nothing is
published under: the window follows it, and the strip over the panes says
why it waits and that binding it runs nothing. The window draws frames while
it waits, without input, so the follower's answers land and `query` refuses
after its ten seconds; the window then stops following the alias, and the
last capture shows the strip gone.

Chat waits three seconds for a call here, so the second `query` outlives
the wait and chat follows it in the background. Its line in the transcript
said running when the turn landed; once play refuses, the line says so.
No turn starts for the result (`BOXER_CHAT_WAKES_MAX` is 0), so the script
needs no reply for one.

```jsonl trace
{"do":"wait","contains":"Send","role":"button"}
{"do":"click","contains":"Send","role":"button","comment":"turn 1, the seeded draft"}
{"do":"read","valueContains":"Waiting for dataset nothing_here","role":"label","pattern":"(?P<then>A task asked for it here; binding it runs nothing)\\.$","comment":"play's strip, while query waits for the alias"}
{"do":"capture","text":"chat-play-query-keelson-waiting"}
{"do":"read","valueContains":"Reading the host","role":"label","pattern":"query in window 2 · (?P<tables>done)","comment":"a table of the plane, not an alias to wait for"}
{"do":"sleep","settleMs":12000,"comment":"past query's ten seconds: it gives up, and play stops following the alias"}
{"do":"read","valueContains":"Reading an alias nothing publishes","role":"label","pattern":"query in window 2 · (?P<settled>refused) in the background","comment":"chat's line for the call that outlived its wait says how it ended"}
{"do":"capture","text":"chat-play-query-keelson"}
```
