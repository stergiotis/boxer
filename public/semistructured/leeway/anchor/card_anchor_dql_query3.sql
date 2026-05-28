/*
Query 3: Deep Dive into Text Co-Containers (Full-Text Search)

When the Drone delivery generated a text note or the Cyber attack recorded a SQL Injection payload, Leeway populated the Text section's Co-Containers (wordLength, wordBag).

ClickHouse natively loves this format. If we want to find any event (Drone or Cyber) where the text payload contained the exact words "quietly" or "union", we don't need expensive LIKE '%...%' text parsing. We can search the pre-tokenized wordBag column directly.

Note: the `text` scalar column has one element per attribute, while `wordBag` is a flat *non-scalar* co-container (sum over the `len` support column). They cannot be parallel-ARRAY-JOINed, so we use `arrayExists` to filter rows and `arrayFilter` to project the matching tokens.
*/
SELECT
    `id:id:u64:2k:0:0:` AS id,
    `tv:symbol:value:val:s:m:0:24:0::data`[1] AS event_type,
    arrayStringConcat(`tv:text:text:val:s:0:0:0:0::`, ' | ') AS text_payload,
    arrayFilter(w -> w IN ('quietly', 'union'), `tv:text:wordBag:val:sh:0:0:0:0::`) AS matched_tokens
FROM anchor.facts
WHERE arrayExists(w -> w IN ('quietly', 'union'), `tv:text:wordBag:val:sh:0:0:0:0::`)
LIMIT 10;
