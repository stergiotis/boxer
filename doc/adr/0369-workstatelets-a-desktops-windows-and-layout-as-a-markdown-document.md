---
type: adr
status: proposed
date: 2026-11-12
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0369: Workstatelets — a desktop's windows and layout as a markdown document

## Context

A person who works with the same few windows side by side opens each one by
hand and arranges them again in every session. The parts of an answer exist
separately. A launch config opens a window in a given state
([ADR-0135](./0135-app-launch-requests.md)). A workingset is that same config
composed from a live window ([ADR-0148](./0148-app-workingsets.md)). A tiled
layout places windows by name and order
([ADR-0346](./0346-imzero2-tiled-desktop-dynamic-layouts-over-floating-windows.md)).
A committed markdown document becomes a launcher entry
([ADR-0132](./0132-sqlapplet-sql-defined-applets.md),
[ADR-0364](./0364-chatlets-the-chat-specialised-by-a-committed-markdown-document.md)).
Nothing names several windows and their layout as one thing.

Two earlier records deferred this ground. ADR-0148 deferred desktop resume,
named sets and host-side geometry. ADR-0346 §SD7 left the layout, the order
and the float set session-scoped until that deferral moved.

The four forks below were put to the maintainer on 2026-11-12 and decided:
documents come from committed books and from a capture of the live desktop;
launch arguments are written as the config's bytes; the layout is a named
tiled layout; opening adds to the desktop.

## Decision

A **workstatelet** is one markdown document that lists windows to open and,
optionally, the desktop layout they take. A committed book or a capture
supplies the document, the launcher lists it, and the window host opens it.

### SD1 — The document

The frontmatter is the descriptor: `title`, `summary`, `icon`, `topics`,
`keywords`, and an optional `desktop` mapping with `layout` (a layout ident of
ADR-0346), `mainRatio` and `mainCount`. Each fence opened as `yaml window` is
one window, in document order:

| Key | Meaning |
| --- | --- |
| `app` | The app's id or subject alias. May be omitted when `kind` is given. |
| `kind` | The launch kind of `config`. Without `app`, the host resolves the kind to an app ([ADR-0361](./0361-the-playground-is-a-launch-kind-the-runtime-owns-and-the-host-resolves.md) §SD2). |
| `config` | The launch config, base64 (SD2). Requires `kind`. |
| `floating` | The window is out of a tiled layout. |

Text outside the fences is commentary for the reader, and a committed
document carries the repository's document stanza (`type`, `audience`,
`status`) as an applet document does. The parser and the writer are one
package, `runtime/workstatelet`, so a captured document and a committed one
are the same shape.

### SD2 — Launch arguments are the config's bytes

`config` is the standard base64 of the launch config's facts-CBOR, the bytes
`OpenWithConfig` takes; whitespace inside it is ignored. There is no text form
per kind. A workingset record is already these bytes (ADR-0148 §SD2), so a
capture needs nothing from an app beyond what a close asks of it, and an open
passes the boundary every launch passes.

The cost is stated under *Consequences*: the bytes are not readable in a
review, and nobody writes them by hand.

### SD3 — A launcher entry, not an app with a window

One document mints one manifest of kind `workstatelet`, as an applet document
mints one of kind `applet`. The launcher, its search, the launch limit
([ADR-0272](./0272-limit-launchable-apps-with-a-sql-predicate.md)) and
`keelson('apps')` then need no second entry type. The host's `Open` of such a
manifest opens the document's windows and no window of its own. The registry
still requires a constructor; it yields a card that names the document's
windows, which is what a host without a window host shows.

The minted id ends in `ws-<slug>`, so the alias a document is launched by is
never the alias of the app its windows show: a desktop captured as "Play"
would otherwise be refused by the registry as a second `play`.

### SD4 — Opening adds, in order, and skips what is refused

Opening closes nothing. The document's windows join the desktop with the
first one as the main and in front, in document order. When `desktop.layout`
is present the host sets it and the parameters given; when it is absent the
desktop keeps its mode. A tiled layout is desktop-wide (ADR-0346), so windows
that were already open follow the document's in the order.

