---
type: adr
status: accepted
date: 2026-10-10
reviewed-by: "p@stergiotis"
reviewed-date: 2026-10-11
---

# ADR-0308: The run journal is the only source of model input — entries with a label and a chain digest, Keep governs the trail, deletion is a tombstone

Supersedes [ADR-0303](./0303-a-conversation-outlives-its-process-a-run-journal-written-before-each-act.md),
whose journal, resume fold, grant rule and pinning are carried forward here
under a stronger reading.

## Context

ADR-0303 proposed a run journal written before each act and folded on
resume, as a crash record beside the coordinator's in-memory history. The
design work of 2026-10-10 ([agent-harness §2.2](../adr-background-work/agent-harness.md))
found that every further property worth having — a request anyone can
re-derive, compaction as an appended entry, replaying a stored run under
another configuration, laws over production journals, an agent's reads
scoped to its own run — follows only if the journal is the *only* source of
what the model receives, and that the systems read for that page which
resume well (OpenHands, Codex, pi) all derive their messages from the log
rather than beside it. The owner took that reading on the compile date.

Two further questions ADR-0303 left open are settled here because they
change the journal's shape: what "Keep off" means once a journal is always
written, and what deleting a run does once entries carry a chain digest that
[ADR-0314](./0314-tamper-evident-records-chain-digests-and-anchored-heads.md) anchors outside the cell.

## Design space (QOC)

**Question.** Where does the model's input come from?

**Options.**

- **O1 — A crash record beside an in-memory history.** ADR-0303 as written.
- **O2 — The journal is the only source.** The coordinator keeps no
  history; every wire request is a pure function of the journal prefix and
  the profile.
- **O3 — O2 with a cached projection.** The coordinator may keep the last
  projection in memory for latency, provided it equals what the projection
  would return.

**Criteria.**

- **C1 — Reproducibility.** A request can be rebuilt from stored data and
  compared with the one sent.
- **C2 — One state.** Compaction, edits, replay and laws need no second
  store of truth.
- **C3 — Latency.** What a model call waits for.
- **C4 — Consistency with ADR-0264 §SD6** — the ditch test holds, the kept
  `llmMessage` rows stay audit.
- **C5 — Cost** of the change to the chat.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 |
|----|----|----|----|
| C1 | −  | ++ | ++ |
| C2 | −− | ++ | +  |
| C3 | ++ | −  | +  |
| C4 | +  | ++ | ++ |
| C5 | +  | −  | −  |

O1 leaves the defects the inventory found in place ("told" watermarks the
turn does not restore, a prompt frozen in process memory) and makes every
later feature carry its own state. O3 is O2 with a cache whose correctness
law L4 already checks; it is admitted as an implementation detail of O2, not
a separate option, and is why the row is close. O2 is taken.

## Decision

We will make the run journal the **only source of model input**: the
coordinator keeps no conversation history of its own, and every wire request
is a pure function of a journal prefix and a profile,
`request(run, n) = encode_route(π_profile(J[0..n]))`, where π is
[ADR-0309](./0309-context-projection-as-sql-over-the-journal-in-a-local-worker.md)'s projection and the profile is
[ADR-0310](./0310-harness-profile-and-registry-composition-declared-per-seam.md)'s. The journal's tables, write-ahead rule, resume fold, grant
rule and pinning are ADR-0303's, adjusted below; the journal is always
written, Keep governs the trail only, and deletion is a tombstone.

### SD1 — Tables, carried forward and widened

ADR-0303 §SD1 stands: a generated record store in the house layout
(`agentrunstore`) with its own tables, `boxer.agentrun` (one row per run,
latest version wins) and `boxer.agentrunentry` (append-only), not a kind on
`boxer.facts`, with its own retention; the ditch test keeps holding because
the chat reads its journal and never the kept `llmMessage` rows. Each entry
gains:

