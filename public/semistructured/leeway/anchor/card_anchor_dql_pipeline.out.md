---
type: reference
audience: contributor
status: draft
generated: true
generator: go test, TestDqlPipelineGeneration
---

> **Status: draft — pre-human-review.** Machine-generated demo artifact;
> regenerate via `go test`, do not edit.

# anchor DQL — friendly source to executable SQL, stage by stage

Each query starts as
friendly-handle SQL (leeway `section:column` handles, unqualified tables)
and passes through the nanopass pre-execute chain; a stage that changed
nothing is omitted. The final stage's output is the committed
`card_anchor_dql_queryN.out.sql` artifact.

## card_anchor_dql_query1.sql

### source (friendly handles)

```sql
/* Query 1 — attack types and their target ports, via the pack's LW_RAGGED_NEST.

Lists each cyber incident's attack type (the `symbol` section value) together
with its target network ports, which ride the same attributes as low-cardinality
ref memberships (`lr`). LW_RAGGED_NEST — from the co/ragged function pack
(ADR-0162) that hosts reconcile at connect — regroups the flat `lr` stream by
the per-attribute `lrcard` counts so it can be ARRAY-JOINed in parallel with
the value column. Nesting at an ARRAY JOIN boundary is exactly the pack's
documented use for LW_RAGGED_NEST: the codomain is genuinely nested here.

Column references are friendly leeway handles (`section:column`, ADR-0116); the
nanopass pipeline resolves them to physical names (see the .out.sql neighbour).
`symbol:lr` and `symbol:lrcard` are support columns — handles cover those too.

The pack call sits inline in ARRAY JOIN rather than in a `WITH expr AS name`
clause: the resolve pass walks each SELECT's own subtree, and a query-level
WITH-expression clause is outside it — handles there would pass through
unresolved. (deferred: teach ResolveColumnNames the query-level WITH clause.)
*/
SELECT
    `id:id` AS id,
    `id:naturalKey` AS incident_ticket,
    attack_type,
    target_ports
FROM facts
-- parallel ARRAY JOIN: both lists carry one element per attribute
ARRAY JOIN
    `symbol:value` AS attack_type,
    LW_RAGGED_NEST(`symbol:lr`, `symbol:lrcard`) AS target_ports
WHERE has(['DDOS', 'SQL_INJECTION', 'PORT_SCAN'], attack_type)
```

### after StripComments

```sql
 
SELECT
    `id:id` AS id,
    `id:naturalKey` AS incident_ticket,
    attack_type,
    target_ports
FROM facts
 ARRAY JOIN
    `symbol:value` AS attack_type,
    LW_RAGGED_NEST(`symbol:lr`, `symbol:lrcard`) AS target_ports
WHERE has(['DDOS', 'SQL_INJECTION', 'PORT_SCAN'], attack_type)
```

### after CanonicalizeFull

```sql
SELECT "id:id" AS "id", "id:naturalKey" AS "incident_ticket", "attack_type", "target_ports" FROM "facts" ARRAY JOIN "symbol:value" AS "attack_type", "LW_RAGGED_NEST"("symbol:lr", "symbol:lrcard") AS "target_ports" WHERE "has"("array"('DDOS', 'SQL_INJECTION', 'PORT_SCAN'), "attack_type")
```

### after QualifyTables

```sql
SELECT "id:id" AS "id", "id:naturalKey" AS "incident_ticket", "attack_type", "target_ports" FROM "anchor"."facts" ARRAY JOIN "symbol:value" AS "attack_type", "LW_RAGGED_NEST"("symbol:lr", "symbol:lrcard") AS "target_ports" WHERE "has"("array"('DDOS', 'SQL_INJECTION', 'PORT_SCAN'), "attack_type")
```

### after ResolveColumnNames

```sql
SELECT "id:id:u64:47::0:" AS "id", "id:naturalKey:y:4::0:" AS "incident_ticket", "attack_type", "target_ports" FROM "anchor"."facts" ARRAY JOIN "tv:symbol:value:val:s:124::I:0::data" AS "attack_type", "LW_RAGGED_NEST"("tv:symbol:lr:lr:u64:1247:::0::data", "tv:symbol:lrcard:lrcard:u64:4E:::0::data") AS "target_ports" WHERE "has"("array"('DDOS', 'SQL_INJECTION', 'PORT_SCAN'), "attack_type")
```

## card_anchor_dql_query2.sql

### source (friendly handles)

```sql
/* Query 2 — cross-domain correlation over H3 cells.

The three demo domains (drone missions, avalanche sensors, cyber incidents)
share one table. This query asks whether a drone in transit crossed an H3 cell
that simultaneously carried a seismic anomaly or a DDoS-affected facility on
2026-03-11 — one GROUP BY over the shared geo sections, no per-domain tables to
join.

`geoPoint:h3` (points) and `geoArea:h3` (polygon covers) are both H3 index
arrays; concatenating them yields every cell an entity touches. Handles sit in
the subquery, where `facts` is in scope; the outer scope sees only aliases (the
resolve pass is scope-aware and does not reach through subqueries).
*/
SELECT
    h3_hex,
    groupUniqArray(entity_type) AS simultaneous_events,
    count() AS total_incidents
FROM (
    SELECT
        `id:id` AS id,
        `symbol:value`[1] AS entity_type,
        arrayConcat(`geoPoint:h3`, `geoArea:h3`) AS all_h3_indices
    FROM facts
    WHERE `timeRange:beginIncl`[1] >= toDateTime64('2026-03-11 00:00:00', 9, 'UTC')
)
ARRAY JOIN all_h3_indices AS h3_hex
GROUP BY h3_hex
HAVING has(simultaneous_events, 'IN_TRANSIT')
   AND (
       has(simultaneous_events, 'SEISMIC_ANOMALY') OR
       has(simultaneous_events, 'DDOS')
   )
ORDER BY total_incidents DESC
```

