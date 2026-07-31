---
type: adr
status: accepted
date: 2026-07-29
reviewed-by: "p@stergiotis"
reviewed-date: 2026-07-29
---

# ADR-0148: App workingsets — restorable working state as launch-shaped facts

## Context

What a keelson app keeps when its window closes is today an app-private
convention with one sanctioned channel. The lifecycle is `Open → Mount →
Frame× → Unmount` ([ADR-0026](./0026-app-runtime-and-capability-subjects.md));
every first-class app is factory-registered, so in-struct state dies with the
window. The only survival path is `runtime.persist.{alias}.{key}` blobs
(`StorageI`), and its limits shape everything downstream:

- **Opaque and conventional.** Keys are raw NATS tokens, values are `[]byte`;
  the runtime knows nothing about what an app keeps beyond the
  `Manifest.PersistedKeys` string list. Restore precedence, save timing, and
  key schemas are re-invented per app. play is the only full adopter (editor
  buffer + Timeline bands SQL, restored in Mount below `BOXER_PLAY_SQL`,
  saved on user-intent Run and as an Unmount fallback); an adopter repo's
  playground embedder reproduces the same ~150 lines under its own alias by
  cloning play's manifest wholesale.
- **No instance or name dimension.** Keys are per app alias. Two windows of
  the same app race last-writer-wins on close order, silently.
- **Ambient inheritance is unconditional.** Save-on-Unmount persists whatever
  the buffer holds. An adopter's handover affordances — publish an ephemeral
  ad-hoc dataset ([ADR-0134](./0134-adhoc-datasets.md)), open a playground
  window seeded with a `keelson('<handle>')` read
  ([ADR-0135](./0135-app-launch-requests.md)) — poison the stored buffer:
  closing the seeded window unedited persists a query over a handle the
  producer retracts at its own Unmount. The next plain open inherits a dead
  query.
- **Durability is thinner than documented.** The persist service backend is
  in-memory (`persist.NewMemoryBackend()` in the carousel); the
  `boxer.facts`-backed backend of ADR-0026 §SD6 is a recorded follow-up. The
  facts store itself, however, *is* CH-backed when ClickHouse is up — the
  window host already writes app-lifecycle and launch facts through it.

Meanwhile the pieces of a better answer already exist. ADR-0135 gave every
argument-accepting app a leeway-declared launch-config DTO, validated at the
host boundary (`kindcheck`), delivered frozen at Mount, and *persisted as an
audit fact* (`LaunchRow`: target, kind, config bytes, tile key, caller).
ADR-0132 framed the applet as play with frozen, committed arguments. The
missing point on that axis is the ambient one: the state a closed window
leaves behind and a fresh window inherits.

The mainstream platforms converged on how this should behave, and this ADR
deliberately adopts their semantics rather than inventing new ones:

