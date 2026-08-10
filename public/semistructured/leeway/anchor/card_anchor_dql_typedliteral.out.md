---
type: reference
audience: contributor
status: draft
generated: true
generator: go test, TestDqlTypedLiteralGeneration
---

> **Status: draft — pre-human-review.** Machine-generated demo artifact;
> regenerate via `go test`, do not edit.

# anchor DQL — TypedLiteral: Go values to SQL literals and back

## Go value → SQL literal

| Go value | SQL |
|---|---|
| `uint64(61029384)` (the engineered H3 cell) | `61029384` |
| `"leave quietly at the back door"` | `'leave quietly at the back door'` |
| `"it's urgent"` (escaping) | `'it\'s urgent'` |
| `[]string{"DDOS", "SQL_INJECTION", "PORT_SCAN"}` (query 1's IN list) | `array('DDOS', 'SQL_INJECTION', 'PORT_SCAN')` |
| `[]uint64{22, 443, 8123}` | `array(22, 443, 8123)` |
| `float32(0)` with PreserveCasts (query 4/5's zeroed coordinate) | `CAST(0.0, 'Float32')` |
| `[]int32{-1, 0, 1}` with PreserveCasts | `CAST(array(CAST(-1, 'Int32'), CAST(0, 'Int32'), CAST(1, 'Int32')), 'Array(Int32)')` |

## ExtractLiterals on query 2 — literals become named parameters

```sql
SET param_x_has_a261610161681b542ea26ada6b704f = 'IN_TRANSIT';
SET param_x_has_a261610161681b789f31c79d6ae139 = 'SEISMIC_ANOMALY';
SET param_x_has_a261610161681be86f041ba5b2615b = 'DDOS';
SET param_x_todatetime64_a261610061681b5740eb4b493f6d91 = '2026-03-11 00:00:00';
SET param_x_todatetime64_a261610261681b41bc932cf1b36073 = 'UTC';
SELECT "h3_hex", "groupUniqArray"("entity_type") AS "simultaneous_events", "count"() AS "total_incidents" FROM ( SELECT "id:id:u64:2k:0:0:" AS "id", "arrayElement"("tv:symbol:value:val:s:m:0:12:0::data", 1) AS "entity_type", "arrayConcat"("tv:geoPoint:h3:val:u64:g:0:0:0::geo", "tv:geoArea:h3:val:u64m:g:0:0:0::geo") AS "all_h3_indices" FROM "anchor"."facts" WHERE "arrayElement"("tv:timeRange:beginIncl:val:z64:2k:0:0:0::data", 1) >= "toDateTime64"({param_x_todatetime64_a261610061681b5740eb4b493f6d91: String}, 9, {param_x_todatetime64_a261610261681b41bc932cf1b36073: String}) ) ARRAY JOIN "all_h3_indices" AS "h3_hex" GROUP BY "h3_hex" HAVING "has"("simultaneous_events", {param_x_has_a261610161681b542ea26ada6b704f: String}) AND ( "has"("simultaneous_events", {param_x_has_a261610161681b789f31c79d6ae139: String}) OR "has"("simultaneous_events", {param_x_has_a261610161681be86f041ba5b2615b: String}) ) ORDER BY "total_incidents" DESC
```

Each parameter deserializes to a TypedLiteral and marshals back to the
same literal text (the round-trip asserted by this test):

| parameter | literal | context | round-trip |
|---|---|---|---|
| `param_x_has_a261610161681b542ea26ada6b704f` | `'IN_TRANSIT'` | `has` | `'IN_TRANSIT'` |
| `param_x_has_a261610161681b789f31c79d6ae139` | `'SEISMIC_ANOMALY'` | `has` | `'SEISMIC_ANOMALY'` |
| `param_x_has_a261610161681be86f041ba5b2615b` | `'DDOS'` | `has` | `'DDOS'` |
| `param_x_todatetime64_a261610061681b5740eb4b493f6d91` | `'2026-03-11 00:00:00'` | `todatetime64` | `'2026-03-11 00:00:00'` |
| `param_x_todatetime64_a261610261681b41bc932cf1b36073` | `'UTC'` | `todatetime64` | `'UTC'` |