### after StripComments

```sql
 
SELECT
    h3_hex,
    groupUniqArray(entity_type) AS simultaneous_events,
    count() AS total_incidents
FROM (
    SELECT
        `id:id` AS id,
        `symbol:value`[1] AS entity_type,
        arrayConcat(`geoPoint:h3`, `geoArea:h3`) AS all_h3_indices
    FROM facts
    WHERE `timeRange:beginIncl`[1] >= toDateTime64('2026-03-11 00:00:00', 9, 'UTC')
)
ARRAY JOIN all_h3_indices AS h3_hex
GROUP BY h3_hex
HAVING has(simultaneous_events, 'IN_TRANSIT')
   AND (
       has(simultaneous_events, 'SEISMIC_ANOMALY') OR
       has(simultaneous_events, 'DDOS')
   )
ORDER BY total_incidents DESC
```

### after CanonicalizeFull

```sql
SELECT "h3_hex", "groupUniqArray"("entity_type") AS "simultaneous_events", "count"() AS "total_incidents" FROM ( SELECT "id:id" AS "id", "arrayElement"("symbol:value", 1) AS "entity_type", "arrayConcat"("geoPoint:h3", "geoArea:h3") AS "all_h3_indices" FROM "facts" WHERE "arrayElement"("timeRange:beginIncl", 1) >= "toDateTime64"('2026-03-11 00:00:00', 9, 'UTC') ) ARRAY JOIN "all_h3_indices" AS "h3_hex" GROUP BY "h3_hex" HAVING "has"("simultaneous_events", 'IN_TRANSIT') AND ( "has"("simultaneous_events", 'SEISMIC_ANOMALY') OR "has"("simultaneous_events", 'DDOS') ) ORDER BY "total_incidents" DESC
```

### after QualifyTables

```sql
SELECT "h3_hex", "groupUniqArray"("entity_type") AS "simultaneous_events", "count"() AS "total_incidents" FROM ( SELECT "id:id" AS "id", "arrayElement"("symbol:value", 1) AS "entity_type", "arrayConcat"("geoPoint:h3", "geoArea:h3") AS "all_h3_indices" FROM "anchor"."facts" WHERE "arrayElement"("timeRange:beginIncl", 1) >= "toDateTime64"('2026-03-11 00:00:00', 9, 'UTC') ) ARRAY JOIN "all_h3_indices" AS "h3_hex" GROUP BY "h3_hex" HAVING "has"("simultaneous_events", 'IN_TRANSIT') AND ( "has"("simultaneous_events", 'SEISMIC_ANOMALY') OR "has"("simultaneous_events", 'DDOS') ) ORDER BY "total_incidents" DESC
```

### after ResolveColumnNames

```sql
SELECT "h3_hex", "groupUniqArray"("entity_type") AS "simultaneous_events", "count"() AS "total_incidents" FROM ( SELECT "id:id:u64:47::0:" AS "id", "arrayElement"("tv:symbol:value:val:s:124::I:0::data", 1) AS "entity_type", "arrayConcat"("tv:geoPoint:h3:val:u64:4:::0::geo", "tv:geoArea:h3:val:u64m:4:::0::geo") AS "all_h3_indices" FROM "anchor"."facts" WHERE "arrayElement"("tv:timeRange:beginIncl:val:z64:47:::0::data", 1) >= "toDateTime64"('2026-03-11 00:00:00', 9, 'UTC') ) ARRAY JOIN "all_h3_indices" AS "h3_hex" GROUP BY "h3_hex" HAVING "has"("simultaneous_events", 'IN_TRANSIT') AND ( "has"("simultaneous_events", 'SEISMIC_ANOMALY') OR "has"("simultaneous_events", 'DDOS') ) ORDER BY "total_incidents" DESC
```

## card_anchor_dql_query3.sql

### source (friendly handles)

```sql
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
```

### after StripComments

```sql
 
SELECT
    `id:id` AS id,
    `symbol:value`[1] AS event_type,
    arrayStringConcat(`text:text`, ' | ') AS text_payload,
    arrayFilter(w -> w IN ('quietly', 'union'), `text:wordBag`) AS matched_tokens
FROM facts
WHERE hasAny(`text:wordBag`, ['quietly', 'union'])
LIMIT 10
```

### after CanonicalizeFull

```sql
SELECT "id:id" AS "id", "arrayElement"("symbol:value", 1) AS "event_type", "arrayStringConcat"("text:text", ' | ') AS "text_payload", "arrayFilter"("w" -> "w" IN "array"('quietly', 'union'), "text:wordBag") AS "matched_tokens" FROM "facts" WHERE "hasAny"("text:wordBag", "array"('quietly', 'union')) LIMIT 10
```

### after QualifyTables

```sql
SELECT "id:id" AS "id", "arrayElement"("symbol:value", 1) AS "event_type", "arrayStringConcat"("text:text", ' | ') AS "text_payload", "arrayFilter"("w" -> "w" IN "array"('quietly', 'union'), "text:wordBag") AS "matched_tokens" FROM "anchor"."facts" WHERE "hasAny"("text:wordBag", "array"('quietly', 'union')) LIMIT 10
```

### after ResolveColumnNames

