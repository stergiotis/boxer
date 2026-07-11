---
type: how-to
audience: end-user
status: draft
title: Example queries
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Example queries

These run against `anchor.facts`, a self-contained demo table of leeway-encoded
events (drone deliveries, cyber incidents, alpine sensor readings). They walk
each tab of the playground. Column names are leeway physical names, so they must
be backtick-quoted. Do not add a `FORMAT` clause — the app appends one.

In a real boxer deployment the equivalent table is `spinnaker.facts`; the
queries below transfer by swapping the table name (physical column names
differ per schema).

## Loading the demo table

`anchor.facts` is populated by an integration test that talks to a local
ClickHouse on `localhost:8123` (it skips silently if none is reachable):

```bash
go test -tags="$(cat ./tags)" -run TestLeewayClickHouse \
  ./public/semistructured/leeway/anchor/
```

That creates the `anchor` database, loads an array-unflatten UDF, and inserts
~60 entities across the three scenarios. Re-running appends another batch; to
reset, `TRUNCATE TABLE anchor.facts` from a ClickHouse client — not from the
playground, since the appended `FORMAT ArrowStream` is invalid on DDL.

## Inspecting whole entities — the Detail card

`SELECT *` returns the full leeway row, so the **Detail** tab renders the leeway
card: the plain `id` section, every tagged section, and the membership chips.

```sql
SELECT * FROM anchor.facts LIMIT 50
```

Run it, then click rows in **Table** — the **Detail** tab should show the tagged
sections (symbol, text, geoPoint, geoArea, timeRange, …), not just the plain id.

Narrow to one scenario to vary which sections are populated:

```sql
-- drone missions: always a geoPoint; sometimes geoArea / text
SELECT * FROM anchor.facts
WHERE hasAny(`tv:symbol:value:val:s:m:0:24:0::data`,
             ['IN_TRANSIT', 'DELIVERED', 'HEARTBEAT'])

-- cyber incidents
SELECT * FROM anchor.facts
WHERE hasAny(`tv:symbol:value:val:s:m:0:24:0::data`,
             ['DDOS', 'PORT_SCAN', 'SQL_INJECTION'])

-- alpine sensor events
SELECT * FROM anchor.facts
WHERE hasAny(`tv:symbol:value:val:s:m:0:24:0::data`,
             ['SEISMIC_ANOMALY', 'SNOW_SHIFT'])
```

A single entity that carries the sparse `geoArea` section (ids 10005, 10010,
10015, 10020, 500003 have one):

```sql
SELECT * FROM anchor.facts WHERE `id:id:u64:2k:0:0:` = 10005
```

## Plotting time — the Timeline tab

The Timeline tab reads canonical slot columns. Map the `timeRange` section onto
them; timestamps must be `DateTime64`:

```sql
SELECT
  `tv:timeRange:beginIncl:val:z64:2k:0:0:0::data`[1] AS _tl_time,
  `tv:timeRange:endExcl:val:z64:2k:0:0:0::data`[1]   AS _tl_time_end,
  `tv:symbol:value:val:s:m:0:24:0::data`[1]          AS _tl_lane
FROM anchor.facts
WHERE length(`tv:timeRange:beginIncl:val:z64:2k:0:0:0::data`) > 0
ORDER BY _tl_time
```

Contract shapes:

- **Points** — `_tl_time`
- **Intervals** — `_tl_time` + `_tl_time_end` (plus optional `_tl_lane`, `_tl_intensity`)
- **Annotations** — `_tl_time` + `_tl_label`

## Ordinary results — the ad-hoc Detail path

Aliased or aggregated columns are not leeway-shaped, so Detail falls back to
prefix grouping and Table shows a plain grid:

```sql
SELECT
  `id:id:u64:2k:0:0:`                       AS id,
  `id:naturalKey:y:g:0:0:`                  AS natural_key,
  `tv:symbol:value:val:s:m:0:24:0::data`[1] AS event_type
FROM anchor.facts
ORDER BY id
```

The repo ships richer relational examples next to the fixture —
`card_anchor_dql_query1.sql` (explode nested ports with `ARRAY JOIN`),
`…query3.sql` (pre-tokenised full-text search over a co-container), and
`…query6.sql` (a leeway integrity scanner).

## Parameters

`SET param_*` rides the request URL; the `{name:Type}` placeholder is substituted
by ClickHouse:

```sql
SET param_event = 'DDOS';
SELECT * FROM anchor.facts
WHERE has(`tv:symbol:value:val:s:m:0:24:0::data`, {event:String})
```

An *unbound* placeholder — no `SET` — is a live **signal** instead:

```sql
SELECT * FROM anchor.facts LIMIT {lim:UInt64}
```

Run refuses while nothing fills `lim`, with a hint in the status bar. Open the
Graph tab's **signals** section, give `lim` a value, press **set**, and Run —
the value rides the URL exactly like a `SET`-bound constant. Check **Live** in
the top bar and the query re-runs by itself whenever a referenced signal moves
(a further **set**, or a panel write such as a Table row click when the query
references `{selection:Int64}`); edits to the SQL itself still wait for Run.

## Empty and trivial states

```sql
SELECT * FROM anchor.facts LIMIT 1      -- single card
SELECT * FROM anchor.facts WHERE 1 = 0  -- empty-state rendering
SELECT 1 AS hello, now() AS ts          -- non-leeway, ad-hoc path
```

## A note on co-sections

In the Detail card, `anchor.facts` shows `geoPoint` and `geoArea` as two
separate tagged sections. They are deliberately *not* a co-section group: an
entity always has a `geoPoint` but only sometimes a `geoArea`, so the two are
not row-aligned, and co-sections require equal per-entity attribute counts. The
merged co-section rendering is exercised only by the in-memory `leewaywidgets`
fixture, which is not loaded into ClickHouse.
