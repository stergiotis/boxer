---
type: explanation
audience: platform designer weighing how boxer's dataset binding compares with the object-lifecycle machinery of operating systems
status: draft
# reviewed-by: "@<handle>"   # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD  # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

> **Provenance.** Compiled 2026-08-15. Claims about this repository were
> verified against the working tree at the commits that built the
> mechanism ([ADR-0188](../adr/0188-app-instance-effect-tracking.md) M0–M3,
> the same day). Claims about operating systems are general knowledge —
> the API and manual-page names are given so a reader can verify each one,
> but no source or documentation was re-read for this page; treat them as
> pointers of the composition survey's tier (c), and check a detail before
> building on it.

# Dataset following, and what operating systems already do about vanishing objects

"Dataset following" is the short name for what a keelson applet does with a
declared ad-hoc dataset since ADR-0188 §SD3: the applet declares a stable
*alias*; the runtime resolves it at open to a *handle* — an unguessable
identity minted per publish, carrying a revision and a publisher; the
dataset service announces every publish and every retract on the bus; and
the applet's binding follows — it binds without polling when a dataset
appears under the alias, re-runs under Live when its handle is republished,
unbinds and says so when its handle is retracted, and rebinds on the next
publish, whichever producer that is. A retract is two-phase: the dataset
leaves the namespace at once (the catalog and `adhoc.resolve` stop naming
it) and its provider, key and file go after a bounded grace, so a query that
had already resolved the handle completes. All of it crosses windows, and
all of its state is a query (`keelson('adhoc')`, `keelson('subscriptions')`).

None of the ingredients is new. Each is a move an operating system made
decades ago for files, services or connections, and lining them up says
three useful things: where boxer's cut is the standard one, where it is a
deliberate variant with a name, and what the operating systems do that boxer
does not — yet, or on purpose.

## 1. A file has three names — POSIX

The oldest version of the whole design is the POSIX file: a *directory
entry* (the name), an *inode* (the object), and an *open file description*
(a handle a process holds). `unlink(2)` removes the name: from that instant
no `open` finds it, but the object stays while any descriptor holds it and
is freed at the last `close`. Read against boxer: alias / dataset / query in
flight; *leave* is `unlink`, *unload* is the last close. Linux even shows the
in-between state — `/proc/<pid>/fd` renders an unlinked-but-open file as
`… (deleted)`; boxer's equivalent is a handle that `keelson('adhoc')` no
longer lists while `keelson('tables')` still does, for the length of the
grace.

`rename(2)` over an existing name is a new provider under an old alias.
Holders of the old descriptor keep the old object and are told nothing —
which is exactly the stance [ADR-0134](../adr/0134-adhoc-datasets.md) takes
for an open applet: it tracks re-captures through its handle and does not
re-resolve to a newer sibling under the alias. The difference is that boxer
announces the event and chooses to ignore it; POSIX needs a watcher.

Notification is a separate facility in both worlds. Linux has `inotify(7)`
(`IN_DELETE_SELF`, `IN_MOVE_SELF` on the object; `IN_CREATE`, `IN_DELETE` on
its directory) and `fanotify(7)`; BSD and macOS have `kqueue(2)` with
`EVFILT_VNODE` (`NOTE_DELETE`, `NOTE_RENAME`, `NOTE_REVOKE`); macOS adds
FSEvents, which is path-level, coalesced and may degrade to "rescan this
subtree". boxer's `adhoc.event.published` / `adhoc.event.retracted` are the
same add-on, per event, immediate, in-process, and — the part the file
systems never had — carrying the provider's identity and revision.

## 2. Five policies for "someone still holds it"

What happens to the holder when the object is withdrawn is the interesting
design axis, and the operating systems have tried every point on it:

- **Exact, by reference count.** POSIX inodes; Linux `umount -l`
  (`MNT_DETACH`: make the mount unreachable for new accesses now, perform
  the unmount when it stops being busy — leave/unload verbatim); the
  kernel driver model's kobject refcounts. The withdrawal completes when the
  last holder is gone, however long that takes.
