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
