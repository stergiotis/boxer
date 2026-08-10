---
type: reference
audience: contributor
status: draft
generated: true
generator: go test, TestDqlLwsqlGeneration
---

> **Status: draft — pre-human-review.** Machine-generated demo artifact;
> regenerate via `go test`, do not edit.

# anchor DQL — lwsql selection conditions and result labels

## selection conditions (ADR-0121) on query 7

The WHERE is two OR-ed disjuncts, so each returned row gains one boolean
column per disjunct reporting which alternative admitted it. The rewrite
applies because query 7 passes the ADR-0117 passthrough triage; the
computing queries 1-6 do not, and the pass declines them untouched.

### input (query 7, executable form)

```sql
SELECT "id:id:u64:47::0:", "id:naturalKey:y:4::0:", "tv:symbol:value:val:s:124::I:0::data", "tv:timeRange:beginIncl:val:z64:47:::0::data", "tv:geoPoint:h3:val:u64:4:::0::geo" FROM "anchor"."facts" WHERE "has"("tv:symbol:value:val:s:124::I:0::data", 'DELIVERED') OR "has"("tv:symbol:value:val:s:124::I:0::data", 'SEISMIC_ANOMALY')
```

### plain naming (no namer) — `cond_N` beside the schema

```sql
SELECT "id:id:u64:47::0:", "id:naturalKey:y:4::0:", "tv:symbol:value:val:s:124::I:0::data", "tv:timeRange:beginIncl:val:z64:47:::0::data", "tv:geoPoint:h3:val:u64:4:::0::geo", ("has"("tv:symbol:value:val:s:124::I:0::data", 'DELIVERED')) AS cond_1, ("has"("tv:symbol:value:val:s:124::I:0::data", 'SEISMIC_ANOMALY')) AS cond_2 FROM "anchor"."facts" WHERE cond_1 OR cond_2
```

### leeway naming (resolver as ConditionNamerI) — a `conditions` section

```sql
SELECT "id:id:u64:47::0:", "id:naturalKey:y:4::0:", "tv:symbol:value:val:s:124::I:0::data", "tv:timeRange:beginIncl:val:z64:47:::0::data", "tv:geoPoint:h3:val:u64:4:::0::geo", ("has"("tv:symbol:value:val:s:124::I:0::data", 'DELIVERED')) AS "tv:conditions:c1:val:b::::0::", ("has"("tv:symbol:value:val:s:124::I:0::data", 'SEISMIC_ANOMALY')) AS "tv:conditions:c2:val:b::::0::" FROM "anchor"."facts" WHERE "tv:conditions:c1:val:b::::0::" OR "tv:conditions:c2:val:b::::0::"
```

The synthesized condition columns are physical leeway names, so a result
carrying them classifies back into the schema like any other column:

- `tv:conditions:c1:val:b::::0::` → label `conditions:c1`
- `tv:conditions:c2:val:b::::0::` → label `conditions:c2`

## result labels — BuildLabels over every anchor column

The SQL shipped to the server keeps physical names; a result surface shows
these labels instead (physical name on hover). Support columns and the
plain/backbone columns label the same way.