| Column | Carries | Decided in |
| --- | --- | --- |
| `label_level`, `label_tier`, `label_exposure`, `label_origin` | an integrity level, a TLP tier, and the exposure and origin marks, by name | [ADR-0311](./0311-labels-integrity-levels-tlp-tiers-marks-and-endorsement.md) |
| `prev`, `digest` | the chain digest: a digest over the previous entry's digest and this entry's canonical form; the first entry of a run chains to the run row | ADR-0314 |
| `body_digest`, `calls_digest` | content digests of the body and the tool calls, what the canonical form commits to instead of the bodies, so a tombstone (SD8) leaves the chain intact | ADR-0314 §SD5 |
| `writer_run`, `writer_turn`, `writer_ordinal` | the writer of a body a model wrote, for re-entry detection | ADR-0311 §SD4 |
| `attachments` | the images a user entry carries, by digest | ADR-0287 |
| `key_id` | reserved and empty: the key under which a body would be encrypted if per-run encryption is taken up | [ADR-0307](./0307-threat-model-for-an-interactive-single-person-cell.md) §SD12 |
| `parent` | reserved: a parent ordinal for branching, unused until a consumer asks | ADR-0264 §SD2 |

The run row carries, beside the state (`open`, `closed`, `stranded`,
`deleted`), fingerprint, profile, settings and label of SD5 and SD6, its
chain seed (`digest`), the chain's `head`, a `version` that orders its
writes, so a clock tie or step cannot resurrect an older row, and
`deleted_by` and `deleted_at` once tombstoned.

A run id is **cell-qualified**: the row names its cell, so that entries
arriving by sync from a peer cell can keep their run id and ordinals and
the one-writer rule of SD9 holds per (cell, run). Sync itself is not built;
this cell's writer mints `<app>-<namespace>-<n>` ids.

### SD2 — Entry kinds: a closed enum with a golden

ADR-0303 §SD2's kinds (`user`, `request`, `reply`, `intent`, `outcome`,
`asked`, `answered`, `note`, `turn`) and its rule — appended and flushed
before the act leaves, a failed write refuses the act — stand. The enum
gains the kinds the projection and the later records need, and is closed:
ordinals never retire, a golden pins them, and an `ignorable` kind carries
extension-private entries the projection skips. As built, `asked` and
`answered` also carry the exit-approval question and answer (form `exit`,
ADR-0312 §SD3) and a task grant in force (form `grant`, ADR-0311 §SD3),
and `ignorable` carries the compaction records of ADR-0316 (name
`compaction`). The journal's context is passed to appends unlinked from the
turn's cancellation, so that a stopped turn still records its tail.

| Kind | Carries | Moves the projection |
| --- | --- | --- |
| `section` | a named prompt section set or cleared, with its book and slug | yes; rewrites the head |
| `tools` | tools added and removed, by name and schema digest | yes, on change |
| `compact` | a cut ordinal, a summary, the summariser's call key, the summary's label ([ADR-0316](./0316-compaction-as-a-journal-entry-plan-act-apply.md)) | yes; rewrites from the cut |
| `edit` | a target ordinal and a replacement or nothing; the latest edit wins | yes; rewrites one entry |
| `profile` | profile id and fingerprint at the first request and on change | no |
| `note` | as before, with a source and a form (snapshot or notice) | yes |
| `candidate`, `verdict`, `select` | a sample of one request; a verifier's result on it; the chosen sample with its rule | no |
| `completed` | completion of asynchronous work a command started, apart from its `outcome` ([ADR-0315](./0315-coordinator-tools-as-operation-specs-and-a-contract-for-stateful-tools.md)) | no |
| `endorse` | who, the digest of the body as shown, the display epoch, the act classes, the diff since the last endorsement (ADR-0311) | no |
| `approval` | an exit above a channel's free tier: channel, tier, approver, justification ([ADR-0312](./0312-exits-sinks-from-the-cell-boundary-approval-tiers-and-exit-budgets.md)) | no |
| `head` | a chain head shipped to the anchor store: ordinal covered, digest, receipt (ADR-0314) | no |
| `copy`, `issued` | a clipboard write of labelled text with its digest; a watermark payload issued for a frame ([ADR-0317](./0317-clipboard-and-export-copy-by-reference-and-one-choke-point.md)) | no |
| `deleted` | the tombstone of SD8 | no |
| `keyuse` | reserved, unused while encryption is deferred | — |
| `ignorable` | extension-private | no |

