---
type: adr
status: proposed
date: 2026-06-20
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0091: Claude session transcripts as a queryable Arrow event table

## Context

Claude Code persists every session as a JSONL transcript under
`~/.claude/projects/<encoded-cwd>/<session-uuid>.jsonl` — one JSON event per line.
These transcripts accumulate across every project directory Claude Code has been used in.
They are a rich, currently-unmined record of AI-assisted work: token spend,
context growth, model choice, the human's typed prompts, every file read/written/edited,
and every commit made. We want this as **boxer data** — queryable, visualizable — not as
inert log files. The concrete near-term consumer is a `keelson` app that renders the
telemetry and runs ad-hoc queries through `clickhouse-local`; the near-term deliverable is
the export plus a CLI that already answers questions, mirroring how
[ADR-tooling](../../public/app/commands/adr/) ships an overview/query CLI over its Arrow tables.

**The idiom already exists.** `public/app/commands/adr/` (built 2026-06-20) is exactly this
shape: *parse a corpus → emit Arrow IPC (Zstd) → query via `clickhouse-local`* with a
`CREATE TEMPORARY TABLE t AS SELECT * FROM file('t.arrow','Arrow')` prelude and
`build`/`overview`/`query` subcommands, leaning on `public/keelson/data/chlocalpool`. This ADR
reuses that idiom wholesale; the only novelty is the parser and the schema.

**What the transcript gives us (verified against real sessions).**

| Want | Source field |
|---|---|
| token use | `assistant.message.usage.{input_tokens, output_tokens, cache_read_input_tokens, cache_creation_input_tokens, service_tier}` |
| context length | per response = `input + cache_read + cache_creation` (the prompt size); session peak = max over responses |
| timestamps | every event's ISO-8601 `timestamp` |
| my inputs | `user` events with `promptSource:"typed"` (excludes tool-results / `isMeta` / compact-summary) |
| model | `assistant.message.model` (e.g. `claude-opus-4-8`) |
| referenced commits | `toolUseResult.gitOperation.commit.{sha,kind}` — authoritative, no regex |
| written / read files | Read/Write/Edit `tool_use.input.file_path` + `toolUseResult.structuredPatch` (lines ±) |

Each event also carries `sessionId`, `cwd`, `gitBranch`, `version`, `uuid`/`parentUuid`,
`isSidechain`; `ai-title` events give a human session title (`aiTitle`).

## Design space (QOC)

**Question.** In what shape, over what corpus, and in what home should the transcripts be
materialized so that "references to code in a given repo" are queryable and
the keelson app can consume them directly?

Three sub-decisions were resolved with the user before writing code:

- **Corpus — all projects, fully (chosen).** Mine *every* session dir, not only those whose
  working directory is a target repo. Rationale: a reference to a repo's code can be made from a
  session rooted elsewhere (a session in one repo reads or edits another repo's files via a
  relative or absolute path). Repo membership is therefore not a corpus filter but a **per-event
  classification** of the touched path (`target_repo`), so cross-repo references are captured
  wherever they occur, and a global usage view falls out for free.
  *Rejected — only the target repos' own session dirs:* would miss every cross-repo reference made
  from elsewhere, which is precisely the interesting "references to my code" signal.

- **Shape — one denormalized event table (chosen).** A single Arrow file, one row per event,
  discriminated by a `kind` column, with kind-specific columns left null. Rationale: every datum
  the user asked for is an event on a per-session timeline; a single table is the simplest thing
  the keelson app and `clickhouse-local` consume, and session-level rollups (token totals, peak
  context, file/commit counts) are one `GROUP BY session_id` away.
  *Rejected — multi-table star (sessions/messages/file_ops/commits):* cleaner grains but forces
  the consumer to join four files for the common "timeline of a session" view; the denormalized
  table degrades to that star via `WHERE kind=…` projections anyway.

- **Home — boxer app subcommand (chosen).** A new Go command `public/app/commands/claudemine/`
  mirroring `adr`, registered in `public/app/main.go`, reusing `chlocalpool` + the Arrow IPC emit
  pattern. Rationale: in-ecosystem, sovereign, and the native data source for the keelson app.
  *Rejected — standalone pyarrow/Go script:* faster to a first file but off-ecosystem and a
  dead-end for the app.

Two smaller calls, recorded with their kill-reasons:

- **Timestamps as Arrow `Timestamp(us, UTC)`**, not strings — the keelson app wants time-range
  and time-series queries; `clickhouse-local` reads Arrow timestamps as `DateTime64` natively.
