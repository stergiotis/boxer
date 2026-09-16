---
type: adr
status: proposed
date: 2026-09-15
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0240: Ad-hoc datasets v2 — a sealed store, an owned capability, a platform follower

## Context

[ADR-0134](./0134-adhoc-datasets.md) gave a running app a way to hand
ephemeral tabular data to SQL: an AEAD-encrypted Arrow file whose key lives
only in process memory, an unguessable handle that is a valid `keelson('…')`
table name, a stable alias, and an audited capability service. It has
proven its value: six apps publish through it, five committed applet
books declare aliases, and [ADR-0145](./0145-sealed-app-data.md) built the
placement wall on top of it. It is in production shape in the sense that it
works; it is not in first-class shape, and a review of the tree in
September 2026 found the gap is structural rather than cosmetic.

**The accepted text no longer describes the mechanism.** ADR-0134's body
specifies a named-pipe decrypt route, a signal-based freshness model, a flat
type set and an embedder-only binding; eight dated Updates and ADR-0145
retired or replaced each. A reader of the body alone gets the store, the
keys and the handles right and the transport, the freshness, the type set
and the consumer binding wrong. That is the documentation standard's
supersession condition.

**The service has correctness defects under concurrency.** All verified in
code at the time of writing:

- The store directory is shared and swept whole at start and close. A
  second host process as the same user — a second carousel, a headless
  launch, a test binary — deletes the first's live ciphertext and tally's
  staged recordings. Nothing leaks; data disappears early.
- A republish racing a retract of the same handle re-uses a record the
  retract has already removed: quota accounting drifts, a fresh key is
  registered onto a dataset in grace, and the publisher is told success for
  data the grace timer then deletes.
- Two concurrent republishes of one handle write the same temporary file
  without exclusivity and the same revision; the ETag then names two
  different byte streams.
- The `/table` handler assembles the read reference from four separately
  locked accessors, so a republish between them yields an old ETag over new
  bytes — the splice `If-Range` exists to prevent.
- A publish that lands after `Close` leaks its key, provider and file past
  shutdown.
- A publish holds about six full copies of the dataset, and the AEAD writer
  moves the unwritten remainder once per chunk, so one large publish moves
  bytes quadratically in the dataset size.
- The retract grace is the bus request timeout, but a read is a sequence of
  ranged HTTP requests the bus timeout does not bound.
- Ownership is absent: any app holding the publish or retract capability
  can republish onto or retract any handle it learns from the catalog.

**One package holds seven concerns.** The AEAD file format, the key-custody
vocabulary (with the key store itself parked in the chlocal broker for a
reason that left with the pipe route), the capability service, the bus
wire, the catalog provider, the Arrow-to-ClickHouse structure mapping and
process hardening. Two structs hold the same five fields under two
mutexes. tally uses the package as a bare sealed-file primitive and relies
on the service's sweep as a feature. The service is gated at boot on the
chlocal service, which it no longer needs, and is skipped silently when
that service is off.

**Consumers re-implement what the platform should offer.** Five publishers
carry the same handle / single-flight / generation bookkeeping; four copy
the same Arrow-IPC encoder; four splice raw handles into the SQL of a play
window they launch, because a launch cannot carry an alias binding; the
only alias follower lives inside sqlapplet, generic apart from a
three-method target; the embedder demo hand-delivers revision notifies
instead of following events; three apps block the render thread with bus
round-trips in `Mount` or `Unmount`. The tree holds two contradictory
retract policies — never, and on the publisher's `Unmount` while a launched
reader may still be open — with no rule for choosing.

**Some surface is dead.** `adhoc.grant` has no caller; the publisher field
on the wire is ignored; the stream reader, the reference's structure and
revision fields, the event's publisher and several resolve fields are read
by nothing. The sibling brokers ship their wire DTOs as generated codecs
over vdd memberships ([ADR-0042](./0042-keelson-leeway-codec-soa-generator.md));
this one hand-rolls structs on the codec default. There is no package
properties row and no status-bar segment.

None of this touches the invariants ADR-0134 was written for. They are
listed under the Decision because v2 keeps every one of them.

## Design space (QOC)

