---
type: reference
audience: contributor
status: draft
scene:
  launch: play
  size: 1920x1200
  stepSettleMs: 350
  env:
    BOXER_PLAY_WINDOW_SIZE: "1888x1100"
    BOXER_PLAY_AUTORUN: "1"
    BOXER_PLAY_FOCUS_TABLE: "1"
    BOXER_PLAY_FOCUS_GLOSSES: "1"
  requires: ["clickhouse", "table:anchor.facts"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# detail glosses

Detail + Glosses — the rule route on a leeway result (ADR-0186): `-- play: gloss` lines bind glosses to physical columns by their spec line, both grids render the inline faces, the leeway card renders a markdown block face, and the Glosses tab shows the catalog, the rules and each column's resolution — including the rule a column's binding shadowed

```sql
-- play: gloss text/markdown name:text section:text
-- play: gloss gloss/masked name:value section:symbol
-- play: gloss gloss/bytes name:wordLength
-- play: gloss gloss/raw section:text
SET param_selection = 0;
SELECT `id:id`, `symbol:value`, `text:text`, `text:wordLength`, `geoPoint:pointLat`
FROM anchor.facts
ORDER BY `id:id`
LIMIT 20
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1800}
{"do":"capture","text":"03_detail_glosses","settleMs":600}
{"do":"click","name":"per attribute"}
{"do":"capture","text":"03_detail_glosses_per_attribute","settleMs":600}
{"do":"click","x":1450,"y":300,"comment":"park the pointer over the Glosses tab"}
{"do":"scroll","x":0,"y":-4000,"settleMs":500}
{"do":"capture","text":"03_detail_glosses_columns","settleMs":600}
```