### SD3 — No history outside the journal

The coordinator holds no message list. Prompt sections, the tool set and
the mid-turn notes are entries. ADR-0303 §SD6's fingerprint is the
profile's ([ADR-0310](./0310-harness-profile-and-registry-composition-declared-per-seam.md)
§SD7): a digest over the rendered sections, the tool schemas and the
constants, recorded on the run row and as a `profile` entry; no engine
version enters it while the Go fold is the production projection
(ADR-0309 §SD1). The coordinator projects with that fold on every request;
the request rebuilt from the journal equals the one sent by the identity
ADR-0309 §SD3 defines (law L4), checked in the test lanes against the
engine's twin of the projection.

### SD4 — Resume, carried forward

ADR-0303 §SD3 (settle each unfinished act by effect class: a `request`
without `reply` is remade, a query without `outcome` is reported, a command
is never resent, a consequential one is listed to the person first), §SD4
(grants and approvals do not survive and are asked again as a new task in
the same conversation) and §SD5 (windows are opened again, never assumed)
stand unchanged. Only the open turn's acts are unsettled: a `turn` entry
closes its turn with whatever did not settle in it. A call a reply made
whose outcome was never recorded gets an `outcome` saying so on resume, so
the projected request pairs every call (ADR-0309 L2) and the model is told.
A closed run resumes by moving back to open; a deleted or stranded one
reopens read-only. Reading each resource's **rebuild class** (ADR-0315
§SD4) and treating "applied, not completed" as its own state are deferred
until a resource declares them.

### SD5 — Pinning, carried forward

ADR-0303 §SD6 stands: a run is pinned to the fingerprint of the code it
started with; a mismatch strands it, readable and not continuing; the build
id is recorded and shown, not compared. Re-projecting the run's last request
at resume and comparing its digest, the finer check that would let a run
survive an engine upgrade, is deferred: the fold keeps the last request for
it, and nothing reads it while the fingerprint alone decides.

### SD6 — Labels and settings travel with the run

ADR-0303 §SD7 stands with the label widened to ADR-0311's: the run row
carries the run's current label, the ceiling and the settings fixed at the
first send. The store joins every appended entry's label into the row's as
it appends, and an update never lowers it. A run that read AMBER stays
AMBER.

### SD7 — Keep governs the trail only

The journal is always written and kept: it is the run's state, not audit.
"Keep" keeps the one meaning
[ADR-0264](./0264-retained-model-conversations-on-facts.md) gave it —
whether `llmMessage` bodies are retained on the trail — and nothing else. A
person who wants a run gone deletes it (SD8). ADR-0265 §SD2's "a new window
starts an empty conversation" no longer describes the chat: a new window
starts a new run, and the conversation list of ADR-0303 M3 is where old ones
are.

### SD8 — Deletion is a tombstone

Deleting a run appends a `deleted` entry naming who (in its name column,
which no tombstone empties) and when, then empties every body column of the
run's entries; entry rows, their metadata, labels, content digests and
chain digests stay, so the chain and the heads ADR-0314 anchored still
verify, and a reader sees that a run was deleted and when, not what it held.
The SQL store appends body-less twins of the rows, which win under
latest-wins, and blanks the originals by a later mutation; it never removes
rows, so a failure part-way leaves the journal whole. This replaces ADR-0303 §SD8's "deletion removes the
entries". Data-subject
erasure is out of scope ([ADR-0307](./0307-threat-model-for-an-interactive-single-person-cell.md) §SD12);
if it returns, the reserved key column is where a per-run key would make
bodies unreadable without touching the chain.

### SD9 — One writer per run

The coordinator is the single writer of its run: it assigns ordinals,
computes each entry's chain digest before the append, and flushes before the
act. ClickHouse provides no total order; the writer does. The host service
holds the invariants a second writer would break, serialising appends per
run: one run per batch, the sender owns it, the run is open (or the batch
is the closing `deleted` entry, after which nothing appends), ordinals are
contiguous, each entry chains to the row's head, bodies match their
digests, a `compact` or `edit` targets an earlier ordinal, a mark carries
no comma; a row update keeps the run's identity, seed and head, moves only
between the declared states, and reaches `deleted` only behind a `deleted`
entry. Verification accepts a tombstoned entry only on a deleted chain.
Concurrent coordinators on one run are not designed.

