/* Query 3 — token search in the text section's co-containers.

When an entity carries a text attribute, the `text` section's co-containers
(`wordLength`, `wordBag`) hold a pre-tokenized form. Searching `wordBag` for
exact tokens avoids LIKE-style scans over the raw text.

Shape note: `text:text` has one element per attribute, while `text:wordBag` is
a flat co-container (its per-attribute lengths live in the `text:len` support
column). They cannot be parallel-ARRAY-JOINed, so the row filter is hasAny —
the constant-argument membership form index analysis can serve (the guard
discipline of ADR-0162) — and arrayFilter projects the matching tokens.
*/
SELECT
    `id:id` AS id,
    `symbol:value`[1] AS event_type,
    arrayStringConcat(`text:text`, ' | ') AS text_payload,
    arrayFilter(w -> w IN ('quietly', 'union'), `text:wordBag`) AS matched_tokens
FROM facts
WHERE hasAny(`text:wordBag`, ['quietly', 'union'])
LIMIT 10