Each window goes through the same door as any open. One the boundary refuses —
an app this process does not register or may not launch, a kind the app does
not take, bytes its probe refuses, a config for a singleton that already has a
window — is skipped with a logged reason and its siblings still open. The open
fails as a whole only when nothing opened or the layout is not one the host
has.

A window without `config` opens as a click on the app would, which restores
the app's default workingset where it keeps one.

### SD5 — The person opens one; a launch request does not

A workstatelet opens from the launcher and, for a committed document, at
boot (`--launch`); a stored one is minted after launch resolution, as a
stored applet is.
`windowhost.open` and `runtime.agent.launch` refuse it. A launch grant names
apps and counts ([ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)),
and the layout sits under the desktop grant
([ADR-0276](./0276-agents-read-and-arrange-windows.md) §SD4); one request
that opens several apps and sets the layout would cross both.

### SD6 — The launch rows name the workstatelet

Each window a workstatelet opens writes a launch row whose caller is the
workstatelet's manifest id, as a restored window's caller is
`runtime.workingset` (ADR-0148 §SD6). Which windows a document opened is then
one predicate on the caller column.

### SD7 — Capture

**Window ▸ Save desktop as workstatelet** writes a document of the open
windows in the desktop's order. For each window the config is, in this order:
what the app composes as its workingset, taken whether or not the window is
dirty, since the person asked; else the launch config the window was opened
with; else none. The composed bytes pass the checks a workingset save passes.
A window marked floating is written so, and a tiled desktop writes its layout
and parameters.

Windows of shell apps — the launcher, help — are left out. A floating
desktop's geometry is not captured; see *Deferred*.

### SD8 — Captured documents are stored and minted like stored applets

A captured document is kept through `runtime.persist` under a synthetic
identity and minted at once and at every boot, under the applet store's rules
(ADR-0132, Update "O4"): the slug derives from the title, a committed
document of the same slug wins, and saving a stored slug again replaces its
document while the manifest stays as registered until the next boot.

A host a scene launched keeps its store in memory. Scene hosts share the
persist store of the machine they run on, so a scene's capture would
otherwise appear in a person's launcher, and that person's captures in every
scene that draws one.

A stored document has had no review. It is admitted because it holds nothing
with authority: it can ask only for opens the person's own click could make,
with bytes the target app's probe accepts, and each open is refused or
allowed where every open is.

`keelson('workstatelets')` lists the documents of both origins with their
text, which is how a captured document reaches a book.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `app.KindE` | `KindWorkstatelet` added | `keelson('apps')` `kind` values; readers switching on kind |
| Exported Go API (`windowhost`) | `OpenWorkstatelet`, `CaptureWorkstatelet`, the catalog setter; `Open` dispatches on the kind; `OpenWithConfig` refuses it | Shell chrome Window menu; host boot |
| `runtime/workstatelet` | Added: document, book registry, catalog, store | Shells mint the books before launch resolution, as for applets |
| `keelson('workstatelets')` | Added | Introspection catalog |
| Persist keys under `runtime.workstatelet` | Added | — |

## Alternatives

- **A YAML form per launch kind.** A kind would register a decoder from a
  mapping to its DTO. Not taken: it is a second, per-kind text dialect beside
  the codec, and capture would need its inverse for every kind before it could
  write a document.
- **A URL per window** (`mdedit://…`, [ADR-0347](./0347-mdedit-links-a-url-scheme-a-location-grammar-and-launch-options.md)).
  One app has a scheme; every other would need one.
- **Rects as fractions of the work area.** A window's content need overrides a
  rect that is too small (ADR-0275 §SD4), so a document would not describe
  what appears. Deferred rather than rejected, for the floating desktop.
- **Replace the desktop on open.** It closes windows the person did not name.
- **Boot seed only.** No way to open one in a running session.
- **A registry of its own and a second launcher row type.** The launcher's
  filter, search, history and keys are written over manifests.
