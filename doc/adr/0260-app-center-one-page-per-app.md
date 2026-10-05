---
type: adr
status: proposed
date: 2026-09-25
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0260: The app center — one page per app over the tables that name it

## Context

The runtime records a good deal about each app, and each record has its own
reader. Every one of them keys the app the same way, as `app.AppIdT` (the
app's import path), so nothing stops them from being read side by side. The
readers are these:

- **When it ran.** The ADR-0191 `app-lifecycle` rows sit in `boxer.facts`.
  The launcher folds them into a ranking (ADR-0214 §SD8), and
  `keelson('runtime_events')` shows them for the current process only.
- **What it logged.** The `log` kind in `boxer.facts` has a Go reader
  (`RecentLogs`) and no table. `logviewer` shows only the in-memory tail.
- **What it may do and what it did.** Declared caps are in
  `keelson('apps')`, held caps are in `keelson('client_caps')`, and audited
  requests are in `boxer.facts`. `apps/capinspector` is organised by
  capability, not by app.
- **What it keeps.** `keelson('app_state')` holds this, shown by the
  app-state manager (ADR-0185).
- **How much of it ran.** `keelson('coverage_pkgs')` has it (ADR-0169).
  The table is per package and process-wide, and has no app column.
- **Which decisions name it.** `keelson('coderef')` has the citations
  (ADR-0122), per package directory.
- **What else it did.** `llm_calls` (ADR-0254), watchbill jobs by owner
  (ADR-0236), published datasets (`adhoc`, ADR-0240) and live tasks
  (ADR-0188) each carry the app id.

Answering "what has this app done" therefore takes five windows and some
SQL. The launcher already has an app detail pane, but it is a recall
surface: it opens on a key, declares no capabilities, and ADR-0214 §SD10
keeps its detail pane to what the manifest says.

Two properties of the read path constrain the design. A `keelson.query`
statement may name only the table its grant covers (ADR-0253 §SD3), so a
join has to happen in the reader. And an introspection provider sees no
predicate (the `Projection` is columns only), so a table over the whole
fact trail has to be bounded by construction, not by the caller's `WHERE`.

## Decision

We will add an **app center**: a window that lists the registered apps and
shows one page per app, assembled from the tables above filtered by app id.
The launcher's detail pane gains an **Inspect** action that opens the
window on that app.

### SD1 — A window beside the launcher, not a larger launcher

The app center is its own app in `public/keelson/runtime/appcenter`, next to
the launcher and the help host. It lives in the runtime, not under `apps/`,
because the host's boot wires the launcher's Inspect action to it, the same
way it wires Help to the help host.

It takes a launch config (ADR-0135) that names the app to show.
**Inspect** opens it with that config. Each click opens a new window, since
a factory app takes its config at Mount. Raising an open app center and
retargeting it would need a verb it does not have; that is deferred.

The launcher keeps its budget. It still declares no capabilities, and a key
press on it still costs one registry read.

### SD2 — The app id is the key, and the list is the registry

The left pane lists `keelson('apps')`, so it covers every registration:
windowed apps, headless apps and minted SQL applets. Ids that appear in a
data table but name no registration are not listed (a retired app, or a
service's own client such as `runtime.coverage`). Listing them is deferred.

### SD3 — One read per lens, composed in Go

Each lens is one `keelson.query` statement over one table, filtered by app
id where the table has an app column. The manifest declares one sticky grant
per table (ADR-0253 §SD1). A poller runs the statements off the frame
goroutine and swaps in a snapshot, as the app-state manager does.

A lens with nothing to show says why, and the three causes are distinct:
the table is empty, the read was refused, or the host minted no bus.

`llm_calls` carries `prompt` and `completion` columns. The app center never
selects them. It reads counts, model, purpose and outcome only. The grant
covers the table, so this is a property of the reader and not of the grant;
the ADR records it as such.

### SD4 — What each lens claims

Each lens states what it measures:

- **Coverage** is the app's own package subtree (`pkg_path` equal to the app
  id or under it). It is live and for this process only. Shared packages the
  app calls are not counted.
- **ADRs** are the ADRs cited anywhere in the app's package subtree
  (`coderef.pkg`, matched against the app id's tail). This is a lower bound
  on what governs the app, the reading ADR-0122 already prescribes for
  `code_refs`. An applet has no package and so shows none.
- **State** is read-only here. The page links to the app-state manager for
  deletes, rather than carrying a second delete seam.

### SD4a — Every section opens in play

Each section carries **Open in play**. It opens the SQL playground on the
introspection endpoint with a statement over the section's table, filtered
to the app, and runs it (ADR-0135 launch config, play's `Endpoint`). Play
reads the table live and whole, including the columns the page leaves out.
Where the page composed in Go, the statement joins instead: the ADR section
joins `coderef` to `adr` over the app's directory. The endpoint allows the
join; the app center's own gated reads do not. The model-call statement
leaves out the prompt and completion, as the page does.

Publishing each section as an ad-hoc dataset (ADR-0240) was weighed and
rejected. Every section is already a table play can name, so a copy would
go stale, count against the dataset quotas, and add a publish grant to buy
nothing.

The Runs section opens on play's Timeline tab, one bar per session. The
starter book's *App sessions* applet draws every app's sessions the same
way, with a lane per app. A bar without a close ends at its process's last
heartbeat. A session whose start lies outside the look-back is a mark at its
close, not a bar from the look-back's edge. Which apps were open together is
not inferred from this: no run records whether it was a person or a scene,
and overlap is not use.

The app center imports play's launch-config package. That package is a
leaf with no registration, kept apart from play for callers like this one.

### SD5 — Three bounded cross-run tables over the fact trail

Run history, logs and audit exist across processes only in `boxer.facts`,
and only Go can read them. Three providers make them readable, each
bounded by construction:

- `app_runs` holds one row per window session, pairing the `started` and
  `stopped` rows on (run id, instance key), over a fixed look-back window.
  An open or crashed session has no stop time. Each row carries the last
  heartbeat of its process, the latest end the trail supports for a session
  without a close. Rows that name no run are left out: they predate
  ADR-0191, and since the instance key restarts in every process they
  cannot be paired.
- `app_logs` holds the newest N log rows per app (`LIMIT n BY app_id`).
  Columns are level, message, caller, error, time, app, instance and run. It
  has no fields and no stack.
- `app_audit` holds audited requests aggregated per (app, subject, result)
  over the same window: count, first and last seen, and latency.

They register only where the facts store offers the reader. Against the
in-memory store they are empty, not absent, as `runtime_events` is.

The log table is the sensitive one. A message may carry anything the app
chose to log, and the table lets a granted reader see other apps' messages
across processes. The grant is the gate, as for every table. Leaving out
fields and stacks keeps the table to what `logviewer` already shows live.
Whether a reader should see only its own trail unless separately granted is
**open** (see Status).

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| vdd vocabulary registry | one membership for the launch config's app id | the assignments golden |
| Codec kind | `appCenterLaunch` | kindcheck; its golden `.out.go` |
| `launcher.Inst` | an Inspect action, injected like Help | hostboot wiring |
| `factsstore` | `AppTrailReaderI`, optional like `RunEventReaderI`; implemented by `chstore` | nothing: consumers type-assert |
| Introspection tables | `app_runs`, `app_logs`, `app_audit` (SD5) | `introspecthost` registration |
| capslock gate | none: the gate analyses `apps/…` and the demo apps, and the app center sits in the runtime beside the launcher, outside its patterns | — |

## Alternatives

- **Grow the launcher's detail pane.** Rejected. It would give a
  zero-capability recall surface a dozen read grants and a poller.
- **A SQL applet book.** Rejected as the home, though fine as a sketch. The
  one-table gate means every lens is its own buffer, and composing across
  them (a page per app) is exactly what a book cannot do without joins.
- **Embed the app-state and watchbill windows as sections (ADR-0155).**
  Deferred. Both render whole-window layouts. A filtered summary with a
  link costs less and duplicates no verb.
- **A manifest field listing an app's ADRs.** Deferred. The citation index
  already exists. A field is worth adding when a citation-derived list is
  shown to mislead.

## Consequences

### Positive

- One place answers "what has this app done", with every read attributed
  to the app center and audited.
- The three cross-run tables are reusable. Play and applets can read them,
  subject to their own gate.

### Negative

- The window holds many grants, and so has a wide read authority by
  design.
- Coverage and ADR lenses are package-derived. An app spread across
  packages outside its own subtree reads as less covered and less governed
  than it is.
- Each Inspect opens another window until the retarget verb exists.
- The logs lens shows only rows the log bridge attributed to an app. Log
  output that carries no app id reaches the trail unattributed and does not
  appear, so for most apps the lens is sparse.

### Neutral

- Data about unregistered ids stays reachable through the tables, but not
  through this window.

## Verification plan — Tier 1

- **Lane: default `go test`.**
  - The poller lands each lens's state over a fixture reader: rows, none,
    refused, failed, and no bus.
  - The app-id-to-package match covers a nested package, a same-named
    top-level directory, a segment prefix and an applet.
  - The launch config round-trips through its golden.
  - The manifest declares a grant for every table it reads.
  - Every statement passes the gate against a registry built the way the
    host builds it.
  - Where a clickhouse binary is present, every statement also runs over the
    bus through the keelson.query service and decodes into its columns.
  - The SD5 providers are tested for their bounds, the empty-store case and
    a failed read.
  - The SD5 reads themselves run against a local server, skipped without
    one, as the package's other live tests are.
  - Every SD4a statement runs on the introspection engine over the host's
    registry, and the applet's buffer runs over fixed sessions covering each
    ending.
- **Scenes.** `appcenter` opens the window, walks two pages, and opens the
  Runs section on play's Timeline. `app-sessions` runs the applet. `inspect`
  opens the launcher, presses Inspect on an app, and waits for its page in
  the app center.

## Status

Proposed 2026-09-25. SD1–SD5 were built the same day and checked headless by
the app center's scene against a host whose store holds a real trail. Open
for the owner:

- **Q1.** Should `app_logs` show another app's messages without a separate
  grant, or should it default to the reader's own trail?
- **Q2.** Should unregistered ids be listed (SD2)?

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way)
for the edit-policy tiers.

## References

- [ADR-0026](./0026-app-runtime-and-capability-subjects.md): the subjects and grants every read goes through.
- [ADR-0135](./0135-app-launch-requests.md): the launch config Inspect sends.
- [ADR-0185](./0185-durable-app-state-manager.md): the state lens's source, and the window it links to.
- [ADR-0191](./0191-runtime-instance-attribution.md): the run and instance keys SD5 pairs on.
- [ADR-0214](./0214-launcher-as-app-summary-detail-recall.md): the launcher's detail pane that carries Inspect.
- [ADR-0253](./0253-introspection-table-reads-as-a-bus-capability.md): the one-table read and its gate.
