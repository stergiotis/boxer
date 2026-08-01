/* Query 3 — token search in the text section's co-containers.

When an entity carries a text attribute, the `text` section's co-containers
(`wordLength`, `wordBag`) hold a pre-tokenized form. Searching `wordBag` for
exact tokens avoids LIKE-style scans over the raw text.

Shape note: `text:text` has one element per attribute, while `text:wordBag` is
a flat co-container (its per-attribute lengths live in the `text:len` support
column). They cannot be parallel-ARRAY-JOINed, so arrayExists filters rows and
arrayFilter projects the matching tokens.
*/
SELECT
    `id:id` AS id,
    `symbol:value`[1] AS event_type,
    arrayStringConcat(`text:text`, ' | ') AS text_payload,
    arrayFilter(w -> w IN ('quietly', 'union'), `text:wordBag`) AS matched_tokens
FROM facts
WHERE arrayExists(w -> w IN ('quietly', 'union'), `text:wordBag`)
LIMIT 10