- **Bounded, by time.** Linux file leases (`fcntl(2)`, `F_SETLEASE`): when
  another process opens or truncates a leased file the kernel signals the
  lease holder and waits up to `/proc/sys/fs/lease-break-time` (45 s by
  default) before breaking the lease. Kubernetes pod termination: the
  endpoint leaves the Service (no new traffic), `SIGTERM`, a
  `terminationGracePeriodSeconds` (30 s by default), `SIGKILL`. boxer's
  `RetractGrace` — one bus request timeout — is this policy.
- **Cooperative and exact.** macOS `NSFileCoordinator` /
  `NSFilePresenter`: before a coordinated delete or move, every presenter of
  the item receives `accommodatePresentedItemDeletion(completionHandler:)`
  or `presentedItemDidMove(to:)`, and the operation waits for their
  completion handlers. That is the paper's guard — a provider withdraws only
  after every dependent that resolved to it has finished — implemented by
  cooperation, and it is what boxer's recorded deferral (bindings tracked
  through the instance's client, so the host releases them and the service
  can wait for exactly them) would look like.
- **Forced.** BSD `revoke(2)` invalidates every descriptor to a vnode at
  once — for terminals, where the holder must not be able to delay the
  withdrawal. No grace, by design.
- **Refused.** Windows' sharing modes make the *delete* fail with a sharing
  violation while a handle is open: the holder wins, the withdrawer waits.

The axis runs: refuse → wait for cooperation → count references → count
down → revoke. boxer sits at count-down; the paper's calculus sits at
count-references (its `¬relied` guard); ADR-0188 records the step from one
to the other as a deferral with its trigger, and this list is why that step
is a policy choice rather than a defect: leases and Kubernetes made the same
one, for the same reason — the withdrawer does not hold the list of holders.

## 3. Identity, not value

