/* Query 5 — a "gold" composite: one synthetic entity per H3 cell per day.

Aggregates the per-domain events of 2026-03-11 into one summary entity per H3
cell: distinct event types, an incident count, a generated summary line, and
the cell as the entity's own location. Aggregation results land in Array(T)
columns, which is exactly leeway's physical shape — the projection aliases
spell out the physical names, so the result rows are a valid leeway table
again.

The subquery selects the handles it needs and aliases them (`symbols`,
`h3_index`, `event_date`); the outer scope works on those aliases — the
resolve pass does not reach through subqueries, so handles stay where their
table is in scope.
*/
WITH
    arrayDistinct(groupArrayArray(symbols)) AS distinct_symbols,
    toUInt64(count()) AS event_count
SELECT
    cityHash64(h3_index, event_date) AS `id:id:u64:47::0:`,
    concat('COMPOSITE-H3-', toString(h3_index), '-20260311') AS `id:naturalKey:y:4::0:`,

    -- symbol section: all distinct event types seen in this cell today
    distinct_symbols AS `tv:symbol:value:val:s:124::I:0::data`,

    -- u64Array section: the incident count, packed as a one-element array
    [event_count] AS `tv:u64Array:value:val:u64h:4:::0::data`,

    -- text section: a generated summary line
    [concat('Regional summary: ', toString(event_count), ' events. Includes: ',
            arrayStringConcat(distinct_symbols, ', '))] AS `tv:text:text:val:s:0:0:0:0::`,

    -- geoPoint section: the cell itself is the entity's location
    [CAST(0.0 AS Float32)] AS `tv:geoPoint:pointLat:val:f32:g:0:0:0::geo`,
    [CAST(0.0 AS Float32)] AS `tv:geoPoint:pointLng:val:f32:g:0:0:0::geo`,
    [h3_index] AS `tv:geoPoint:h3:val:u64:g:0:0:0::geo`,

    -- timeRange section: the 24-hour composite window
    [toDateTime64(toStartOfDay(toDateTime(event_date)), 9, 'UTC')] AS `tv:timeRange:beginIncl:val:z64:2k:0:0:0::data`,
    [toDateTime64(toStartOfDay(toDateTime(event_date)) + 86400, 9, 'UTC')] AS `tv:timeRange:endExcl:val:z64:2k:0:0:0::data`

FROM (
    SELECT
        `geoPoint:h3`[1] AS h3_index,
        toDate(`timeRange:beginIncl`[1]) AS event_date,
        `symbol:value` AS symbols
    FROM facts
    WHERE length(`geoPoint:h3`) > 0
)
WHERE event_date = '2026-03-11'
GROUP BY h3_index, event_date
