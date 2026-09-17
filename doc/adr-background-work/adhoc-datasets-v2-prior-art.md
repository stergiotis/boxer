---
type: explanation
audience: platform designer weighing ADR-0240's mechanisms against what comparable systems do
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Compiled 2026-09-15 to feed
> [ADR-0240](../adr/0240-adhoc-datasets-v2-sealed-store-owned-capability.md)
> (accepted 2026-09-17); nothing here is a decision. Provenance is two-tiered: (a)
> claims about this repository were checked against the working tree on the
> compile date; (c) claims about other systems are general knowledge — the
> product, API or manual-page names are given so a reader can verify each
> one, but no source was re-read for this page. Check a detail before
> building on it. No measurement was taken.

# Ad-hoc datasets v2 — prior art, element by element

ADR-0240 restates the ad-hoc dataset capability in three parts. The question
this page answers is whether each part follows an established pattern, sits
below what comparable systems do, or does something they do not. The short
answer: every *mechanism* has a named precedent in a mature system; where
boxer differs it is mostly in how explicitly the guarantee is stated and
verified; two places were behind current practice, and the ADR takes one of
them up (§9).

Verdicts use three words. **Aligned** — the same mechanism, in a system that
has run it for years. **Ahead** — no comparable system in this class does
it, or does it as explicitly. **Behind** — a mature system does it better,
with the reason boxer does not.

## 1 Ephemeral intermediate data under an in-memory key — aligned

Spark's shuffle and I/O encryption and Trino's spill encryption do what the
sealed store does: a random per-job key held only in process memory,
intermediate files AES-encrypted at rest, unreadable once the process is
gone. At the OS level, Linux ephemeral swap (a `crypttab` entry keyed from
`/dev/urandom`) and systemd's in-memory credential store make the same move.

What those systems do not do is write the threat model down. Boxer's states
that the disk is the adversary and intra-process isolation is not claimed
(ADR-0134 §SD2, kept by ADR-0240), and closes the two RAM→disk bridges the
claim depends on — core dumps by `RLIMIT_CORE=0`, swap as a recorded
box-level caveat. Spark and Trino ship the feature behind a configuration
flag with no such statement. Same mechanism; the explicitness is the better
practice.

## 2 Chunked AEAD with fixed segment geometry — aligned

The `.bxad` format is the STREAM construction (Hoang, Reyhanitabar,
Rogaway, Vizár, CRYPTO 2015) as deployed by age and by Tink's
`StreamingAead`. Random access by chunk arithmetic over a fixed segment size
is how Tink's seekable channel works. ADR-0240 changes the writer's buffering
(linear instead of quadratic) and nothing about the format.

## 3 Unguessable handles as the only name — aligned in shape, enforcement deliberately weaker

A handle that is a random identifier and the sole way to reach an object is
the capability pattern: Ray's `ObjectRef`, Fuchsia handles, Cap'n Proto
capabilities, signed URLs. Those systems *enforce* — the kernel or the object
store refuses a holder it did not hand the reference to. Boxer's stance is
audited-not-enforced under the disk-only threat model, and ADR-0240 §SD2's
ownership check is a convenience on top, not a security boundary. This is a
conscious position below the field's ceiling, recorded with its trigger
(a multi-principal deployment, or the broker leaving the process). It should
never be quoted as isolation.

## 4 Alias → handle → revision → publisher; events as hints, resolve as truth — aligned with the dominant practice

The identity split is D-Bus's well-known name versus unique connection name
with `NameOwnerChanged`, and Wayland's `global` / `global_remove`; the
level-triggered follow with a periodic resync is the Kubernetes informer
model (watch plus relist). The companion page
[dataset following and OS analogues](../explanation/dataset-following-os-analogues.md)
maps these in detail. Lifting the follower into the platform (ADR-0240 §SD6)
is what client-go did for Kubernetes and `LoaderManager` /
`ContentObserver` did for Android: no consumer is expected to write the
reconcile loop itself.

