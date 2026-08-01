---
type: reference
audience: contributor
status: draft
generated: true
generator: go test, TestDqlAnalysisGeneration
---

> **Status: draft — pre-human-review.** Machine-generated demo artifact;
> regenerate via `go test`, do not edit.

# anchor DQL — nanopass analysis of the executable queries

## card_anchor_dql_query1.sql

- statement kind: `read-only`
- security class: `read`
- tables: `anchor.facts`
- passthrough tables (ADR-0117): (none)
- columns (8 refs): `attack_type`, `id:id:u64:2k:0:0:`, `id:naturalKey:y:g:0:0:`, `target_ports`, `tv:symbol:lr:lr:u64:2q:0:0:0::data`, `tv:symbol:lrcard:lrcard:u64:4gw:0:0:0::data`, `tv:symbol:value:val:s:m:0:24:0::data`
- functions: `ANCHOR_UNFLATTEN_LEEWAY_ARRAY`, `array`, `has`

## card_anchor_dql_query2.sql

- statement kind: `read-only`
- security class: `read`
- tables: `anchor.facts`
- passthrough tables (ADR-0117): (none)
- columns (13 refs): `all_h3_indices`, `entity_type`, `h3_hex`, `id:id:u64:2k:0:0:`, `simultaneous_events`, `total_incidents`, `tv:geoArea:h3:val:u64m:g:0:0:0::geo`, `tv:geoPoint:h3:val:u64:g:0:0:0::geo`, `tv:symbol:value:val:s:m:0:24:0::data`, `tv:timeRange:beginIncl:val:z64:2k:0:0:0::data`
- functions: `arrayConcat`, `arrayElement`, `count`, `groupUniqArray`, `has`, `toDateTime64`

## card_anchor_dql_query3.sql

- statement kind: `read-only`
- security class: `read`
- tables: `anchor.facts`
- passthrough tables (ADR-0117): (none)
- columns (7 refs): `id:id:u64:2k:0:0:`, `tv:symbol:value:val:s:m:0:24:0::data`, `tv:text:text:val:s:0:0:0:0::`, `tv:text:wordBag:val:sh:0:0:0:0::`, `w`
- functions: `array`, `arrayElement`, `arrayExists`, `arrayFilter`, `arrayStringConcat`

## card_anchor_dql_query4.sql

- statement kind: `read-only`
- security class: `read`
- tables: `anchor.facts`
- passthrough tables (ADR-0117): (none)
- columns (11 refs): `id:id:u64:2k:0:0:`, `id:naturalKey:y:g:0:0:`, `tv:geoPoint:h3:val:u64:g:0:0:0::geo`, `tv:geoPoint:pointLat:val:f32:g:0:0:0::geo`, `tv:geoPoint:pointLng:val:f32:g:0:0:0::geo`, `tv:symbol:lrcard:lrcard:u64:4gw:0:0:0::data`, `tv:symbol:value:val:s:m:0:24:0::data`, `tv:timeRange:beginIncl:val:z64:2k:0:0:0::data`, `tv:timeRange:endExcl:val:z64:2k:0:0:0::data`
- functions: `CAST`, `array`, `arrayMap`, `has`

## card_anchor_dql_query5.sql

- statement kind: `read-only`
- security class: `read`
- tables: `anchor.facts`
- passthrough tables (ADR-0117): (none)
- columns (18 refs): `distinct_symbols`, `event_count`, `event_date`, `h3_index`, `symbols`, `tv:geoPoint:h3:val:u64:g:0:0:0::geo`, `tv:symbol:value:val:s:m:0:24:0::data`, `tv:timeRange:beginIncl:val:z64:2k:0:0:0::data`
- functions: `CAST`, `array`, `arrayDistinct`, `arrayElement`, `arrayStringConcat`, `cityHash64`, `concat`, `count`, `groupArrayArray`, `length`, `toDate`, `toDateTime`, `toDateTime64`, `toStartOfDay`, `toString`, `toUInt64`

## card_anchor_dql_query6.sql

- statement kind: `read-only`
- security class: `read`
- tables: `anchor.facts`, `anchor.facts`
- passthrough tables (ADR-0117): (none)
- columns (36 refs): `actual_flattened_length`, `base_attribute_count`, `error_type`, `expected_flattened_length`, `id`, `id:id:u64:2k:0:0:`, `id:naturalKey:y:g:0:0:`, `naturalKey`, `section`, `tv:geoPoint:hr:hr:u64:2k:0:0:0::geo`, `tv:geoPoint:hrcard:hrcard:u64:4gw:0:0:0::geo`, `tv:geoPoint:lmr:lmr:u64:2q:0:0:0::geo`, `tv:geoPoint:lmrcard:lmrcard:u64:4gw:0:0:0::geo`, `tv:geoPoint:lr:lr:u64:2q:0:0:0::geo`, `tv:geoPoint:lrcard:lrcard:u64:4gw:0:0:0::geo`, `tv:geoPoint:mrhp:mrhp:y:g:0:0:0::geo`, `tv:geoPoint:pointLat:val:f32:g:0:0:0::geo`, `tv:text:len:len:u64:28o:0:0:0::`, `tv:text:text:val:s:0:0:0:0::`, `tv:text:wordBag:val:sh:0:0:0:0::`, `tv:text:wordLength:val:u32h:0:0:0:0::`
- functions: `arraySum`, `length`, `multiIf`, `toUInt64`

## card_anchor_dql_query7.sql

- statement kind: `read-only`
- security class: `read`
- tables: `anchor.facts`
- passthrough tables (ADR-0117): `anchor.facts`
- columns (7 refs): `id:id:u64:2k:0:0:`, `id:naturalKey:y:g:0:0:`, `tv:geoPoint:h3:val:u64:g:0:0:0::geo`, `tv:symbol:value:val:s:m:0:24:0::data`, `tv:timeRange:beginIncl:val:z64:2k:0:0:0::data`
- functions: `has`

## card_anchor_udf_unflatten_leeway_array.sql

- statement kind: `mutating`