The paper's target view records *which provider* a dependent activated
against, not the value it provided, so a replaced provider is detected even
when it provides an equal value. The operating systems agree, each in its
idiom. macOS bookmarks (`NSURL` bookmark data, and the Alias records before
them) resolve by file ID first and by path second, and report staleness, so
a moved file is found and a replaced one is noticed. Linux exposes the same
identity as device and inode numbers, and as opaque file handles through
`name_to_handle_at(2)` / `open_by_handle_at(2)` — handles independent of the
name, which is what an ad-hoc handle is. D-Bus separates a *well-known bus
name* (`org.freedesktop.NetworkManager` — an alias) from the *unique
connection name* of whoever owns it (`:1.42` — the provider's identity), and
its `NameOwnerChanged` signal announces every loss and gain of ownership,
so a client that remembered the unique name tells a replacement from a
same-value successor. Wayland's `wl_registry` announces each global the
compositor offers with `global` and withdraws it with `global_remove`, and a
client is expected to unbind — the closest wire-level twin of
`published`/`retracted`. boxer's alias / handle / revision / publisher is
the same shape.

## 4. Android

Android's content URIs are aliases (`content://authority/path`) resolved to
a provider by authority; the package behind an authority can be replaced
and the alias survives. `ContentResolver.registerContentObserver` with the
provider's `notifyChange` is the `published` half — observers re-query, and
the loader classes automate the re-run, which is what a Live applet does on
a republish. There is no `retracted` in that API; a provider that has gone
surfaces as a `DeadObjectException` on the next call — the pre-M1 state of
boxer's consumers. What Android does have is Binder death: `linkToDeath`
(`DeathRecipient.binderDied`) and, for bound services,
`ServiceConnection.onServiceDisconnected` / `onBindingDied`, which announce
a vanished provider so the client can rebind — the retracted event by
another route.

Two other Android rules match ADR-0188's closing edge rather than its
dataset half. URI permission grants attached to an `Intent`
(`FLAG_GRANT_READ_URI_PERMISSION`) live as long as the receiving component
and are revoked when it finishes, unless the app takes a persistable
permission — grants that die with the holder, with a sticky opt-in, which is
§SD1's "a runtime grant is an attribute of the instance's client" and
ADR-0026's sticky grants. And `LiveData.observe(owner, …)` removes the
observer when its `LifecycleOwner` is destroyed — the runtime, not the
author, releases the subscription, which is what the host now does for bus
subscriptions at reap.

## 5. Fuchsia

Fuchsia is the operating system whose component model is closest to the
paper's, and it says something about boxer's manifest. A CML manifest
declares `use` (what the component reads — the paper's `d`) and `offer` /
`expose` (what it provides to children and to its parent — `p`), and
component manager routes capabilities along the component tree from those
declarations. The declared-provisions half is exactly what boxer's manifest
lacks and the composition survey's F3 asks for; Fuchsia is the existence
proof that it can be declared statically and routed by a runtime.

For withdrawal, Fuchsia's unit is the channel: when the server end closes,
the client observes `ZX_CHANNEL_PEER_CLOSED` (with an epitaph status where
the protocol sends one) — a per-connection retracted event with a reason —
and reconnects through the routed capability, which is a re-resolution. And
when a component instance is stopped, component manager first asks it to
stop and kills it after a timeout — the bounded-grace shape again — after
which the kernel closes every handle its process held. That last part is
the closing edge made structural at process granularity: nothing an author
forgot to release survives, because a handle cannot outlive its process.
boxer's host-run release at window granularity is the same idea one level
up, where a window is not a process and the host has to do by bookkeeping
what the kernel does by construction. (Fuchsia's per-instance storage
capabilities, keyed by moniker or instance id, are the named realms the
workingset design already borrowed.)

## 6. What the comparison says about boxer's cut

Standard, with long precedent: the three phases; announcement as a
separate facility; identity over value; grants that die with the holder;
lifecycle-owned subscriptions.

Deliberate variants, now with names: the guard is *count-down* (leases,
Kubernetes) rather than *count-references* (POSIX, `umount -l`) or
*cooperative* (`NSFileCoordinator`); and a publish under a bound alias onto
a *different* handle is ignored (POSIX `rename` semantics) although the
event is there to act on (D-Bus tells its clients; the composition survey's
D12 would make this a per-wire policy).

What the operating systems have and boxer does not: an explicit view of the
in-grace state (the `(deleted)` marker) — today it is the difference between
two tables; the exact or cooperative guard (a deferral with its trigger);
and notification for holders that never declared — a stock play window
handed a raw handle in its SQL is an `fd` holder without `inotify`, and
learns of the withdrawal from its next query.

What boxer has that an operating system does not hand its applications:
the relation itself as a query. An OS gives a process `/proc` and `lsof`;
boxer gives `keelson('adhoc')`, `keelson('subscriptions')` and
`keelson('client_caps')`, joinable on the window's key — the P3/P4 stance
applied to a lifecycle mechanism, and the reason the closing edge could be
verified from a shell rather than a debugger
([launch-apps-non-interactively](../howto/launch-apps-non-interactively.md) §6).

## Sources

- boxer: [ADR-0134](../adr/0134-adhoc-datasets.md) (dataset capability;
  Update 2026-08-15), [ADR-0188](../adr/0188-app-instance-effect-tracking.md)
  §SD1–§SD4, [ADR-0026](../adr/0026-app-runtime-and-capability-subjects.md)
  (sticky grants), [spatiotemporal-composability-lessons](./spatiotemporal-composability-lessons.md)
  (the paper's guard and target view),
  [app-composition-survey](../adr-background-work/app-composition-survey.md)
  (F3, D12).
- POSIX / Linux / BSD, tier (c): `unlink(2)`, `rename(2)`, `inotify(7)`,
  `fanotify(7)`, `fcntl(2)` §Leases with `/proc/sys/fs/lease-break-time`,
  `umount(2)` `MNT_DETACH`, `name_to_handle_at(2)`, `kqueue(2)`
  `EVFILT_VNODE`, `revoke(2)`; the D-Bus specification's `NameOwnerChanged`;
  the Wayland `wl_registry` protocol; Kubernetes pod-termination lifecycle.
- macOS, tier (c): `NSFileCoordinator` / `NSFilePresenter`, `NSURL`
  bookmark data, FSEvents.
- Android, tier (c): `ContentResolver.registerContentObserver` /
  `notifyChange`, `IBinder.linkToDeath`, `ServiceConnection`,
  `Intent.FLAG_GRANT_READ_URI_PERMISSION` /
  `ContentResolver.takePersistableUriPermission`, `LiveData.observe`.
- Fuchsia, tier (c): CML `use` / `offer` / `expose`, component manager
  capability routing, `ZX_CHANNEL_PEER_CLOSED` and FIDL epitaphs, component
  stop timeout, storage capabilities.
