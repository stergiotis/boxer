---
type: reference
audience: contributor
status: draft
scene:
  launch: trailviewer
  size: 1600x1000
  requires: [clickhouse]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# trailviewer — the host's own run, an exception and its clearing

ADR-0350 on the headless host. The viewer opens on the host's own writer
run, which is never empty: the recorder writes a `config` row at start, and
the host's own bus requests are audit events of the same run. The scene
reads the strip's line, zooms the window out and back through the
navigation row and reads the line again, finds the `trail.config` row in the table and the
verdict line above it, selects the row and finds its `trail-required`
attribute in the detail pane, lists the rules and the exception the
`trail-row` rule raises for that row, marks the row reviewed with a reason
through the detail pane's button and sees the exception leave the list,
then steps the selection to the next row in time with the replay row.
It then reads the line of the actor × object matrix, of the dotted chart,
of the day × hour grid and of the series.

The mark is written to the trail itself, so the scene needs a host whose
trail is durable: the persist service on, its backend ClickHouse, and the
facts table and the trail views installed on that server. The
`clickhouse` requirement stands for all three; on a host whose trail is not
durable the mark buttons refuse and the last steps fail, which is the
honest outcome.

```jsonl trace
{"do":"note","text":"ADR-0350 M5: the viewer on this host's own run"}
{"do":"wait","name":"Refresh","role":"button","settleMs":500}
{"do":"read","valueContains":"buckets of","role":"label","pattern":"(?P<rows>\\d+) rows in","settleMs":6000}
{"do":"expect","of":"rows","min":1}
{"do":"wait","value":"rows","role":"label","comment":"the read's state chip rests on rows between reads"}
{"do":"click","name":"Zoom out","role":"button","comment":"ADR-0350 M8: the window doubles through set_range, the read follows the settled window"}
{"do":"wait","value":"rows","role":"label","settleMs":1500}
{"do":"read","valueContains":"buckets of","role":"label","pattern":"(?P<rows2>\\d+) rows in","settleMs":500}
{"do":"expect","of":"rows2","min":1}
{"do":"click","name":"Back","role":"button","comment":"the previous window from the history"}
{"do":"wait","value":"rows","role":"label","settleMs":1500}
{"do":"wait","valueContains":"Evidence:","role":"label"}
{"do":"wait","contains":"trail config","role":"button","comment":"the recorder's own configuration row"}
{"do":"click","contains":"trail config","role":"button"}
{"do":"read","valueContains":"trail-required=","role":"label","pattern":"trail-required=(?P<required>true|false)"}
{"do":"click","name":"Rules","role":"button"}
{"do":"wait","valueContains":"trail-row (","role":"label"}
{"do":"wait","valueContains":"novel-action (","role":"label"}
{"do":"click","contains":"Exceptions (","role":"button"}
{"do":"read","valueContains":"open among the","role":"label","pattern":"(?P<open>\\d+) open among the \\d+ rows held; (?P<cleared>\\d+) cleared"}
{"do":"expect","of":"open","min":1}
{"do":"wait","contains":"trail-row: trail.config row","role":"button"}
{"do":"type","role":"text_input","nth":2,"text":"the host's own configuration, seen in a scene","comment":"the mark reason box: text inputs carry no name, and the case value and the band's time field come before it"}
{"do":"click","name":"reviewed","role":"button"}
{"do":"read","valueContains":"Marked reviewed","role":"label","pattern":"Marked (?P<mark>reviewed)"}
{"do":"read","valueContains":"open among the","role":"label","pattern":"(?P<open2>\\d+) open among the \\d+ rows held; (?P<cleared2>\\d+) cleared"}
{"do":"expect","of":"cleared2","min":1}
{"do":"expect","of":"open","minus":"open2","min":1}
{"do":"capture","text":"trailviewer"}
{"do":"read","valueContains":"oldest first","role":"label","pattern":"row (?P<place>\\d+) of (?P<held>\\d+), oldest first","comment":"ADR-0350 M8: the selected row's place among the held rows, in time order"}
{"do":"click","contains":"Next row","role":"button","comment":"replay steps the selection to the row after, in time"}
{"do":"read","valueContains":"oldest first","role":"label","pattern":"row (?P<place2>\\d+) of \\d+, oldest first","settleMs":800}
{"do":"expect","of":"place2","minus":"place","eq":1}
{"do":"click","name":"Actor × object","role":"button"}
{"do":"read","valueContains":"actors ×","role":"label","pattern":"(?P<actors>\\d+) actors × (?P<objects>\\d+) objects","settleMs":4000}
{"do":"expect","of":"actors","min":1}
{"do":"capture","text":"trailviewer-matrix"}
{"do":"click","name":"Dotted chart","role":"button"}
{"do":"read","valueContains":"rows over","role":"label","pattern":"(?P<runs>\\d+) rows over (?P<buckets>\\d+) buckets","settleMs":4000}
{"do":"expect","of":"runs","eq":1}
{"do":"capture","text":"trailviewer-dotted"}
{"do":"click","name":"Day × hour","role":"button"}
{"do":"read","valueContains":"outside Mon–Fri","role":"label","pattern":"(?P<dh>\\d+) rows; busiest \\w+ \\d+:00 with (?P<busiest>\\d+); (?P<off>\\d+)% outside","settleMs":4000}
{"do":"expect","of":"dh","min":1}
{"do":"expect","of":"busiest","min":1}
{"do":"capture","text":"trailviewer-dayhour"}
{"do":"click","name":"Series","role":"button"}
{"do":"read","valueContains":"peak","role":"label","pattern":"rows over (?P<sbuckets>\\d+) buckets of \\S+ from (?P<srows>\\d+) rows; peak","settleMs":4000}
{"do":"expect","of":"srows","min":1}
{"do":"expect","of":"sbuckets","min":1}
{"do":"capture","text":"trailviewer-series"}
```
