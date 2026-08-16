-- Fixture for BenchmarkPlayPipeline (nanopass_pipeline_bench_test.go).
--
-- This is the ADR-0191 event-timeline applet's buffer as it stood before §SD7
-- moved its extraction into the keelson('runtime_events') provider, with its
-- four parameters bound to their defaults so the statement is complete. It is
-- kept as a real-world worst case: ~9 KB, twelve membership kinds, and the
-- leeway extraction calls that make it big. Against a ClickHouse answering in
-- 90 ms it cost seconds per Run, all of it in the client-side pass pipeline.
--
-- Do not "fix" it. Its shape is the measurement.


WITH
  -- The twelve kinds this applet draws, as ids. Everywhere else a membership
  -- is named; here it cannot be, because hasAny() is plain SQL and no macro
  -- turns a name into a literal inside an array. Keeping the list in one
  -- place is the compromise: it is the granule guard, and it prunes the rows
  -- of other vocabularies sharing this table (sysmetrics, capmap).
  [9223372049739677696,  -- runtimeKindGrant
   9223372049739677697,  -- runtimeKindAudit
   9223372049739677698,  -- runtimeKindState (pre-ADR-0105-D3a persist rows)
   9223372049739677699,  -- runtimeKindEvent
   9223372049739677700,  -- runtimeKindLog
   9223372049739677715,  -- runtimeKindRuntimeRun
   9223372049739677724,  -- runtimeKindRuntimeHeartbeat
   9223372049739677725,  -- runtimeKindAppLifecycle
   9223372049739677736,  -- runtimeKindQueryRun
   9223372049739677757,  -- runtimeKindLaunch
   9223372049739677761,  -- runtimeKindWorkingset
   9223372049739677763]  -- runtimeKindColumnWidth
  AS runtime_kinds,

  -- Every row that names a run, and when it landed. min() per run is the
  -- process-start row; max() is the last thing it wrote.
  runmarks AS (
    SELECT
      arrayElement(LW_CO_GATHER(`symbol:value`,
        LW_SEL_ATTRS('symbol', 'runtimeRun', 'chan:low-card-ref-high-card-params')), 1) AS run_id,
      `timestamp:ts` AS ts
    FROM boxer.facts
    WHERE has(`symbol:lmr`, 9223372049739677716)   -- runtimeRun
  ),
  runs AS (
    SELECT run_id, min(ts) AS t0, max(ts) AS last_ts FROM runmarks GROUP BY run_id
  ),
  ranked AS (
    SELECT run_id, t0, last_ts, row_number() OVER (ORDER BY t0 DESC) AS rk FROM runs
  ),
  -- The run this applet is about, and the window of time it occupies, as ONE
  -- scalar: (run id, first row, last row). A CTE here would be re-scanned
  -- once per reference below, and there are five of them.
  --
  -- A run still writing has no end, so the newest one — rank 1 — runs to now.
  (SELECT (run_id, t0, if(rk = 1, now64(9, 'UTC'), last_ts))
     FROM ranked
     WHERE '' = '' OR run_id = ''
     ORDER BY t0 DESC
     LIMIT 1) AS span,

  facts_ev AS (
    SELECT
      toString(`id:id`) AS ev_id,
      `timestamp:ts`    AS ts,
      -- Run and app are the VALUE lane of a mixed-channel membership: the
      -- parameter lane carries the same bytes, but LW_GET refuses a mixed
      -- channel without a param: token and here the parameter is the thing
      -- being read. LW_SEL_ATTRS answers the plural question instead.
      arrayElement(LW_CO_GATHER(`symbol:value`,
        LW_SEL_ATTRS('symbol', 'runtimeRun', 'chan:low-card-ref-high-card-params')), 1) AS row_run,
      arrayElement(LW_CO_GATHER(`symbol:value`,
        LW_SEL_ATTRS('symbol', 'runtimeApp', 'chan:low-card-ref-high-card-params')), 1) AS app_id,
      arrayElement(LW_GET_LIST('u64Array', 'runtimeLifecycleTileKey', 'chan:low-card-ref'), 1) AS instance,
      multiIf(
        notEmpty(LW_SEL('symbol', 'runtimeKindAppLifecycle', 'chan:low-card-ref')),    'lifecycle',
        notEmpty(LW_SEL('symbol', 'runtimeKindAudit', 'chan:low-card-ref')),           'audit',
        notEmpty(LW_SEL('symbol', 'runtimeKindRuntimeHeartbeat', 'chan:low-card-ref')), 'heartbeat',
        notEmpty(LW_SEL('symbol', 'runtimeKindRuntimeRun', 'chan:low-card-ref')),      'run start',
        notEmpty(LW_SEL('symbol', 'runtimeKindLog', 'chan:low-card-ref')),             'log',
        notEmpty(LW_SEL('symbol', 'runtimeKindGrant', 'chan:low-card-ref')),           'grant',
        notEmpty(LW_SEL('symbol', 'runtimeKindLaunch', 'chan:low-card-ref')),          'launch',
        notEmpty(LW_SEL('symbol', 'runtimeKindWorkingset', 'chan:low-card-ref')),      'workingset',
        notEmpty(LW_SEL('symbol', 'runtimeKindQueryRun', 'chan:low-card-ref')),        'query run',
        notEmpty(LW_SEL('symbol', 'runtimeKindColumnWidth', 'chan:low-card-ref')),     'column width',
        notEmpty(LW_SEL('symbol', 'runtimeKindState', 'chan:low-card-ref')),           'persist',
        notEmpty(LW_SEL('symbol', 'runtimeKindEvent', 'chan:low-card-ref')),           'event',
        '?') AS kind,
      -- One line per kind, from that kind's own memberships.
      multiIf(
        notEmpty(LW_SEL('symbol', 'runtimeKindAppLifecycle', 'chan:low-card-ref')),
          concat(LW_GET('symbol', 'runtimeLifecyclePhase', 'chan:low-card-ref'),
                 arrayStringConcat(arrayMap(x -> concat(' — ', x),
                   LW_GET_LIST('stringArray', 'runtimeLifecycleStopReason', 'chan:low-card-ref')))),
        notEmpty(LW_SEL('symbol', 'runtimeKindAudit', 'chan:low-card-ref')),
          concat(LW_GET('symbol', 'runtimeAuditRequestSubject', 'chan:low-card-ref'), ' → ',
                 LW_GET('symbol', 'runtimeAuditResult', 'chan:low-card-ref')),
        notEmpty(LW_SEL('symbol', 'runtimeKindRuntimeRun', 'chan:low-card-ref')),
          concat(LW_GET('symbol', 'runtimeRunHostname', 'chan:low-card-ref'), ' pid ',
                 toString(arrayElement(
                   LW_GET_LIST('u64Array', 'runtimeRunPid', 'chan:low-card-ref'), 1))),
        notEmpty(LW_SEL('symbol', 'runtimeKindLog', 'chan:low-card-ref')),
          concat(LW_GET('symbol', 'runtimeLogLevel', 'chan:low-card-ref'), ': ',
                 arrayStringConcat(
                   LW_GET_LIST('stringArray', 'runtimeLogMessage', 'chan:low-card-ref'))),
        notEmpty(LW_SEL('symbol', 'runtimeKindGrant', 'chan:low-card-ref')),
          concat(LW_GET('symbol', 'runtimeSubjectFilterDirection', 'chan:low-card-ref'), ' ',
                 LW_GET('symbol', 'runtimeSubjectFilterPattern', 'chan:low-card-ref')),
        notEmpty(LW_SEL('symbol', 'runtimeKindLaunch', 'chan:low-card-ref')),
          concat('opened by ', arrayElement(LW_CO_GATHER(`symbol:value`,
            LW_SEL_ATTRS('symbol', 'runtimeLaunchCaller',
              'chan:low-card-ref-high-card-params')), 1)),
        notEmpty(LW_SEL('symbol', 'runtimeKindWorkingset', 'chan:low-card-ref')),
          concat('saved ', LW_GET('symbol', 'runtimeWorkingsetName', 'chan:low-card-ref')),
        notEmpty(LW_SEL('symbol', 'runtimeKindQueryRun', 'chan:low-card-ref')),
          LW_GET('symbol', 'runtimeQueryRunEventType', 'chan:low-card-ref'),
        notEmpty(LW_SEL('symbol', 'runtimeKindColumnWidth', 'chan:low-card-ref')),
          concat(LW_GET('symbol', 'runtimeColWidthTier', 'chan:low-card-ref'), ' ',
                 LW_GET('symbol', 'runtimeColWidthScope', 'chan:low-card-ref')),
        notEmpty(LW_SEL('symbol', 'runtimeKindState', 'chan:low-card-ref')),
          LW_GET('symbol', 'runtimePersistKey', 'chan:low-card-ref'),
        '') AS detail
    FROM boxer.facts
    -- The cheap necessary guard: one hasAny() over the kind lane prunes the
    -- granules holding other vocabularies' rows.
    WHERE hasAny(`symbol:lr`, runtime_kinds)
  ),

  -- App state writes. Since ADR-0191 §SD5 these rows carry the run and the
  -- window that wrote them; rows written before it carry neither and are
  -- placed by timestamp, like any other unattributed row.
  persist_ev AS (
    SELECT
      `id:id`        AS ev_id,
      `timestamp:ts` AS ts,
      LW_GET('stateRunId', 'runtimeRun', 'chan:low-card-ref')                 AS row_run,
      LW_GET('stateAppId', 'runtimeApp', 'chan:low-card-ref')                 AS app_id,
      LW_GET('stateInstanceKey', 'runtimeLifecycleTileKey', 'chan:low-card-ref') AS instance,
      'persist'      AS kind,
      concat(LW_GET('stateKey', 'runtimePersistKey', 'chan:low-card-ref'),
             if(`lifecycle:lifecycle` = 1, ' (deleted)', '')) AS detail
    FROM boxer.persiststate
  ),

  ev AS (
    -- A row that names the run is selected by that name; a row that names no
    -- run is selected by falling inside the run's span, which is the weaker
    -- claim the Reading-it-honestly note below is about.
    SELECT * FROM facts_ev
    WHERE row_run = span.1
       OR (row_run = '' AND ts >= span.2 AND ts <= span.3)
    UNION ALL
    SELECT * FROM persist_ev
    WHERE row_run = span.1
       OR (row_run = '' AND ts >= span.2 AND ts <= span.3)
  ),

  laned AS (
    SELECT
      ts, kind, detail, app_id, instance, ev_id,
      -- A full app id is too long to read as a lane label, and its last
      -- segment is the name people use.
      if(app_id = '', '', arrayElement(splitByChar('/', app_id), -1)) AS app,
      multiIf(app_id = '',  '(runtime)',
              instance = 0, concat(app, ' (no window)'),
              concat(app, ' #', toString(instance))) AS lane
    FROM ev
    -- `kinds`: all, a list to keep, or a `-`-prefixed list to drop.
    WHERE 'all' = 'all'
       OR if(startsWith('all', '-'),
             NOT has(arrayMap(x -> trimBoth(x),
               splitByChar(',', substring('all', 2))), kind),
             has(arrayMap(x -> trimBoth(x),
               splitByChar(',', 'all')), kind))
  ),
  capped AS (
    SELECT * FROM laned ORDER BY ts DESC LIMIT 20000
  )
SELECT
  toDateTime64(ts, 3, 'UTC') AS at,
  kind,
  lane,
  detail,
  app_id,
  instance,
  ev_id,
  -- The drawn triple, last: the panel finds these by name, and everything a
  -- reader wants first is above them.
  toDateTime64(ts, 3, 'UTC')                                          AS _tl_time,
  toDateTime64(ts + toIntervalMillisecond(1000), 3, 'UTC') AS _tl_time_end,
  lane                                                                AS _tl_lane
FROM capped
ORDER BY _tl_time ASC, lane ASC, kind ASC
