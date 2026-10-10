---
type: adr
status: accepted
date: 2026-10-09
reviewed-by: "p@stergiotis"
reviewed-date: 2026-10-10
---

# ADR-0299: SQL applets a browser tab loads from its page's origin

## Context

A SQL applet ([ADR-0132](./0132-sqlapplet-sql-defined-applets.md)) is one
markdown document: frontmatter, prose, and a SQL buffer that a host mints into
an app. Two paths admit a document: a committed book, review-gated, which
ADR-0132 §SD5 names the actual security boundary; and the runtime store (its
Update "O4"), which persists what an author saved through the persist
service. Both pass the same parser and the same security classification.

A browser tab ([ADR-0278](./0278-tab-mode-for-downstream-apps.md)) had
neither. It mounted one app and ran no minting step, so no applet could open
in it, and it boots no persist service, so the store is out of reach there
too. Yet a tab is where a document that lives outside the tree is cheapest to
deliver: the server that serves the page can serve a markdown file beside it.

## Design space (QOC)

**Question.** How does a tab open an applet document that is not compiled into
its module?

**Options.**

- **O1** — **A downstream book.** The consumer's tab binary calls
  `sqlapplet.RegisterBook` on its own embedded files.
- **O2** — **Fetch from the page's origin at start.** The worker names a
  document by path; the module fetches it before mounting, admits it through
  the parser and mints it. (chosen)
- **O3** — **The runtime store on browser storage.** Saved applets persist in
  the tab.
- **O4** — **Fetch from any URL.**

**Criteria.**

- **C1** — A document can change without rebuilding the module.
- **C2** — The tab reaches no site it did not reach before.
- **C3** — A document no one reviewed cannot do more than the tab's own play
  already lets a visitor do.
- **C4** — Nothing new is needed in the tab first.

| | C1 | C2 | C3 | C4 |
|---|---|---|---|---|
| O1 | − | + | + (reviewed by the consumer) | + |
| O2 | + | + | + with SD3 | + |
| O3 | + | + | ± | − (needs persist in the tab, ADR-0278 SD2) |
| O4 | + | − | − | − (CORS, a trust list) |

O1 works without this ADR once the tab mints (SD4) and stays the path for a
curated downstream corpus. O3 waits for a persist service in the tab. O4 is
O2 with C2 given up, and nothing asks for it yet.

## Decision

### SD1 — A prepare step in `tabhost`, and the applet loader behind it

`tabhost.Options` gains `Prepare`, a function the tab calls once at start,
after its HTTP transport and services are installed and before the app is
looked up. It receives the page's base URL (SD2) and may return an app id that
replaces `-app`; an error is drawn in place of the app, so the visitor sees
why nothing mounted. `tabhost` stays free of any app package: the tab binary
supplies the function.

`sqlapplet.LoadTabApplet` is such a function. When
`BOXER_SQLAPPLET_TAB_DOC` names a document, it fetches it, parses it with
`ParseDocSource` — the parser the books and the store use — mints it into the
default registry, and returns its id. The document's base name is its slug,
as for a book; its book id is `origin`, which the Definition drawer shows.

### SD2 — Same origin only, resolved against the page

The worker passes the URL of the directory the page is served from as
`BOXER_TAB_BASE`. The document is named by a path — relative to the page, as
`applets/x.md`, or absolute on its origin — and resolved against that base; a
URL, a scheme-relative reference, a query, a fragment, or a path that does not
end in `.md` is refused, and so is any resolution that would leave the base's
origin. A relative path is what lets a link survive the site it is published
on: a page under `/boxer/demo/` names `applets/x.md`, not a path that spells
out the repository's name. The serving side needs no new route: `serve`
already serves the bundle directory, and a static host serves whatever sits
beside the page.

`Services.NoEgress` lets the base's origin through and refuses every other
site. A page that says it loads nothing from elsewhere still holds to that
when it reads a file served beside it, so the published demo (SD5) keeps the
option on.

### SD3 — Read-class only

A document from the origin passed no review, so the stance ADR-0132 §SD5 takes
for the committed corpus — the corpus is the gate, `readonly` is defence in
depth — cannot hold for it. The loader admits only a buffer the parser
classifies **read**. A **read-egress** buffer reaches past the endpoint from
the ClickHouse server (`url()`, `s3()` and kin), and a **mutating** one
writes; both are refused, whatever the document says about itself.

With that, a linked document does no more than a tab URL already can: play
seeded through `env=BOXER_PLAY_SQL` with `BOXER_PLAY_AUTORUN` runs any SQL on
open, with no class check. The applet path is the narrower of the two.

### SD4 — The committed applets mint in the tab

`imzero2tab` links `apps/sqlapplet` and calls `MintManifests` in its prepare
step, so a committed applet opens by its id with `app=`, and an origin
document whose slug a committed applet holds is refused — committed wins, as
in the store (ADR-0132 O4-D3). Minting runs in the tab's action, not at
initialisation, so `bundle` and the other subcommands do not mint. Linking the
applet host costs little beside play, which the binary already links: about
0.3 MB of a 104 MB module, measured once on 2026-10-09.

### SD5 — The published demo carries the repository's complexity map

