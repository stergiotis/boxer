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
grant does not list: the run fails with the agent limit before anything is
sent.

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
{"do":"focus","id":3557791720187110425}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":3557791720187110425,"text":"set_sql"}
{"do":"focus","id":13823422873042478622}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":13823422873042478622,"text":"{\"sql\":\"SELECT * FROM keelson('windows')\"}"}
{"do":"click","name":"Call","role":"button","comment":"k5: a table the grant does not list"}
{"do":"wait","valueContains":"k5 · set_sql · rendered","role":"label"}
{"do":"focus","id":3557791720187110425}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":3557791720187110425,"text":"run"}
{"do":"focus","id":13823422873042478622}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":13823422873042478622,"text":"{}"}
{"do":"click","name":"Call","role":"button","comment":"k6"}
{"do":"wait","valueContains":"k6 · run · rendered","role":"label","settleMs":1500}
{"do":"focus","id":3557791720187110425}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":3557791720187110425,"text":"describe_result"}
{"do":"click","name":"Call","role":"button","comment":"k7"}
{"do":"wait","valueContains":"\"error\":\"agent limit: the grant does not list keelson:windows","role":"label","comment":"the console's read of the result; play's own summary line says the same"}
{"do":"capture","text":"agentconsole-play-limit"}
```