- **A typed state row instead of a persisted document.** The document is the
  unit a person commits to a book; a row would need the writer anyway.

## Consequences

- A `config` cannot be read in a review. The corpus test checks that each
  committed config decodes as its kind; that it is the config the author
  meant takes a test per document that writes the value out in Go, which the
  in-tree book has and nothing enforces.
- A config outlives the code that wrote it. A kind whose codec stops
  accepting old bytes turns that window into a skipped one.
- A capture is not gated on intent as a workingset save is (ADR-0148 §SD4). A
  window seeded with an ephemeral dataset handle is captured with the handle,
  and opens later on a query over nothing.
- The registry holds entries that are not apps. A reader that enumerates
  manifests and constructs each one gets the card.
- Opening a workstatelet that names a layout re-tiles the windows the person
  already had open.
- A skipped window is a log line. The person sees fewer windows than the
  document lists and no reason on screen.
- A launch limit applied at boot does not cover a document captured
  afterwards (ADR-0272 §SD2 names the apps the process booted with). Each of
  its windows is still checked at its own open.
- The capture's save runs off the render goroutine and answers in the menu a
  moment later; a second save started before the first answered shows only
  the later outcome.

## Verification plan — Tier 1

- **Lane.** Default `go test`: a document survives parse and write; the host
  opens in order with the first window as main, sets the layout and the float
  set, skips a refused window and opens the rest, refuses a workstatelet at
  `OpenWithConfig`, and writes the launch rows; a capture takes the composed
  bytes, falls back to the launch config, and leaves shell windows out; the
  store refuses a committed slug, replaces a stored one and mints at start.
  The corpus test parses every committed document and probes every config.
- **Scene.** `apps/workstatelets/scenes` launches the committed document by
  its alias on the headless host, reads the two windows it opened, and saves
  the desktop from the Window menu. It runs under `scripts/dev/scene.sh` and
  in no test lane.
- **Gap.** Nothing checks the cell each window settles in, and no test opens
  a stored document after a restart against a durable store.

## Milestones

1. **M1 — The document and the books.** Parser, writer, book registry,
   minting, the kind.
2. **M2 — The host opens one.** SD4–SD6.
3. **M3 — Capture and the store.** SD7, SD8, the Window menu entry.
4. **M4 — `keelson('workstatelets')`.**
5. **M5 — A committed document and its scene.** `apps/workstatelets`; its
   test states the document's config in Go and compares the bytes.

## Deferred

- **Geometry on a floating desktop.** Trigger: a workstatelet someone wants
  floating and placed.
- **Opening by a launch request or an agent.** Needs a grant that names the
  document's apps and the desktop. Trigger: a chatlet that wants to set up a
  desktop.
- **Deleting or renaming a stored document.** The registry has no removal.
  Trigger: the first person with stale entries.
- **A readable rendering of a config.** Trigger: a review that could not judge
  a committed document.
- **A notice for skipped windows.** Trigger: someone misled by a partial open.
- **Reopening the last desktop at boot.** A capture at shutdown and an open at
  boot would do it; nothing takes either automatically.

## Status

Proposed 2026-11-12.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0135](./0135-app-launch-requests.md), [ADR-0148](./0148-app-workingsets.md) — the launch config and the record composed from a window.
- [ADR-0275](./0275-imzero2-window-arrangement-rust-reports-go-decides.md), [ADR-0346](./0346-imzero2-tiled-desktop-dynamic-layouts-over-floating-windows.md) — placement and the tiled layouts.
- [ADR-0132](./0132-sqlapplet-sql-defined-applets.md), [ADR-0364](./0364-chatlets-the-chat-specialised-by-a-committed-markdown-document.md) — a committed document minted into a launcher entry; the stored-document rules.
- [ADR-0272](./0272-limit-launchable-apps-with-a-sql-predicate.md), [ADR-0361](./0361-the-playground-is-a-launch-kind-the-runtime-owns-and-the-host-resolves.md) — the launch limit and the open by kind.