`boxer code analysis sccapplet` scans a git worktree with scc and writes an
applet document whose buffer carries one row per directory as literals,
`SELECT … FROM values(…)`, which the tab's evaluator answers with no database
(ADR-0290 §SD3): area the directory's own lines of code, colour the
cyclomatic complexity per 100 lines under it, generated files and tests left
out as the repo code exploration app leaves them out; it can fold directories
past a depth into their ancestor, which keeps every line and drops those
directories' own cells. The document states the commit it was taken at and
nothing time-dependent, so one commit gives one document.

The `tab-host` workflow's demo build runs it on the checkout, before anything
is written into it, at the dispatched commit and with nothing folded, and puts
the document beside the demo page; the landing page links it by its relative path.
`imzero2tabdemo` takes `LoadTabApplet` as its prepare step and mints no
committed applet, since nearly all of them read tables a tab does not have.
The map refreshes when the demo is rebuilt, which is a manual dispatch.

## Alternatives

- **O1, O3, O4** — see the QOC above.
- **A route of its own in `serve` (`--applets <dir>` at `/applets/`).** Not
  taken: `serve` already serves the bundle directory, and a static host has no
  such flag to honour, so the route would bind the feature to one server.
- **Compiling the demo's document into its module as a book.** Would leave
  `NoEgress` as it was. Not taken: `go:embed` of a file the build generates
  needs a placeholder in the tree for every other build, and it would not
  exercise the path SD1 adds.
- **Admitting read-egress with an explicit Run, as a committed applet gets.**
  Not taken: the explicit Run is a guard for a reviewed document; for one
  nobody reviewed, a click on a page someone linked to is not a decision.

## Consequences

### Positive

- A tab opens an applet document that lives beside the page, without a
  rebuild; the committed applets open in a tab too.
- Every document passes the gate the other two paths use, and the class rule
  holds whatever the document claims.

### Negative

- An applet's reach in a tab is the tab's: an `endpoint: introspection`
  applet finds no in-process endpoint there (the tab does not publish one),
  and an applet that declares `datasets:` waits for an ad-hoc data service
  the tab does not run.
- One document per tab, chosen at start; there is no launcher in a tab
  (ADR-0278, Deferred).
- The fetch runs on the goroutine that runs the tab's setup and holds the
  first frame until it returns.
- The published map is as current as the last demo dispatch, not the branch.

### Neutral

- Tab mode keeps ADR-0263's posture: a demo and per-user tier, no isolation
  credit.

## Verification

- The loader's tests serve documents from a local HTTP server: a read-class
  document mints under its slug with the store's default topic; a read-egress
  one, one without a SQL fence, one not served, and each refused path shape
  mint nothing; a second document of a taken slug is refused.
- On 2026-10-09 a bundle of `imzero2tab` was served and opened in headless
  Chromium with the document path in the worker's environment and
  `CLICKHOUSE_URL` at the in-process keelson endpoint: a document reading
  `keelson('apps')` mounted as an applet and drew its rows; a document reading
  `url(…)` drew the class refusal in place of the app.
- `sccapplet`'s tests answer a composed document with the trivial evaluator, as
  a tab does, and check its tree, its sums, the folding and that the same scan
  gives the same bytes; an applet test checks the document parses read-class.
  A tabhost test pins `NoEgress` to the base's scheme, host and port.
- On 2026-10-09 a site laid out as the published one — the generator's output
  beside an `imzero2tabdemo` bundle under `/boxer/demo/`, served by a plain
  static server — was opened from the landing page's link in headless
  Chromium: the document resolved against the page, `NoEgress` let it through,
  the server saw no request for anything outside the site, and the treemap
  drew 574 directories at depth 4. The whole tree, 976 directories with
  nothing folded, ran out of memory in a tab before the nanopass text fix and
  drew in about 3 s after it, against about 1.2 s at depth 4, in single runs
  under software rendering. The published demo folds nothing: the treemap's
  own drill and depth controls bound what is drawn, and a depth of 4 left a
  quarter of the lines, 11% in one directory, without cells of their own.

## Deferred

- Loading from another origin (O4), and a list of documents rather than one.
- Saving applets in a tab (O3), after a persist service in the tab.
- Publishing the in-process keelson endpoint as the tab's local query
  endpoint, which would let `endpoint: introspection` applets run there. It
  changes how play routes its own queries in a tab, so it is its own change.

## Status

Accepted 2026-10-10. SD1–SD5 were built on 2026-10-09, beside this record,
and reviewed with it; the published demo has carried SD5 since.

## References

- [ADR-0132](./0132-sqlapplet-sql-defined-applets.md) — the applet document, the class, the store.
- [ADR-0278](./0278-tab-mode-for-downstream-apps.md) — `tabhost` and its services.
- [ADR-0263](./0263-imzero2-browser-both-modules-in-one-worker-mesh-to-the-painter.md) — the tab and its posture.
- [ADR-0290](./0290-keelson-named-arguments-and-a-trivial-sql-endpoint.md) — the in-process keelson endpoint a tab can read without ClickHouse.
- [imzero2-in-the-browser](../howto/imzero2-in-the-browser.md) — how to open one.