### SD10 — The read surface

ADR-0303 §SD8 stands: a host service `runtime.agentrun.*` scoped to
`Msg.Sender` — `create`, `update`, `get`, `list`, `append`, `entries`,
`tombstone`, with deletion composed by the writer as the `deleted` entry,
the tombstone and the row's state — and the introspection tables
`agent_runs` and `agent_run_entries`, the latter a snapshot bounded to the
newest runs. The relations an *agent* reads are not these tables but
the generated schema of
[ADR-0313](./0313-agent-schema-generated-relations-an-agent-may-read-in-full.md), whose `my_run` is this run's entries in full.

### SD11 — What stays out

ADR-0303 §SD9 stands: automatic resume and window-less runs, triggers, and
seeding a new conversation from a stranded one. Added: branching beyond the
reserved `parent` column; multi-writer merges; per-run encryption.

### Milestones

- **M1 — The store and the service ✓**, with the widened columns and the
  closed enum's golden; no coordinator writes yet.
- **M2 — The chat writes its journal ✓**, every kind of SD2 that the compile-date
  chat can produce, the chain digest included; a failed write refuses the act.
- **M3 — The journal is the only input ✓**: the coordinator's history, prompt
  constants and tool set become entries and the projection of ADR-0309 builds
  every request; law L4 runs on each request in the test lanes.
- **M4 — The fold and resume ✓**, the conversation list, pinning (ADR-0303
  M3–M4).
- **M5 — Tombstone deletion ✓**, and the resume conformance lane of ADR-0303
  M5, as the chat's resume tests over a ctx-honouring store and the
  preservation harness's law checks.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `boxer.agentrun`, `boxer.agentrunentry` | added: generated record store; label, `prev`, `digest`, `body_digest`, `calls_digest`, `key_id`, `parent` columns on entries, `head` and `version` on runs; the kind enum and its golden | the store's generator test; the vocabulary ids the store's columns take |
| `runtime.agentrun.*` | added, as ADR-0303 §SD8, with the invariants of SD9 | the chat manifest's caps; `hostboot` wiring |
| `keelson('agent_runs')`, `keelson('agent_run_entries')` | added | the introspection registry |
| Chat app ([ADR-0265](./0265-chat-app-over-retained-model-calls.md)) | reshaped: no in-memory history; prompts and tools as entries; a conversation list. §SD2 ("the history the app resends each turn is its state") and the deferred items of §SD5 this record settles (resume, context window with ADR-0316, system prompt as sections) are superseded in substance; a dated Update on ADR-0265 records which, on acceptance. Whether ADR-0265 as a whole is superseded by a new chat record is a later decision | its scenes; its tests through the preservation harness |
| ADR-0264 §SD6 | unchanged; the ditch test holds by construction | — |
| ADR-0280 §SD7 "persisting the settings" | settled by SD6; a dated Update on acceptance | — |

## Alternatives

O1 and O3 are weighed in the QOC. Three narrower choices:

- **Keep off deletes the run at close.** Rejected: it gives "Keep" a second
  meaning and loses the resume-after-crash case for exactly the runs a
  person did not mark; deletion is one gesture away (SD8).
- **Keep off keeps the journal in a non-durable store.** Rejected: the design
  would lose its only input source for those runs, and a confined run would
  need a second store with the same locality rule.
- **Deletion as a recorded gap** — remove the entries, keep the last head.
  Rejected in favour of the tombstone: the chain then has a gap the anchor
  store must explain, and the regulator's check weakens for every deleted run.
- **No deletion where an anchor store exists.** Rejected: it removes a
  control the person has today for a benefit the tombstone gives anyway.

## Consequences

### Positive

- Every request is reproducible from stored data, by anyone with the engine
  (law L4); compaction, edits, replay and the laws need no second state.
- The prompt and tool set stop being coordinator state, which removes the
  "told" watermark defects.
