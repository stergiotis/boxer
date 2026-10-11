---
type: adr
status: accepted
date: 2026-10-11
reviewed-by: "p@stergiotis"
reviewed-date: 2026-10-11
---

# ADR-0325: The journal's canonical form is the canonical wire over its rows

## Context

The run journal's chain (ADR-0314 §SD1) commits to a hand-listed CBOR
struct of the entry's fields. Every body that is not prose is a JSON string
inside that struct: request and reply metadata, compaction intents,
endorsements, grants, approvals, head records, settings, the reply's tool
calls, and the model's tool-call arguments. Since ADR-0320 those strings
live in the vault and the chain sees them only through two per-field
commitments. Three things follow:

- **The chain commits to a serialisation, not to the data.** Two writers
  that agree on a request's metadata but not on its JSON key order would
  disagree on its digest. The struct is a second description of the entry
  beside the table description the store generates from.
- **The ledger cannot answer a question about a body.** "Output tokens per
  run per route" or "approvals per sink" is a JSON extract over vault
  rows, and a row policy under ADR-0313 cannot see inside a blob.
- **Run ids are strings the chat window chose,** and an entry has no
  identity another row can point at (ADR-0319, whose id switch was moved
  here so every digest moves once).

plan002 Part B prepared the ground. B1 built the shredder (`jsonshred`),
and B0 put the ledger and the vault on the `boxer.facts` data sections.
The canonical wire (ADR-0210) is a canonical CBOR encoding of a leeway
entity, generated per table from the same description the store uses.

## Design space (QOC)

**Question.** What does the chain hash for an entry, and how does a writer
compute it before the row exists in any table?

**Options.**

- **O1: the generated canonwire encoder.** Build the entry as a one-row
  Arrow batch through the generated DML, read it back through the
  generated read-access classes, and encode it with
  `leeway canonwire table generate go`'s encoder.
- **O2: the wire runtime from Go values.** Encode the entry's attributes
  directly with the canonwire runtime's slot writer, in the slot order the
  table's generated encoder uses. A test holds the result equal to O1's
  for every kind.
- **O3: keep the hand struct,** widened with the new fields.

**Criteria.**

- **C1: one description.** The preimage follows from the table
  description, not a second list.
- **C2: cost per append.** The writer hashes every entry it writes.
- **C3: the store can re-derive it.** A verifier reading rows from SQL
  computes the same bytes.
- **C4: no import of store code into the writer's path.**

|    | O1 | O2 | O3 |
|----|----|----|----|
| C1 | ++ | +  | −− |
| C2 | −  | ++ | ++ |
| C3 | ++ | ++ | −  |
| C4 | −  | +  | ++ |

O1 builds and drops an Arrow batch of about two hundred columns for every
entry, and it puts the generated DML and read-access classes on the
writer's path, which `MemStore` and the bus client do not otherwise need.
O3 is the status quo the context rejects. O2 is taken. The generated
encoder stays the reference that O2 is tested against, and it is what a
verifier reading rows from SQL uses.

## Decision

We will make **the entry's chain preimage the canonical wire entity item
of its ledger row**, chain columns projected out. The item is encoded from
Go values by the wire runtime in the ledger table's slot order. **One
commitment per vault row** covers that row's canonical wire item. The
**closed shapes become typed components and the open shapes shredded
attributes**, so nothing structured is a JSON string. And **run and entry
ids become tagged u64 ids** from ADR-0319's streams.

### SD1: what the rows hold

- **Prose** — the user's message, a reply's content, notes, sections,
  outcomes, summaries — is one `textArray` attribute in the vault.
- **Closed shapes** are components. ADR-0320 §SD8's classification decides
  where each attribute lives. Content, or a deterministic digest of content
  (ADR-0320 §SD6), goes in the vault: the endorsement, the copy record, a
  grant's task and terms, an exit's question and answer, and an approval's
  request digest. The rest goes in the ledger: request and reply metadata,
  the compaction intent, settings, the profile fingerprint, the turn's
  state, the tool catalog, the head record, and an approval's decision and
  approver.
- **Open shapes** are shredded (`jsonshred`, lossless mode) into the
  vault's sections on the verbatim name + parameters channel: the model's
  tool-call arguments, the reply's call list and an attachment list.
  Text that does not parse is kept as prose with `Form: "unparsed"`.
- `agentrun.Entry` carries the shapes as typed optional fields. The JSON
  helpers at the call sites go. `agentrun` defines its own approval and
  head-record types, since `cell` and `headstore` import it.

### SD2: the preimage

The canonical form is the ledger row's entity item: the envelope (run id,
ordinal) and every ledger attribute, encoded by the canonwire runtime in
the slot order of the ledger's generated encoder. The chain columns
(`Prev`, `Digest`) and the revision are left out. The vault commitment is
in. `ChainDigest(prev, item)` is unchanged. The digest name becomes
`keyed-blake3-256/v1/agentrun-chain/v3`. The run row's seed digest is the
canonical wire item of the run row's immutable attributes, encoded the
same way.

