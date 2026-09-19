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
    BOXER_PLAY_AUTORUN: ""
    BOXER_PLAY_FOCUS_CARDS: "1"
  requires: ["clickhouse"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# cards

The Cards pane (ADR-0245) from an EMPTY workbench: the sample cards published as an ORDINARY ad-hoc dataset, then the scaffold run — a paged grid of uniform cards whose heroes are images and recordings, each row naming its own media type in card_hero_gloss, and whose awkward rows (a panorama, a strip, an icon, a header over the pixel budget, a truncated file, a misspelt type, a NULL hero, a path for a title) keep their line

```sql
-- Nothing has run yet. The Cards tab below publishes sample cards as an
-- ordinary ad-hoc dataset and writes the query that reads them.
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":2000}
{"do":"capture","text":"08_cards_empty","settleMs":600}
{"do":"click","contains":"publish sample cards"}
{"do":"sleep","settleMs":3000}
{"do":"click","name":"Run"}
{"do":"wait","name":"Run","comment":"Run is Cancel while the query runs, so this resolves when it lands"}
{"do":"sleep","settleMs":1500,"comment":"the page fills in over a few frames: artifacts are built under a per-frame budget"}
{"do":"capture","text":"08_cards"}
{"do":"click","valueContains":"Panorama","pointer":true,"comment":"a label carries its text as the accessible VALUE, and the click sense is the card behind it: press the bounds centre"}
{"do":"capture","text":"08_cards_detail_follows","settleMs":800}
```