- **Nullable payload columns**, not zero/`''` sentinels (the `adr` command's choice) — in a
  sparse event table the honest "N/A for this kind" is `NULL`; it also keeps "0 tokens" distinct
  from "not a token-bearing row". Identity columns (`session_id`, `seq`, `kind`, `ts`,
  `project_repo`) stay non-nullable.

## Decision

Ship `boxer claudemine` with `build` / `overview` / `query` subcommands that parse the Claude
transcript corpus into a **single denormalized Arrow event table** (`claude_events.arrow`) and
query it through `clickhouse-local`, exactly mirroring `boxer adr`.

### Schema — table `events` (one row per transcript event)

Discriminator `kind ∈ { session, user_input, assistant, file_read, file_write, file_edit, commit }`.

**Identity / context** (non-nullable unless noted):

| column | type | notes |
|---|---|---|
| `session_id` | String | transcript UUID |
| `seq` | Int32 | line ordinal within the file (stable tie-break for equal `ts`) |
| `kind` | String | the discriminator (read as `LowCardinality`) |
| `ts` | Timestamp(us, UTC) | event time; for `kind=session`, the first event's time |
| `project_dir` | String | raw encoded directory name (the cwd with `/` replaced by `-`) |
| `project_repo` | String | repo of the session's `cwd`: a configured repo name, or `claude-meta`/`other` |
| `cwd` | String? | absolute working directory |
| `git_branch` | String? | |
| `version` | String? | Claude Code version |
| `uuid`, `parent_uuid` | String? | event identity / threading |
| `is_sidechain` | Bool? | subagent vs main thread |

**Tokens / model** (`kind=assistant`):

| column | type | notes |
|---|---|---|
| `model` | String? | e.g. `claude-opus-4-8` |
| `input_tokens`, `output_tokens` | Int64? | |
| `cache_read_tokens`, `cache_creation_tokens` | Int64? | |
| `context_tokens` | Int64? | `input + cache_read + cache_creation` — the prompt size |
| `service_tier` | String? | |
| `stop_reason` | String? | |
| `request_id` | String? | |
| `n_tool_use` | Int32? | tool_use blocks in this response |
| `has_thinking` | Bool? | |

**User input** (`kind=user_input`):

| column | type | notes |
|---|---|---|
| `text` | String? | the typed prompt verbatim (also holds the title on `kind=session`) |
| `prompt_source` | String? | `typed`, … |
| `permission_mode` | String? | |

**File op** (`kind=file_read|file_write|file_edit`):

| column | type | notes |
|---|---|---|
| `tool_name` | String? | `Read`/`Write`/`Edit`/`MultiEdit`/`NotebookEdit` |
| `file_path` | String? | absolute |
| `target_repo` | String? | **repo of `file_path`** — the cross-repo reference key |
| `file_rel` | String? | path within `target_repo` |
| `file_ext` | String? | |
| `lines_added`, `lines_removed` | Int32? | from `structuredPatch` |

**Commit** (`kind=commit`):

| column | type | notes |
|---|---|---|
| `commit_sha` | String? | |
| `commit_kind` | String? | `committed` (from `gitOperation`) or `referenced` (mined from `git show/log/…` bash args, best-effort) |
| `commit_subject` | String? | first line of the commit's stdout |
| `commit_insertions`, `commit_deletions`, `commit_files_changed` | Int32? | parsed from `git` stdout when present |
| `target_repo` | String? | repo of `cwd` for commit rows |

`target_repo` is the spine of the actual question — *"references to code in a given repo"* =
`SELECT * FROM events WHERE target_repo = '<repo>'` — independent of which session made the
reference. Repo roots are flag-configurable (`--repo name=/abs/path`); no repo set is hardcoded
in the source.

### Integration

- `public/app/commands/claudemine/{claudemine_cmd.go, parse.go, classify.go, arrowemit.go, query.go}`,
  registered beside `adr.NewCliCommand()` at `public/app/main.go:91`.
- `--projects-dir` flag (default `~/.claude/projects`); `--out` (default `.claudeminecache`).
- `arrowemit.go` reuses the `adr` Arrow IPC pattern (`ipc.WithZstd`, `RecordBuilder`), extended
  with nullable builders (`AppendNull`).
- `query.go` reuses `chlocalpool.DefaultBinaryPath` + the basename-`file()` prelude; ships canned
  `overview` queries (spend by repo/model/day, busiest sessions, most-touched files, commit
  cadence) and a passthrough `query <SQL>`.
- No-`clickhouse-local` fallback: print the Arrow path and a plain-text top-N board (as `adr` does).

## Consequences

- One self-contained command turns the opaque JSONL corpus into a compact Arrow file the keelson
  app and `clickhouse-local` query directly; the requested fields are columns, and
  cross-repo references survive the all-projects corpus via `target_repo`.
- The denormalized table is sparse (most columns null on most rows); Arrow + Zstd + ClickHouse
  absorb this cheaply, and `kind` keeps queries unambiguous.
- Re-running `build` is a full re-parse (no incremental cache); the parse is fast enough that this
  is acceptable and keeps the tool stateless.
- Transcripts contain prompt text and file contents; the Arrow output is local-only and
  git-ignored (`.claudeminecache/`). It is **not** to be committed or published.

## Descoped / future

- **The keelson visualization widget** — deferred. The `overview`/`query` CLI is the interim
  surface (same staging as `adr`); the widget is a follow-up once the schema settles in use.
- **`commit_kind='referenced'` mining** from bash `git show|log|cherry-pick|revert <sha>` — shipped
  best-effort and clearly discriminated, so false positives are filterable; the authoritative
  `committed` rows from `gitOperation` are the trustworthy core.
- **Incremental/streaming ingest** (only re-parse changed transcripts; or publish `claude.*` to
  the bus à la ADR-0090) — not needed at present scale; revisit if the corpus grows by an order of magnitude.
- **Generic `tool_call` rows** (Bash/Grep/Agent timing) — out of scope for the requested fields; the
  schema has room to add a `kind=tool` later without disturbing existing columns.