```sql
SELECT "id:id:u64:47::0:" AS "id", "arrayElement"("tv:symbol:value:val:s:124::I:0::data", 1) AS "event_type", "arrayStringConcat"("tv:text:text:val:s::::0::", ' | ') AS "text_payload", "arrayFilter"("w" -> "w" IN "array"('quietly', 'union'), "tv:text:wordBag:val:sh::::0::") AS "matched_tokens" FROM "anchor"."facts" WHERE "hasAny"("tv:text:wordBag:val:sh::::0::", "array"('quietly', 'union')) LIMIT 10
```

## card_anchor_dql_query4.sql

### source (friendly handles)

```sql
/* Query 4 — a sanitized "silver" projection for third-party sharing.

The drone operator must hand flight data to an aviation authority without the
exact GPS coordinates or the customer refs attached to the geoPoint section.
This keeps symbol and timeRange, zeroes the coordinates with arrayMap, keeps
the coarse H3 index, and blanks the high-cardinality membership columns.

Two naming mechanisms combine so the result is itself leeway-shaped:

  - An unaliased handle (`id:id`, `symbol:value`, …) resolves to its physical
    column, and ClickHouse derives the result name from the rewritten
    expression — the output column is automatically the physical name.
  - A computed column needs an explicit alias, and that alias must be the
    physical name (spelled out verbatim; the resolve pass rewrites references,
    never aliases). The alias is what defines the silver entity's shape.
*/
SELECT
    `id:id`,
    `id:naturalKey`,

    -- pass symbol and timeRange through untouched
    `symbol:value`,
    `symbol:lrcard`,
    `timeRange:beginIncl`,
    `timeRange:endExcl`,

    -- anonymize geoPoint: zero the coordinates, keep the H3 index
    arrayMap(x -> CAST(0.0 AS Float32), `geoPoint:pointLat`) AS `tv:geoPoint:pointLat:val:f32:g:0:0:0::geo`,
    arrayMap(x -> CAST(0.0 AS Float32), `geoPoint:pointLng`) AS `tv:geoPoint:pointLng:val:f32:g:0:0:0::geo`,
    `geoPoint:h3`,

    -- erase the high-cardinality references (customer ids)
    CAST([] AS Array(UInt64)) AS `tv:geoPoint:hr:hr:u64:2k:0:0:0::geo`,
    CAST([] AS Array(UInt64)) AS `tv:geoPoint:hrcard:hrcard:u64:4gw:0:0:0::geo`

FROM facts
WHERE has(`symbol:value`, 'DELIVERED')
   OR has(`symbol:value`, 'IN_TRANSIT')
```

### after StripComments

```sql
 
SELECT
    `id:id`,
    `id:naturalKey`,

         `symbol:value`,
    `symbol:lrcard`,
    `timeRange:beginIncl`,
    `timeRange:endExcl`,

         arrayMap(x -> CAST(0.0 AS Float32), `geoPoint:pointLat`) AS `tv:geoPoint:pointLat:val:f32:g:0:0:0::geo`,
    arrayMap(x -> CAST(0.0 AS Float32), `geoPoint:pointLng`) AS `tv:geoPoint:pointLng:val:f32:g:0:0:0::geo`,
    `geoPoint:h3`,

         CAST([] AS Array(UInt64)) AS `tv:geoPoint:hr:hr:u64:2k:0:0:0::geo`,
    CAST([] AS Array(UInt64)) AS `tv:geoPoint:hrcard:hrcard:u64:4gw:0:0:0::geo`

FROM facts
WHERE has(`symbol:value`, 'DELIVERED')
   OR has(`symbol:value`, 'IN_TRANSIT')
```

### after CanonicalizeFull

```sql
SELECT "id:id", "id:naturalKey", "symbol:value", "symbol:lrcard", "timeRange:beginIncl", "timeRange:endExcl", "arrayMap"("x" -> "CAST"(0.0, 'Float32'), "geoPoint:pointLat") AS "tv:geoPoint:pointLat:val:f32:g:0:0:0::geo", "arrayMap"("x" -> "CAST"(0.0, 'Float32'), "geoPoint:pointLng") AS "tv:geoPoint:pointLng:val:f32:g:0:0:0::geo", "geoPoint:h3", "CAST"("array"(), 'Array(UInt64)') AS "tv:geoPoint:hr:hr:u64:2k:0:0:0::geo", "CAST"("array"(), 'Array(UInt64)') AS "tv:geoPoint:hrcard:hrcard:u64:4gw:0:0:0::geo" FROM "facts" WHERE "has"("symbol:value", 'DELIVERED') OR "has"("symbol:value", 'IN_TRANSIT')
```

### after QualifyTables

```sql
SELECT "id:id", "id:naturalKey", "symbol:value", "symbol:lrcard", "timeRange:beginIncl", "timeRange:endExcl", "arrayMap"("x" -> "CAST"(0.0, 'Float32'), "geoPoint:pointLat") AS "tv:geoPoint:pointLat:val:f32:g:0:0:0::geo", "arrayMap"("x" -> "CAST"(0.0, 'Float32'), "geoPoint:pointLng") AS "tv:geoPoint:pointLng:val:f32:g:0:0:0::geo", "geoPoint:h3", "CAST"("array"(), 'Array(UInt64)') AS "tv:geoPoint:hr:hr:u64:2k:0:0:0::geo", "CAST"("array"(), 'Array(UInt64)') AS "tv:geoPoint:hrcard:hrcard:u64:4gw:0:0:0::geo" FROM "anchor"."facts" WHERE "has"("symbol:value", 'DELIVERED') OR "has"("symbol:value", 'IN_TRANSIT')
```