- **Three state tiers** (Android's saving-states guidance): in-memory UI
  state; small serialized *restoration state* holding references and
  positions, not content; and persistent user data. Restoration state stores
  IDs — "persist the complex objects in local storage and store a unique ID
  for these objects in the saved state APIs."
- **User- vs system-initiated dismissal** (Android and iOS, identically): a
  user who dismisses a screen expects a clean slate — restoration state is
  deliberately discarded; only durable drafts survive. System-initiated
  eviction restores seamlessly.
- **Continuity is keyed by a caller-chosen stable name, never the
  framework-minted instance key**: Android documents in Recents
  (`documentLaunchMode`), Fuchsia `persistent_storage` collections (a
  destroyed child's storage survives; a same-named successor inherits it),
  Wayland `xx-session-management-v1` toplevels (client-supplied unique
  names).
- **The restoration payload is the launch payload.** iOS uses one type —
  `NSUserActivity` — for deep links, Handoff, and scene restoration.
- **The host owns save points and window state; the app supplies content.**
  iOS asks the scene delegate at detach; the Wayland compositor stores the
  state; X11's XSMP, which demanded full app cooperation, decayed for
  decades until the revived protocol narrowed responsibilities.

## Design space (QOC)

**Question.** How does an app's user-authored working state survive window
close and process restart, and how do fresh instances inherit it —
first-class, typed, introspectable, compatible with launch configs and
applets — without inventing constructs the platform literature already
settled?

**Options.**

- **O1 — Status quo, documented.** Keep per-app persist-key conventions;
  write the guidance down.
- **O2 — App-side library over `StorageI`.** A helper package (save/load,
  envelope, index) formalizing the appletstore pattern; apps call it from
  Mount/Unmount.
- **O3 — Workingset service + subject family.** A runtime service owning
  `runtime.workingset.>` save/load subjects, typed rows behind it.
- **O4 — Host-mediated launch-shaped facts.** Apps expose a compose hook;
  the window host writes a typed workingset fact beside the launch facts it
  already writes, and restores by routing plain opens through its own
  `OpenWithConfig`.

**Criteria.**

- **C1 — Marginal cost per app** (adoption and ongoing drift).
- **C2 — Data-centricity**: records are typed, queryable, retained facts.
- **C3 — Semantic uniformity**: dismissal/promotion rules live in one place.
- **C4 — Future-proofness**: named sets, desktop resume, crash recovery,
  a user dimension — without migration.
- **C5 — Hygiene fit**: caps, audit, one CBOR dialect (ADR-0026, ADR-0135).
- **C6 — Convergence**: matches the platform-literature semantics.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 status quo | O2 library | O3 service | O4 host-mediated |
|----|---------------|------------|------------|------------------|
| C1 | − (per-app reinvention) | + | − (caps + subjects per app) | ++ (compose-only) |
| C2 | −− (opaque blobs) | − (blobs + hand index) | + (typed rows) | ++ (facts beside LaunchRow) |
| C3 | −− | − (N conventions) | + | ++ (host policy) |
| C4 | −− | − (key migrations) | + | ++ (columns + host params) |
| C5 | + | + | + (new surface to govern) | ++ (no new caps/subjects) |
| C6 | −− | − | + | ++ (iOS/Wayland shape) |

## Decision

Adopt **O4**. A **workingset** is the named, typed record of an app's
user-authored working state, expressed as the app's own launch-config DTO
and stored as a fact — "the launch that would reproduce this window,"
written at the closing edge exactly as `LaunchRow` records the opening edge.

- **SD1 — Scope: drafts in, caches and chrome out.** A workingset carries
  what the user authored or chose: buffers, draft parameters, the active
  view. It never carries recomputable caches (result histories, memos —
  cache-tier state in the Fuchsia `data`/`cache`/`tmp` sense, evictable by
  construction), window geometry (host-owned; see the deferral), published
  artifacts (the applet store's job), or team-shared documents (files and
  facts tables, reviewed and versioned outside the runtime). Content too
  large or complex for the record follows the platform store-the-ID rule:
  it lives under the app's declared persist keys, and the record carries
  the reference.

- **SD2 — The record is the launch config.** A workingset record is an
  instance of the app's `Manifest.LaunchKind` DTO — the same kind, codec,
  golden, and vocabulary entry ADR-0135 already requires. Restore is
  therefore *definitionally* `OpenWithConfig`. An app without a launch
  config cannot have a workingset; declaring one is the same investment as
  becoming launchable. The identity fields (name, timestamps, provenance)
  live on the fact row, not inside the DTO.

- **SD3 — Identity is (app id, name).** The store keys workingsets by the
  durable app id plus a caller-chosen name. Names use the persist-key
  charset (one NATS token) so a future subject family or key projection
  needs no migration. v1 wires exactly one name, `default` — behaviour
  matches today's ambient inheritance — but the store, row, and host
  parameters carry the name from day one. Concurrent windows on one name
  remain last-writer-wins, now as documented policy rather than accident.

- **SD4 — Save is host-pulled, gated on intent.** A participating app
  implements one optional interface:
  `WorkingsetComposerI { ComposeWorkingset() (cfg []byte, dirty bool, err error) }`.
  At window close (reap) and at shutdown (`ReapAll`), the host calls it
  **before** `Unmount` and writes the record **iff `dirty`** — dirty
  meaning user intent occurred in this window (edits, a manual Run; play
  already tracks exactly this for its persist points). The rule is uniform
  and closes the poisoned-inheritance case by convention: a
  launch-seeded window closed untouched writes nothing; a plainly-opened
  window nobody typed into writes nothing; the moment the user acts, the
  next close persists. The host validates before writing — the claimed
  kind's `kindcheck` probe and the 64 KiB cap, the same boundary rules as
  an open — and never stores bytes that fail them. Compose errors are
  logged and skip the save. Participation requires factory registration
  (the ADR-0135 singleton-refusal precedent applies unchanged).

- **SD5 — Restore rides the existing door, with a visible reason.** At a
  plain open of a participating app, the host looks up the latest record
  for (app id, `default`) and, when present and kind-matching, routes the
  open through its own `OpenWithConfig` with the stored bytes. No second
  delivery channel exists: the app decodes a launch config in Mount, which
  SD2 guarantees it already does. `MountContextI` gains the one method this
  forces, `LaunchReason()` — `plain | caller | restore` (the Wayland
  `launch/recover/session_restore` gradation, minus the deferred recover
  tier) — so adopters can keep environment overrides between the two
  config tiers. The documented precedence for adopters becomes: **caller
  config > env override > restored config > default**. A missing or
  mismatched record degrades to a plain open; a facts store that is down
  degrades the same way (best-effort, the audit-trail stance).

- **SD6 — The fact.** `factsstore` gains a workingset row beside
  `LaunchRow`, sharing its vocabulary where the columns coincide (config
  kind, config bytes, `tileKey`, run id, the reply-cohort `reason` term for
  save provenance) plus the workingset name. The contract grows
  `WriteWorkingset`, `LatestWorkingset(appId, name)`, and
  `DeleteWorkingset` with `LatestState`-style semantics (append-only rows,
  reverse-scan latest, tombstone delete) — history and undo are therefore
  the row trail, not a feature. Restores are audited as launches whose
  caller is the synthetic id `runtime.workingset`, so "which windows opened
  from restored state" is a facts query. The vocabulary cohort file sorts
  lexically after the existing dimdata files (the ADR-0135 ordering
  constraint).

- **SD7 — Declaration.** `Manifest.Workingset bool` marks participation;
  registration validation refuses it without a non-empty `LaunchKind`.
  `PersistedKeys` is unchanged and remains the right declaration for raw
  keys and SD1-referenced draft content. A `keelson.workingsets`
  introspection provider is a recorded follow-up (the ADR-0135
  `keelson('apps')` precedent), not built here.

- **SD8 — play adopts, as the reference.** play composes `PlayLaunch` from
  its live state (`Sql`, `BandsSql`, active `Tab`, `Live`; `AutoRun`
  composes false — re-execution is user intent, not restoration), maps its
  existing user-intent tracking onto `dirty`, and inserts the restored tier
  into its documented precedence. For one release play also falls back to
  reading its legacy persist keys when no workingset row exists, then both
  keys and their `PersistedKeys` entry retire. The embedder seam is a
  consequence: play exposes its compose/seed helpers so a manifest-cloning
  embedder inherits the contract under its own app id instead of copying
  Mount code.

- **SD9 — Naming.** The concept is **workingset**, one word, lowercase —
  in identifiers `Workingset`. The term names the working state itself,
  not the mechanism; "session" is avoided (already taken by remote-access
  and auth contexts), as is "snapshot" (implies immutability this record
  does not have).

## Alternatives

- **Status quo, documented (O1).** Rejected: writing down N conventions
  does not merge them, and the poisoned-inheritance case stays. Every
  criterion scores it lowest.

- **App-side library over `StorageI` (O2).** Rejected for the core:
  records stay opaque blobs under key conventions, enumeration stays a
  hand-rolled index per consumer (the appletstore precedent), restore and
  launch remain two seeding code paths per app, and the dismissal rule
  stays N app conventions. The library shape survives inside SD1's
  referenced-content path, which keeps using declared persist keys.

- **Workingset service + subject family (O3).** Rejected: it buys typed
  rows at the cost of a new capability surface (subjects, caps, a service
  lifecycle) for state that is per-app-private, and still leaves save
  timing and gating to each app. The host already stands at both lifecycle
  edges and already holds the facts store; a service would re-create that
  position beside it.

- **egui-side persistence** (persisting the Rust layer's widget memory).
  Rejected: opaque to the data plane, keyed by widget ids that are
  deliberately instance-salted and never reused, and it would preserve
  chrome state (scroll, collapse) rather than user content — the tier the
  platform guidance says to *discard* on user dismissal.

- **Unconditional save-on-close (today's play behaviour).** Rejected as
  the default by the dismissal convention (Android/iOS discard on
  user-initiated dismissal; only intent-promoted drafts survive) and by
  the observed handle-poisoning. A per-app override was considered and
  dropped: no current app wants it, and the gate is one boolean.

**Deferrals** (recorded, with triggers):

- **Desktop resume** — the host replaying (app id, name) pairs from its own
  rows at boot; needs nothing from apps beyond this ADR. Trigger: a user
  asks for "reopen my desktop"; the launch facts + workingset rows already
  suffice.
- **Crash recovery** (`reason = recover`) — a host save cadence for dirty
  windows plus a recover tier that may restore more than a plain launch
  (the Wayland reason-scaling). Trigger: the SD8 durability note below
  actually biting someone.
- **Named-set UX** — a picker, open-with-name, delete affordances, and a
  deliberate "fresh window" gesture. The store is name-ready (SD3); the
  surfaces are not designed here.
- **Host-side geometry** — window position/size continuity, name-keyed,
  host-owned (the Wayland compositor split). Explicitly out of the
  workingset record.
- **Per-applet parameter overlays** — applets stay stateless across close
  by design (their manifests declare no workingset); revisit if applet
  users demonstrably re-enter parameters.
- **A user dimension** — workingsets are personal by definition; a
  deployment that multiplexes users per process would add a row membership
  and host stamp. Team-shared state remains files and facts tables.

## Consequences

### Positive

- One place — the host — owns save timing, the intent gate, restore
  routing, and validation; adopting apps write a compose method and a
  dirty flag.
- Restore ≡ `OpenWithConfig`: the ambient → launch → applet axis becomes
  mechanical, and every workingset is also a shareable deep link.
- Workingsets are typed facts beside the launch facts: queryable, retained,
  auditable, with history-as-rows; durability arrives with the CH-backed
  facts store that is already wired, independent of the persist-backend
  follow-up.
- The poisoned-inheritance case (ephemeral `keelson('<handle>')` seeds)
  is closed by the uniform dirty gate, not a special case.

### Negative

- Contract growth again (the ADR-0135 negative repeated): an optional
  `AppI` interface, a `MountContextI` method, a `Manifest` field, and a
  `FactsStoreI` extension that both backends must implement.
- v1 saves only at close and shutdown: play's current persist-on-every-Run
  is mid-session more durable against crashes than SD4 until the crash-
  recovery deferral lands. Recorded, accepted.
- A restored config now outranks the persisted-buffer tier it replaces
  while sitting *below* env overrides; scripted flows relying on the old
  `env > persisted` order are unaffected, but flows that expected a plain
  open to ignore prior state must pass `reason`-visible configs or clear
  the workingset.
- With ClickHouse down, workingsets silently degrade to process-lifetime
  (in-memory facts store) — the same best-effort stance as the audit
  trail, and the same honesty obligation.

### Neutral

- The persist subject family, `PersistedKeys`, and the appletstore are
  untouched; the facts-backed persist backend remains a follow-up worth
  landing for referenced content and stored applets, but is no longer a
  prerequisite for workingsets.
- Hygiene-not-security unchanged: the host validates and audits; nothing
  here is an isolation boundary.
- Applets remain stateless across close; the applet primitive is the
  frozen, curated point of the same axis, unchanged by this ADR.

## Update — 2026-07-29: implemented

SD1–SD9 are implemented and green: the facts row and both store backends,
the `runtime/app` contract growth, the host's save and restore paths, and
play's adoption. What the implementation settled differently from the
decision text, or had to decide because the text was silent:

- **Vocabulary home.** SD6 anticipated a dimdata cohort file. There was
  nothing to declare there: SD2 makes the record an instance of the app's
  existing `LaunchKind` DTO, so no new codec module and no new DTO
  columns exist. The two new terms — the workingset kind tag and the set
  name — are appended to the end of `runtime/vocab`'s membership block
  instead, which is the same append-only id discipline the ordering
  constraint asks for. Everything else reuses the launch cohort
  (`MembRuntimeApp`, `MembRuntimeRun`, `MembLifecycleTileKey`,
  `MembLaunchConfigKind`, `MembLaunchConfig`, `MembLifecycleStopReason`);
  a delete row reuses `MembPersistTombstone` rather than minting a
  parallel term, since the kind tag already disambiguates the row.

- **Where the restore's launch row is written.** `WriteLaunch` lives in
  the bus-facing open service, which attributes callers from the bus
  envelope. A restore has no envelope, and the only place that knows a
  restore happened is `OpenWithConfig`, so the host writes that row
  itself beside the app-lifecycle "started" row. A plain open that
  arrived over the bus and then restored therefore produces two launch
  rows — the caller's plain request and the restore — which is the
  honest record; `caller = runtime.workingset` still isolates restores.

- **Singleton participation.** The registry does not expose whether an
  app was registered as a singleton, and nothing was added to make it. On
  the restore side the existing config-delivery refusal already covers it
  (SD4's "applies unchanged"): a second window of a singleton participant
  is refused, loudly, which is the enforcement. On the save side the host
  skips a window whose AppI instance another window still holds — that
  state belongs to the survivor — and logs why.

- **Restore-tier field rules, beyond the recorded `BandsSql` case.** A
  field a record composes by construction rather than by intent must not
  be applied on restore at all: play composes `AutoRun` false always, so
  its restore tier leaves the `BOXER_PLAY_AUTORUN` decision untouched
  rather than overriding it with a meaningless false. The general rule
  for adopters: on restore, apply what the user chose (unconditionally,
  empty included), and stay out of what the composer hard-codes.

- **The legacy bridge is narrower than "when env did not win".** play
  consults its persist keys only for a window that received no config at
  all. A restored record is the authority for its own window even where
  it is empty; falling through to the keys there would resurrect exactly
  what the record says the user cleared.

- **play's "active tab" is what play raised.** The dock's own focus state
  lives on the Rust side and is not readable from Go, so `ComposeLaunch`
  reports the last tab play itself raised (panes menu, a snippet
  delivering into the editor, a launch config's `Tab` tier). A tab the
  user raised by clicking the dock strip is invisible to the record.

- **Intent tracking is a per-frame comparison.** The editor and the Live
  checkbox write through `SendRespVal`, whose change-detection callback
  never fires, so there is no edit event to hang `dirty` on. play
  snapshots the composed fields each frame and takes its baseline on the
  first frame — after Mount has finished seeding — which is what makes a
  launch-seeded window closed untouched read as clean. The one machine
  writer of a composed field (the Live circuit breaker) re-anchors the
  baseline instead of marking intent.

- **chstore tests.** The workingset round-trip tests follow the chstore
  package's existing probe-and-skip convention (skip when no live
  ClickHouse answers) rather than moving to the integration lane, which
  would have split one package's tests across two lanes.

Deferred items are unchanged and untouched.

## Update — 2026-07-30: the factory requirement is enforced at registration

The Update above recorded that SD4's "participation requires factory
registration" was left to the host's existing config-delivery refusal —
a singleton participant worked until its second window, which then failed
to open. That is now refused where it is stated instead:
`Registry.Register` rejects a manifest declaring `Workingset`, naming
`RegisterFactory` in the error. Nothing in the tree was registered that
way, so no app changed behaviour.

Two consequences worth recording:

- **The host's delivery-time refusal stays, and is no longer redundant.**
  A factory whose ctor returns one shared instance is indistinguishable
  from one that allocates, so the registry cannot see that case; the host
  still refuses to deliver a config — a restore included — to an instance
  that already has a window, and still skips the save while another window
  holds it. The windowhost tests cover exactly that shape now (a factory
  handing out a fixed instance) rather than a declared singleton, which
  the registry no longer admits.

- **The registration mode became data.** The mode is not in the manifest
  and cannot be recovered from the ctor, so the registry records which
  entry point each app came through and `Registrations()` reports it;
  `keelson('apps').registration` renders it beside `launch_kind` and
  `workingset`. With the refusal in place that column no longer serves as
  a misdeclaration audit — the combination is unreachable — and answers
  the question it is actually good for: whether two windows of an app are
  independent.

## Update — 2026-07-30: §SD7's `keelson.workingsets` follow-up is built

The introspection provider §SD7 recorded as a follow-up exists:
`keelson('workingsets')`, registered through
`providers.RegisterWorkingsets(reg, facts)` and wired from the host by
`introspecthost.Deps.Facts` — the same store the window host saves through, so
the table is not a second account of the records. Roster row and provider-pattern
notes are in [ADR-0094](./0094-keelson-introspection-tables.md)'s dated update;
what this ADR settled:

- **The table is the stored set, not the trail.** `FactsStoreI` grew one method,
  `ListWorkingsets`, returning the latest non-tombstoned record per (app id,
  name) — definitionally what a restore would find, since it is `LatestWorkingset`
  generalised over every key rather than a second reading of the rows. The trail
  stays a `boxer.facts` query: §SD6 makes history the row sequence, and a
  `workingset_history` table would only be a view over it with a schema to keep
  in step. There are no filter arguments for the same reason — a caller who wants
  the trail is already in SQL.

- **Judging the tombstone is the whole difficulty.** The obvious ClickHouse
  shape — filter the tombstone rows out, then take the newest survivor per key —
  is wrong in a way that is easy to ship: it returns the write *before* the
  delete and so resurrects a deleted record. The predicate has to run on the
  winning row (`HAVING argMax(is_tomb, …) = 0`), and the sort key has to be
  (ts, entity id) rather than ts alone, because two saves can share a timestamp
  and §SD3's last-writer-wins only says the later write wins. Both backends are
  tested on exactly that sequence: write, delete, write.

- **No payload column, generically or per app.** The record is an instance of the
  app's `LaunchKind` DTO (§SD2) — facts-CBOR up to 64 KiB, decodable only by that
  app's codec — so the table carries its size and its kind. A generic
  CBOR-diagnostic column was considered and rejected: it would decode every
  record on every query to produce text no user reads, and it would quietly make
  a diagnostic surface the place where one app's state becomes another's input.

- **Writing through the table stays out.** `keelson('…')` tables are read-only by
  construction, so deletion remains `DeleteWorkingset` behind the named-set UX
  this ADR defers. The table does make that deferral cheaper to design: a picker
  now has a query to enumerate from.

- **The degradation is documented where it is visible.** With ClickHouse down the
  runtime runs on the in-memory facts store, so the table then reports this
  process's own saves only. That is the stance the Consequences already record;
  the play help text says so rather than leaving a reader to infer that an empty
  table means nothing was ever saved. In the same spirit the provider *returns* a
  failed store read instead of rendering zero rows — "no records stored" and "the
  store did not answer" are different claims about restorable state.

Deferred items are unchanged and untouched.

## Update — 2026-07-30: the persist-backend follow-up landed

The *Neutral* consequences record the facts-backed persist backend as "a
follow-up worth landing for referenced content and stored applets, but no
longer a prerequisite for workingsets". It is now built and wired
(`persist.FactsBackend`; see [ADR-0026](./0026-app-runtime-and-capability-subjects.md),
Update 2026-07-30), so `runtime.persist` is durable whenever ClickHouse is
up.

This changes nothing about the design here. Workingsets were deliberately
routed around `StorageI` and onto typed facts, and they stay there —
`WorkingsetRow` is not reimplemented over the persist channel. What the
backend closes is the store-the-ID half of §SD2: a record that references
big content by persist key can now expect that content to still be there on
the next open, which was the one place workingsets depended on a channel
weaker than their own.

The Context's description of the wired backend as in-memory is superseded
for the same reason the *Consequences* degradation note is not: with
ClickHouse down, persist and workingsets both fall back to process
lifetime, together, and the runtime status bar says so.

## Update — 2026-07-30: data-centricity is the rule, not this record's preference

**Two of the rejections above were argued as if they were local taste.**
The *Alternatives* kill O2 because "records stay opaque blobs under key
conventions", and kill egui-side persistence because it is "opaque to the
data plane". Both reasons are properties of the platform, not of
workingsets: nothing about a saved window makes an opaque blob worse than
it would be for any other app state. Read narrowly, they licensed the
reading that this ADR simply preferred typed rows for its own record while
leaving every other route open. They did not, and this Update says so
directly rather than leaving it to be inferred from two bullet points.

**The invariant.** Runtime and app state is stored in the runtime facts
table (`boxer.facts`) and modelled as facts there. No other storage
location is permitted, and storing bytes *in* the table is not the same as
storing data *as* facts — an opaque payload in the blob section
reconstructs, one column down, exactly the flat-`payload` shape
[ADR-0026](./0026-app-runtime-and-capability-subjects.md) §SD6 rejected for
the table as a whole, and forfeits the same properties: typed query,
dictionary compression, membership ACL, subset projection.

**What this prohibits.**

- **Opaque payloads as a destination for new state.** `runtime.persist`
  values are `[]byte` by construction (§SD3), so the channel satisfies the
  location rule and fails the modelling rule. New state is modelled;
  persist is not the route to reach for.
- **Rust-side persistence of egui memory.** `eframe`'s `persistence`
  feature must stay off. It is not an abstract hazard: it enables
  `egui/persistence`, which serializes the `IdTypeMap`, and
  `egui_table::TableState` (0.9.0) derives serde and stores itself with
  `insert_persisted` while carrying `col_widths`. Turning the feature on
  would write column widths to a file under the home directory — and
  because `TableState` overrides the Go-supplied `currentWidth` on every
  frame after first show, that file would be the *authoritative* copy while
  `boxer.facts` became the shadow. The line in
  `rust/imzero2/Cargo.toml` that keeps it off is load-bearing.
- **Per-app files and local config files** for app state, which also
  bypass the capability model.

**What this does not prohibit**, since a rule that cannot be applied is not
a rule:

- **Read-only asset loads** — fonts read at startup are inputs, not state.
- **Outputs the user asked for** — PNG/SVG capture, fs Powerbox document
  export. A file the user requested is a product, not a store.
- **Transport** — the IPC pipes and shared memory the render loop runs on.
- **Ephemeral in-memory interaction state** — egui's `Memory` between
  frames, scroll offsets, caret position, and a table's live drag between
  authoritative applies. The test is twofold: it must not outlive the
  process, and facts must be authoritative at boot. State that fails
  either test is storage and belongs in the table.

**Existing adopters are grandfathered, with a direction rather than a
deadline.** play's `lastSql` and `timelineBandsSql`, the applet store's
documents and its hand-rolled `index`
([ADR-0132](./0132-sqlapplet-sql-defined-applets.md) O4), and
[ADR-0151](./0151-table-column-width-overrides.md)'s `colw` document all
predate this Update and all currently store opaque bytes. They are durable
and they work; nothing here breaks them. What changes is that their shape
is now a known debt to be paid as each is next touched, not a pattern to
copy. ADR-0151 is the nearest case — it is accepted but unimplemented, so
it carries its own Update rather than shipping against a rule it was
written before.

**A rule with no gate is a comment.** The `eframe` prohibition is checked
in `scripts/ci/lint.sh`; the modelling rule is not mechanically checkable
and rests on review, which is the honest description of its strength.

## Update — 2026-07-31: the invariant names a substrate, not a table

**The invariant above over-specified itself, and as written it contradicts
an accepted decision.** It says state "is stored in the runtime facts table
(`boxer.facts`) and modelled as facts there".
[ADR-0105](./0105-keelson-adopts-generated-record-stores.md) §D3a — accepted
2026-07-05, before that Update was written — puts the durable persist
backend on its own generated table and says in as many words that "persist
state thereby leaves the `boxer.facts` substrate". Both cannot be followed.

**The table name was never doing the work.** Every argument the invariant
makes is about modelling: that storing bytes *in* a table is not storing
data *as* facts, and that an opaque payload forfeits typed query, dictionary
compression, membership ACL and subset projection. A generated leeway table
has all four. Read literally, the invariant would forbid the more
data-centric of the two options and permit the less — a blob on
`boxer.facts` passes, a typed row on a generated table fails — which is the
opposite of what it exists to say.

**So the invariant binds the substrate, not the table.** Runtime and app
state lives in the runtime's modelled fact substrate — `boxer.facts`, or a
generated leeway table such as ADR-0105's `runtime.persiststate` — and is
modelled there. Nothing else changes: opaque payloads are still not a
destination for new state, `eframe` persistence is still prohibited and
still gated, per-app files are still out, and the carve-outs for asset
loads, user-requested outputs, transport and ephemeral interaction state
stand as written.

This is a correction of wording, not a relaxation. A second table is
permitted only because it is *more* modelled, and only where an ADR has
decided one; "some other store would be convenient here" remains exactly
what the invariant refuses.

ADR-0105 also records that the tree currently deviates from D3a in the other
direction — a facts-bound persist backend shipped on 2026-07-30 — with the
reasoning and the exit. Workingsets themselves are unaffected either way:
`WorkingsetRow` is append-shaped, has no state view to lose, and stays on
`boxer.facts`.

## Status

Accepted (2026-07-29). Implemented 2026-07-29.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way)
for the edit-policy tiers.

## References

Internal:

- [ADR-0135 — app-launch requests](./0135-app-launch-requests.md) — the
  launch-config DTOs, `kindcheck`, `LaunchRow`, the frozen-arguments axis
  this ADR completes at the ambient end.
- [ADR-0132 — sqlapplet](./0132-sqlapplet-sql-defined-applets.md) — the
  curated point of the axis; the store whose index workaround motivated
  rejecting O2.
- [ADR-0026 — app runtime and capability subjects](./0026-app-runtime-and-capability-subjects.md)
  — lifecycle, `StorageI`, §SD6 state-as-facts, hygiene-not-security.
- [ADR-0134 — ad-hoc datasets](./0134-adhoc-datasets.md) — the ephemeral
  handles whose seeded reads exposed the unconditional-save defect.
- [ADR-0009 — environment variable registry](./0009-environment-variable-registry.md)
  — the env tier the SD5 precedence keeps between the two config tiers.
- [ADR-0094 — keelson introspection tables](./0094-keelson-introspection-tables.md)
  — the recorded `keelson.workingsets` follow-up's home.

External prior art (verified 2026-07-29):

- **Android — Save UI states** (developer.android.com,
  topic/libraries/architecture/saving-states) — the three-tier table
  (ViewModel / saved instance state / persistent storage), user- vs
  system-initiated dismissal, store-IDs-not-content, system-owned save
  points at Activity stop, serialization size cautions.
- **Android — Recents documents** (`documentLaunchMode`,
  `persistableMode="persistAcrossReboots"`, `PersistableBundle`) — named
  document instances with opt-in cross-reboot persistence; the SD3
  named-continuity precedent and the deferred reuse-policy analog.
- **iOS/iPadOS — UIScene state restoration** — per-scene
  `stateRestorationActivity` (`NSUserActivity`): one typed payload for
  deep links, Handoff, and restoration (SD2); state deliberately deleted
  when the user swipes the app away (SD4's gate).
- **Fuchsia — storage capabilities and sessions** (fuchsia.dev:
  concepts/components/v2/capabilities/storage, reference/cml, RFC-0092) —
  `data`/`cache`/`tmp` eviction-in-the-name (SD1's cache exclusion), the
  component ID index decoupling storage identity from topology (the
  alias/window-key split this repo already has), `persistent_storage`
  collections keying continuity on the child's *name* (SD3), sessions
  re-proposing persistent elements (the desktop-resume deferral's shape).
- **Wayland — `xx-session-management-v1`** (wayland.app; KWin and Chromium
  adopting) — compositor-stored state keyed by client-chosen unique
  toplevel names, the `launch/recover/session_restore` reason enum scaling
  restoration (SD5), and XSMP as the failed full-cooperation ancestor the
  narrowed split replaced.
