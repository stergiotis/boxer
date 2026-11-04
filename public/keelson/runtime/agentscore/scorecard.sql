-- Scorecard of one chat conversation, from the trail and the journal.
-- Parameter: {conv:String}, the conversation id (an artefact's chat_conversation).
-- One row; columns grouped as identity, time, model, tools, operations,
-- dialogs, labels, outcome.
WITH
  jr AS (
    SELECT `id:id:u64:4::0:` AS jid, `oq:state:s:::0:` AS jstate
    FROM boxer.dm_journal_runs
    WHERE `oq:conversation:s:::0:` = {conv:String}
    ORDER BY `oq:version:u32:::0:` DESC LIMIT 1 BY jid
  ),
  je AS (
    SELECT `oq:kind:s:::0:` AS kind, `oq:role:s:::0:` AS role, `oq:name:s:::0:` AS name,
           `oq:form:s:::0:` AS form, `oq:key:s:::0:` AS key, `oq:at:z64:::0:` AS at,
           `oq:label-tier:u8:::0:` AS tier, `id:ordinal:u64:47::0:` AS ord
    FROM boxer.dm_journal_entries
    WHERE `id:id:u64:4::0:` IN (SELECT jid FROM jr)
  ),
  -- A tool call: its intent (the tool's name) and its outcome (a headline).
  tc AS (
    SELECT i.key AS key, i.name AS tool, o.form AS headline
    FROM (SELECT key, name FROM je WHERE kind = 'intent') AS i
    LEFT JOIN (SELECT key, form FROM je WHERE kind = 'outcome') AS o USING (key)
  ),
  mc AS (
    SELECT `oq:run:s:::0:` AS run, `oq:purpose:s:::0:` AS purpose, `oq:model:s:::0:` AS model,
           `oq:endpoint-host:s:::0:` AS endpoint, `ts:ts:z64:47::0:` AS ts,
           `oq:input-tokens:u32:::0:` AS tin, `oq:cached-input-tokens:u32:::0:` AS tcached,
           `oq:output-tokens:u32:::0:` AS tout, `oq:reasoning-tokens:u32:::0:` AS treason,
           `oq:elapsed-ms:u64:::0:` AS ms, `oq:refused:b:::0:` AS refused,
           `oq:incomplete:b:::0:` AS incomplete, length(`oq:error:sh:::0:`) > 0 AS errored,
           `oq:retention:s:::0:` AS retention
    FROM boxer.dm_trail_llm_model_calls
    WHERE `oq:conversation:s:::0:` = {conv:String}
  ),
  -- An operation call's last word, by its key.
  op AS (
    SELECT `oq:key:s:::0:` AS key,
           argMax(`oq:operation:s:::0:`, `ts:ts:z64:47::0:`) AS operation,
           argMax(`oq:phase:s:::0:`, `ts:ts:z64:47::0:`) AS phase,
           argMax(arrayStringConcat(`oq:reason:sh:::0:`, ' '), `ts:ts:z64:47::0:`) AS reason,
           max(`oq:confined:b:::0:`) AS confined, max(`oq:tainted:b:::0:`) AS tainted
    FROM boxer.dm_trail_llm_agent_actions
    WHERE `oq:conversation:s:::0:` = {conv:String}
    GROUP BY key
  ),
  gr AS (
    SELECT `oq:event:s:::0:` AS event, `oq:decided-by:s:::0:` AS decided_by
    FROM boxer.dm_trail_llm_agent_grants
    WHERE `oq:conversation:s:::0:` = {conv:String}
  ),
  run_id AS (SELECT anyIf(run, run != '') AS run_value FROM mc),
  rr AS (
    SELECT `oq:driver:s:::0:` AS driver, `oq:driver-job:s:::0:` AS job,
           `oq:driver-attempt:u64:::0:` AS attempt, `oq:driver-job-kind:s:::0:` AS job_kind,
           `oq:vcs-revision:s:::0:` AS revision, `oq:vcs-modified:b:::0:` AS modified
    FROM boxer.dm_trail_runs
    WHERE `oq:run:s:::0:` = (SELECT run_value FROM run_id)
    ORDER BY `ts:ts:z64:47::0:` LIMIT 1
  )
SELECT
  -- identity
  {conv:String} AS conversation,
  (SELECT run_value FROM run_id) AS run,
  (SELECT any(driver) FROM rr) AS driver,
  (SELECT any(job) FROM rr) AS bench_job,
  (SELECT any(attempt) FROM rr) AS bench_attempt,
  (SELECT any(job_kind) FROM rr) AS bench_kind,
  (SELECT any(revision) FROM rr) AS host_revision,
  (SELECT any(modified) FROM rr) AS host_modified,
  (SELECT anyIf(model, purpose = 'chat/turn') FROM mc) AS model,
  (SELECT anyIf(endpoint, purpose = 'chat/turn') FROM mc) AS endpoint,
  (SELECT any(jstate) FROM jr) AS journal_state,
  -- time
  (SELECT min(ts) FROM mc) AS started,
  (SELECT max(ts) FROM mc) AS ended,
  round((SELECT dateDiff('millisecond', min(ts), max(ts)) FROM mc) / 1000) AS span_s,
  -- the span less every pause between model calls beyond ten minutes
  round((SELECT arraySum(arrayMap(d -> least(d, 600), arrayDifference(arrayMap(t -> toUnixTimestamp64Milli(t) / 1000, arraySort(groupArray(ts)))))) FROM mc)) AS active_s,
  (SELECT countIf(kind = 'user') FROM je) AS prompts,
  -- model
  (SELECT countIf(purpose = 'chat/turn') FROM mc) AS model_calls,
  (SELECT countIf(purpose != 'chat/turn') FROM mc) AS aux_calls,
  (SELECT sum(tin) FROM mc) AS input_tokens,
  (SELECT sum(tcached) FROM mc) AS cached_input_tokens,
  round((SELECT sum(tcached) / nullIf(sum(tin), 0) FROM mc), 3) AS cached_share,
  (SELECT sum(tout) FROM mc) AS output_tokens,
  (SELECT sum(treason) FROM mc) AS reasoning_tokens,
  round((SELECT avgIf(tin, purpose = 'chat/turn') FROM mc)) AS input_per_call,
  (SELECT maxIf(tin, purpose = 'chat/turn') FROM mc) AS input_per_call_max,
  round((SELECT avgIf(ms, purpose = 'chat/turn') FROM mc)) AS latency_ms,
  round((SELECT quantileIf(0.9)(ms, purpose = 'chat/turn') FROM mc)) AS latency_ms_p90,
  (SELECT countIf(errored OR incomplete OR refused) FROM mc) AS model_call_failures,
  (SELECT countIf(retention = 'kept') FROM mc) AS calls_kept,
  -- tools: every call the model made, the app operations among them, and the
  -- chat's own tools whose headline reads as a failure
  (SELECT count() FROM tc) AS tool_calls,
  (SELECT countIf(tool = 'call_operation') FROM tc) AS operation_tool_calls,
  (SELECT countIf(tool != 'call_operation' AND (match(headline, concat('(^|· )', tool, ': '))
                 OR match(headline, ' · (refused|conflict) · '))) FROM tc) AS chat_tool_failures,
  -- operations
  (SELECT count() FROM op) AS operations,
  (SELECT countIf(phase IN ('completed', 'rendered')) FROM op) AS op_ok,
  (SELECT countIf(phase = 'refused') FROM op) AS op_refused,
  (SELECT countIf(phase = 'conflict') FROM op) AS op_conflict,
  (SELECT countIf(phase NOT IN ('completed', 'rendered', 'refused', 'conflict')) FROM op) AS op_unfinished,
  (SELECT countIf(phase = 'refused' AND reason LIKE '%do not fit the schema%') FROM op) AS refused_schema,
  (SELECT countIf(phase = 'refused' AND (reason LIKE '%grant%' OR reason LIKE '%agent limit%')) FROM op) AS refused_grant,
  (SELECT countIf(phase = 'refused' AND (reason LIKE '%sealed%' OR reason LIKE '%confined%')) FROM op) AS refused_sealed,
  (SELECT countIf(phase IN ('refused', 'conflict') AND reason LIKE 'read first%') FROM op) AS refused_read_first,
  -- dialogs: what the person decided
  (SELECT countIf(decided_by = 'person') FROM gr) AS dialogs,
  (SELECT countIf(event = 'requested') FROM gr) AS grant_requests,
  -- labels
  (SELECT countIf(confined) FROM op) AS confined_operations,
  (SELECT countIf(tainted) FROM op) AS tainted_operations,
  (SELECT max(tier) FROM je) AS max_label_tier,
  (SELECT minIf(ord, tier >= 2) FROM je) AS first_tier2_ordinal,
  -- outcome
  (SELECT countIf(kind = 'ignorable' AND name = 'artefact' AND form = 'exported') FROM je) AS exports,
  (SELECT countIf(kind = 'endorse') FROM je) AS endorsements,
  (SELECT countIf(kind = 'ignorable' AND name = 'export-refused') FROM je) AS export_refusals,
  (SELECT anyIf(form, kind = 'ignorable' AND name = 'export-refused') FROM je) AS export_refused_form,
  (SELECT countIf(tool = 'artefact_capture') FROM tc) AS captures
