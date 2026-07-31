---
type: reference
audience: end-user
status: draft
title: Plane staleness
icon: "⏱"
endpoint: introspection
tabs: [table]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Plane staleness

Provenance clocks of the observed topology tables, per host and domain:
when the sample was taken, when this process received it, and the age in
milliseconds. Check this before trusting a quiet table — the observed
side holds the latest snapshot only, and rows are empty, not absent,
without a publishing scraper. Zero rows here means no scraper has
published since this process started.

```sql
SELECT domain, host, sampled, received, age_ms
FROM (
    SELECT 'procs' AS domain, host,
           fromUnixTimestamp64Milli(max(sampled_at_unix_ms)) AS sampled,
           fromUnixTimestamp64Milli(max(received_at_unix_ms)) AS received,
           toUnixTimestamp64Milli(now64(3)) - max(received_at_unix_ms) AS age_ms
    FROM keelson('procs')
    GROUP BY host
    UNION ALL
    SELECT 'sockets' AS domain, host,
           fromUnixTimestamp64Milli(max(collected_at_unix_ms)) AS sampled,
           fromUnixTimestamp64Milli(max(received_at_unix_ms)) AS received,
           toUnixTimestamp64Milli(now64(3)) - max(received_at_unix_ms) AS age_ms
    FROM keelson('sockets')
    GROUP BY host
)
ORDER BY host, domain
```
