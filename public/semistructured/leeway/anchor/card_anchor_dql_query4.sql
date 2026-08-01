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