## 5 Instance-owned resources, collected when the owner dies, with an opt-out — aligned, precisely

Erlang ETS tables are owned by a process, deleted when it exits, and the
`heir` option lets a table outlive its owner by naming who inherits it.
ADR-0240 §SD5's `KeepAfterClose` is `heir` with the runtime as the heir.
Kubernetes `ownerReferences` with garbage collection, Ray's owner-based
object lifetime and Android Binder death recipients apply the same rule.
The `runtime.instance.closed` event is the piece boxer lacked; each of those
systems has its equivalent (process exit signal, owner deletion, `binderDied`).

## 6 Unload when no reader is open, under a ceiling — aligned

POSIX keeps an unlinked inode alive until the last descriptor closes; Linux
file leases and Kubernetes pod termination add a bounded wait so a holder
cannot stall the withdrawal forever. The hybrid ADR-0240 §SD4 adopts — exact
reference counting for the holders the withdrawer can see (open `/table`
readers), a countdown for everyone else — is where the mature systems
converge.

## 7 Label-driven placement with proven locality — ahead for this class

ADR-0145's wall routes a statement that names a sealed handle only to an
engine whose locality was proven by probe, refusing above every resolver and
again at the engine. Label-driven routing with proof rather than inference
is information-flow-control territory — Flume, Jif, cloud data-residency
policies. Desktop analytics tooling generally has nothing like it: DuckDB
and clickhouse-local read a plaintext file from wherever they are pointed.

## 8 Runtime state as SQL tables joinable with the data — ahead

osquery and the `system.*` tables of Postgres and ClickHouse expose runtime
state as tables. Exposing a live GUI runtime's *capability graph* —
datasets, subscriptions, client caps, windows — as `keelson('…')` tables
next to the user's own data is unusual, and it is what let the September
2026 review that produced ADR-0240 be run from inside the product.

## 9 Where v2 was behind, and what the ADR does about it

**Data movement — behind, engine-constrained.** A publish is bytes over the
bus, then a file, then loopback HTTP into a separate ClickHouse process.
Ray/Plasma, Arrow Flight and the Arrow C Data Interface moved to
shared-memory or zero-copy handoff years ago; DuckDB ingests Arrow
in-process through the C interface. ADR-0240 cuts the copies per publish
from about six to about three but keeps the shape, for two reasons it
names: a reference cannot ride a wire under NATS parity, and ClickHouse has
no in-process Arrow ingest, so the loopback `url()` hop is a property of the
engine rather than of the design. The native `keelson()` table function
stays the recorded direction, and an in-process reference handoff behind
the bus (a token in the request, the service pulls from a reader the
publisher registered — mediation and audit intact) is recorded as a
deferral.

**Named files where unnamed inodes would do — behind, taken up.** The
first draft of ADR-0240 §SD1 proposed a per-process directory with a lock
file and a start-up sweep of dead siblings' directories — the Chrome
`SingletonLock` / Postgres `postmaster.pid` pattern, correct but older than
what Linux offers. `O_TMPFILE` (`open(2)`) creates an inode with no
directory entry that the kernel frees on last close, crash included: no
name on disk ever, nothing to sweep, no lock file, and after a crash not
even ciphertext residue. `/table` reads in process, so the file never needs
a name. Every host this repository targets is Linux (the desktop, the
headless carrier, the gokrazy appliance), tally already relies on `memfd`
for the same reason, and both filesystems the store lands on — ext4 and
tmpfs — support it. ADR-0240 §SD1 was revised the same day to make
`O_TMPFILE` the only shape; the residual costs (a filesystem without
support refuses the store at start; `du` cannot see the bytes) are in its
Consequences.

## 10 Net

Aligned on every mechanism, with named precedents; ahead on the
explicitness of the guarantee, the placement wall and the queryable runtime;
behind on data movement, with the direction recorded and the constraint
named. The comparison found one simplification the first draft had missed
(§9), which is the reason to run it before acceptance rather than after.
