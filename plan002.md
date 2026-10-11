---
type: explanation
audience: the session that closes the recordstore generator gaps the agent-run journal worked around by hand, and then moves the journal's bodies from JSON strings to leeway attributes — a self-contained brief, read before touching `public/storage/recordstore/gen`, `public/keelson/runtime/agentrun/` or `agentrunstore`
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** A working brief, not a decision.
> Written 2026-10-11 after the agent harness landed (ADR-0308, ADR-0309,
> ADR-0314, ADR-0316). Part B changes the entry canonical form, so it ends
> in dated Updates on accepted ADRs or a short ADR of its own; the brief says
> which at each step.

# plan002: the journal store, finished in the generator and made data-centric

Two parts, in order. Part A closes four `recordstore/gen` gaps the journal's
SQL store worked around by hand. Part B, which depends on A's first step,
first moves the journal's two tables onto the `boxer.facts` section
vocabulary (the tables stay the journal's own; only the sections are
shared), then moves every structured entry body out of a JSON string into
leeway attributes: closed shapes as components under vocabulary ids, open
shapes as semi-structured path attributes on mixed membership channels, and
the chain's preimage as the canonical wire over the row. After both, the
journal is queryable without JSON extraction with the same read surface as
the trail, the agent-schema relations of ADR-0313 expose columns rather
than blobs, and the chain hashes data rather than a serialisation.

## Why this exists