**Question.** How does the ad-hoc dataset capability become a first-class
keelson service — correct under concurrency, one concern per package,
reusable by its consumers — while keeping the guarantee it exists for?

**Options.**

- **O1 — Patch in place.** Fix the races and the sweep, delete the dead
  surface, add Updates to ADR-0134.
- **O2 — Restructure into a sealed store, an owned capability and a
  platform follower** (chosen). Three packages with one concern each; the
  capability records and enforces ownership; withdrawal is exact for open
  readers; the consumer machinery moves into the platform.
- **O3 — Fold into the introspection registry.** Sealed datasets become a
  kind of registry provider with no capability service: any app registers
  a file directly, the registry sweeps.
- **O4 — Replace with ClickHouse-side temporary state.** Publish becomes an
  insert into a session-scoped table on the default endpoint.

**Criteria.**

- **C1 — Correctness under concurrency and multi-process use**: the races
  above and the shared sweep.
- **C2 — Orthogonality**: one concern per package; the sealed primitive
  usable without the service (tally today).
- **C3 — Invariants preserved**: ephemerality by cryptography, disk-only
  threat model, keys never on a wire, handles unguessable, explicit
  structure at read, the ADR-0145 wall.
- **C4 — Consumer cost**: what the six publishers, five books and the
  applet host must change.
- **C5 — Parity with sibling services**: generated codecs, boot wiring,
  status, audit.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 patch | O2 restructure | O3 registry-only | O4 ClickHouse state |
|----|----------|----------------|------------------|---------------------|
| C1 | +  | ++ | +  | ++ |
| C2 | −− | ++ | +  | +  |
| C3 | ++ | ++ | −  (no mediation, no audit moment) | −− (durable, keys irrelevant) |
| C4 | ++ | −  | −  | −− |
| C5 | −  | ++ | −  | −  |

## Decision

We supersede ADR-0134 as a whole and re-state the feature in three parts
with one concern each: a **sealed store** that owns the file format, the
keys and a process-scoped directory; an **ad-hoc dataset capability** that
owns handles, aliases, quotas, ownership and lifecycle over that store; and
a **platform follower and publisher** that every consumer uses instead of
its own copy. ADR-0145 (placement) and ADR-0188 §SD3 (withdrawal with
events) stand unchanged and are cited where they apply.

**Kept without change, from ADR-0134 and ADR-0145.** Ephemerality by
cryptography, not by cleanup; the disk is the threat model, never
intra-process isolation; keys exist only in process memory and never ride
a wire; a handle is unguessable and a valid `keelson` identifier, and is
the only way a query names a dataset; committed text names an alias,
never a handle; the structure is explicit at read and the publish gate
bounds the type set so the mapping is total; a mid-stream authentication
or truncation failure fails the request; quotas at publish; not
`StorageI`; the STREAM-shaped chunked AEAD format with fixed chunk
geometry; the placement wall keyed on the registry's sealed marker; events
are hints and request/reply is truth.

### Subsidiary design decisions

- **SD1 — `sealed`: the format and the store, one package, no service.**
  The chunked AEAD format moves to a new package `sealed`, a sibling of
  `adhocdata` under the keelson runtime, with a writer that holds back at
  most one chunk and seals the rest of each write in place — linear in the
  input — and the seekable reader as it is. The package also owns the
  **store**, and a sealed file **never has a name**: it is created with
  `O_TMPFILE|O_EXCL` in the base directory (the existing env var), so the
  inode has no directory entry, cannot be linked in later, and is freed by
  the kernel on last close — crash included. Every reader takes the
  descriptor (`io.ReaderAt`), which is what the seekable reader already
  consumes; `/table` reads in process and needs no path. A republish is a
  new unnamed file swapped into the record under its lock; the old one is
  closed when its readers are gone (SD4). There is nothing to sweep, no
  lock file, no per-process directory, no temporary to rename, and after a
  crash not even ciphertext residue. This is Linux-only by construction,
  which every host this repository targets is (desktop, headless carrier,
  gokrazy); tally already depends on `memfd` for the same reason. Both
  filesystems the store lands on — ext4 where `/perm` is formatted, tmpfs
  on the appliance's file image and on a tmpfs `/tmp` — support it; a base
  directory on a filesystem that does not refuses the store at start with
  a named error rather than falling back to named files. **Custody is
  ownership, not lookup.** The key is a private field of the file object
  beside its descriptor; the object exposes a writer at creation, `Open()`
  for a seekable reader afterwards, its open-reader count, and `Retire()`.
  Whoever holds the object can read it; nobody can resolve a key by name,
  because no such operation exists. This replaces ADR-0134 §SD2's
  registrar / decrypt-executor split, which denied the policy owner a
  lookup it never needed — the policy owner wrote the plaintext — and it
  removes the key store, the reference type and the decryptor interface
  that split required. The reader authenticates the final chunk at open,
  so a truncation the chunk arithmetic cannot see (a prefix ending on a
  full chunk; a prefix that reads as an empty stream) is refused before any
  byte is served. The store is stateless — a base directory and a
  constructor — so no host component owns it; its two clients are the
  dataset capability and tally's recording stage, and a third needs no new
  plumbing. `RLIMIT_CORE=0` is process hardening and moves to hostboot.

