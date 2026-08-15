/* Query 4 — a sanitized "silver" projection for third-party sharing.

The drone operator must hand flight data to an aviation authority without the
exact GPS coordinates or the customer refs attached to the geoPoint section.
This keeps symbol and timeRange, zeroes the coordinates, keeps the coarse H3
index, and blanks the high-cardinality membership columns.

Two naming mechanisms combine so the result is itself leeway-shaped:

  - An unaliased handle (`id:id`, `symbol:value`, …) resolves to its physical
    column, and ClickHouse derives the result name from the rewritten
    expression — the output column is automatically the physical name.
  - A computed column is wrapped in a constructor (ADR-0181 §SD2): LW_TV
    mints the physical tagged-value name, LW_TV_MEMB / LW_TV_SUPPORT the
    membership lanes, each expanding into `<expr> AS "<physical name>"`
    before the statement ships. Minted names carry fresh-table default
    segments rather than the source's aspect hints, and the folded spelling
    (`geoPoint` mints as `geo-point`, the same fold the membership registry
    applies) — the name records authoring intent, and the intent here is
    "these lanes were transformed".

geoPoint is re-minted whole — h3 included, though it passes through
unchanged — because a section's columns must agree on their name segments:
mixing the source's `geo`-group physicals with fresh mints would leave a
section no reader classifies as one.
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
    LW_TV(arrayMap(x -> CAST(0.0 AS Float32), `geoPoint:pointLat`), 'geoPoint', 'pointLat', 'f32'),
    LW_TV(arrayMap(x -> CAST(0.0 AS Float32), `geoPoint:pointLng`), 'geoPoint', 'pointLng', 'f32'),
    LW_TV(`geoPoint:h3`, 'geoPoint', 'h3', 'u64'),

    -- erase the high-cardinality references (customer ids)
    LW_TV_MEMB(CAST([] AS Array(UInt64)), 'geoPoint', 'high-card-ref'),
    LW_TV_SUPPORT(CAST([] AS Array(UInt64)), 'geoPoint', 'hrcard')

FROM facts
WHERE has(`symbol:value`, 'DELIVERED')
   OR has(`symbol:value`, 'IN_TRANSIT')
