/*
Query 4: Silver Tier – The PII "Data Cleanroom" Transformation

The Scenario: By law, AeroDrop must share its drone flight paths with the Swiss Federal Office of Civil Aviation (FOCA). However, they cannot share the exact GPS delivery coordinates (Lat/Lng) or the hr Customer UUIDs attached to the GeoPoint section.
The Solution: We create a sanitized Silver Leeway entity. We keep the Symbol and TimeRange, but we use ClickHouse arrayMap to zero out the exact coordinates and discard the membership columns, leaving only the anonymized H3 hex index.
*/
-- Creates a sanitized Silver Leeway table for third-party sharing
SELECT
    `id:id:u64:2k:0:0:` AS id,
    `id:naturalKey:y:g:0:0:`,

    -- 1. Pass through Symbol and TimeRange sections untouched
    `tv:symbol:value:val:s:m:0:24:0::data`,
    `tv:symbol:lrcard:lrcard:u64:4gw:0:0:0::data`,
    `tv:timeRange:beginIncl:val:z64:2k:0:0:0::data`,
    `tv:timeRange:endExcl:val:z64:2k:0:0:0::data`,

    -- 2. Anonymize GeoPoint: Zero out Lat/Lng using arrayMap, but keep H3
    arrayMap(x -> CAST(0.0 AS Float32), `tv:geoPoint:pointLat:val:f32:g:0:0:0::geo`) AS `tv:geoPoint:pointLat:val:f32:g:0:0:0::geo`,
    arrayMap(x -> CAST(0.0 AS Float32), `tv:geoPoint:pointLng:val:f32:g:0:0:0::geo`) AS `tv:geoPoint:pointLng:val:f32:g:0:0:0::geo`,
    `tv:geoPoint:h3:val:u64:g:0:0:0::geo` AS `tv:geoPoint:h3:val:u64:g:0:0:0::geo`,

    -- 3. Erase High-Cardinality references (Customer UUIDs) by overwriting with empty arrays
    CAST([] AS Array(UInt64)) AS `tv:geoPoint:hr:hr:u64:2k:0:0:0::geo`,
    CAST([] AS Array(UInt64)) AS `tv:geoPoint:hrcard:hrcard:u64:4gw:0:0:0::geo`

-- ... (pass through empty arrays for remaining sections)
FROM anchor.facts
-- Filter only for Drone events
WHERE has(`tv:symbol:value:val:s:m:0:24:0::data`, 'DELIVERED')
   OR has(`tv:symbol:value:val:s:m:0:24:0::data`, 'IN_TRANSIT');
