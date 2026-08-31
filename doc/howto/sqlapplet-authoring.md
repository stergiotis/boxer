---
type: how-to
audience: operator or contributor authoring a SQL applet
status: draft
# reviewed-by: "@<handle>"   # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD  # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# How to author a SQL applet

A SQL applet is a first-class launchable app defined by one markdown
document: frontmatter as its manifest, prose as its help page, and one SQL
fence as its entire behaviour
([ADR-0132](../adr/0132-sqlapplet-sql-defined-applets.md)). The host renders
it as an attenuated SQL Playground — result panels, a params strip, Run and
a Definition view, no exploration chrome. This page walks the two authoring
routes: saving from a running playground, and committing a document into an
applet book.

Caveats up front. A slug is a durably public name the moment it ships —
renaming one is a deprecation event. Applets saved at runtime live in the
runtime persist facility and are only as durable as its backend (a box
running the in-memory fallback — `persist:mem` in the status bar — forgets
them at restart). The security class that gates auto-run is derived by
static analysis of the SQL text and reaches no further than the text shows
(ADR-0132 §SD5).

## The document

One document is one applet. The `runtime-env` applet in the starter book
(`apps/sqlapplet/book/runtime-env.md`) is a complete example:

````markdown
---
type: reference
audience: end-user
status: draft
title: Runtime environment
summary: "Query the env-var registry with the values in force"
icon: "🌡"
endpoint: introspection
tabs: [table, detail]
---

# Runtime environment

Prose — this body is the applet's help page, and the place to say what
the query shows and what its parameters mean.

```md preamble
Every knob the process **declares**, not every knob it reads.
```

```sql
SET param_pattern = '%';
SELECT * FROM keelson('env')
WHERE name LIKE {pattern:String}
```
````

The rules, all enforced by the same gate everywhere:

- **`title` and `summary` are required**; `icon` is optional (one glyph). The
  documentation-standard keys (`type`, `audience`, `status`) keep the
  help-book conformance check quiet.
- **`summary`** is the one line the launcher renders under the title
  ([ADR-0214](../adr/0214-launcher-as-app-summary-detail-recall.md)). Its job
  is to tell this applet from the others in its book, so write what a reader
  choosing between them needs: verb-first, one clause, around 60 characters,
  no full stop, and never opening with the title again. It is not the body's
  first paragraph — that one introduces a document someone has already
  opened.
- The **first role-less `sql` fence is the buffer**, and it must stay
  pasteable-complete: paste it into the playground, press Run, and you have
  the applet. Later role-less fences are prose examples; a `sql bands`
  fence carries the Timeline panel's band query.
- An optional **`md preamble` fence** is explanatory markdown the applet
  shows *above* its result panes — the caveat a reader needs before reading
  the numbers. Keep it to a sentence or two: the strip fits its content, so
  a long one crowds the panels, and the body prose is already one click away
  under **Definition**. Fences do not nest, so a preamble cannot contain a
  fenced block. `markdown` works in place of `md`.
- The **filename is the slug** (`runtime-env.md` → `runtime-env`):
  lowercase alphanumerics and dashes.
- **`endpoint`** is `default` (the env-configured ClickHouse; may be
  omitted) or `introspection` (the in-process
  [ADR-0094](../adr/0094-keelson-introspection-tables.md) endpoint, where
  `keelson('…')` tables live — parameters bind there too, per
  [ADR-0133](../adr/0133-chhttp-server-dialect-and-param-binding.md)).
- **`tabs`** is `auto` (or absent) to offer every result panel, or a list
  to pin the set and order — entries are panel slugs (`table`,
  `projection`, `timeline`, `world`, `kanban`, `network`, `schema`,
  `detail`), optionally bound to a CTE by name (`table:recent`), which is
  how one buffer serves several sub-views.
