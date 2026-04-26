-- trend_mining.sql — Pass 1 of the hindsight-aware summarization pipeline.
--
-- Reads FlatCommitChange rows (one row per file-change, commits with no
-- file-changes represented by a sentinel row with path='') as JSONEachRow.
-- Emits one JSON object describing window-wide trends, intended as input to
-- `gov commitdigest synthesize-threads`.
--
-- Queried directly by `gov commitdigest mine-trends` via clickhouse-local;
-- the structure passed via --structure must match FlatCommitChangeStructure
-- in gov_commitdigest_flatten.go.
--
-- Week bucketing uses toStartOfWeek(ts, 1) (ISO — Monday-start) so the bucket
-- keys are stable across locales.

WITH
    -- Parsed view: git date strings → DateTime.
    parsed AS (
        SELECT
            repoName,
            commitHash,
            commitShortHash,
            parseDateTimeBestEffort(commitDate) AS ts,
            commitAuthor,
            commitAuthorEmail,
            commitSubject,
            subjectType,
            path,
            adds,
            dels
        FROM table
    ),
    -- Distinct commits (deduped across the file-change fan-out).
    commits AS (
        SELECT DISTINCT
            repoName,
            commitHash,
            commitShortHash,
            ts,
            commitAuthor,
            commitAuthorEmail,
            commitSubject,
            subjectType
        FROM parsed
    ),
    -- Non-sentinel file changes only.
    changes AS (
        SELECT * FROM parsed WHERE path <> ''
    ),

    -- 1) Window summary scalars.
    window_summary AS (
        SELECT
            toString(toDate(min(ts))) AS firstDate,
            toString(toDate(max(ts))) AS lastDate,
            count() AS totalCommits,
            uniqExact(commitAuthorEmail) AS uniqueAuthors
        FROM commits
    ),
    window_change_stats AS (
        SELECT
            sum(adds) AS totalAdds,
            sum(dels) AS totalDels,
            uniqExact(path) AS touchedFiles
        FROM changes
    ),

    -- 2) Mode distribution per ISO week.
    mode_week AS (
        SELECT
            toStartOfWeek(ts, 1) AS week,
            subjectType AS kind,
            count() AS cnt
        FROM commits
        GROUP BY week, kind
    ),
    mode_by_week AS (
        SELECT
            toString(week) AS week,
            groupArray(tuple(kind, cnt)) AS mix
        FROM mode_week
        GROUP BY week
        ORDER BY week
    ),

    -- 3) Path-prefix churn per week (top two path segments).
    prefix_week AS (
        SELECT
            toStartOfWeek(ts, 1) AS week,
            arrayStringConcat(arraySlice(splitByChar('/', path), 1, 2), '/') AS prefix,
            uniqExact(commitHash) AS commitCount,
            sum(adds) AS addsSum,
            sum(dels) AS delsSum
        FROM changes
        GROUP BY week, prefix
    ),
    prefix_churn_by_week AS (
        SELECT
            toString(week) AS week,
            groupArray(tuple(prefix, commitCount, addsSum, delsSum)) AS prefixes
        FROM prefix_week
        GROUP BY week
        ORDER BY week
    ),

    -- 4) Follow-up (WIP / fixup / squash) density per week.
    followup_week AS (
        SELECT
            toStartOfWeek(ts, 1) AS week,
            countIf(subjectType IN ('wip', 'fixup', 'squash')) AS followUpCommits,
            count() AS totalCommits,
            followUpCommits / totalCommits AS ratio
        FROM commits
        GROUP BY week
        ORDER BY week
    ),

    -- 5) Revert events (individual commits).
    revert_events AS (
        SELECT
            commitShortHash AS shortHash,
            commitHash AS hash,
            toString(toDate(ts)) AS date,
            commitSubject AS subject
        FROM commits
        WHERE subjectType = 'revert'
        ORDER BY ts
    ),

    -- 6) Net LOC per week (adds - dels) and weekly churn.
    netloc_week AS (
        SELECT
            toStartOfWeek(ts, 1) AS week,
            sum(adds) AS grossAdds,
            sum(dels) AS grossDels,
            grossAdds - grossDels AS netLoc,
            uniqExact(commitHash) AS commitCount
        FROM changes
        GROUP BY week
        ORDER BY week
    ),

    -- 7) Temporal coupling edges — unordered file pairs co-churning in ≥3 commits.
    couple_pairs AS (
        SELECT
            a.path AS pathA,
            b.path AS pathB,
            count() AS support
        FROM changes AS a
        INNER JOIN changes AS b
            ON a.commitHash = b.commitHash AND a.path < b.path
        GROUP BY pathA, pathB
        HAVING support >= 3
        ORDER BY support DESC
        LIMIT 64
    ),

    -- 8) Hot files — top 32 by commit count, with churn totals and activity range.
    hot_files AS (
        SELECT
            path,
            uniqExact(commitHash) AS commitCount,
            sum(adds) AS addsSum,
            sum(dels) AS delsSum,
            toString(toDate(min(ts))) AS firstSeen,
            toString(toDate(max(ts))) AS lastSeen
        FROM changes
        GROUP BY path
        ORDER BY commitCount DESC
        LIMIT 32
    )

SELECT
    -- Window summary object.
    (SELECT tuple(firstDate, lastDate, totalCommits, uniqueAuthors)
     FROM window_summary) AS windowSummaryCore,
    (SELECT tuple(totalAdds, totalDels, touchedFiles)
     FROM window_change_stats) AS windowSummaryChurn,

    -- Weekly series.
    (SELECT groupArray(tuple(week, mix))
     FROM mode_by_week) AS modeDistributionByWeek,
    (SELECT groupArray(tuple(week, prefixes))
     FROM prefix_churn_by_week) AS pathPrefixChurnByWeek,
    (SELECT groupArray(tuple(toString(week), followUpCommits, totalCommits, ratio))
     FROM followup_week) AS followUpDensityByWeek,
    (SELECT groupArray(tuple(toString(week), grossAdds, grossDels, netLoc, commitCount))
     FROM netloc_week) AS netLocByWeek,

    -- Event lists.
    (SELECT groupArray(tuple(shortHash, hash, date, subject))
     FROM revert_events) AS revertEvents,
    (SELECT groupArray(tuple(pathA, pathB, support))
     FROM couple_pairs) AS temporalCouplingEdges,
    (SELECT groupArray(tuple(path, commitCount, addsSum, delsSum, firstSeen, lastSeen))
     FROM hot_files) AS hotFiles

-- The outer SELECT has no FROM so ClickHouse scans an implicit one-row table
-- and emits exactly one JSON document. LIMIT 1 is redundant belt-and-braces
-- in case a future edit introduces a table source.
LIMIT 1
FORMAT JSONEachRow;