- **SD2 — The capability owns one record, and the record has an owner.**
  `adhocdata` keeps its subjects, handle prefix and catalog name (SD9). The
  registry entry becomes the single record — one struct, one mutex, one
  `Snapshot()` that returns a consistent reference — and the private
  `dataset` mirror goes. Publish decodes the incoming Arrow stream batch by
  batch straight into the sealed writer; the request's payload length is
  checked against the per-dataset quota before decoding. The service
  refuses requests after `Close`. The publisher is the request envelope's
  sender and sender instance, recorded on the record; a **republish or
  retract by any other identity than the recorded publisher or the runtime
  itself is refused** with a typed error. This is hygiene, not security —
  the threat model is unchanged — but it turns "any app that can read the
  catalog can delete anyone's dataset" into an accident that cannot happen.
  `adhoc.grant` is retired; the resolve step is the audited hand-out
  moment and has been since the 2026-08-01 update. The service subscribes
  to its four request subjects, not to its own events.

- **SD3 — `/table` opens the record.** The registry's sealed marker
  gains `Open()` returning a standard read-seek-closer, so the table
  source serves a dataset by asking its own registry entry — no decryptor
  is wired in from outside, and the chlocal broker loses the key store and
  the decrypt file and goes back to being a query engine. The dataset service no longer requires the chlocal service to be
  on — a read still does, through `/query`, which is the real dependency
  and is where a missing worker already fails visibly. A service that is
  off or failed says so in the log like its siblings, and the status bar
  gains an `adhoc` segment.

- **SD4 — Withdrawal is exact for readers, bounded for everyone else.**
  ADR-0188 §SD3's leave → notify → unload stands, and unload splits in
  two. The provider stays registered for one grace after leave, so a query
  that resolved the handle but has not fetched yet still finds it — the
  case a countdown is right for, because the withdrawer cannot see that
  query. Then the sealed file retires when its last open reader closes, or
  at a second grace, whichever comes first — the case the withdrawer *can*
  see, since every open and close passes through the file. A republish
  retires the previous revision's file the same way. On the axis the
  OS-analogues page draws (refuse → cooperate → count references → count
  down → revoke) this moves the reader case from count-down to
  count-references and leaves the not-yet-fetched case where it was.

- **SD5 — An instance owns its datasets; the runtime retracts them.**
  The bus envelope already carries the sender's instance key; the
  ADR-0188 deferral that waited for it has its trigger. When a bus client
  closes — the window host closes it after `Unmount` — the bus publishes
  `runtime.instance.closed` carrying app id and instance key, the first
  member of the lifecycle family ADR-0026 §SD3 reserved. The dataset
  service subscribes and retracts every dataset the instance published,
  unless the publish set `KeepAfterClose`, in which case the dataset lives
  until process exit — what imzrt's Profiles tab wants. Publishers stop
  retracting in `Unmount`, which also removes the blocking bus round-trip
  from the mount thread; a publisher that wants to retract earlier still
  can. One rule replaces two contradictory ones: **a dataset lives as long
  as the window that published it, unless the publisher says otherwise.**

