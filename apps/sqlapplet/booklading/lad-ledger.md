---
type: reference
audience: end-user
status: draft
title: Snapshot ledger
icon: "📒"
endpoint: default
tabs: [table, timeline]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Snapshot ledger

Every complete snapshot in the lading store (ADR-0198), newest first: which
mount, when it was taken, how many entries and bytes it holds, the policy it
was walked under, and the day it disappears. This is the bill of lading
itself — one row per walk that finished, because the index holds only root
rows and a root row is written last.

**`m` is `'*'` by default**, every mount the caller may see. Paste one mount id
(decimal or `0x`-hex) to narrow it. The mount's declared name comes from its
policy record in `boxer.facts` when one was written
(`ladingingest.RecordPolicy`); a mount without one shows its id and nothing
else, which is the honest answer — name → id resolution belongs to the
application, not to the store.

The Timeline tab lays the snapshots out by instant, one lane per mount, so a
cadence that slipped or a mount that stopped being walked is visible as a gap.

```sql
SET param_m = '*';
WITH
  policy AS (
    SELECT tupleElement(p, 'Id') AS mount,
           argMax(tupleElement(p, 'Name'), tupleElement(p, 'Ts')) AS name,
           argMax(tupleElement(p, 'Store'), tupleElement(p, 'Ts')) AS store
    FROM (SELECT LW_COMPONENT('LadingMount') AS p FROM boxer.facts)
    GROUP BY mount
  )
SELECT
  s.mount AS "mount@gloss/taggedid",
  policy.name AS name,
  s.snap AS _tl_time,
  lower(hex(s.mount)) AS _tl_lane,
  s.snap_entries AS entries,
  s.snap_bytes AS "bytes@gloss/bytes",
  s.ttl_class AS ttl_class,
  s.text_rule AS text_rule,
  s.inline_max AS "inline_max@gloss/bytes",
  s.expires_at AS expires_at
FROM fssnap({m:String}) AS s
LEFT JOIN policy ON policy.mount = s.mount
ORDER BY s.snap DESC
```