The agent-run journal (`public/keelson/runtime/agentrun`) is stored by a
`recordstore/gen` store on two tables of its own (`agentrunstore`). That is
the house pattern (ADR-0105 D3a, ADR-0223 §SD1, ADR-0308 §SD1) and it is the
right lane: a run's entries are deleted with the run, which the shared
`boxer.facts` audit table must never see (ADR-0303, rejected option "journal
on facts").

What the generator did not offer, the journal's `SqlStore` built by hand, and
each hand-built piece was where the reviews found defects:

| Need | What `SqlStore` does today | Where |
| --- | --- | --- |
| One identity per (run, ordinal) | one Key (run id) + one Order (ordinal as a 1970-epoch `SeqTs`); a tombstone twin is written at `Order + 1<<40` and reads collapse twin-first | `sqlstore.go` `tombstoneOffset`, `scanEntriesLocked` |
| Compare-and-set on the run row's version | compared in memory against a `live` map; relies on one writer per process | `sqlstore.go` `UpdateRun` (`r.Version <= was.Version`) |
| Newest-first bounded run list | `ScanLiveRun` reads every run, sorts in Go, truncates to `limit` | `sqlstore.go` `ListRuns` |
| `async_insert = 0` on journal inserts (ADR-0309 §SD4) | not possible; `ExecutorI.InsertArrow` takes no settings | `sqlstore.go` top-of-file `// deferred:` |

The same four gaps exist for every other store-owned table (watchbill,
persist, lading). Closing them in the generator removes code from the journal
and gives the next consumer the verbs instead of the trick.

Separately, every structured entry body is a JSON string in one `entryBody`
text column: request and reply metadata, compaction intents, endorsements,
grants, approvals, anchor heads, copy records, settings, the tool catalog,
attachments, and the model's tool-call arguments. A question like "output
tokens per run per route" or "approvals per sink" is a JSON extract today,
and a row policy under ADR-0313 cannot see inside a blob. The chain digest
commits to those bytes, so identity is a serialisation rather than the data.
Leeway already has the shape for the open tail: the leeway-advanced skill's
JSON mapping (path, parameters, value, one section per canonical type on the
mixed channel), and `agentrunstore/schema.go`'s `sectionOf` already admits
every membership channel on every section.

The journal's table descriptions share their genes with `boxer.facts`
(`factsschema.LoadRuntimeFactsMapping`): multi-attribute rows, one `data`
streaming group, the same membership channels, the same vocabulary registry.
They differ in layout, and the difference is where the journal is weak.
Facts has one section per canonical type, so an attribute is typed by where
it sits and lists are native (`symbolArray`, `u32Set`); the journal has one
scalar section per attribute, so its mark lists are joined strings and its
calls and attachments are JSON. Every entry column already has a facts
section it maps onto, so the open tail's sections exist in the facts
vocabulary before anything is added. Putting the journal's kinds on
`boxer.facts` itself stays rejected (ADR-0303, and structurally: the facts
Key is a per-process counter, its engine is `ORDER BY ts`, it has no u8
lifecycle, and its DDL exists on every host). Adopting the facts *sections*
on the journal's *own* tables is the move, and the facts loader's comment
says it was exposed for a composite mapping to extend without forking.

## Goal

After Part A, `agentrun.SqlStore` has no `tombstoneOffset`, no in-memory
version check, no Go-side sort-and-truncate, and no `// deferred:` for
`async_insert`. Every one of those is a generated verb or an option on the
executor. The journal's behaviour, its goldens and its laws (L31, L32) do not
change; the integration tests under `agentrun` and `agentschema` stay green.

After Part B, the journal's two tables are built from the facts section
vocabulary plus three value-less sections of their own, and no entry body
written by the harness or by chat is a JSON string. Prose is one text
attribute. Closed shapes are components. The model's tool-call arguments
and the attachments are semi-structured attributes recomposed on demand.
The chain preimage is the canonical wire item of the row without its chain
columns, under a new form version, from the same generated encoder family
a trail row would use. The chat goldens are re-recorded once, in the step
that changes the form, and not again.

## Constraints you inherit

- Build and test with `-tags="$(cat ./tags)"`; `go mod tidy --diff` for
  drift; live-server tests stay under `//go:build integration`.
- Stage and commit by explicit path. Do not touch files you did not create or
  change for this plan. Do not commit unless asked.
- Errors via `eh`/`eb` with `%w`; interfaces end in `I`, enums in `E`; cite
  ADRs with `§SD`. Run `go run ./public/app gov codelint ./public/storage/...
  ./public/keelson/runtime/agentrun/... ./apps/chat/...` and `go run
  ./public/app gov doclint doc/` before you stop. Pre-existing findings in
  `apps/chat`, `apps/play` and `gov_codelint_driver.go` are not yours.
- The generator's output is checked in (`*.out.go`, `*.out.sql`). Regenerate
  through each package's `gen_test.go`, never by hand.
- `rowcas` has conditions (its package doc): `update_parallel_mode='sync'`,
  block-position table settings, no heavyweight mutation ever, immutable key,
  no `async_insert` on the claim path, one server. A store that opts into CAS
  opts into those; the generated DDL must carry them and the store doc must
  say so.
- Vocabulary ids are claimed in `public/keelson/runtime/vocab` with the
  membership golden and the `AllMembs` set-comparison test; a new id is a
  new line there, never a literal elsewhere.
- Accepted ADRs change only by dated `## Updates` entries. ADR-0100 (record
  store), ADR-0223 (watchbill store and `rowcas`), ADR-0308, ADR-0309,
  ADR-0313, ADR-0314, ADR-0316 and ADR-0319 are the ones a step below may
  touch; ADR-0320 is new, written in B5.
- One membership channel per section: `marshallreflect.PlanFor` refuses a
  section mixing channels across fields. Static components and
  semi-structured attributes mix within a row, never within a section.

## Part A: four generator gaps

Each step is one commit, buildable on its own, in this order.

### Step A1: a secondary Order role, so (Key, Order) is a composite identity

**Gap.** `gen.Roles` binds one Key and one Order. A tombstone that must
*supersede at the same position* has nowhere to go, hence the `1<<40` twin.

**Shape.** Add `Roles.Revision`, a `u32` envelope plain column (settled
2026-10-11: it counts supersessions of one position, and four billion is
headroom). When bound:

- `ORDER BY` becomes `(key, order, revision)`.
- `Scan<Kind>`, `Replay` and `Entries`-style range reads collapse rows by
  (key, order) keeping the highest revision, in SQL (`LIMIT 1 BY key, order`
  after `ORDER BY revision DESC`, or `argMax`), so a superseding row is one
  insert and reads never see both.
- `Begin(key, order).Revision(n)` on the builder; `Latest` and `GetLive`
  unchanged.

**Where.** `gen/gen.go` (`Roles`, validation in the role binder),
`gen/store_emit.go` (DDL `ORDER BY` seam near the ADR-0102 table clause,
scan emitters around the `ORDER BY ... ASC` lines), a new example under
`recordstore/example` with the role bound, and the generator's tests.

**Journal change.** `agentrunstore/schema.go` binds `entryRevision`;
`SqlStore.Tombstone` writes the blank twin at revision 1 and drops the
`tombstoneOffset` constant, the twin-first sort and the best-effort `DELETE`
of originals. `Entries(from)` goes back to a one-range predicate.

**Record.** ADR-0100 gains an Update naming the role; ADR-0308 §SD8
(deletion is a tombstone) gains an Update ("a tombstone is the entry at its
next revision").

### Step A2: generated compare-and-set on the state-view row

**Gap.** A store with a Lifecycle column has latest-wins reads, but no
conditional write. `rowcas` composes the statements and watchbill uses it by
hand (`watchbillstore/claim.go`); the journal checks the version in memory.

**Shape.** `gen.Input.Cas bool` (settled 2026-10-11: a flag, with the guard
as a predicate, so watchbill's state-and-time guard fits as well as the
journal's version equality). When set the generator emits:

- `Provision`/`EnsureTable` DDL with `rowcas.TableSettingsTail`, and
  `AlterTableSettingsSQL` applied on an existing table.
- `Update<Kind>If(ctx, key, where string, sets ...)`: `rowcas.UpdateSQL`
  with the key equality ANDed to the caller's guard. `where` is SQL over the
  physical columns, trusted input only, as `ScanOpts.ExtraPredicate` is.
  The caller reads back and decides `won`. `rowcas` has no `Guard` type,
  and the plan does not add one.
- `Delete` composed through `rowcas.DeleteSQL`, so no heavyweight mutation
  ever runs on a CAS table.
- A store doc sentence stating the `rowcas` conditions it now lives under.

**Journal change.** `SqlStore.UpdateRun` becomes one CAS on `runVersion`;
`Run.Version` is already the counter. The run table changes from one row
appended per version and folded latest-wins (`versionTs`) to one row per
run, inserted at create and updated in place. A lightweight update
rewrites a row, and a guard has to see the one row it guards. The `live` map
stays as a read cache only. The per-run mutex in `agentrun/service.go` stays (it also serialises
appends); the comment that it is the only thing keeping versions honest goes.

**Watch for.** `rowcas` forbids `async_insert` on the claim path, so Step
A4's setting is `0` on CAS tables regardless of what a caller asks. Say so
in the generated doc.

**Record.** ADR-0223 gains an Update (CAS is now a generator option, not a
watchbill hand-build); ADR-0308 §SD9's one-writer paragraph gains an Update.

### Step A2b: watchbill's claim on the generated compare-and-set

Settled 2026-10-11: watchbill moves too, in a commit of its own after A2.
`watchbillstore/claim.go`'s `ClaimSQL`, transitions and delete compose
through `UpdateJobIf` instead of `rowcas` directly. The DDL-scraping
`valueColumnsOf` goes once the generated verb names the columns. The
watchbillstore integration test and the twenty-worker race are the check.

### Step A3: ordered, bounded scans

**Gap.** `ScanOpts` has `Limit` but every scan sorts `ORDER BY order ASC,
key ASC` (two emitters in `store_emit.go`). A newest-first list reads the
table and truncates in Go.

**Shape.** `ScanOpts.Descending bool` and `ScanOpts.OrderBy` naming an
envelope plain column (default: the Order role). The state-view scan
(`ScanLive<Kind>`) takes the same. Validation at generation time pins the
column set; at run time an unknown column is an error, not SQL text.

**Journal change.** `ListRuns(ownerApp, limit)` becomes one `ScanLiveRun`
with `OrderBy: runCreated, Descending: true, Limit: limit` and the
`ExtraPredicate` on owner. The Go sort goes.

**Record.** ADR-0100 Update.

### Step A4: per-insert settings on the executor

**Gap.** `recordstore.ExecutorI.InsertArrow(ctx, table, records)` carries no
settings, so a store cannot ask for `async_insert = 0` (ADR-0309 §SD4) or
`wait_for_async_insert`.

**Shape.** Keep `ExecutorI` as is. Add an optional interface
`InsertSettingsI` with `InsertArrowWith(ctx, table, records, settings
InsertSettings) error`, where `InsertSettings{AsyncInsert *bool,
WaitForAsyncInsert *bool}` renders to the `SETTINGS` tail of the insert.
`chexec.LocalExecutor` and `ipcexec` implement it; a store generated with
`gen.Input.InsertSettings` calls it when the executor has it and falls back
with a logged `deferred` otherwise. CAS tables (Step A2) force
`AsyncInsert=false`.

**Journal change.** `agentrunstore` generates with `AsyncInsert=false`; the
`// deferred:` at the top of `sqlstore.go` goes.

**Record.** ADR-0309 §SD4 gains an Update saying the setting is now applied.

### Step A5: the journal's SQL store, shrunk

Delete what Steps A1–A4 made redundant from `sqlstore.go` and its tests, keep
`repairLocked` (two tables are still two tables; no cross-table atomicity
exists and none is promised), re-record nothing in `apps/chat/testdata` (the
goldens are over the Go fold and must not move in Part A), and run:

```sh
go test -tags="$(cat ./tags)" ./public/storage/recordstore/... ./public/keelson/runtime/agentrun/... ./public/keelson/runtime/watchbill/...
scripts/ci/gotest-integration.sh   # agentrun, agentschema, watchbillstore against the scratch server
go test -tags="$(cat ./tags)" -race ./public/keelson/runtime/agentrun/... ./apps/chat/...
```

The L31/L32 checks (`agentrun.VerifyRun`, `anchorstore.VerifyHeads`) and the
SQL twin golden under `agentrun/projection` are the proof the chain did not
move.

### Step A6: id streams and the writer lease (ADR-0319)

**Gap.** Nothing mints a row id that is unique across two processes on the
one server, and the run row's version check is in memory, so two instances
can both open a run. ADR-0319 decides both on the same primitive as A2.

**Shipped 2026-10-11, ahead of A1** (ADR-0319's Update of that date):
- the four stream claims in `vocab.IdStreams`, the persisted pair and SD6's
  in-memory twins, pinned by a new `idstream-claims.golden` kind that
  `namemint/assignments`'s union check reads;
- `boxer.idlease` and its allocator, `identgen/leased/rowalloc`. The lease
  row is the tag's row at the lowest block position, so a duplicate from a
  racing first insert is never the lease;
- the table's provisioning at host boot.

**Remaining shape.** In this order, after A2. Neither changes an id, so no
golden moves:

- A generator option to mint the Key from a stream: an `identgen` sequence
  over `rowalloc`, with the block size a store config value defaulting to
  1024 (settled 2026-10-11, the same for runs and entries; an appliance sets
  it large, ADR-0319 §SD4). Exercised by a recordstore example here; the
  journal binds it in B5.
- The writer lease on the run row (ADR-0319 §SD7): open is a CAS setting
  the writer and bumping the version; each append batch is claim, insert,
  confirm; readers follow the chain from the head; `repairLocked` adopts
  only rows that chain. The writer registers presence through
  `watchbillpresence`, and a takeover is allowed only when the holder is
  dead.

The id switch itself (u64 run and entry ids, SD6's in-memory minting, the
first `foreignKey` users) moves to Step B5. Settled 2026-10-11:
`RunDigestSeed` hashes the run id, so the switch moves every chain seed and
every digest golden. Landing it in B5's commit keeps Part A's goldens still
and re-records them once.

**Where.** `recordstore/gen` (the Key-from-stream option),
`agentrunstore/schema.go`, `agentrun/sqlstore.go`, `agentrun/service.go`,
`agentrun/writer.go` (`Open`). A two-store test on one scratch server in
the integration lane.

**Record.** ADR-0319 is accepted (2026-10-11); what ships under it is a
dated Update there, and ADR-0308 §SD9 gains an Update pointing at it.

## Part B: bodies as attributes, the chain over the row

Part B needs Step A1 (edits and tombstones of exploded rows want the
revision role), and B5 needs A6's Key-from-stream option for the id switch;
nothing else from Part A. Each step is one commit. Step
B0 changes the tables' layout without changing what they hold; the chain
form changes once, in Step B5, and every digest-dependent golden is
re-recorded there and only there.

### The three groups of bodies

Every body the journal holds falls into one group, and the group decides its
treatment. The inventory, from `apps/chat` and the runtime packages as they
stand:

| Group | Bodies | Treatment |
| --- | --- | --- |
| **Prose the model reads verbatim** | user message, reply content, notes, prompt sections, tool outcomes, summaries and elisions, the ignorable records' text | one text attribute, as today; nothing to explode |
| **Closed shapes the harness owns** | request meta `requestBody{Digest, Messages, Tools, Round, Label}`; reply meta `replyBody{CallId, FinishReason, InputTokens, OutputTokens}`; `compactIntent{Cut, From, Span}`; `endorseBody{Digest, Epoch, Classes, Previous, Diff}`; `grantBody{Task, Handle, Terms, Unattended}`; `exitQuestion` and its answer; `cell.Approval`; `anchorstore.HeadRecord`; the copy record; `journalSettings`; the profile fingerprint; the turn's terminal state; the tool catalog (`KindTools`, names and schema digests) | one component per shape, attributes under vocabulary ids |
| **Open shapes the model or a provider writes** | tool-call arguments (`KindIntent.Body`, schema per tool), the reply's call list (`Calls`), attachments (media type, digest, bytes reference), provider metadata if any | semi-structured attributes: path, parameters, value on the mixed channel, one section per canonical type; the raw text kept only when it does not parse |

Strike from the second group anything you would rather keep as text, but
say why in the step's commit message; the default is every row of the table.

### Step B0: the journal's tables on the facts section vocabulary

**Gap.** `factsschema.LoadRuntimeFactsMapping` loads the facts envelope and
the facts data sections in one call. The journal needs the sections with an
envelope of its own.

**Shape.** Split the loader in `factsschema` into `LoadFactsEnvelope(manip)`
and `LoadFactsDataSections(manip)`, with the existing function calling both
so `boxer.facts` and its generated DDL are byte-identical before and after
(`factsschema/ddl`'s golden is the check). Beside them goes an opt-in
`LoadValuelessSections(manip)` adding the three value-less sections `null`,
`emptyObject`, `emptyArray` for Step B1. `LoadRuntimeFactsMapping` never
calls it: a section on `boxer.facts` is a DDL change on every host's audit
table. It lives in `factsschema` rather than the journal so the shredder's
other consumers find it beside the sections it pairs with (names and home
settled 2026-10-11). Then `agentrunstore/schema.go`:

- Run table: its own envelope (`id` S, `ts` Z64, `lifecycle` U8) plus
  `LoadFactsDataSections`; `sectionOf` goes. `id` stays the conversation
  string here, and the conversation string also becomes an attribute
  (`stringArray,unit`), which is what B5's switch to a u64 id keeps.
- Entry table: envelope `id` (the run id, as above), `ordinal` U64 as the
  Order role (settled: A1 lands first, so the `SeqTs` disguise of an ordinal
  as a timestamp goes here), Step A1's revision column, plus
  `LoadFactsDataSections` and `LoadValuelessSections`.
- Both DTOs re-sited onto the facts sections, in the trail's spelling:
  kind, role, form, effect, book, turn and the writer refs to `symbol`;
  keys, digests and names to `stringArray,unit`; body to `textArray,unit`;
  ordinals, targets and parents to `u32Array,unit`; level and tier to
  `u8Array,unit`; the write instant to `timeArray,unit`; `Exposure` and
  `Origin` to `symbolArray` as arrays, so `unionMarks`'s joined string and
  its comma refusal (`MarkValid`) become an array and a set test.
  `Calls` and `Attachments` stay text until B3.

**What does not change.** The tables, their names, their engines, the
store-owned pattern, ADR-0303's reasons, the chain form (B5 is where that
moves; here `CanonicalForm` reads the same fields from the new columns and
the digests are unchanged). The SQL fold's column references move; its
golden against the Go fold must still match.

**Where.** `factsschema/factsschema.go`, `agentrunstore/schema.go`,
`run_dto.go`, `entry_dto.go`, `gen_test.go` and the regenerated `.out.*`,
`agentrun/sqlstore.go` (column names), `agentrun/label.go` (marks as
arrays), `projection/sql.go`, `agentschema`'s relation declarations and its
scratch-server test.

**Record.** ADR-0308 §SD1 Update (the tables are facts-shaped); ADR-0313
Update for the renamed columns. ADR-0026 §SD6 needs none: facts itself is
untouched.

**The vault.** ADR-0320 moved `Body` and `Calls` to a third table,
`agentrunbody`, with a per-entry opening, and the ledger's `BodyDigest`
and `CallsDigest` are commitments under it. B0 lays the vault table out on
the facts sections too (`textArray` for the body, `stringArray` for the
opening and the calls), and the drop of the ledger's empty body columns
that ADR-0320 §SD8 defers lands in B0. B3's semi-structured sections go
on the vault table, not the ledger: exploded arguments are content.

### Step B1: the semi-structured shredder

**Gap.** Nothing in the tree turns JSON into leeway's path, parameters,
value triples. Card JSON was removed by ADR-0289; the only shred-like code
is markdown front matter. The leeway-advanced skill describes the mapping;
no package implements it.

**Shape.** A new leeway package beside `canonform`, `jsonshred` (name open),
with `Explode(doc []byte) (attrs []Attr, err error)` and `Compose(attrs
[]Attr) ([]byte, error)`, where `Attr{Path string; Params []uint64; Section
SectionE; Value any}` and `SectionE` names facts sections: `textArray`,
`stringArray`, `symbol`, `i64Array`, `f64Array`, `bool`, plus the three
value-less journal sections `null`, `emptyObject`, `emptyArray`. Two modes,
as the skill's `LoadJsonMapping` and `LoadJsonMappingLossless` already
split them: the lossless mode uses the value-less sections and round-trips
byte-for-canonical-byte; the plain mode drops `null`, `{}` and `[]` under
the stated quotient "absent, null and empty are one". The journal uses the
lossless mode. No consumer of the journal needs the fidelity (no tool reads
a recomposed argument, and the digests need determinism only), but the
shredder is a shared asset for ADR-0171 §SD5's companion table and
ADR-0025's vault, and three sections are its whole cost. The fidelity
rules, each pinned by a property test over `rapid`:

| Input | Rule |
| --- | --- |
| object keys | JSON Pointer escaping (RFC 6901: `~0`, `~1`); key order is not preserved; `Compose` emits sorted keys; a duplicate key is refused |
| array index | the high-cardinality parameter; the path carries `_` at that position |
| a literal key `_` | escaped `~2`, so it never reads as the parameter placeholder |
| integer within int64 | `i64` |
| integer beyond int64, or a decimal float64 cannot hold | `string` with the lexical form, under a path suffix the composer recognises (`~n`) |
| float64 | `f64`; `-0.0`, `1.0` and `1e0` recompose as the shortest round-trip form, which is the dCBOR reduction canonform already applies |
| `""`, `{}`, `[]`, `null` | lossless mode: the value-less sections; `""` is `stringArray` with an empty value. Plain mode: the quotient above |
| low-cardinality strings | `symbol` only when the caller's hint says so (a tool's schema `enum`), never inferred from data |
| invalid JSON | `Explode` fails; the caller stores the raw text with `Form: "unparsed"` and no attributes |

**Where.** New package; the skill's section becomes a pointer to it. No
DDL. A fuzz test on `Compose(Explode(x)) == canonical(x)`.

**Settled 2026-10-11.** The path grammar is JSON Pointer (RFC 6901) with
`_` at each array-index position, the indices carried as parameters in
order, and the `~` escape space extended by `~2` (a literal `_` key) and
`~n` (a number kept as its lexical form). It is the spelling the
leeway-advanced skill already uses. It is the one grammar for the journal,
the ADR-0171 §SD5 companion table and the ADR-0025 vault; a second spelling
would mean a migration later. The journal uses the lossless mode, and no
closed shape is struck from the second group above.

### Step B2: components for the closed shapes

**Shape.** One DTO per closed shape in `agentrunstore`, each a component on
the entry store over the facts sections of B0, scalar and `unit` shapes
only (the facts doc's "avoids all three refusals by construction"); a list
field such as `endorseBody.Classes` is a `symbolArray` under one
membership. Vocabulary ids for every attribute in `vocab`, with the golden
and `AllMembs` updated. The entry's body attribute stays for prose; a
closed-shape entry has an empty body and its component set. Because the
sections are shared, components tell apart by membership id alone, which
is how the trail's fourteen kinds already coexist on `boxer.facts`.

The writer's API changes shape: `agentrun.Entry` gains typed optional fields
(`Request *RequestMeta`, `Reply *ReplyMeta`, `Compact *CompactIntent`,
`Endorse *Endorsement`, `Grant *Grant`, `Approval *cell.Approval`, `Head
*anchorstore.HeadRecord`, `Copy *CopyRecord`, `Settings *Settings`, `Tools
*ToolCatalog`, `Turn *TurnState`), each mapped to its component, and the
chat's `jsonMarshal` call sites in `chat_journal.go`, `chat_seams.go`,
`chat_compact.go`, `chat_endorse.go`, `chat_approval.go` set the field
instead. `KindEdit` with `Form: "restore"` restores the components it
restores. Tombstones blank them.

**Where.** `agentrunstore/*_dto.go` (new files), `schema.go`, `gen_test.go`,
`agentrun/entry.go`, `agentrun/wire.go`, `agentrun/fold.go`,
`projection/fold.go` and `projection/sql.go` (read the components, not the
body), the chat call sites above. `MemStore` carries the same fields.

**Record.** ADR-0308 §SD2's entry table gains an Update listing the
components; ADR-0313's relation declarations gain the new columns.

### Step B3: the semi-structured sections and the hand DML

**Shape.** After B0 the per-canonical-type sections exist and carry the
mixed channel already (the facts loader puts the spinnaker triple on every
data section). `KindIntent` arguments, `KindReply` calls and attachments
are written through `Explode` into them, on
`MembershipSpecMixedLowCardVerbatimHighCardParameters`, with the path as
the verbatim name and the array indices as parameters. The components of
B2 sit on the same sections under the low-cardinality ref channel, which is
the mix-within-a-row the constraints allow, since no DTO field mixes
channels within one section. Because the generated store cannot read a
mixed channel (`ReadRowSupported` refuses carrier channels; the read-back
generator's plan maps only the four simple channels), the write and read
of the exploded attributes are a hand DML beside the generated store, in
`agentrunstore/semi.go` (name open), with a `// deferred:` naming Step B6.
Two templates exist: `gov/capmapfacts`'s hand-rolled write side, and
`factsstore/chstore`'s log fan-out, which writes carrier-channel
memberships onto the very same facts sections.

The fold recomposes through `Compose` where a provider echo needs the
arguments as text; the SQL twin reads the attributes directly through the
read surface (`LW_SEL` by path, `LW_GET` with `param:`), which is the first
place the SQL fold becomes simpler than the Go fold.

**Where.** `schema.go`, `agentrunstore/semi.go`, `agentrun/sqlstore.go`,
`projection/encode.go` (`CallsBody` goes), `projection/fold.go`,
`projection/sql.go`, `chat_seams.go` (intent and reply call sites). The
agentrun integration test gains a round trip of an intent with nested
arguments through the scratch server.

**Record.** ADR-0308 §SD2 Update; ADR-0313 Update for the new columns and
what a row policy can now see.

### Step B4: endorsement over attributes (in B5's commit)

ADR-0316's endorsement digest is over the arguments' bytes. After B3 the
bytes are not stored, so the digest is over the canonical form of the
exploded attributes (Step B5's preimage restricted to the intent's
semi-structured sections). `chat_endorse.go` and `checkEndorsed` compute it
that way. Settled 2026-10-11: this lands in B5's commit, not before it. It
needs B5's encoder, and one commit means the `consent-held` and
`operation-tools` goldens move once along with every other digest golden.

**Record.** ADR-0316 Update.

### Step B5: the chain preimage is the canonical wire over the row

**Shape.** The entry's canonical form stops being a hand-listed CBOR struct
over `buscodec`. It becomes the canonwire entity item (ADR-0210) of the
entry row with the chain columns (`entryPrev`, `entryDigest`) projected out,
fed to a generated per-table encoder from the same table description the
store uses (`leeway canonwire table generate go`, checked in as
`agentrunentry_canonwire.out.go`). Prose, components and semi-structured
attributes are all in the item; bodies no longer enter as content digests,
so `entryBodyDigest` and `entryCallsDigest` go — or, under ADR-0320,
stay as the commitments the vault rows open, with the vault row's own
canonical item the thing committed to. `ChainDigest(prev, item)`
is unchanged. `DigestName()` becomes `keyed-blake3-256/v2/agentrun-chain`
and `RunDigestSeed` moves onto the same writer.

The same commit carries B4 and ADR-0319's id switch, so every digest moves
once:

- The run table's `id` becomes the tagged u64 of `IdStreamAgentRun`, minted
  through A6's Key-from-stream option and hashed as an integer. Chat then
  finds its run by the conversation attribute, not by id
  (`chat_journal.go` sets `Run{Id: inst.conversation}`).
- The entry table gains `entryId` (`u64Array,unit`) from
  `IdStreamAgentRunEntry`, the thing a trail row's `foreignKey` points at.
  The entry's Key stays the run id, so a run still reads as one range.
- `MemStore` mints under `IdStreamAgentRunMem` and
  `IdStreamAgentRunEntryMem` (ADR-0319 §SD6).
- The first `foreignKey` users: trail rows that reference journal entries
  carry the entry id.

The approval request digest in `llm/approval.go` stays over the provider
wire bytes: what leaves the cell is what must be bound (ADR-0312 §SD3).

Because the row is facts-shaped after B0, the encoder is the one a trail
row would use with its own table description. ADR-0314 §SD7's deferred
"trail rows gain the same chain per row" becomes a second call of the same
generator rather than a second form; note that in the Update, do not do it
here.

**Where.** `agentrun/entry.go` (`canonical`, `CanonicalForm`, `Digested`,
`BodiesMatch`, `Verify`), `agentrunstore` (generated encoder),
`agentrun/service.go` and `anchorstore/store.go` (`VerifyHeads` re-verifies
through the same form), `projection/batch.go` (message digests over the
same writer, order kept as an array). Every golden with a digest in it is
re-recorded in this commit: `apps/chat/testdata/preserve/*`,
`agentrun/testdata`, `projection/testdata`.

**Record.** A new ADR, ADR-0320 "the journal's canonical form is the
canonical wire" (settled 2026-10-11). It supersedes ADR-0314 §SD1 (the
record form is no longer deferred) and ADR-0309 §SD3 (the message item is
the same writer), and covers the v2 digest name, B4's endorsement digest
and the u64 ids. The superseded sections and ADR-0316 each gain a dated
Update pointing at it, and ADR-0319 gains one recording that the id switch
shipped.

### Step B6: the bag component in the generator (may be deferred)

**Gap.** Step B3 left the semi-structured sections on a hand DML. The
generator could own them: a component shape of `[]struct{Path string;
Params []uint64; Value T}` per canonical-type section, written and read by
the generated store, with the read-back generator's plan front-end taught
the mixed channel. ADR-0100 defers this by name; `gov/capmapfacts` is the
second consumer and the better test of the shape.

Do this step only if B1–B5 landed with room to spare (settled 2026-10-11:
decide then, not now). Otherwise leave the `// deferred:` from B3 and put
this section into ADR-0100's Updates as the next piece.

## Non-goals

- Moving the journal onto `boxer.facts`. Rejected in ADR-0303 and kept so in
  ADR-0308 §SD1; the facts lane's `ORDER BY ts` primary key and missing u8
  lifecycle make it the wrong shape regardless.
- Any change to `boxer.facts`'s own DDL. B0 splits its loader so the
  journal can call the sections part; the facts table's generated DDL stays
  byte-identical, and the three value-less sections are the journal's.
- `[]byte` keys for the facts lane, `Roles` on `storegen.Input`, a composite
  Key. Real gaps, but not ones the journal has.
- A durable anchor store. ADR-0314 §SD2 puts it outside the person's host.
- A Go-value canonical encoder (a `canonform` or `canonwire` over arbitrary
  structs). Part B makes it unnecessary for the journal: the row is the
  value.
- Typed per-tool argument components for operation tools (ADR-0315). The
  semi-structured sections carry them; a typed twin is a later plan.
- Inferring `symbol` from data. Low cardinality comes from a schema hint or
  not at all.

## Decisions settled with the user, 2026-10-11

Before Part A:
1. `Roles.Revision`, `u32` only.
2. An `Input.Cas` flag, with the guard as a caller predicate.
3. Watchbill moves onto the generated CAS, as Step A2b.
3a. The tag values are claimed and pinned in
    `vocab/testdata/idstream-claims.golden`; the default block size is
    1024 for runs and entries alike.

Before Part B:
4. `LoadFactsEnvelope`, `LoadFactsDataSections` and an opt-in
   `LoadValuelessSections`, all in `factsschema`.
5. The entry's Order becomes a u64 `ordinal` in B0.
5a. A6 is split: the generator option and the writer lease stay in Part A,
    and the id switch lands in B5's commit.
6. JSON Pointer with `_` for an index position, `~2` for a literal `_` key
   and `~n` for a lexical number.
7. Lossless mode for the journal.
8. No closed shape stays as text.
9. B5 is recorded by a new ADR-0320, with B4 in the same commit.
10. B6 is decided once B1–B5 have landed.

No question is open.