- **SD6 — A follower and a publisher in the platform.** The alias-following
  state machine leaves sqlapplet and becomes `adhocdata.Follower`: events as
  hints, resolve as truth, replay in order, periodic reconcile, a pending
  set the host renders as it likes, over a target interface of bind /
  unbind / revision. sqlapplet keeps only its notice text and its re-run
  glue; an embedded applet runs the same follower over the embedder's
  client instead of receiving hand-delivered revision notifies.
  `adhocdata.Publisher` holds alias → handle, single flight and a
  generation counter, exposes publish and retract, and `EncodeRecords`
  writes an Arrow record batch to an IPC stream once for everyone.

- **SD7 — A launched window binds by alias.** `PlayLaunch`
  ([ADR-0135](./0135-app-launch-requests.md) §SD7) gains a `Datasets` list;
  play runs the follower for them at mount. The four publishers that
  today splice a raw handle into seeded SQL name the alias instead, and
  the window they open follows republish and retract like an applet. The
  client-side alias rewrite covers bare table names as well as
  `keelson('…')` calls, matching what the ADR-0145 wall already inspects,
  so an alias never reaches the wall under either spelling.

- **SD8 — The wire is generated.** The four request/reply pairs and the
  event become vdd memberships with generated codecs under
  `runtime/codec/`, as every sibling broker's DTOs are; the publish
  request keeps the Arrow stream as a blob. The publisher field leaves the
  request; the sender is authoritative. The package gains its properties
  row.

- **SD9 — Names, closed.** ADR-0134 §SD9 left them open; they shipped and
  stay: concept *ad-hoc dataset*; capability subjects `adhoc.publish`,
  `adhoc.retract`, `adhoc.resolve`, `adhoc.event.published`,
  `adhoc.event.retracted`; handle `adhoc_` + sixteen hex; catalog
  `keelson('adhoc')`; frontmatter `datasets:` / `datasets_hint:`; env
  `BOXER_ADHOC_DIR`; service identity `runtime.adhoc`. The format and store
  are *sealed*, the word ADR-0145 introduced; `.bxad` stays the file
  suffix for every sealed file, since the store's per-owner directories
  make the shared suffix harmless.

### Milestones

- **M1 — `sealed`:** format, store, key store, tally moved over, writer
  linear, hardening in hostboot.
- **M2 — the capability over the store:** one record, ownership,
  streaming publish, payload cap, closed flag, grant retired, boot
  decoupled, status segment. With custody by ownership M3 is the same
  change — the record's `Open()` is what `/table` needs and the unload is
  the file's retirement — so the two land together.
- **M3 — `/table` opens the record; reader-counted unload** (with M2).
- **M4 — lifecycle event and runtime retract;** `KeepAfterClose`;
  publishers stop retracting in `Unmount`.
- **M5 — follower and publisher in the platform;** sqlapplet, the
  embedder demo and the five publishers adopt them; the fixture lab reuses
  its handles.
- **M6 — launch by alias;** bare-name rewrite; generated wire; how-to and
  the `AGENTS.md` row; stale comments and the FIFO-driven test removed.

### Deferred

- **In-process reference handoff for publish.** A co-resident publisher
  could hand the service a reader rather than bytes — a token in the bus
  request, the service pulling from a reader registry — keeping mediation
  and audit while removing the remaining copies. Trigger: a publisher whose
  datasets approach the per-dataset quota routinely, or the native
  `keelson()` table function landing and making the loopback hop the only
  copy left.
- **Engine-level grant tokens.** Unchanged from ADR-0134 §SD2; trigger: the
  broker leaving the process, or a multi-principal deployment.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| package `sealed` (new, keelson runtime) | added: `File` (`Create`, `Writer`, `Open`, `Retire`, `Close`), `Writer`, `Reader`, `BaseDir` (the env var moves here), `ErrUnsupported` | tally's recording stage; `adhocdata` |
