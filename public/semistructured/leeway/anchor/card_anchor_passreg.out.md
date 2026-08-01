---
type: reference
audience: contributor
status: draft
generated: true
generator: go test, TestDqlPassRegistryGeneration
---

> **Status: draft — pre-human-review.** Machine-generated demo artifact;
> regenerate via `go test`, do not edit.

# anchor DQL — the pass registry seam (ADR-0108)

## catalog — what keelson('sql_passes') serves for this registry (ADR-0094)

| stage | order | name | late-bound | description |
|---|---|---|---|---|
| pre-execute | 40 | `StripComments` | false | strip comments from the shipped body |
| pre-execute | 50 | `CanonicalizeFull` | false | rewrite the statement into canonical form |
| pre-execute | 100 | `ExpandLwIdMacros` | false | expand LW_ID_* identity-macro calls into bit arithmetic |
| pre-execute | 150 | `QualifyTables` | false | qualify unqualified table references with the anchor database |
| pre-execute | 200 | `ResolveColumnNames` | true | resolve friendly leeway column handles to physical names |

## observation trace — query 7 through the stage

| order | pass | outcome | changed |
|---|---|---|---|
| 40 | `StripComments` | applied | true |
| 50 | `CanonicalizeFull` | applied | true |
| 100 | `ExpandLwIdMacros` | applied | false |
| 150 | `QualifyTables` | applied | true |
| 200 | `ResolveColumnNames` | applied | true |

## result

```sql
SELECT "id:id:u64:2k:0:0:", "id:naturalKey:y:g:0:0:", "tv:symbol:value:val:s:m:0:24:0::data", "tv:timeRange:beginIncl:val:z64:2k:0:0:0::data", "tv:geoPoint:h3:val:u64:g:0:0:0::geo" FROM "anchor"."facts" WHERE "has"("tv:symbol:value:val:s:m:0:24:0::data", 'DELIVERED') OR "has"("tv:symbol:value:val:s:m:0:24:0::data", 'SEISMIC_ANOMALY')
```