### after ResolveColumnNames

```sql
SELECT "id:id:u64:47::0:", "id:naturalKey:y:4::0:", "tv:symbol:value:val:s:124::I:0::data", "tv:symbol:lrcard:lrcard:u64:4E:::0::data", "tv:timeRange:beginIncl:val:z64:47:::0::data", "tv:timeRange:endExcl:val:z64:47:::0::data", "arrayMap"("x" -> "CAST"(0.0, 'Float32'), "tv:geoPoint:pointLat:val:f32:4:::0::geo") AS "tv:geoPoint:pointLat:val:f32:g:0:0:0::geo", "arrayMap"("x" -> "CAST"(0.0, 'Float32'), "tv:geoPoint:pointLng:val:f32:4:::0::geo") AS "tv:geoPoint:pointLng:val:f32:g:0:0:0::geo", "tv:geoPoint:h3:val:u64:4:::0::geo", "CAST"("array"(), 'Array(UInt64)') AS "tv:geoPoint:hr:hr:u64:2k:0:0:0::geo", "CAST"("array"(), 'Array(UInt64)') AS "tv:geoPoint:hrcard:hrcard:u64:4gw:0:0:0::geo" FROM "anchor"."facts" WHERE "has"("tv:symbol:value:val:s:124::I:0::data", 'DELIVERED') OR "has"("tv:symbol:value:val:s:124::I:0::data", 'IN_TRANSIT')
```

## card_anchor_dql_query5.sql

### source (friendly handles)

```sql
/* Query 5 — a "gold" composite: one synthetic entity per H3 cell per day.

Aggregates the per-domain events of 2026-03-11 into one summary entity per H3
cell: distinct event types, an incident count, a generated summary line, and
the cell as the entity's own location. Aggregation results land in Array(T)
columns, which is exactly leeway's physical shape — the projection aliases
spell out the physical names, so the result rows are a valid leeway table
again.

The subquery selects the handles it needs and aliases them (`symbols`,
`h3_index`, `event_date`); the outer scope works on those aliases — the
resolve pass does not reach through subqueries, so handles stay where their
table is in scope.
*/
WITH
    arrayDistinct(groupArrayArray(symbols)) AS distinct_symbols,
    toUInt64(count()) AS event_count
SELECT
    cityHash64(h3_index, event_date) AS `id:id:u64:47::0:`,
    concat('COMPOSITE-H3-', toString(h3_index), '-20260311') AS `id:naturalKey:y:4::0:`,

    -- symbol section: all distinct event types seen in this cell today
    distinct_symbols AS `tv:symbol:value:val:s:124::I:0::data`,

    -- u64Array section: the incident count, packed as a one-element array
    [event_count] AS `tv:u64Array:value:val:u64h:4:::0::data`,

    -- text section: a generated summary line
    [concat('Regional summary: ', toString(event_count), ' events. Includes: ',
            arrayStringConcat(distinct_symbols, ', '))] AS `tv:text:text:val:s:0:0:0:0::`,

    -- geoPoint section: the cell itself is the entity's location
    [CAST(0.0 AS Float32)] AS `tv:geoPoint:pointLat:val:f32:g:0:0:0::geo`,
    [CAST(0.0 AS Float32)] AS `tv:geoPoint:pointLng:val:f32:g:0:0:0::geo`,
    [h3_index] AS `tv:geoPoint:h3:val:u64:g:0:0:0::geo`,

    -- timeRange section: the 24-hour composite window
    [toDateTime64(toStartOfDay(toDateTime(event_date)), 9, 'UTC')] AS `tv:timeRange:beginIncl:val:z64:2k:0:0:0::data`,
    [toDateTime64(toStartOfDay(toDateTime(event_date)) + 86400, 9, 'UTC')] AS `tv:timeRange:endExcl:val:z64:2k:0:0:0::data`

FROM (
    SELECT
        `geoPoint:h3`[1] AS h3_index,
        toDate(`timeRange:beginIncl`[1]) AS event_date,
        `symbol:value` AS symbols
    FROM facts
    WHERE length(`geoPoint:h3`) > 0
)
WHERE event_date = '2026-03-11'
GROUP BY h3_index, event_date
```

### after StripComments

```sql
 
WITH
    arrayDistinct(groupArrayArray(symbols)) AS distinct_symbols,
    toUInt64(count()) AS event_count
SELECT
    cityHash64(h3_index, event_date) AS `id:id:u64:47::0:`,
    concat('COMPOSITE-H3-', toString(h3_index), '-20260311') AS `id:naturalKey:y:4::0:`,

         distinct_symbols AS `tv:symbol:value:val:s:124::I:0::data`,

         [event_count] AS `tv:u64Array:value:val:u64h:4:::0::data`,

         [concat('Regional summary: ', toString(event_count), ' events. Includes: ',
            arrayStringConcat(distinct_symbols, ', '))] AS `tv:text:text:val:s:0:0:0:0::`,

         [CAST(0.0 AS Float32)] AS `tv:geoPoint:pointLat:val:f32:g:0:0:0::geo`,
    [CAST(0.0 AS Float32)] AS `tv:geoPoint:pointLng:val:f32:g:0:0:0::geo`,
    [h3_index] AS `tv:geoPoint:h3:val:u64:g:0:0:0::geo`,

         [toDateTime64(toStartOfDay(toDateTime(event_date)), 9, 'UTC')] AS `tv:timeRange:beginIncl:val:z64:2k:0:0:0::data`,
    [toDateTime64(toStartOfDay(toDateTime(event_date)) + 86400, 9, 'UTC')] AS `tv:timeRange:endExcl:val:z64:2k:0:0:0::data`

FROM (
    SELECT
        `geoPoint:h3`[1] AS h3_index,
        toDate(`timeRange:beginIncl`[1]) AS event_date,
        `symbol:value` AS symbols
    FROM facts
    WHERE length(`geoPoint:h3`) > 0
)
WHERE event_date = '2026-03-11'
GROUP BY h3_index, event_date
```

