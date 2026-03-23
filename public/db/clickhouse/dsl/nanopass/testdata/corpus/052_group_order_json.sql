SELECT toStartOfDay(event_created) as day,
       count(*)                    as count
FROM order_events
WHERE event_created >= '2024-04-01' and event_created < '2024-05-01'
  AND event_type = 'Departed'
  AND JSONExtractString(event_payload, 'terminal') = 'Berlin'
GROUP BY day
ORDER BY count desc, day
