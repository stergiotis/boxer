/*
Query 2: The Cross-Domain Correlation (The "Holy Grail")

The Scenario: It is March 11, 2026. A massive snowstorm hits the Alps, while simultaneously, anomalous network activity is detected.

The Goal: Write a single query across our Unified Enterprise Event Bus to find if any Drone (IN_TRANSIT) flew through an active Avalanche Zone (SNOW_SHIFT or SEISMIC_ANOMALY) or an area affected by a Cyber Attack (DDOS).
*/
SELECT
    h3_hex,
    groupUniqArray(entity_type) AS simultaneous_events,
    count() AS total_incidents
FROM (
         SELECT
             `id:id:u64:2k:0:0:` AS id,
             -- Extract the primary symbol as the "Entity Type"
             `tv:symbol:value:val:s:m:0:24:0::data`[1] AS entity_type,
             -- We extract H3 from GeoPoint (Drones/Cyber) OR GeoArea (Avalanche)
             arrayConcat(
                     `tv:geoPoint:h3:val:u64:g:0:0:0::geo`,
                     `tv:geoArea:h3:val:u64m:g:0:0:0::geo`
             ) AS all_h3_indices
         FROM anchor.facts
         -- Restrict to the last 24 hours (March 11, 2026)
         WHERE `tv:timeRange:beginIncl:val:z64:2k:0:0:0::data`[1] >= toDateTime64('2026-03-11 00:00:00', 9, 'UTC')
         )
-- Explode all H3 regions touched by this entity
         ARRAY JOIN all_h3_indices AS h3_hex
GROUP BY h3_hex
-- The Magic: Only show H3 hexes that contain MULTIPLE orthogonal domains at the same time!
HAVING has(simultaneous_events, 'IN_TRANSIT')
   AND (
    has(simultaneous_events, 'SEISMIC_ANOMALY') OR
    has(simultaneous_events, 'DDOS')
    )
ORDER BY total_incidents DESC;