### after CanonicalizeFull

```sql
WITH "arrayDistinct"("groupArrayArray"("symbols")) AS "distinct_symbols", "toUInt64"("count"()) AS "event_count" SELECT "cityHash64"("h3_index", "event_date") AS "id:id:u64:47::0:", "concat"('COMPOSITE-H3-', "toString"("h3_index"), '-20260311') AS "id:naturalKey:y:4::0:", "distinct_symbols" AS "tv:symbol:value:val:s:124::I:0::data", "array"("event_count") AS "tv:u64Array:value:val:u64h:4:::0::data", "array"("concat"('Regional summary: ', "toString"("event_count"), ' events. Includes: ', "arrayStringConcat"("distinct_symbols", ', '))) AS "tv:text:text:val:s:0:0:0:0::", "array"("CAST"(0.0, 'Float32')) AS "tv:geoPoint:pointLat:val:f32:g:0:0:0::geo", "array"("CAST"(0.0, 'Float32')) AS "tv:geoPoint:pointLng:val:f32:g:0:0:0::geo", "array"("h3_index") AS "tv:geoPoint:h3:val:u64:g:0:0:0::geo", "array"("toDateTime64"("toStartOfDay"("toDateTime"("event_date")), 9, 'UTC')) AS "tv:timeRange:beginIncl:val:z64:2k:0:0:0::data", "array"("toDateTime64"("toStartOfDay"("toDateTime"("event_date")) + 86400, 9, 'UTC')) AS "tv:timeRange:endExcl:val:z64:2k:0:0:0::data" FROM ( SELECT "arrayElement"("geoPoint:h3", 1) AS "h3_index", "toDate"("arrayElement"("timeRange:beginIncl", 1)) AS "event_date", "symbol:value" AS "symbols" FROM "facts" WHERE "length"("geoPoint:h3") > 0 ) WHERE "event_date" = '2026-03-11' GROUP BY "h3_index", "event_date"
```

### after QualifyTables

```sql
WITH "arrayDistinct"("groupArrayArray"("symbols")) AS "distinct_symbols", "toUInt64"("count"()) AS "event_count" SELECT "cityHash64"("h3_index", "event_date") AS "id:id:u64:47::0:", "concat"('COMPOSITE-H3-', "toString"("h3_index"), '-20260311') AS "id:naturalKey:y:4::0:", "distinct_symbols" AS "tv:symbol:value:val:s:124::I:0::data", "array"("event_count") AS "tv:u64Array:value:val:u64h:4:::0::data", "array"("concat"('Regional summary: ', "toString"("event_count"), ' events. Includes: ', "arrayStringConcat"("distinct_symbols", ', '))) AS "tv:text:text:val:s:0:0:0:0::", "array"("CAST"(0.0, 'Float32')) AS "tv:geoPoint:pointLat:val:f32:g:0:0:0::geo", "array"("CAST"(0.0, 'Float32')) AS "tv:geoPoint:pointLng:val:f32:g:0:0:0::geo", "array"("h3_index") AS "tv:geoPoint:h3:val:u64:g:0:0:0::geo", "array"("toDateTime64"("toStartOfDay"("toDateTime"("event_date")), 9, 'UTC')) AS "tv:timeRange:beginIncl:val:z64:2k:0:0:0::data", "array"("toDateTime64"("toStartOfDay"("toDateTime"("event_date")) + 86400, 9, 'UTC')) AS "tv:timeRange:endExcl:val:z64:2k:0:0:0::data" FROM ( SELECT "arrayElement"("geoPoint:h3", 1) AS "h3_index", "toDate"("arrayElement"("timeRange:beginIncl", 1)) AS "event_date", "symbol:value" AS "symbols" FROM "anchor"."facts" WHERE "length"("geoPoint:h3") > 0 ) WHERE "event_date" = '2026-03-11' GROUP BY "h3_index", "event_date"
```

### after ResolveColumnNames

```sql
WITH "arrayDistinct"("groupArrayArray"("symbols")) AS "distinct_symbols", "toUInt64"("count"()) AS "event_count" SELECT "cityHash64"("h3_index", "event_date") AS "id:id:u64:47::0:", "concat"('COMPOSITE-H3-', "toString"("h3_index"), '-20260311') AS "id:naturalKey:y:4::0:", "distinct_symbols" AS "tv:symbol:value:val:s:124::I:0::data", "array"("event_count") AS "tv:u64Array:value:val:u64h:4:::0::data", "array"("concat"('Regional summary: ', "toString"("event_count"), ' events. Includes: ', "arrayStringConcat"("distinct_symbols", ', '))) AS "tv:text:text:val:s:0:0:0:0::", "array"("CAST"(0.0, 'Float32')) AS "tv:geoPoint:pointLat:val:f32:g:0:0:0::geo", "array"("CAST"(0.0, 'Float32')) AS "tv:geoPoint:pointLng:val:f32:g:0:0:0::geo", "array"("h3_index") AS "tv:geoPoint:h3:val:u64:g:0:0:0::geo", "array"("toDateTime64"("toStartOfDay"("toDateTime"("event_date")), 9, 'UTC')) AS "tv:timeRange:beginIncl:val:z64:2k:0:0:0::data", "array"("toDateTime64"("toStartOfDay"("toDateTime"("event_date")) + 86400, 9, 'UTC')) AS "tv:timeRange:endExcl:val:z64:2k:0:0:0::data" FROM ( SELECT "arrayElement"("tv:geoPoint:h3:val:u64:4:::0::geo", 1) AS "h3_index", "toDate"("arrayElement"("tv:timeRange:beginIncl:val:z64:47:::0::data", 1)) AS "event_date", "tv:symbol:value:val:s:124::I:0::data" AS "symbols" FROM "anchor"."facts" WHERE "length"("tv:geoPoint:h3:val:u64:4:::0::geo") > 0 ) WHERE "event_date" = '2026-03-11' GROUP BY "h3_index", "event_date"
```