- A deleted run keeps its place in the chain; tamper evidence and deletion
  do not fight.
- ADR-0303's resume, grant and pinning rules are kept word for word where
  they were right.

### Negative

- An engine takes part in every model call (ADR-0309 contains this with a
  warm local worker; it is still a dependency the chat did not have).
- The journal is the most complete record of what an agent saw in a
  deployment; inside a single-person cell that is the person's own data, and
  its protection at rest is the host's, not the harness's.
- Two engines whose versions must agree, and a closed enum with a golden to
  keep.

### Neutral

- Watchbill and unattended runs are not involved (SD11).

## Migration — Tier 1

- **Breaks.** Nothing: the store and service are new; conversations started
  before M3 have no journal and are not resumable. ADR-0303 is superseded
  before any of it was built.
- **Path.** None.
- **Regeneration.** The record store's generator test; the kind enum golden.
- **Old shape.** A chat without a journal behaves as before until M3.

## Verification plan — Tier 1

- **Lane.** Default `go test` for the fold, for L31 and the compaction and
  endorsement laws over the preservation harness's corpus, and for L4 against
  the engine's twin where `clickhouse` is installed; the integration lane for
  the store against `clickhouse-local`; the chat's resume tests as the
  conformance lane of ADR-0303 M5. L1 as a query over stored journals is
  not written.
- **What would fail.** A request whose rebuilt digest differs from the
  recorded one; a command whose `intent` has no `outcome` dispatched again
  after resume; a run whose fingerprint differs resuming instead of
  stranding; a deleted run whose chain no longer verifies; a kind written
  that the golden does not know.
- **Gap.** Effects outside the process are reported as unknown, not
  reconciled, as in ADR-0303.

## Status

Accepted 2026-10-11.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-10-11 — SD8: a tombstone is the entry at its next revision

The entry table binds the record store's Revision role (ADR-0100's Update of
this date). A tombstone appends each entry at revision 1 with its bodies
empty, and every read keeps the highest revision of an ordinal in SQL. The
`1<<40` order offset the twin was written at, and the reads' twin-first
sort, are gone. The later deletion of the originals stays best-effort and
now names them by revision 0, so it is exact.

### 2026-10-11 — SD9: the run row is rewritten by compare-and-set

The run table is generated with `gen.Input.Cas` (ADR-0223's Update of this
date). A run's one row is inserted when the run is created and then
rewritten in place, guarded on the version the store last read. The
rewrite counts as won only when the row reads back as exactly what was
written. A second store, in another process, that moved the row first makes
this store's rewrite fail, and this store then drops its cached row. It
used to append a row the reads would also have taken. So the version check
no longer holds only within one process, but this is not yet the writer
lease of ADR-0319 §SD7: two coordinators can still interleave entries on
one run until that lands.

## References

- [ADR-0303](./0303-a-conversation-outlives-its-process-a-run-journal-written-before-each-act.md) (superseded by this record) — the journal, resume fold, grant rule and pinning carried forward.
- [ADR-0264](./0264-retained-model-conversations-on-facts.md) — retained conversations; the ditch test; Keep.
- [ADR-0265](./0265-chat-app-over-retained-model-calls.md) — the chat app.
- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) — operations, grants, effect classes.
- [ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md) — the trail; call keys.
- [ADR-0280](./0280-a-ceiling-on-what-a-chats-model-may-do-scored-on-a-ladder.md) — the ceiling and its deferred settings.
- [ADR-0307](./0307-threat-model-for-an-interactive-single-person-cell.md) — erasure out of scope; the key column reserved.
- [ADR-0309](./0309-context-projection-as-sql-over-the-journal-in-a-local-worker.md), [ADR-0310](./0310-harness-profile-and-registry-composition-declared-per-seam.md), [ADR-0311](./0311-labels-integrity-levels-tlp-tiers-marks-and-endorsement.md), [ADR-0314](./0314-tamper-evident-records-chain-digests-and-anchored-heads.md) — the projection, the profile, the label, the chain.
- [agent-harness](../adr-background-work/agent-harness.md) §2.2 — the analysis.