### SD3: one commitment per vault row

For an entry with content, the writer draws the opening (ADR-0320 §SD2) and
commits to the vault row's canonical wire item: keyed BLAKE3-256 under a
key derived from the opening with the context
`commit/blake3-keyed/v2 vault`. The ledger column `vaultCommitment` holds
it, replacing `BodyDigest` and `CallsDigest`. This amends ADR-0320 §SD2.
Erasure is unchanged: the vault row and its opening go, the commitment
stays, and law L31 accepts an absent row exactly where the chain records
the erasure.

### SD4: the endorsement digest

ADR-0316's endorsement names the arguments it endorses. The digest is over
the canonical wire item of the intent's shredded argument attributes, so it
no longer depends on the arguments' JSON spelling. It lives in the
endorsement component, in the vault (SD1).

### SD5: ids

- **Run ids** are tagged u64 ids minted from `IdStreamAgentRun` through the
  run store's KeyStream option. `Run.Id` becomes `uint64` across
  `agentrun`, the bus wire and its callers. The conversation is the run
  row's attribute, and the chat window finds its run by conversation.
- **Entry ids** are tagged u64 ids from `IdStreamAgentRunEntry`, one ledger
  attribute per entry. The entry's Key stays the run id, so a run still
  reads as one range.
- `MemStore` mints from process counters under `IdStreamAgentRunMem` and
  `IdStreamAgentRunEntryMem` (ADR-0319 §SD6).
- **The first `foreignKey` user** is the `llmcall` trail row's approval,
  which names the journal entry it went under by entry id instead of by
  key.

### SD6: deferred, recorded

- Trail rows on the same chain construction (ADR-0314 §SD7): a second call
  of the same encoder over the trail's table description, not done here.
- Typed per-tool argument components for operation tools (ADR-0315).
- The generator owning the shredded attributes (plan002 B6). Until then
  they are written and read by hand-written SQL beside the generated vault
  store.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| ledger and vault tables (ADR-0308 §SD1, ADR-0320 §SD1) | components and shredded attributes on the facts sections; `vaultCommitment` replaces the two per-field commitments; an entry id attribute | the generated stores, the membership golden (a vocabulary id per attribute) |
| run table | the Key becomes a tagged u64 minted by KeyStream | the run store, the SQL store, the bus wire |
| canonical form (ADR-0314 §SD1) | the canonical wire item of the ledger row; digest name v3 | every chain digest; every golden that holds one |
| vault commitment (ADR-0320 §SD2) | one per row, over its canonical wire item | L31, the vaulted store's join |
| endorsement (ADR-0316) | the digest over the shredded arguments' item | chat's endorsement and its check |
| `agentrun.Entry` and `Run` | typed shape fields; `uint64` ids | `apps/chat`'s call sites, `headstore`, `cell`, the introspection tables |
| trail `llmcall` | the approval as a `foreignKey` to the entry id | the trail DTO and its golden |

## Alternatives

O1 and O3 are weighed in the QOC. One narrower choice:

- **Keep per-field commitments beside a commitment for the rest.**
  Rejected: three commitments where one does, and erasure is per row
  anyway.

## Consequences

### Positive

- The chain hashes data in a form the table description defines, and a
  verifier reading rows from SQL computes it with the generated encoder.
- Ledger questions about requests, replies, approvals and compactions are
  column reads, and ADR-0313's row policies see the columns.
- A run and an entry have u64 identities other rows can point at.

### Negative

- Every journal digest moves, and every golden that holds one is
  re-recorded.
- One more encoder, the runtime one, has to agree with the generated one.
  A test holds them together.
- The shredded attributes are written and read by hand-written SQL until
  B6.

### Neutral

- The preimage no longer depends on the shapes' JSON spelling, so a key
  order or a number format cannot move a digest.

## Migration — Tier 1

- **Breaks.** The chain form (v3), the vault commitment, the run id type
  and the ledger's columns. Journals written before do not verify, and
  their tables fail provisioning's shape check (ADR-0308 §SD1's Update of
  2026-10-11), so a host falls back to its in-process store until the
  tables are dropped. Journals exist only in tests and on scratch servers,
  and every `boxer.*` table may be dropped and recreated for this change
  (agreed 2026-10-11).
- **Path.** One change: components, shredded attributes, preimage,
  commitment, endorsement digest and ids land together, and the goldens are
  re-recorded once.
- **Old shape.** None kept.

## Verification plan — Tier 1