| `adhocdata` exported API | reshaped: `Config.Keys` removed; `Publish` streams; `PublishInput.Publisher` becomes `By Identity` and `KeepAfterClose` is added; `Retract` takes the caller's `Identity`; `Grant`, `GrantResult` removed; `ErrNotOwner`, `ErrClosed`, `LiveCount` added; `NewWriter`/`NewReader`/`NewSeekableReader`/`KeySize`/`ResolveStoreDir`/`DisableCoreDumps`/`StoreDir`/`Ref`/`PlaintextI`/`DecryptorI`/`KeyRegistrarI` removed | hostboot wiring; tally |
| `adhocdata.Follower`, `adhocdata.Publisher`, `adhocdata.EncodeRecords` | added | sqlapplet's binder, the embedder demo, the five publishers |
| Capability subjects | removed: `adhoc.grant`; added: `runtime.instance.closed` (`app.SubjectInstanceClosed`, payload `app.InstanceClosed`, published by the bus in the closed client's name) | manifests that carried `Pub adhoc.grant` (none in tree); the dataset service subscribes; the three publishers that retracted in `Unmount` drop `Pub adhoc.retract` |
| `buscodec` wire for `adhoc.*` | reshaped: generated codecs over new vdd memberships; publisher field removed from the publish request | `keelson/vdd`, `runtime/codec/adhoc*`, both client helpers and handlers |
| `chlocalbroker` | removed: `KeyStore`, `OpenDatasetPlaintext`, the `adhocdata` import | `introspecthost` wiring |
| `introspecthttp`, `introspecthost` | removed: the `Decryptor` field on both; `/table` calls the entry's `Open()` | — |
| `introspect.EncryptedDatasetI` | reshaped: `Path()` removed, `Open() (io.ReadSeekCloser, error)` added; `EncryptedEntry` removed — `adhocdata`'s record is the provider | `keelsonsql.URLPass`, the ADR-0145 predicate (unchanged in shape), `introspectengine`'s refusal |
| `hostboot.Services` | reshaped: `AdhocData` no longer implies `ChLocal` | carousel; status snapshot gains `AdhocActive` |
| `inprocbus.Client.Close` | reshaped: publishes `runtime.instance.closed` | ADR-0188 §SD1 order unchanged |
| `launchcfg.PlayLaunch` | added: `Datasets` | generated `launchcfg.out.go`; the four launching publishers |
| `keelsonsql.RewriteAliases` | reshaped: bare names rewritten too | play's residual path |
| On-disk layout under `BOXER_ADHOC_DIR` | removed: no named files; the directory is only where unnamed inodes are allocated | nothing durable depends on it; v1's loose `.bxad` files are keyless ciphertext the how-to says to delete once |
| `keelson('adhoc')` | reshaped: gains `publisher_instance`, `keep_after_close`, `open_readers` | none |
| a how-to page for ad-hoc datasets under [doc/howto/](../howto/), and its [AGENTS.md](../../AGENTS.md) row | added | — |

## Alternatives

- **Patch in place (O1).** Fixes the races but leaves the key store in the
  wrong package, the follower in one app, the ownership gap, and an ADR
  whose body describes a different mechanism; the Updates chain would grow
  by another entry that contradicts the body.
- **Keeping the registrar / decrypt-executor split of ADR-0134 §SD2.**
  The first draft kept it as two interfaces on one key store. Killed once
  the store became an object: the split guarded against a lookup by the
  party that already held the plaintext, and its cost was a key store, a
  reference type, a decryptor interface and a wiring field in two hosts.
  Ownership of the file object gives the same custody with none of them.
- **Zero-copy publish by handing the publisher a key.** The publisher
  would seal the file itself and send only a reference over the bus. Killed
  because the key would ride the bus — a wire under NATS parity — which
  ADR-0134 forbids for a reason that has not changed.
- **Named files in a per-process directory with a lock file and a sweep.**
  The first draft of SD1, and the Chrome `SingletonLock` / Postgres
  `postmaster.pid` pattern. Killed because `O_TMPFILE` gives the same
  outcome with no moving parts — no sweep, no lock, no rename, no
  per-process directory — and a strictly stronger crash story (no residue
  at all). Its one cost, Linux-only, is not a cost on any host this
  repository targets.
- **A single grace long enough for any read.** Killed because no constant
  is: a large dataset read through ranged requests can outlast any fixed
  grace, and a short one is what large profiles already hit. Counting open
  readers is cheap and the handler already sees every open and close.
- **Lifetime by expiry, as lading does (ADR-0222 §SD5).** Killed because
  the ephemerality guarantee rests on the key dying with the process; a
  retention class would make it rest on a clock.
- **Ownership audited but not enforced.** Rejected: enforcement costs one
  comparison against a field the record already carries, and the case it
  prevents — a reader retracting a producer's dataset — is an accident
  with no legitimate witness. Engine-level grant tokens remain deferred
  with ADR-0134's trigger (a broker out of process, or a multi-principal
  deployment).