- **`topics`** classifies the applet for the launcher
  ([ADR-0158](../adr/0158-app-classification-topics-keywords-kind.md)).
  Omit it and the applet inherits its book's default, which is usually
  right — set it only when a document sits off its book's subject, or
  belongs under more than one. Entries come from the registered vocabulary
  (`runtime`, `code`, `topology`, `observability`, `data`, `sql`, `ui`,
  `geo`, `about`); anything else fails the gate. List every topic that
  clearly applies and none that only vaguely applies: an applet under two
  topics is browsable under both, which is the point.
- **`keywords`** is free retrieval text — the words someone would type
  looking for this applet when its title is not what came to mind. No
  vocabulary, no validation: keywords are only ever matched against, never
  shown as structure, so nothing has to be kept consistent with anything.

The SQL itself determines the rest. A `SET param_<name>` prelude plus a
`{name:Type}` placeholder becomes a widget in the applet's params strip
([ADR-0124](../adr/0124-play-param-editing-widgets.md)); a placeholder the
prelude does *not* bind is a signal, and the applet opens Live so panel
interactions re-run it. Result-shape conventions light panels up — `lane`
and `title` columns for the Kanban
([ADR-0122](../adr/0122-play-kanban-panel.md)), `label@mime` cells for
Detail ([ADR-0123](../adr/0123-play-content-typed-detail-cells.md)),
country codes for the World map
([ADR-0114](../adr/0114-play-world-choropleth-panel.md)). The playground's
own help book documents the conventions from the user side.

The **security class** is computed from the buffer at the gate: `read`
applets auto-run on open; `read-egress` (reaching out via `url()`, `s3()`
and kin) and `mutating` (a non-`param_*` SET — every other mutating form
fails to parse and classifies the same way) wait for an explicit Run. The
playground's Diagnostics tab shows the class and its witnesses while you
author.

## Route 1 — save from the playground

1. Explore in the SQL Playground until the buffer is worth keeping. If it
   reads `keelson('…')` tables, switch the endpoint to *Keelson
   introspection* first (Endpoint ▸) — the saved document records the
   endpoint you authored against.
2. Open **Save applet** in the top bar, fill slug, title, and optionally an
   icon, and press Save. The store service validates exactly as the
   committed corpus gate does; a refusal names its reason (unparseable
   buffer, slug collision with a committed applet, malformed slug, a
   buffer containing a ``` fence line).
3. The applet appears under **Apps ▸ Applets** immediately. Saving the
   same slug again replaces the definition for future opens; the launcher
   entry's title and icon refresh at the next boot.

## Route 2 — commit into an applet book

Add the document to `apps/sqlapplet/book/` (or ship a book from another
package via `sqlapplet.RegisterBook` in an `init`, the help-facility
pattern). The corpus test — `TestStarterBookCorpus` and the ParseBook rules
it pins — is the hard gate: an applet that fails to parse, classify, or
validate is a build failure, never a runtime surprise. Committed applets
win slug collisions against runtime-saved ones, and review of the document
is the curation step that makes the launcher entry trustworthy.

## Launching

Applets sit in **Apps ▸ Applets**, and `--launch` accepts them like any
app. A dash-free slug works bare (`--launch demo`); a dashed slug is not a
bare alias (the launch selector is a SQL WHERE, where a dash reads as
minus), so quote it:

```sh
main_go … --launch "subject_alias = 'runtime-env'"
```

## When it breaks

A broken applet fails visibly, never silently: the status bar carries the
server error, and the panels show the failed state. To debug, press
**Open in Playground** and work with the full chrome — Diagnostics for the
class and grammar verdict, Preview for the as-sent body. **Definition**
opens the applet's own document beside the result, with a `Copy` button on
each SQL fence if you would rather paste it somewhere yourself. The most
common failure is an endpoint mismatch: a buffer
using `keelson('…')` against a plain server errors with *Unknown table
function keelson* — set `endpoint: introspection` (or re-save from a
playground pointed there).
