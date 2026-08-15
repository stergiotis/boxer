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
| pre-execute | 75 | `ExpandDescriptiveStatistics` | false | expand descriptiveStatistics(cols…) into the ADR-0161 distribution result contract |
| pre-execute | 80 | `DocsearchExpand` | false | expand docsearch('query') into the ADR-0164 documentation search UNION |
| pre-execute | 100 | `ExpandLwIdMacros` | false | expand LW_ID_* identity-macro calls into bit arithmetic |
| pre-execute | 120 | `LwExtractExpand` | true | expand LW_GET/LW_GET_NULL/LW_GET_LIST into leeway locate-and-extract expressions |
| pre-execute | 129 | `LwConstructExpandTarget` | true | constructor calls adopt a resolved INSERT target's naming — segments, aspects and spelling |
| pre-execute | 130 | `LwConstructExpand` | false | expand LW_PLAIN/LW_TV* constructor calls into aliased expressions minting physical leeway column names |
| pre-execute | 140 | `GlossExpand` | false | expand gloss(expr, 'media type', 'key', value…) into a `label@media type;k=v` alias, validated against the gloss catalog |
| pre-execute | 150 | `QualifyTables` | false | qualify unqualified table references with the anchor database |
| pre-execute | 200 | `ResolveColumnNames` | true | resolve friendly leeway column handles to physical names |

## observation trace — query 7 through the stage

| order | pass | outcome | changed |
|---|---|---|---|
| 40 | `StripComments` | applied | true |
| 50 | `CanonicalizeFull` | applied | true |
| 75 | `ExpandDescriptiveStatistics` | applied | false |
| 80 | `DocsearchExpand` | applied | false |
| 100 | `ExpandLwIdMacros` | applied | false |
| 120 | `LwExtractExpand` | applied | false |
| 129 | `LwConstructExpandTarget` | applied | false |
| 130 | `LwConstructExpand` | applied | false |
| 140 | `GlossExpand` | applied | false |
| 150 | `QualifyTables` | applied | true |
| 200 | `ResolveColumnNames` | applied | true |

## result

```sql
SELECT "id:id:u64:47::0:", "id:naturalKey:y:4::0:", "tv:symbol:value:val:s:124::I:0::data", "tv:timeRange:beginIncl:val:z64:47:::0::data", "tv:geoPoint:h3:val:u64:4:::0::geo" FROM "anchor"."facts" WHERE "has"("tv:symbol:value:val:s:124::I:0::data", 'DELIVERED') OR "has"("tv:symbol:value:val:s:124::I:0::data", 'SEISMIC_ANOMALY')
```
