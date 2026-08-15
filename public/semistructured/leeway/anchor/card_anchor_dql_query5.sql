/* Query 5 — a "gold" composite: one synthetic entity per H3 cell per day.

Aggregates the per-domain events of 2026-03-11 into one summary entity per H3
cell: distinct event types, an incident count, a generated summary line, and
the cell as the entity's own location. Aggregation results land in Array(T)
columns, which is exactly leeway's physical shape — each projection item is
wrapped in a constructor (ADR-0181 §SD2) that mints the physical name, so the
result rows are a valid leeway table again: LW_PLAIN for the backbone
(`item:id` picks the id item kind), LW_TV for the tagged sections. The
constructors are what re-admit computed columns into closure; hand-spelling
the physical alias is the fallback when no pipeline runs. Minted names use
the folded spelling (`naturalKey` → `natural-key`) and fresh-table default
segments — see query 4's comment.

The subquery selects the handles it needs and aliases them (`symbols`,
`h3_index`, `event_date`); the outer scope works on those aliases — the
resolve pass does not reach through subqueries, so handles stay where their
table is in scope.
*/
WITH
    arrayDistinct(groupArrayArray(symbols)) AS distinct_symbols,
    toUInt64(count()) AS event_count
SELECT
    LW_PLAIN(cityHash64(h3_index, event_date), 'id', 'u64', 'item:id'),
    LW_PLAIN(concat('COMPOSITE-H3-', toString(h3_index), '-20260311'), 'naturalKey', 'y', 'item:id'),

    -- symbol section: all distinct event types seen in this cell today
    LW_TV(distinct_symbols, 'symbol', 'value', 's'),

    -- u64Array section: the incident count, packed as a one-element array
    LW_TV([event_count], 'u64Array', 'value', 'u64h'),

    -- text section: a generated summary line
    LW_TV([concat('Regional summary: ', toString(event_count), ' events. Includes: ',
                  arrayStringConcat(distinct_symbols, ', '))], 'text', 'text', 's'),

    -- geoPoint section: the cell itself is the entity's location
    LW_TV([CAST(0.0 AS Float32)], 'geoPoint', 'pointLat', 'f32'),
    LW_TV([CAST(0.0 AS Float32)], 'geoPoint', 'pointLng', 'f32'),
    LW_TV([h3_index], 'geoPoint', 'h3', 'u64'),

    -- timeRange section: the 24-hour composite window
    LW_TV([toDateTime64(toStartOfDay(toDateTime(event_date)), 9, 'UTC')], 'timeRange', 'beginIncl', 'z64'),
    LW_TV([toDateTime64(toStartOfDay(toDateTime(event_date)) + 86400, 9, 'UTC')], 'timeRange', 'endExcl', 'z64')

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
