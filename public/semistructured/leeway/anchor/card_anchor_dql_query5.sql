/*
Query 5: Gold Tier – "Daily Regional Risk Composite" (Data Synthesis)

The Scenario: The Swiss Crisis Management team wants a daily dashboard summarizing the health of the country. They don't care about individual drones, cyber pings, or snow loads. They want a single Gold Entity per H3 Region per Day that aggregates all anomalies.
The Solution: We group our orthogonal Bronze events (Avalanche + Cyber + Drone) by H3 index and Date (March 11, 2026). We synthesize a brand-new Leeway entity where ClickHouse aggregation functions natively map to Leeway's Array(T) lists!
*/
WITH
    arrayDistinct(groupArrayArray(`tv:symbol:value:val:s:m:0:24:0::data`)) AS distinct_symbols,
    toUInt64(count()) AS event_count
SELECT
    -- 1. Generate a new synthetic Entity ID and NaturalKey based on H3 and Date
    cityHash64(h3_index, event_date) AS id,
    concat('COMPOSITE-H3-', toString(h3_index), '-20260311') AS `id:naturalKey:y:g:0:0:`,

    -- 2. Symbol Section: Merge all distinct event types seen in this Hex today into the Array
    distinct_symbols AS `tv:symbol:value:val:s:m:0:24:0::data`,

    -- 3. U64Array Section: Total incident count packed into a single-element Array
    [event_count] AS `tv:u64Array:value:val:u64h:g:0:0:0::data`,

    -- 4. Text Section: Generate an automated insight string based on combined data
    [
    concat(
            'Regional Summary: ', toString(event_count), ' events. ',
            'Includes: ', arrayStringConcat(distinct_symbols, ', ')
    )
    ] AS `tv:text:text:val:s:0:0:0:0::`,

    -- 5. GeoPoint Section: Set the Entity's location to the H3 Hex itself
    [CAST(0.0 AS Float32)] AS `tv:geoPoint:pointLat:val:f32:g:0:0:0::geo`,
    [CAST(0.0 AS Float32)] AS `tv:geoPoint:pointLng:val:f32:g:0:0:0::geo`,
    [h3_index] AS `tv:geoPoint:h3:val:u64:g:0:0:0::geo`,

    -- 6. TimeRange: The 24-hour composite window
    [toDateTime64(toStartOfDay(toDateTime(event_date)), 9, 'UTC')] AS `tv:timeRange:beginIncl:val:z64:2k:0:0:0::data`,
    [toDateTime64(toStartOfDay(toDateTime(event_date)) + 86400, 9, 'UTC')] AS `tv:timeRange:endExcl:val:z64:2k:0:0:0::data`

FROM (
    -- Subquery to extract the base dimensions (H3 and Date) from the flat Bronze data
    SELECT
    `tv:geoPoint:h3:val:u64:g:0:0:0::geo`[1] AS h3_index,
    toDate(`tv:timeRange:beginIncl:val:z64:2k:0:0:0::data`[1]) AS event_date,
    *
    FROM anchor.facts
    -- Ensure the entity actually has a GeoPoint
    WHERE length(`tv:geoPoint:h3:val:u64:g:0:0:0::geo`) > 0
    )
-- Target our specific date: March 11, 2026
WHERE event_date = '2026-03-11'
GROUP BY h3_index, event_date;