## card_anchor_dql_query6.sql

### source (friendly handles)

```sql
/* Query 6 — an integrity scanner over leeway's support-column invariants.

Checks the structural consistency of the geoPoint and text sections: base
value arrays and per-attribute cardinality columns must have equal lengths,
and each flattened channel (refs, co-containers) must sum to its cardinality
column. A returned row means a transformation corrupted the shape; extend the
UNION ALL with one branch per further section.

Support columns are addressed through the same friendly handles as value
columns (`geoPoint:hr`, `geoPoint:hrcard`, `text:len`, …) — the resolver knows
every column of a section, value and support alike.
*/
SELECT
    id,
    naturalKey,
    section,
    error_type,
    base_attribute_count,
    expected_flattened_length,
    actual_flattened_length
FROM (
    -- geoPoint section
    SELECT
        `id:id` AS id,
        `id:naturalKey` AS naturalKey,
        'GeoPoint' AS section,
        multiIf(
            -- base arrays and cardinality columns must have identical lengths
            length(`geoPoint:pointLat`) != length(`geoPoint:hrcard`), 'Base/Card Length Mismatch',
            length(`geoPoint:pointLat`) != length(`geoPoint:lrcard`), 'Base/Card Length Mismatch',
            -- each flattened ref channel must sum to its cardinality column
            length(`geoPoint:hr`) != toUInt64(arraySum(`geoPoint:hrcard`)), 'High-Card (hr) Integrity Failure',
            length(`geoPoint:lr`) != toUInt64(arraySum(`geoPoint:lrcard`)), 'Low-Card (lr) Integrity Failure',
            length(`geoPoint:lmr`) != toUInt64(arraySum(`geoPoint:lmrcard`)), 'Mixed-Card (lmr) Integrity Failure',
            length(`geoPoint:mrhp`) != toUInt64(arraySum(`geoPoint:lmrcard`)), 'Mixed-Card Payload (mrhp) Integrity Failure',
            'Valid'
        ) AS error_type,
        length(`geoPoint:pointLat`) AS base_attribute_count,
        toUInt64(arraySum(`geoPoint:hrcard`)) AS expected_flattened_length,
        length(`geoPoint:hr`) AS actual_flattened_length
    FROM facts

    UNION ALL

    -- text section (co-containers)
    SELECT
        `id:id` AS id,
        `id:naturalKey` AS naturalKey,
        'Text' AS section,
        multiIf(
            -- support-column lengths must match the base string array
            length(`text:text`) != length(`text:len`), 'Base/Len Length Mismatch',
            -- flattened co-containers must sum to the len column
            length(`text:wordLength`) != toUInt64(arraySum(`text:len`)), 'Co-Container (wordLength) Integrity Failure',
            length(`text:wordBag`) != toUInt64(arraySum(`text:len`)), 'Co-Container (wordBag) Integrity Failure',
            'Valid'
        ) AS error_type,
        length(`text:text`) AS base_attribute_count,
        toUInt64(arraySum(`text:len`)) AS expected_flattened_length,
        length(`text:wordBag`) AS actual_flattened_length
    FROM facts
)
WHERE error_type != 'Valid'
```

### after StripComments

```sql
 
SELECT
    id,
    naturalKey,
    section,
    error_type,
    base_attribute_count,
    expected_flattened_length,
    actual_flattened_length
FROM (
         SELECT
        `id:id` AS id,
        `id:naturalKey` AS naturalKey,
        'GeoPoint' AS section,
        multiIf(
                         length(`geoPoint:pointLat`) != length(`geoPoint:hrcard`), 'Base/Card Length Mismatch',
            length(`geoPoint:pointLat`) != length(`geoPoint:lrcard`), 'Base/Card Length Mismatch',
                         length(`geoPoint:hr`) != toUInt64(arraySum(`geoPoint:hrcard`)), 'High-Card (hr) Integrity Failure',
            length(`geoPoint:lr`) != toUInt64(arraySum(`geoPoint:lrcard`)), 'Low-Card (lr) Integrity Failure',
            length(`geoPoint:lmr`) != toUInt64(arraySum(`geoPoint:lmrcard`)), 'Mixed-Card (lmr) Integrity Failure',
            length(`geoPoint:mrhp`) != toUInt64(arraySum(`geoPoint:lmrcard`)), 'Mixed-Card Payload (mrhp) Integrity Failure',
            'Valid'
        ) AS error_type,
        length(`geoPoint:pointLat`) AS base_attribute_count,
        toUInt64(arraySum(`geoPoint:hrcard`)) AS expected_flattened_length,
        length(`geoPoint:hr`) AS actual_flattened_length
    FROM facts

    UNION ALL

         SELECT
        `id:id` AS id,
        `id:naturalKey` AS naturalKey,
        'Text' AS section,
        multiIf(
                         length(`text:text`) != length(`text:len`), 'Base/Len Length Mismatch',
                         length(`text:wordLength`) != toUInt64(arraySum(`text:len`)), 'Co-Container (wordLength) Integrity Failure',
            length(`text:wordBag`) != toUInt64(arraySum(`text:len`)), 'Co-Container (wordBag) Integrity Failure',
            'Valid'
        ) AS error_type,
        length(`text:text`) AS base_attribute_count,
        toUInt64(arraySum(`text:len`)) AS expected_flattened_length,
        length(`text:wordBag`) AS actual_flattened_length
    FROM facts
)
WHERE error_type != 'Valid'
```

