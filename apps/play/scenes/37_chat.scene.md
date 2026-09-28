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
    BOXER_PLAY_FOCUS_CHAT: "1"
  requires: ["clickhouse"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chat

Chat — the result as a transcript (ADR-0239): `ts`, `sender` and `body` by name, the optional chrome columns (a reply, a system line, a retraction, an edit, a delivery status), a `participants` CTE naming and colouring the senders and a `reactions` CTE joined on `id`, each demanded on its own lane

```sql
-- color is a design-system token name; the reaction keys are Phosphor glyphs
-- (thumbs-up, confetti, eye) as UTF-8 bytes, since no face draws colour emoji.
WITH participants AS (
  SELECT * FROM values('sender String, name String, color String',
    ('ana', 'Ana', 'info.default'), ('ben', 'Ben', 'success.default'), ('bot', 'Build bot', 'warning.default'))
), reactions AS (
  SELECT * FROM values('id UInt32, key String, sender String',
    (2, '\xEE\x92\x8E', 'ben'), (2, '\xEE\x92\x8E', 'bot'), (4, '\xEE\xA0\x9A', 'ana'), (7, '\xEE\x88\xA0', 'ben'))
)
SELECT
  id,
  toDateTime64('2026-09-29 09:00:00', 3, 'UTC') + toIntervalMinute(minute) AS ts,
  sender,
  body AS `body@text/markdown`,
  reply_to,
  system,
  deleted,
  if(edited_min IS NULL, NULL, toDateTime64('2026-09-29 09:00:00', 3, 'UTC') + toIntervalMinute(edited_min)) AS edited_at,
  status
FROM values('id UInt32, minute UInt32, sender String, body String, reply_to Nullable(UInt32), system Bool, deleted Bool, edited_min Nullable(UInt32), status String',
  (1, 0, 'bot', 'Ben joined the channel', NULL, true, false, NULL, ''),
  (2, 1, 'ana', 'The nightly run is **green** again after the cache fix.', NULL, false, false, NULL, 'read'),
  (3, 3, 'ben', 'Nice. Did the flaky `sealed` test go away too?', 2, false, false, NULL, ''),
  (4, 4, 'ana', 'It did — the fix was one line:\n\n```go\nf.Sync()\n```', 3, false, false, 6, 'delivered'),
  (5, 7, 'ben', 'wrong channel, sorry', NULL, false, true, NULL, ''),
  (6, 9, 'bot', 'Build #812 finished: 0 failures, 3 skipped.', NULL, false, false, NULL, ''),
  (7, 12, 'ana', 'Shipping it after lunch.', NULL, false, false, NULL, 'sent'))
ORDER BY ts
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1800,"comment":"the two CTE lanes land after the main result"}
{"do":"capture","text":"37_chat"}
{"do":"click","role":"combo_box","value":"nobody","settleMs":400,"comment":"the viewer picker: the viewer's own bubbles move right and carry their status"}
{"do":"click","name":"Ana","settleMs":400}
{"do":"key","text":"Escape","settleMs":400}
{"do":"capture","text":"37_chat_viewer","settleMs":600}
{"do":"hover","role":"label","valueContains":"Shipping it after lunch","comment":"the transcript follows the tail; wheel up to its head"}
{"do":"scroll","y":2000,"settleMs":600}
{"do":"capture","text":"37_chat_head","settleMs":600}
```
