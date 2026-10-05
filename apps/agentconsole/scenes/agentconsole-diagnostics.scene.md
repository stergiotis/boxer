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

# agentconsole — play's Diagnostics pane, read by an agent

ADR-0270's update of 2026-10-04 on the headless host. The console opens
play under a test grant and reads `get_diagnostics` three times: over a
statement neither boxer's grammar nor ClickHouse accepts, which reports
ClickHouse's own diagnostic; over a statement that parses and ran, which
reports its class and split; and after a run the server failed, which
reports the run's full error.

```jsonl trace
{"do":"note","text":"ADR-0270: get_diagnostics, the Diagnostics pane for an agent"}
{"do":"focus","id":10463678445297493114,"comment":"the console's app field"}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":10463678445297493114,"text":"play"}
{"do":"click","name":"Open","role":"button","settleMs":2500}
{"do":"click","name":"Request grant","role":"button"}
{"do":"wait","valueContains":"grant task-","role":"label"}
{"do":"click","name":"Call","role":"button","comment":"k1: get_state, the read a write starts from"}
{"do":"wait","valueContains":"k1 · get_state · completed","role":"label"}
{"do":"focus","id":3557791720187110425}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":3557791720187110425,"text":"set_sql"}
{"do":"focus","id":13823422873042478622}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":13823422873042478622,"text":"{\"sql\":\"SELECT count( FROM keelson('apps')\"}"}
{"do":"click","name":"Call","role":"button","comment":"k2: a statement that does not parse"}
{"do":"wait","valueContains":"k2 · set_sql · rendered","role":"label","settleMs":2500,"comment":"the editor settles and ClickHouse is asked"}
{"do":"focus","id":3557791720187110425}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":3557791720187110425,"text":"get_diagnostics"}
{"do":"focus","id":13823422873042478622}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":13823422873042478622,"text":"{}"}
{"do":"click","name":"Call","role":"button","comment":"k3"}
{"do":"wait","valueContains":"k3 · get_diagnostics · completed","role":"label"}
{"do":"wait","valueContains":"\"status\":\"rejected\"","role":"label","nth":0,"comment":"neither grammar parses it"}
{"do":"wait","valueContains":"Code: 62","role":"label","nth":0,"comment":"ClickHouse's own diagnostic"}
{"do":"capture","text":"agentconsole-diagnostics-rejected"}
{"do":"focus","id":3557791720187110425}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":3557791720187110425,"text":"set_sql"}
{"do":"focus","id":13823422873042478622}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":13823422873042478622,"text":"{\"sql\":\"SELECT id FROM keelson('apps') ORDER BY id LIMIT 3\"}"}
{"do":"click","name":"Call","role":"button","comment":"k4"}
{"do":"wait","valueContains":"k4 · set_sql · rendered","role":"label"}
{"do":"focus","id":3557791720187110425}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":3557791720187110425,"text":"run"}
{"do":"focus","id":13823422873042478622}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":13823422873042478622,"text":"{}"}
{"do":"click","name":"Call","role":"button","comment":"k5"}
{"do":"wait","valueContains":"k5 · run · rendered","role":"label","settleMs":2000}
{"do":"focus","id":3557791720187110425}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":3557791720187110425,"text":"get_diagnostics"}
{"do":"focus","id":13823422873042478622}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":13823422873042478622,"text":"{}"}
{"do":"click","name":"Call","role":"button","comment":"k6"}
{"do":"wait","valueContains":"k6 · get_diagnostics · completed","role":"label"}
{"do":"wait","valueContains":"\"status\":\"parses\"","role":"label","nth":0}
{"do":"wait","valueContains":"\"class\":\"read\"","role":"label","nth":0}
{"do":"wait","valueContains":"split into","role":"label","nth":0,"comment":"the query graph of the run"}
{"do":"focus","id":3557791720187110425}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":3557791720187110425,"text":"set_sql"}
{"do":"focus","id":13823422873042478622}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":13823422873042478622,"text":"{\"sql\":\"SELECT no_such_column FROM keelson('apps')\"}"}
{"do":"click","name":"Call","role":"button","comment":"k7: a statement that parses and fails on the server"}
{"do":"wait","valueContains":"k7 · set_sql · rendered","role":"label"}
{"do":"focus","id":3557791720187110425}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":3557791720187110425,"text":"run"}
{"do":"focus","id":13823422873042478622}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":13823422873042478622,"text":"{}"}
{"do":"click","name":"Call","role":"button","comment":"k8"}
{"do":"wait","valueContains":"k8 · run · rendered","role":"label","settleMs":2000}
{"do":"focus","id":3557791720187110425}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":3557791720187110425,"text":"get_diagnostics"}
{"do":"focus","id":13823422873042478622}
{"do":"key","text":"A","modifiers":16}
{"do":"type","id":13823422873042478622,"text":"{}"}
{"do":"click","name":"Call","role":"button","comment":"k9"}
{"do":"wait","valueContains":"k9 · get_diagnostics · completed","role":"label"}
{"do":"wait","valueContains":"\"last_run_error\":true","role":"label","nth":0,"comment":"the run failed, and its full error is read"}
{"do":"wait","valueContains":"no_such_column","role":"label","nth":0}
{"do":"capture","text":"agentconsole-diagnostics-run-error"}
```