- **Renaming the capability to `dataset.*`.** Rejected: five manifests,
  five books and the ADR-0145 vocabulary would churn for a name no reader
  has found wrong; the concept stays *ad-hoc dataset* and the primitive
  *sealed*.
- **Fold into the registry (O3).** Killed for C3: the registry has no
  audited request moment and no notion of a publisher; datasets would
  become durable public names by another route.

## Consequences

### Positive

- The defects listed in Context have no code path: an unnamed file cannot
  be swept by a sibling or collide with a concurrent republish, a single
  record cannot be stale, a closed service cannot commit, and a crash leaves
  no ciphertext behind.
- A publish costs the decoded batches and the ciphertext, and the writer is
  linear; large datasets stop being the case the tests never ran.
- One lifetime rule for every publisher, executed by the runtime, and no
  blocking bus call in `Mount` or `Unmount`.
- A second sealed-store client (tally today, whatever comes next) needs no
  service and cannot be broken by a store rename.
- The five publishers and the two followers become one `Publisher` and one
  `Follower`; a launched play window follows its data like an applet does.

### Negative

- Six apps, one host, two brokers and the codec vocabulary change in one
  pass; the milestones bound the blast radius per commit but not the total.
- Ownership by instance means a dataset published from one window cannot
  be republished from a sibling window of the same app without
  `KeepAfterClose` and the runtime identity; the fixture and projection
  publishers in play are per-window today, so nothing in tree hits this,
  but it is a rule that did not exist.
- A lifecycle event on the bus is a new surface that other brokers will
  want (fsbroker's handles are the recorded second candidate); this ADR
  adds the event and one subscriber, not the policy for the others.
- Reader counting makes `/table` stateful per handle; a handler that
  panics past the header still decrements, but the ceiling is what makes
  the design safe rather than the accounting.

### Neutral

- Sealed bytes are invisible to `ls` and `du`; the catalog's `bytes` column
  and the quotas are the only accounting. A base directory on a filesystem
  without `O_TMPFILE` support (some FUSE and network filesystems) is refused
  at start; the operator points `BOXER_ADHOC_DIR` at ext4 or tmpfs.
- The store is Linux-only. So is the process it serves; no non-Linux host
  exists in the tree to lose.
- The threat model and the hygiene-not-security stance are unchanged;
  enforcement of ownership is a convenience under that stance.
- The transport (loopback `/table` via `url()` with explicit structure)
  is unchanged; the native `keelson()` table function stays the recorded
  direction with ADR-0145's wall built to survive it.
- Alias-latest resolution, per-open binding and the rename-semantics of a
  publish under a bound alias onto a different handle are unchanged.

## Migration — Tier 1

- **Breaks.** `adhocdata.NewWriter`, `NewReader`, `NewSeekableReader`,
  `KeySize`, `ResolveStoreDir`, `StoreDir`, `DisableCoreDumps` (moved to
  `sealed` / hostboot); `adhocdata.Config.Keys` and `.Dir`; `Ref`,
  `PlaintextI`, `DecryptorI`, `KeyRegistrarI`; `Grant`, `GrantResult`,
  `SubjectGrant`; `PublishInput.Publisher` on the wire;
  `chlocalbroker.KeyStore`, `Service.KeyStore()`, `OpenDatasetPlaintext`;
  `introspecthttp.Config.Decryptor`, `introspecthost.Deps.Decryptor`;
  `introspect.EncryptedEntry`, `EncryptedDatasetI.Path()`;
  sqlapplet's unexported binder types; the `adhoc.*` wire bytes (generated
  codec replaces hand-rolled CBOR — in-process only, no persisted form).
- **Path.** Land M1 with tally moved in the same commit; M2 with hostboot
  in the same commit; M4 removes the three `Unmount` retracts and sets
  `KeepAfterClose` on imzrt in the same commit as the event; M5 replaces
  each publisher's bookkeeping one app per commit; M6 last.