| physical | label |
|---|---|
| `id:id:u64:47::0:` | `id:id` |
| `id:naturalKey:y:4::0:` | `id:naturalKey` |
| `tv:foreignKey:value:val:u64:4:M::0::foreignKey` | `foreignKey:value` |
| `tv:foreignKey:lr:lr:u64:1247:M::0::foreignKey` | `foreignKey:lr` |
| `tv:foreignKey:lrcard:lrcard:u64:4E:M::0::foreignKey` | `foreignKey:lrcard` |
| `tv:symbol:value:val:s:124::I:0::data` | `symbol:value` |
| `tv:symbol:hr:hr:u64:47:::0::data` | `symbol:hr` |
| `tv:symbol:lr:lr:u64:1247:::0::data` | `symbol:lr` |
| `tv:symbol:lv:lv:y:124:::0::data` | `symbol:lv` |
| `tv:symbol:lmr:lmr:u64:1247:::0::data` | `symbol:lmr` |
| `tv:symbol:mrhp:mrhp:y:4:::0::data` | `symbol:mrhp` |
| `tv:symbol:hrcard:hrcard:u64:4E:::0::data` | `symbol:hrcard` |
| `tv:symbol:lrcard:lrcard:u64:4E:::0::data` | `symbol:lrcard` |
| `tv:symbol:lvcard:lvcard:u64:4E:::0::data` | `symbol:lvcard` |
| `tv:symbol:lmrcard:lmrcard:u64:4E:::0::data` | `symbol:lmrcard` |
| `tv:symbolArray:value:val:sh:4::I:0::data` | `symbolArray:value` |
| `tv:symbolArray:hr:hr:u64:47:::0::data` | `symbolArray:hr` |
| `tv:symbolArray:lr:lr:u64:1247:::0::data` | `symbolArray:lr` |
| `tv:symbolArray:lv:lv:y:124:::0::data` | `symbolArray:lv` |
| `tv:symbolArray:lmr:lmr:u64:1247:::0::data` | `symbolArray:lmr` |
| `tv:symbolArray:mrhp:mrhp:y:4:::0::data` | `symbolArray:mrhp` |
| `tv:symbolArray:len:len:u64:4D:::0::data` | `symbolArray:len` |
| `tv:symbolArray:hrcard:hrcard:u64:4E:::0::data` | `symbolArray:hrcard` |
| `tv:symbolArray:lrcard:lrcard:u64:4E:::0::data` | `symbolArray:lrcard` |
| `tv:symbolArray:lvcard:lvcard:u64:4E:::0::data` | `symbolArray:lvcard` |
| `tv:symbolArray:lmrcard:lmrcard:u64:4E:::0::data` | `symbolArray:lmrcard` |
| `tv:stringArray:value:val:sh:4:::0::data` | `stringArray:value` |
| `tv:stringArray:hr:hr:u64:47:::0::data` | `stringArray:hr` |
| `tv:stringArray:lr:lr:u64:1247:::0::data` | `stringArray:lr` |
| `tv:stringArray:lv:lv:y:124:::0::data` | `stringArray:lv` |
| `tv:stringArray:lmr:lmr:u64:1247:::0::data` | `stringArray:lmr` |
| `tv:stringArray:mrhp:mrhp:y:4:::0::data` | `stringArray:mrhp` |
| `tv:stringArray:len:len:u64:4D:::0::data` | `stringArray:len` |
| `tv:stringArray:hrcard:hrcard:u64:4E:::0::data` | `stringArray:hrcard` |
| `tv:stringArray:lrcard:lrcard:u64:4E:::0::data` | `stringArray:lrcard` |
| `tv:stringArray:lvcard:lvcard:u64:4E:::0::data` | `stringArray:lvcard` |
| `tv:stringArray:lmrcard:lmrcard:u64:4E:::0::data` | `stringArray:lmrcard` |
| `tv:text:text:val:s::::0::` | `text:text` |
| `tv:text:wordLength:val:u32h::::0::` | `text:wordLength` |
| `tv:text:wordBag:val:sh::::0::` | `text:wordBag` |
| `tv:text:hr:hr:u64:47:::0::` | `text:hr` |
| `tv:text:lr:lr:u64:1247:::0::` | `text:lr` |
| `tv:text:lv:lv:y:124:::0::` | `text:lv` |
| `tv:text:lmr:lmr:u64:1247:::0::` | `text:lmr` |
| `tv:text:mrhp:mrhp:y:4:::0::` | `text:mrhp` |
| `tv:text:len:len:u64:4D:::0::` | `text:len` |
| `tv:text:hrcard:hrcard:u64:4E:::0::` | `text:hrcard` |
| `tv:text:lrcard:lrcard:u64:4E:::0::` | `text:lrcard` |
| `tv:text:lvcard:lvcard:u64:4E:::0::` | `text:lvcard` |
| `tv:text:lmrcard:lmrcard:u64:4E:::0::` | `text:lmrcard` |
| `tv:blobArray:value:val:yh:4:::0::data` | `blobArray:value` |
| `tv:blobArray:hr:hr:u64:47:::0::data` | `blobArray:hr` |
| `tv:blobArray:lr:lr:u64:1247:::0::data` | `blobArray:lr` |
| `tv:blobArray:lv:lv:y:124:::0::data` | `blobArray:lv` |
| `tv:blobArray:lmr:lmr:u64:1247:::0::data` | `blobArray:lmr` |
| `tv:blobArray:mrhp:mrhp:y:4:::0::data` | `blobArray:mrhp` |
| `tv:blobArray:len:len:u64:4D:::0::data` | `blobArray:len` |
| `tv:blobArray:hrcard:hrcard:u64:4E:::0::data` | `blobArray:hrcard` |
| `tv:blobArray:lrcard:lrcard:u64:4E:::0::data` | `blobArray:lrcard` |
| `tv:blobArray:lvcard:lvcard:u64:4E:::0::data` | `blobArray:lvcard` |
| `tv:blobArray:lmrcard:lmrcard:u64:4E:::0::data` | `blobArray:lmrcard` |
| `tv:u8Array:value:val:u8h:4:::0::data` | `u8Array:value` |
| `tv:u8Array:hr:hr:u64:47:::0::data` | `u8Array:hr` |
| `tv:u8Array:lr:lr:u64:1247:::0::data` | `u8Array:lr` |
| `tv:u8Array:lv:lv:y:124:::0::data` | `u8Array:lv` |
| `tv:u8Array:lmr:lmr:u64:1247:::0::data` | `u8Array:lmr` |
| `tv:u8Array:mrhp:mrhp:y:4:::0::data` | `u8Array:mrhp` |
| `tv:u8Array:len:len:u64:4D:::0::data` | `u8Array:len` |
| `tv:u8Array:hrcard:hrcard:u64:4E:::0::data` | `u8Array:hrcard` |
| `tv:u8Array:lrcard:lrcard:u64:4E:::0::data` | `u8Array:lrcard` |
| `tv:u8Array:lvcard:lvcard:u64:4E:::0::data` | `u8Array:lvcard` |
| `tv:u8Array:lmrcard:lmrcard:u64:4E:::0::data` | `u8Array:lmrcard` |
| `tv:u16Array:value:val:u16h:4:::0::data` | `u16Array:value` |
| `tv:u16Array:hr:hr:u64:47:::0::data` | `u16Array:hr` |
| `tv:u16Array:lr:lr:u64:1247:::0::data` | `u16Array:lr` |
| `tv:u16Array:lv:lv:y:124:::0::data` | `u16Array:lv` |
| `tv:u16Array:lmr:lmr:u64:1247:::0::data` | `u16Array:lmr` |
| `tv:u16Array:mrhp:mrhp:y:4:::0::data` | `u16Array:mrhp` |
| `tv:u16Array:len:len:u64:4D:::0::data` | `u16Array:len` |
| `tv:u16Array:hrcard:hrcard:u64:4E:::0::data` | `u16Array:hrcard` |
| `tv:u16Array:lrcard:lrcard:u64:4E:::0::data` | `u16Array:lrcard` |
| `tv:u16Array:lvcard:lvcard:u64:4E:::0::data` | `u16Array:lvcard` |
| `tv:u16Array:lmrcard:lmrcard:u64:4E:::0::data` | `u16Array:lmrcard` |
| `tv:u32Array:value:val:u32h:4:::0::data` | `u32Array:value` |
| `tv:u32Array:hr:hr:u64:47:::0::data` | `u32Array:hr` |
| `tv:u32Array:lr:lr:u64:1247:::0::data` | `u32Array:lr` |
| `tv:u32Array:lv:lv:y:124:::0::data` | `u32Array:lv` |
| `tv:u32Array:lmr:lmr:u64:1247:::0::data` | `u32Array:lmr` |
| `tv:u32Array:mrhp:mrhp:y:4:::0::data` | `u32Array:mrhp` |
| `tv:u32Array:len:len:u64:4D:::0::data` | `u32Array:len` |
| `tv:u32Array:hrcard:hrcard:u64:4E:::0::data` | `u32Array:hrcard` |
| `tv:u32Array:lrcard:lrcard:u64:4E:::0::data` | `u32Array:lrcard` |
| `tv:u32Array:lvcard:lvcard:u64:4E:::0::data` | `u32Array:lvcard` |
| `tv:u32Array:lmrcard:lmrcard:u64:4E:::0::data` | `u32Array:lmrcard` |
| `tv:u32Set:value:val:u32m:4:::0::data` | `u32Set:value` |
| `tv:u32Set:hr:hr:u64:47:::0::data` | `u32Set:hr` |
| `tv:u32Set:lr:lr:u64:1247:::0::data` | `u32Set:lr` |
| `tv:u32Set:lv:lv:y:124:::0::data` | `u32Set:lv` |
| `tv:u32Set:lmr:lmr:u64:1247:::0::data` | `u32Set:lmr` |
| `tv:u32Set:mrhp:mrhp:y:4:::0::data` | `u32Set:mrhp` |
| `tv:u32Set:card:card:u64:4E:::0::data` | `u32Set:card` |
| `tv:u32Set:hrcard:hrcard:u64:4E:::0::data` | `u32Set:hrcard` |
| `tv:u32Set:lrcard:lrcard:u64:4E:::0::data` | `u32Set:lrcard` |
| `tv:u32Set:lvcard:lvcard:u64:4E:::0::data` | `u32Set:lvcard` |
| `tv:u32Set:lmrcard:lmrcard:u64:4E:::0::data` | `u32Set:lmrcard` |
| `tv:u64Array:value:val:u64h:4:::0::data` | `u64Array:value` |
| `tv:u64Array:hr:hr:u64:47:::0::data` | `u64Array:hr` |
| `tv:u64Array:lr:lr:u64:1247:::0::data` | `u64Array:lr` |
| `tv:u64Array:lv:lv:y:124:::0::data` | `u64Array:lv` |
| `tv:u64Array:lmr:lmr:u64:1247:::0::data` | `u64Array:lmr` |
| `tv:u64Array:mrhp:mrhp:y:4:::0::data` | `u64Array:mrhp` |
| `tv:u64Array:len:len:u64:4D:::0::data` | `u64Array:len` |
| `tv:u64Array:hrcard:hrcard:u64:4E:::0::data` | `u64Array:hrcard` |
| `tv:u64Array:lrcard:lrcard:u64:4E:::0::data` | `u64Array:lrcard` |
| `tv:u64Array:lvcard:lvcard:u64:4E:::0::data` | `u64Array:lvcard` |
| `tv:u64Array:lmrcard:lmrcard:u64:4E:::0::data` | `u64Array:lmrcard` |
| `tv:u64Set:value:val:u64m:4:::0::data` | `u64Set:value` |
| `tv:u64Set:hr:hr:u64:47:::0::data` | `u64Set:hr` |
| `tv:u64Set:lr:lr:u64:1247:::0::data` | `u64Set:lr` |
| `tv:u64Set:lv:lv:y:124:::0::data` | `u64Set:lv` |
| `tv:u64Set:lmr:lmr:u64:1247:::0::data` | `u64Set:lmr` |
| `tv:u64Set:mrhp:mrhp:y:4:::0::data` | `u64Set:mrhp` |
| `tv:u64Set:card:card:u64:4E:::0::data` | `u64Set:card` |
| `tv:u64Set:hrcard:hrcard:u64:4E:::0::data` | `u64Set:hrcard` |
| `tv:u64Set:lrcard:lrcard:u64:4E:::0::data` | `u64Set:lrcard` |
| `tv:u64Set:lvcard:lvcard:u64:4E:::0::data` | `u64Set:lvcard` |
| `tv:u64Set:lmrcard:lmrcard:u64:4E:::0::data` | `u64Set:lmrcard` |
| `tv:i8Array:value:val:i8h:4:::0::data` | `i8Array:value` |
| `tv:i8Array:hr:hr:u64:47:::0::data` | `i8Array:hr` |
| `tv:i8Array:lr:lr:u64:1247:::0::data` | `i8Array:lr` |
| `tv:i8Array:lv:lv:y:124:::0::data` | `i8Array:lv` |
| `tv:i8Array:lmr:lmr:u64:1247:::0::data` | `i8Array:lmr` |
| `tv:i8Array:mrhp:mrhp:y:4:::0::data` | `i8Array:mrhp` |
| `tv:i8Array:len:len:u64:4D:::0::data` | `i8Array:len` |
| `tv:i8Array:hrcard:hrcard:u64:4E:::0::data` | `i8Array:hrcard` |
| `tv:i8Array:lrcard:lrcard:u64:4E:::0::data` | `i8Array:lrcard` |
| `tv:i8Array:lvcard:lvcard:u64:4E:::0::data` | `i8Array:lvcard` |
| `tv:i8Array:lmrcard:lmrcard:u64:4E:::0::data` | `i8Array:lmrcard` |
| `tv:i16Array:value:val:i16h:4:::0::data` | `i16Array:value` |
| `tv:i16Array:hr:hr:u64:47:::0::data` | `i16Array:hr` |
| `tv:i16Array:lr:lr:u64:1247:::0::data` | `i16Array:lr` |
| `tv:i16Array:lv:lv:y:124:::0::data` | `i16Array:lv` |
| `tv:i16Array:lmr:lmr:u64:1247:::0::data` | `i16Array:lmr` |
| `tv:i16Array:mrhp:mrhp:y:4:::0::data` | `i16Array:mrhp` |
| `tv:i16Array:len:len:u64:4D:::0::data` | `i16Array:len` |
| `tv:i16Array:hrcard:hrcard:u64:4E:::0::data` | `i16Array:hrcard` |
| `tv:i16Array:lrcard:lrcard:u64:4E:::0::data` | `i16Array:lrcard` |
| `tv:i16Array:lvcard:lvcard:u64:4E:::0::data` | `i16Array:lvcard` |
| `tv:i16Array:lmrcard:lmrcard:u64:4E:::0::data` | `i16Array:lmrcard` |
| `tv:i32Array:value:val:i32h:4:::0::data` | `i32Array:value` |
| `tv:i32Array:hr:hr:u64:47:::0::data` | `i32Array:hr` |
| `tv:i32Array:lr:lr:u64:1247:::0::data` | `i32Array:lr` |
| `tv:i32Array:lv:lv:y:124:::0::data` | `i32Array:lv` |
| `tv:i32Array:lmr:lmr:u64:1247:::0::data` | `i32Array:lmr` |
| `tv:i32Array:mrhp:mrhp:y:4:::0::data` | `i32Array:mrhp` |
| `tv:i32Array:len:len:u64:4D:::0::data` | `i32Array:len` |
| `tv:i32Array:hrcard:hrcard:u64:4E:::0::data` | `i32Array:hrcard` |
| `tv:i32Array:lrcard:lrcard:u64:4E:::0::data` | `i32Array:lrcard` |
| `tv:i32Array:lvcard:lvcard:u64:4E:::0::data` | `i32Array:lvcard` |
| `tv:i32Array:lmrcard:lmrcard:u64:4E:::0::data` | `i32Array:lmrcard` |
| `tv:i64Array:value:val:i64h:4:::0::data` | `i64Array:value` |
| `tv:i64Array:hr:hr:u64:47:::0::data` | `i64Array:hr` |
| `tv:i64Array:lr:lr:u64:1247:::0::data` | `i64Array:lr` |
| `tv:i64Array:lv:lv:y:124:::0::data` | `i64Array:lv` |
| `tv:i64Array:lmr:lmr:u64:1247:::0::data` | `i64Array:lmr` |
| `tv:i64Array:mrhp:mrhp:y:4:::0::data` | `i64Array:mrhp` |
| `tv:i64Array:len:len:u64:4D:::0::data` | `i64Array:len` |
| `tv:i64Array:hrcard:hrcard:u64:4E:::0::data` | `i64Array:hrcard` |
| `tv:i64Array:lrcard:lrcard:u64:4E:::0::data` | `i64Array:lrcard` |
| `tv:i64Array:lvcard:lvcard:u64:4E:::0::data` | `i64Array:lvcard` |
| `tv:i64Array:lmrcard:lmrcard:u64:4E:::0::data` | `i64Array:lmrcard` |
| `tv:timeRange:beginIncl:val:z64:47:::0::data` | `timeRange:beginIncl` |
| `tv:timeRange:endExcl:val:z64:47:::0::data` | `timeRange:endExcl` |
| `tv:timeRange:hr:hr:u64:47:::0::data` | `timeRange:hr` |
| `tv:timeRange:lr:lr:u64:1247:::0::data` | `timeRange:lr` |
| `tv:timeRange:lv:lv:y:124:::0::data` | `timeRange:lv` |
| `tv:timeRange:lmr:lmr:u64:1247:::0::data` | `timeRange:lmr` |
| `tv:timeRange:mrhp:mrhp:y:4:::0::data` | `timeRange:mrhp` |
| `tv:timeRange:hrcard:hrcard:u64:4E:::0::data` | `timeRange:hrcard` |
| `tv:timeRange:lrcard:lrcard:u64:4E:::0::data` | `timeRange:lrcard` |
| `tv:timeRange:lvcard:lvcard:u64:4E:::0::data` | `timeRange:lvcard` |
| `tv:timeRange:lmrcard:lmrcard:u64:4E:::0::data` | `timeRange:lmrcard` |
| `tv:f32Array:value:val:f32h:4A:::0::data` | `f32Array:value` |
| `tv:f32Array:hr:hr:u64:47:::0::data` | `f32Array:hr` |
| `tv:f32Array:lr:lr:u64:1247:::0::data` | `f32Array:lr` |
| `tv:f32Array:lv:lv:y:124:::0::data` | `f32Array:lv` |
| `tv:f32Array:lmr:lmr:u64:1247:::0::data` | `f32Array:lmr` |
| `tv:f32Array:mrhp:mrhp:y:4:::0::data` | `f32Array:mrhp` |
| `tv:f32Array:len:len:u64:4D:::0::data` | `f32Array:len` |
| `tv:f32Array:hrcard:hrcard:u64:4E:::0::data` | `f32Array:hrcard` |
| `tv:f32Array:lrcard:lrcard:u64:4E:::0::data` | `f32Array:lrcard` |
| `tv:f32Array:lvcard:lvcard:u64:4E:::0::data` | `f32Array:lvcard` |
| `tv:f32Array:lmrcard:lmrcard:u64:4E:::0::data` | `f32Array:lmrcard` |
| `tv:f64Array:value:val:f64h:4A:::0::data` | `f64Array:value` |
| `tv:f64Array:hr:hr:u64:47:::0::data` | `f64Array:hr` |
| `tv:f64Array:lr:lr:u64:1247:::0::data` | `f64Array:lr` |
| `tv:f64Array:lv:lv:y:124:::0::data` | `f64Array:lv` |
| `tv:f64Array:lmr:lmr:u64:1247:::0::data` | `f64Array:lmr` |
| `tv:f64Array:mrhp:mrhp:y:4:::0::data` | `f64Array:mrhp` |
| `tv:f64Array:len:len:u64:4D:::0::data` | `f64Array:len` |
| `tv:f64Array:hrcard:hrcard:u64:4E:::0::data` | `f64Array:hrcard` |
| `tv:f64Array:lrcard:lrcard:u64:4E:::0::data` | `f64Array:lrcard` |
| `tv:f64Array:lvcard:lvcard:u64:4E:::0::data` | `f64Array:lvcard` |
| `tv:f64Array:lmrcard:lmrcard:u64:4E:::0::data` | `f64Array:lmrcard` |
| `tv:geoPoint:pointLat:val:f32:4:::0::geo` | `geoPoint:pointLat` |
| `tv:geoPoint:pointLng:val:f32:4:::0::geo` | `geoPoint:pointLng` |
| `tv:geoPoint:h3:val:u64:4:::0::geo` | `geoPoint:h3` |
| `tv:geoPoint:hr:hr:u64:47:::0::geo` | `geoPoint:hr` |
| `tv:geoPoint:lr:lr:u64:1247:::0::geo` | `geoPoint:lr` |
| `tv:geoPoint:lv:lv:y:124:::0::geo` | `geoPoint:lv` |
| `tv:geoPoint:lmr:lmr:u64:1247:::0::geo` | `geoPoint:lmr` |
| `tv:geoPoint:mrhp:mrhp:y:4:::0::geo` | `geoPoint:mrhp` |
| `tv:geoPoint:hrcard:hrcard:u64:4E:::0::geo` | `geoPoint:hrcard` |
| `tv:geoPoint:lrcard:lrcard:u64:4E:::0::geo` | `geoPoint:lrcard` |
| `tv:geoPoint:lvcard:lvcard:u64:4E:::0::geo` | `geoPoint:lvcard` |
| `tv:geoPoint:lmrcard:lmrcard:u64:4E:::0::geo` | `geoPoint:lmrcard` |
| `tv:geoArea:polyLat:val:f32h:4:::0::geo` | `geoArea:polyLat` |
| `tv:geoArea:polyLng:val:f32h:4:::0::geo` | `geoArea:polyLng` |
| `tv:geoArea:h3:val:u64m:4:::0::geo` | `geoArea:h3` |
| `tv:geoArea:hr:hr:u64:47:::0::geo` | `geoArea:hr` |
| `tv:geoArea:lr:lr:u64:1247:::0::geo` | `geoArea:lr` |
| `tv:geoArea:lv:lv:y:124:::0::geo` | `geoArea:lv` |
| `tv:geoArea:lmr:lmr:u64:1247:::0::geo` | `geoArea:lmr` |
| `tv:geoArea:mrhp:mrhp:y:4:::0::geo` | `geoArea:mrhp` |
| `tv:geoArea:len:len:u64:4D:::0::geo` | `geoArea:len` |
| `tv:geoArea:card:card:u64:4E:::0::geo` | `geoArea:card` |
| `tv:geoArea:hrcard:hrcard:u64:4E:::0::geo` | `geoArea:hrcard` |
| `tv:geoArea:lrcard:lrcard:u64:4E:::0::geo` | `geoArea:lrcard` |
| `tv:geoArea:lvcard:lvcard:u64:4E:::0::geo` | `geoArea:lvcard` |
| `tv:geoArea:lmrcard:lmrcard:u64:4E:::0::geo` | `geoArea:lmrcard` |
| `tv:timeArray:value:val:z64h:4:::0::data` | `timeArray:value` |
| `tv:timeArray:hr:hr:u64:47:::0::data` | `timeArray:hr` |
| `tv:timeArray:lr:lr:u64:1247:::0::data` | `timeArray:lr` |
| `tv:timeArray:lv:lv:y:124:::0::data` | `timeArray:lv` |
| `tv:timeArray:lmr:lmr:u64:1247:::0::data` | `timeArray:lmr` |
| `tv:timeArray:mrhp:mrhp:y:4:::0::data` | `timeArray:mrhp` |
| `tv:timeArray:len:len:u64:4D:::0::data` | `timeArray:len` |
| `tv:timeArray:hrcard:hrcard:u64:4E:::0::data` | `timeArray:hrcard` |
| `tv:timeArray:lrcard:lrcard:u64:4E:::0::data` | `timeArray:lrcard` |
| `tv:timeArray:lvcard:lvcard:u64:4E:::0::data` | `timeArray:lvcard` |
| `tv:timeArray:lmrcard:lmrcard:u64:4E:::0::data` | `timeArray:lmrcard` |