### after CanonicalizeFull

```sql
SELECT "id", "naturalKey", "section", "error_type", "base_attribute_count", "expected_flattened_length", "actual_flattened_length" FROM ( SELECT "id:id" AS "id", "id:naturalKey" AS "naturalKey", 'GeoPoint' AS "section", "multiIf"( "length"("geoPoint:pointLat") != "length"("geoPoint:hrcard"), 'Base/Card Length Mismatch', "length"("geoPoint:pointLat") != "length"("geoPoint:lrcard"), 'Base/Card Length Mismatch', "length"("geoPoint:hr") != "toUInt64"("arraySum"("geoPoint:hrcard")), 'High-Card (hr) Integrity Failure', "length"("geoPoint:lr") != "toUInt64"("arraySum"("geoPoint:lrcard")), 'Low-Card (lr) Integrity Failure', "length"("geoPoint:lmr") != "toUInt64"("arraySum"("geoPoint:lmrcard")), 'Mixed-Card (lmr) Integrity Failure', "length"("geoPoint:mrhp") != "toUInt64"("arraySum"("geoPoint:lmrcard")), 'Mixed-Card Payload (mrhp) Integrity Failure', 'Valid' ) AS "error_type", "length"("geoPoint:pointLat") AS "base_attribute_count", "toUInt64"("arraySum"("geoPoint:hrcard")) AS "expected_flattened_length", "length"("geoPoint:hr") AS "actual_flattened_length" FROM "facts" UNION ALL SELECT "id:id" AS "id", "id:naturalKey" AS "naturalKey", 'Text' AS "section", "multiIf"( "length"("text:text") != "length"("text:len"), 'Base/Len Length Mismatch', "length"("text:wordLength") != "toUInt64"("arraySum"("text:len")), 'Co-Container (wordLength) Integrity Failure', "length"("text:wordBag") != "toUInt64"("arraySum"("text:len")), 'Co-Container (wordBag) Integrity Failure', 'Valid' ) AS "error_type", "length"("text:text") AS "base_attribute_count", "toUInt64"("arraySum"("text:len")) AS "expected_flattened_length", "length"("text:wordBag") AS "actual_flattened_length" FROM "facts" ) WHERE "error_type" != 'Valid'
```

### after QualifyTables

```sql
SELECT "id", "naturalKey", "section", "error_type", "base_attribute_count", "expected_flattened_length", "actual_flattened_length" FROM ( SELECT "id:id" AS "id", "id:naturalKey" AS "naturalKey", 'GeoPoint' AS "section", "multiIf"( "length"("geoPoint:pointLat") != "length"("geoPoint:hrcard"), 'Base/Card Length Mismatch', "length"("geoPoint:pointLat") != "length"("geoPoint:lrcard"), 'Base/Card Length Mismatch', "length"("geoPoint:hr") != "toUInt64"("arraySum"("geoPoint:hrcard")), 'High-Card (hr) Integrity Failure', "length"("geoPoint:lr") != "toUInt64"("arraySum"("geoPoint:lrcard")), 'Low-Card (lr) Integrity Failure', "length"("geoPoint:lmr") != "toUInt64"("arraySum"("geoPoint:lmrcard")), 'Mixed-Card (lmr) Integrity Failure', "length"("geoPoint:mrhp") != "toUInt64"("arraySum"("geoPoint:lmrcard")), 'Mixed-Card Payload (mrhp) Integrity Failure', 'Valid' ) AS "error_type", "length"("geoPoint:pointLat") AS "base_attribute_count", "toUInt64"("arraySum"("geoPoint:hrcard")) AS "expected_flattened_length", "length"("geoPoint:hr") AS "actual_flattened_length" FROM "anchor"."facts" UNION ALL SELECT "id:id" AS "id", "id:naturalKey" AS "naturalKey", 'Text' AS "section", "multiIf"( "length"("text:text") != "length"("text:len"), 'Base/Len Length Mismatch', "length"("text:wordLength") != "toUInt64"("arraySum"("text:len")), 'Co-Container (wordLength) Integrity Failure', "length"("text:wordBag") != "toUInt64"("arraySum"("text:len")), 'Co-Container (wordBag) Integrity Failure', 'Valid' ) AS "error_type", "length"("text:text") AS "base_attribute_count", "toUInt64"("arraySum"("text:len")) AS "expected_flattened_length", "length"("text:wordBag") AS "actual_flattened_length" FROM "anchor"."facts" ) WHERE "error_type" != 'Valid'
```

### after ResolveColumnNames