- **Regeneration.** `keelson/vdd` memberships and `runtime/codec/adhoc*`
  via the ADR-0042 generator; `launchcfg.out.go` via its golden test; the
  package-properties table via the ADR-0080 harvest; `doc/env-vars.md`
  unchanged (no new variable).
- **Old shape.** Removed outright at each milestone; nothing persisted
  depends on the old wire or layout. v2 never creates a named file and does
  not sweep, so loose `.bxad` files a v1 crash left under the base
  directory stay until deleted by hand; they are ciphertext without a key,
  and the how-to says to remove the directory once.

## Verification plan — Tier 1

- **Lane.** Default `go test`: republish racing retract (quota total
  invariant, no key registered for a leaving handle); two concurrent
  republishes (one ciphertext, one revision); publish after close refused;
  ownership refusal; unload waits for an open reader and fires at the
  ceiling; the base directory holds no entry while a dataset is live and
  after a simulated crash (the descriptor closed without unload); a base
  directory without `O_TMPFILE` support refuses the store with the named
  error; no proper prefix of a sealed stream opens through the seekable
  reader (the stream reader's truncation property, carried over); the
  follower's existing seven cases, moved; a writer benchmark whose
  time is linear in input; the windowhost interleaving property test
  extended with instance close ⇒ retract unless kept.
- **Lane.** `//go:build integration` (clickhouse): the existing publish →
  query → republish e2e over `/query`; a ranged continuation across a
  republish fails `If-Range` rather than splicing; a decrypt failure
  mid-stream aborts the connection; the pprof partial-read pin.
- **Lane.** Screenshot tour / live check: the status segment; a launched
  play window bound by alias re-runs on republish and shows the notice on
  retract.
- **What would fail.** Any of the above red; `capslock` if a publisher
  manifest still carries a retired cap; the codec golden if the wire
  drifts.
- **Gap.** Kernel reclamation of an unnamed inode after a real process
  kill is not tested — it is the kernel's contract, not ours; the test
  asserts the absence of a directory entry, which is what makes the
  contract apply.

## Status

Proposed — awaiting review by the code owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way)
for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

On acceptance, ADR-0134 flips to `superseded` with a pointer here; ADR-0145
and ADR-0188 gain a dated Update naming which of their statements this ADR
carries forward.

<!--
## Updates
-->

## References

Internal:

- [ADR-0134 — ad-hoc datasets](./0134-adhoc-datasets.md) — superseded by
  this ADR; its invariants are restated under Decision.
- [ADR-0145 — sealed app data](./0145-sealed-app-data.md) — the placement
  wall; stands.
- [ADR-0188 — app instance effect tracking](./0188-app-instance-effect-tracking.md)
  — §SD1 (the client as accumulator), §SD3 (two-phase withdrawal with
  events); the exact-guard and lifecycle-subject deferrals this ADR takes
  up.
- [ADR-0026 — app runtime and capability subjects](./0026-app-runtime-and-capability-subjects.md)
  — §SD3 reserved lifecycle subjects; hygiene-not-security.
- [ADR-0042 — keelson leeway codec generator](./0042-keelson-leeway-codec-soa-generator.md)
  — the generated-codec pattern SD8 adopts.
- [ADR-0094 — keelson introspection tables](./0094-keelson-introspection-tables.md)
  — the registry, `/table` and `/query`.
- [ADR-0135 — app launch requests](./0135-app-launch-requests.md) — §SD7,
  the `PlayLaunch` config SD7 extends.
- [ADR-0222 — tally composition surface](./0222-tally-composition-surface.md)
  — §SD5, the tree-shaped analogue whose expiry model is not adopted.
- [Dataset following and OS analogues](../explanation/dataset-following-os-analogues.md)
  — the withdrawal axis SD4 moves along.
- [Ad-hoc datasets v2 — prior art](../adr-background-work/adhoc-datasets-v2-prior-art.md)
  — each mechanism against comparable systems; the comparison that turned
  SD1 to `O_TMPFILE`.

External:

- Hoang, Reyhanitabar, Rogaway, Vizár — *Online Authenticated-Encryption
  and its Nonce-Reuse Misuse-Resistance* (CRYPTO 2015) — the STREAM
  construction the sealed format keeps.
