---
type: reference
audience: contributor
status: draft
scene:
  launch: agentconsole
  size: 1400x900
  requires: [clickhouse]
  env:
    BOXER_AGENT_TEST_GRANTS: "true"
    BOXER_AGENTCONSOLE_DESTINATIONS: "keelson:apps"
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# agentconsole — an agent's run in play, under the grant

ADR-0270 on the headless host. The console opens play, takes a test grant
whose only destination is `keelson:apps`, replaces play's buffer, runs it and
describes the result. A second buffer reads `keelson('windows')`, which the
grant does not list: the run is refused when it is asked for, naming the
destination the grant would have to list, and nothing is sent.

Between the two, the person presses play's Run. The button goes through the
same `run` handler as the agent's call (ADR-0270 §SD6), so the change is
logged with the person as writer. The task read the result, so its next
command is refused as paused until a turn reports the person's run. The
turn does not refresh what the task read, so it reads again before writing.

```jsonl trace
{"do":"note","text":"ADR-0270: play's catalog, and the agent limits on a run"}
{"do":"focus","id":10463678445297493114,"comment":"the console's app field"}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":10463678445297493114,"text":"play"}
{"do":"click","name":"Open","role":"button","settleMs":2500}
{"do":"read","valueContains":"opened window","role":"label","pattern":"opened window (?P<play>\\d+)"}
{"do":"click","name":"Request grant","role":"button"}
{"do":"wait","valueContains":"grant task-","role":"label"}
{"do":"click","name":"Call","role":"button","comment":"k1: get_state, the read a write starts from"}
{"do":"wait","valueContains":"k1 · get_state · completed","role":"label"}
{"do":"focus","id":3557791720187110425}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":3557791720187110425,"text":"set_sql"}
{"do":"focus","id":13823422873042478622}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":13823422873042478622,"text":"{\"sql\":\"SELECT id FROM keelson('apps') ORDER BY id LIMIT 3\"}"}
{"do":"click","name":"Call","role":"button","comment":"k2: the buffer, replaced before play draws"}
{"do":"wait","valueContains":"k2 · set_sql · rendered","role":"label"}
{"do":"focus","id":3557791720187110425}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":3557791720187110425,"text":"run"}
{"do":"focus","id":13823422873042478622}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":13823422873042478622,"text":"{}"}
{"do":"click","name":"Call","role":"button","comment":"k3: the run, under the agent limits"}
{"do":"wait","valueContains":"k3 · run · rendered","role":"label","settleMs":2000}
{"do":"focus","id":3557791720187110425}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":3557791720187110425,"text":"describe_result"}
{"do":"click","name":"Call","role":"button","comment":"k4: the result"}
{"do":"read","valueContains":"\"columns\"","role":"label","pattern":"\"rows\":\"(?P<rows>\\d+)\""}
{"do":"expect","of":"rows","eq":3}
{"do":"capture","text":"agentconsole-play"}
{"do":"click","name":"Run","role":"button","settleMs":1500,"comment":"the person's Run, through the catalog's run handler"}
{"do":"focus","id":3557791720187110425}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":3557791720187110425,"text":"set_sql"}
{"do":"focus","id":13823422873042478622}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":13823422873042478622,"text":"{\"sql\":\"SELECT * FROM keelson('windows')\"}"}
{"do":"click","name":"Call","role":"button","comment":"k5: refused, the person changed what the task read"}
{"do":"wait","valueContains":"k5 · set_sql · refused · paused: person changed result","role":"label"}
{"do":"click","name":"Turn","role":"button","comment":"the turn reports the person's run and lifts the pause"}
{"do":"wait","valueContains":"person changed result in window","role":"label"}
{"do":"focus","id":3557791720187110425}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":3557791720187110425,"text":"get_state"}
{"do":"focus","id":13823422873042478622}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":13823422873042478622,"text":"{}"}
{"do":"click","name":"Call","role":"button","comment":"k6: a turn reports changes and does not refresh what the task read, so the task reads again"}
{"do":"wait","valueContains":"k6 · get_state · completed","role":"label"}
{"do":"focus","id":3557791720187110425}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":3557791720187110425,"text":"set_sql"}
{"do":"focus","id":13823422873042478622}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":13823422873042478622,"text":"{\"sql\":\"SELECT * FROM keelson('windows')\"}"}
{"do":"click","name":"Call","role":"button","comment":"k7: a table the grant does not list"}
{"do":"wait","valueContains":"k7 · set_sql · rendered","role":"label"}
{"do":"focus","id":3557791720187110425}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":3557791720187110425,"text":"run"}
{"do":"focus","id":13823422873042478622}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":13823422873042478622,"text":"{}"}
{"do":"click","name":"Call","role":"button","comment":"k8"}
{"do":"wait","valueContains":"k8 · run · refused","role":"label","settleMs":1500,"comment":"refused when asked for, before anything is sent"}
{"do":"wait","valueContains":"agent limit: the grant does not list keelson:windows","role":"label","nth":0,"comment":"the refusal names the destination"}
{"do":"capture","text":"agentconsole-play-limit"}
```