```sql
SELECT "id", "naturalKey", "section", "error_type", "base_attribute_count", "expected_flattened_length", "actual_flattened_length" FROM ( SELECT "id:id:u64:47::0:" AS "id", "id:naturalKey:y:4::0:" AS "naturalKey", 'GeoPoint' AS "section", "multiIf"( "length"("tv:geoPoint:pointLat:val:f32:4:::0::geo") != "length"("tv:geoPoint:hrcard:hrcard:u64:4E:::0::geo"), 'Base/Card Length Mismatch', "length"("tv:geoPoint:pointLat:val:f32:4:::0::geo") != "length"("tv:geoPoint:lrcard:lrcard:u64:4E:::0::geo"), 'Base/Card Length Mismatch', "length"("tv:geoPoint:hr:hr:u64:47:::0::geo") != "toUInt64"("arraySum"("tv:geoPoint:hrcard:hrcard:u64:4E:::0::geo")), 'High-Card (hr) Integrity Failure', "length"("tv:geoPoint:lr:lr:u64:1247:::0::geo") != "toUInt64"("arraySum"("tv:geoPoint:lrcard:lrcard:u64:4E:::0::geo")), 'Low-Card (lr) Integrity Failure', "length"("tv:geoPoint:lmr:lmr:u64:1247:::0::geo") != "toUInt64"("arraySum"("tv:geoPoint:lmrcard:lmrcard:u64:4E:::0::geo")), 'Mixed-Card (lmr) Integrity Failure', "length"("tv:geoPoint:mrhp:mrhp:y:4:::0::geo") != "toUInt64"("arraySum"("tv:geoPoint:lmrcard:lmrcard:u64:4E:::0::geo")), 'Mixed-Card Payload (mrhp) Integrity Failure', 'Valid' ) AS "error_type", "length"("tv:geoPoint:pointLat:val:f32:4:::0::geo") AS "base_attribute_count", "toUInt64"("arraySum"("tv:geoPoint:hrcard:hrcard:u64:4E:::0::geo")) AS "expected_flattened_length", "length"("tv:geoPoint:hr:hr:u64:47:::0::geo") AS "actual_flattened_length" FROM "anchor"."facts" UNION ALL SELECT "id:id:u64:47::0:" AS "id", "id:naturalKey:y:4::0:" AS "naturalKey", 'Text' AS "section", "multiIf"( "length"("tv:text:text:val:s::::0::") != "length"("tv:text:len:len:u64:4D:::0::"), 'Base/Len Length Mismatch', "length"("tv:text:wordLength:val:u32h::::0::") != "toUInt64"("arraySum"("tv:text:len:len:u64:4D:::0::")), 'Co-Container (wordLength) Integrity Failure', "length"("tv:text:wordBag:val:sh::::0::") != "toUInt64"("arraySum"("tv:text:len:len:u64:4D:::0::")), 'Co-Container (wordBag) Integrity Failure', 'Valid' ) AS "error_type", "length"("tv:text:text:val:s::::0::") AS "base_attribute_count", "toUInt64"("arraySum"("tv:text:len:len:u64:4D:::0::")) AS "expected_flattened_length", "length"("tv:text:wordBag:val:sh::::0::") AS "actual_flattened_length" FROM "anchor"."facts" ) WHERE "error_type" != 'Valid'
```

## card_anchor_dql_query7.sql

### source (friendly handles)

```sql
/* Query 7 — a retrieval read: which rows, and why.

Unlike queries 1–6 (which all compute in their projections), this is a plain
information-retrieval read: one table, a verbatim projection, a filtering
WHERE. The ADR-0117 passthrough classifier triages it as such — see the
analysis artifact, where this is the only query with a passthrough table —
and that is the precondition for the ADR-0121 selection-conditions rewrite
(card_anchor_dql_lwsql.out.md), which turns each OR disjunct below into a
result column reporting which alternative admitted the row.
*/
SELECT
    `id:id`,
    `id:naturalKey`,
    `symbol:value`,
    `timeRange:beginIncl`,
    `geoPoint:h3`
FROM facts
WHERE has(`symbol:value`, 'DELIVERED')
   OR has(`symbol:value`, 'SEISMIC_ANOMALY')
```

### after StripComments

```sql
 
SELECT
    `id:id`,
    `id:naturalKey`,
    `symbol:value`,
    `timeRange:beginIncl`,
    `geoPoint:h3`
FROM facts
WHERE has(`symbol:value`, 'DELIVERED')
   OR has(`symbol:value`, 'SEISMIC_ANOMALY')
```

### after CanonicalizeFull

```sql
SELECT "id:id", "id:naturalKey", "symbol:value", "timeRange:beginIncl", "geoPoint:h3" FROM "facts" WHERE "has"("symbol:value", 'DELIVERED') OR "has"("symbol:value", 'SEISMIC_ANOMALY')
```

### after QualifyTables

```sql
SELECT "id:id", "id:naturalKey", "symbol:value", "timeRange:beginIncl", "geoPoint:h3" FROM "anchor"."facts" WHERE "has"("symbol:value", 'DELIVERED') OR "has"("symbol:value", 'SEISMIC_ANOMALY')
```

### after ResolveColumnNames

```sql
SELECT "id:id:u64:47::0:", "id:naturalKey:y:4::0:", "tv:symbol:value:val:s:124::I:0::data", "tv:timeRange:beginIncl:val:z64:47:::0::data", "tv:geoPoint:h3:val:u64:4:::0::geo" FROM "anchor"."facts" WHERE "has"("tv:symbol:value:val:s:124::I:0::data", 'DELIVERED') OR "has"("tv:symbol:value:val:s:124::I:0::data", 'SEISMIC_ANOMALY')
```