- **Lane.** Default: for every entry kind, the runtime-encoded item equals
  the generated encoder's item over the same row read back. A shape
  round-trips writer → store → fold. L31 holds over a run with erasures
  and a deletion. The endorsement digest is the same for two spellings of
  the same arguments. Ids carry their stream's tag and a non-zero body.
  Integration: the journal suites against `clickhouse-local` and a scratch
  server; two stores minting run ids on one server never collide.
- **What would fail.** A JSON string in a ledger or vault column where a
  shape belongs; two encoders disagreeing; a digest that moves when only a
  key order did; a run id from the wrong stream.
- **Gap.** Trail rows are not on the chain (SD6).

## Status

Accepted 2026-10-11.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-10-11 — SD2, SD3: one encoder, the generated one, over the row

The preimage is not encoded by the wire runtime from Go values. The writer
fills the ledger row in memory and the generated canonical-wire encoder
drains it (`gen.Input.CanonWire`, the store's `DrainCanonical`). A runtime
encoder would re-implement the slot and discriminator order the generator
owns, and `agentrun` already depends on the generated store. The chain
columns, the revision and the entry id enter that encoder at their zero
values instead of being projected out. The vault commitment and the
endorsement digest are drained the same way over the vault row. With one
encoder there is no second one to keep in agreement, so the Negative
consequence about two encoders does not hold. The Verification plan's
equivalence check becomes a round trip: a row read back from the table
drains to the item the row as written drained to.

### 2026-10-11 — SD1: what stayed text, and where

- Tool-call arguments that do not parse go to their own vault attribute,
  `ArgumentsText`, not into the prose with `Form: "unparsed"`. A reply's
  call list that does not parse is refused at append.
- The run's settings stay a JSON string on the run row, which keeps its own
  layout (plan002 B0). They are not a ledger component.
- `agentrun.Entry` keeps the call list and the attachment list as strings.
  The writer puts them in `jsonshred`'s canonical form and the store shreds
  them, so the text read back is the canonical text.
- The introspection tables render an entry's record into their `body`
  column as the JSON the body used to be (`Entry.RecordBody`), so the SQL
  laws and the agent-schema relations that read text still read it. The
  `body_digest` and `calls_digest` columns give way to `arguments`,
  `vault_commitment` and `entry_id`.

### 2026-10-11 — SD5 lands in a following change

This change keeps string run ids. It adds the entry-id attribute, which
holds zero until SD5 lands, and SD5 follows as its own change. No golden
holds a chain digest, so the split does not re-record any golden twice.

### 2026-10-11 — SD4 names the wrong record

The endorsement is [ADR-0311](./0311-labels-integrity-levels-tlp-tiers-marks-and-endorsement.md)
§SD3's, not ADR-0316's. SD4, the Surfaces table and the References should
read ADR-0311.

### 2026-10-11 — SD5 landed, and SD2's run seed with it

- The run id is minted before the row is written, because the seed
  hashes it: `StoreI.MintRun`, which the SQL store serves from the run
  store's `MintKey` over a `rowalloc` sequence, `MemStore` from the
  in-memory stream, and the bus through a `mint` verb. `Open` mints when
  the run carries no id.
- `Append` sets each entry's `EntryId` from the entry stream. An entry
  that already carries one keeps it, so a retried batch does not give one
  ordinal two ids. The bus's append reply returns the ids.
- The run seed is the canonical wire item of the run row with only its
  immutable attributes set, drained by the run store's generated encoder.
  B2–B5 left it a record digest over the fields, and that change's Update
  did not say so.
- The chat window keeps the conversation id as its own key. It finds a
  conversation's run through `agentrun.RunOfConversation`, which reads the
  owner's run list, so a conversation whose newest run is past the list
  bound is not found.
- A model request names its run (`llm.Request.Run`), and the approval key
  spells the run in decimal. The `llmcall` trail row's approval is a
  `foreignKey` to the approval entry's id. Laws L14 and L33 join on that
  id, and L33 checks that the approval was written in the exit's
  conversation, read through the approval's run row
  ([ADR-0312](./0312-exits-sinks-from-the-cell-boundary-approval-tiers-and-exit-budgets.md)'s
  Update of this date).
- Where a run id is text — the recalled band, projection's journal
  table, an agent-schema scope setting — it is the decimal form, and the
  scope compares its column as text.

## References

- [ADR-0210](./0210-leeway-canonical-wire-generator.md) — the canonical wire and its generated encoder.
- [ADR-0314](./0314-tamper-evident-records-chain-digests-and-anchored-heads.md) §SD1 — the chain form this supersedes.
- [ADR-0320](./0320-journal-bodies-in-a-vault-committed-to-the-chain-under-a-per-entry-key.md) §SD2 — the per-field commitments this replaces.
- [ADR-0319](./0319-tagged-id-streams-leased-through-one-row-on-the-one-clickhouse-server.md) — the id streams.
- [ADR-0316](./0316-compaction-as-a-journal-entry-plan-act-apply.md) — the endorsement.
- [ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md) — the journal.
