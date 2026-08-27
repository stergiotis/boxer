---
type: explanation
audience: package maintainer
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-27
---

> **Provenance.** Measurements in §1–§11 were taken 2026-08-05 against the tree
> at that date, §12's on the dates each subsection carries. Corpus counts are
> stated as ratios or orders of magnitude on purpose: the exact figures moved
> with every edit to the vault and were stale within weeks.

# Porting a business-capability map into boxer

Background for [ADR-0168](../adr/0168-capmap-business-capability-corpus.md).
This page carries the measurements, the survey of what boxer already provides,
and the reasoning behind each fork; the ADR records only the outcomes.

Provenance markers: **measured** claims were produced by running the stated
command against this tree on 2026-08-05. **Read** claims come from reading
source at that date. Everything else is an estimate and says so.

## 1. What is being ported

A standalone business-capability management prototype: a hierarchical
capability model with maturity and pain scoring, lifecycle tracking, an
Obsidian vault as the editing surface, ClickHouse as the query store, and a
server-rendered HTMX front end.

**Measured** (`find`/`wc` over the prototype's checked-in files):

| | |
| --- | --- |
| Go | 7,413 lines, 16 packages, plus one vendored MIT fork (a squarified-treemap layout, 248 lines) |
| Web | 1,221 lines of HTML/JS/CSS across four webapps — browser, culler, cull-configer, lint — plus a landing page and a vendored HTMX |
| SQL | `schema.sql` + `views.sql`, ~340 lines, `ReplacingMergeTree` |
| Corpus | 2,625 markdown files, 11 MB, three catalogs — a boxer catalog (79 capabilities), a public process-framework catalog (82), and one from a private checkout (27) |

Its own architecture note already describes the write path as CQRS: vault
markdown is the write model, ClickHouse the eventually-consistent read model.
That framing survives the port intact and is the reason §5's fork was cheap to
settle.

## 2. API drift against boxer is near zero — measured

The prototype pins a boxer version from 2026-04-06. To measure the real delta,
its checked-in files were copied to a scratch directory, a
`replace github.com/stergiotis/boxer => <this tree>` appended to `go.mod`, and
the build run under both tag sets with `GOWORK=off`:

- `go build ./...` — **exit 0**
- `go vet ./...` — **exit 0**
- `go test ./...` — **two failures**, both in one file's assertions:
  `astbuilder` now emits quoted identifiers, so a test asserting the substring
  `id = 12345` sees `"id" = 12345`. Behaviour is unchanged; the fix is two
  assertions.

All fourteen boxer import paths it uses still exist. Every external dependency
it needs is already in boxer's `go.mod`; `blackfriday` is present only as an
indirect, and `public/semistructured/markdown/obsidian` can replace it outright.

One blocker is policy rather than API: two files still carry
`//go:build llm_generated_opus4*`, retired by
[ADR-0083](../adr/0083-retire-llm-generated-build-tags.md).

**Conclusion.** Compilation is not the work. Placement and shape are.

## 3. The model to copy: boxer's ADR ecosystem

**Read.** The decision corpus is exposed by five cooperating pieces, and
markdown is the only source of truth in the chain:

| Piece | Where |
| --- | --- |
| Corpus parser (pure library over markdown) | `public/gov/adrcorpus` |
| CLI emitting Arrow, queried via `clickhouse-local` | `public/app/commands/adr` |
| The same tables in-process for the GUI | `providers/adr.go` under `public/keelson/runtime/introspect` |
| Canned SQL lenses | `apps/sqlapplet/book*` ([ADR-0132](../adr/0132-sqlapplet-sql-defined-applets.md)) |
| The committed corpus itself | `doc/adr/` |

Two properties of that chain are worth naming because the port inherits both.
A schema-parity test pins the CLI's Arrow schemas equal to the providers'
schemas, so a query written against one runs verbatim against the other. And
the providers are **empty rather than erroring** off-repo — a shipped binary
with no checkout around it has no corpus, which is a fact about the process
rather than a failure.

## 4. Naming: "capability" is already taken

**Read.** In boxer the word means *runtime/security capability*:
`public/keelson/security/capslock`, `codec/capabilitygrant`,
`apps/capinspector`, `apps/capdemo`, the `CapId`/`CapSpec` types, and
[ADR-0026](../adr/0026-app-runtime-and-capability-subjects.md)'s capability
subjects — a named registry, and therefore Tier 1 by
[CODINGSTANDARDS](../../CODINGSTANDARDS.md#what-triggers-an-adr).

A business-capability corpus cannot claim that word. The port keeps `capmap`
as the subsystem name — a compound that does not collide — and names the
corpus package `capmapcorpus`, mirroring `adrcorpus`.

> **2026-08-05, after the fact.** This section stopped one level too high. It
> settled the *package* names and said nothing about the **unit**, so the
> implementation went on to call the model type `Capability` and the keelson
> tables `capability` / `capsection` / `caprelation` — putting the word back in
> the flattest shared namespace boxer has, which is the one place it collides
> worst. ADR-0168 §SD6 now names the unit a **competence** and states the rule
> the survey left implicit: boxer's names take the fresh word, the vault keeps
> the literature's, and `capmapcorpus` is the boundary.

## 5. The facts model already carries almost everything

The decisive question was whether the corpus should get bespoke Arrow tables or
be modelled as facts. Once modelled as facts, four things had to be checked.

### 5.1 `u64h` and `f64h` already exist

**Measured** (`grep` over the generated DDL at
`public/keelson/runtime/factsschema/ddl/runtime_facts_ddl.out.go`). The schema
promotes every numeric scalar with `ScalarModifierHomogenousArray` in two
loops, so the array sections are already there:

```
u64Array:value:val:u64h      f64Array:value:val:f64h
u8h u16h u32h i8h i16h i32h i64h f32h   z64h   sh   yh
u32m u64m                    ← ScalarModifierSet, also present
```

`ctabb.U64h` and `ctabb.F64h` exist as first-class generated abbreviations.
No extension to the leeway table description is required.

### 5.2 Array-valued promotion is 18 of 22 value columns done

**Measured**, same source. Only four value columns are scalar:

| Section | Type | Status |
| --- | --- | --- |
| `symbol` | `s` | Deliberate — the schema comment records that 1-element-array wrapping would defeat the inter-record low-cardinality encoding that audit and grant rows rely on |
| `u32Range` | `u32`, `u32` | Structural — co-columns (`beginIncl`/`endExcl`), a range pair rather than a value list |
| `foreignKey` | `u64` | Structural — the relation channel itself |
| `bool` | `b` | The one genuinely open candidate |

So a "promote everything" pass is mostly already applied, and the residue is
one section that no capability data needs.

### 5.3 `text` and `string` are byte-identical

**Measured.** The two sections generate the same column specification —
canonical type `sh`, encoding aspects `g`, value aspects `0`, and the same
`Array(String) CODEC(ZSTD(3))`. Only the section *name* differs. Because
leeway schemas are nominal rather than structural they remain distinct
addresses, but nothing downstream can tell them apart.

By contrast `symbol` earns its name: its value-aspect segment is `24`, base62
for 128 = `1<<7` = `AspectCanonicalizedValue`, and its low-cardinality hints
produce `Array(LowCardinality(String))`.

**Read.** The triple arrived wholesale in the upstream import commit
`884e2c9f` and has never been revisited here. The runtime barely exercises the
distinction: the ClickHouse facts store writes `stringArray` four times and
`textArray` once.

The compression mapping makes the difference actionable —
`AspectLightGeneralCompression` emits `ZSTD(3)`, `AspectHeavyGeneralCompression`
emits `ZSTD(12)`, ultra-heavy `ZSTD(19)`.

**A second, richer shape exists in `anchor`**, boxer's leeway showcase, whose
`text` section is a co-section of `text` + `wordLength` + `wordBag` — human
readability carried structurally, by derived retrieval columns, rather than by
a flag. The two schemas also disagree on whether an array of symbols is still
low-cardinality: `anchor`'s `symbolArray` drops the low-card hints,
`factsschema`'s keeps them.

| | `anchor` | `factsschema` |
| --- | --- | --- |
| `symbol` | `S` + canonicalized + low-card | same |
| `symbolArray` | `Sh` + canonicalized, light compression | `Sh` + canonicalized **+ low-card** |
| `stringArray` | `Sh`, light compression | same |
| `text` | co-section: `text` + `wordLength` + `wordBag` | `textArray`: identical to `stringArray` |

### 5.4 The relation channel exists and has never been used

**Read.** `foreignKey` is a `LowCardRef`, `u64`-valued section in its own
streaming group carrying `useaspects.AspectLinking` — the cross-fact reference
channel. [ADR-0109](../adr/0109-leeway-marshall-multi-membership-ref-tuples.md)
already lets one tuple element carry several memberships including ref
channels, with the ref id carried directly as `uint64`, which is an
edge-with-roles in all but name.

`valueaspects.AspectGraphVertex` (52), `AspectGraphEdge` (53) and
`AspectHyperGraphEdge` (54) exist with **zero consumers** repo-wide. They are
schema-level declarations on columns, so using them means changing
`factsschema` — a Tier-1 change with no consumer yet to serve.

## 6. The vocabulary gap

**Read.** `public/keelson/runtime/vocab` registers 73 memberships under
`identifier.TagValue(2)`, all `runtime*`-prefixed, in `LowerSpinalCase`. The
`estSize` arguments (`4`, `32`) are capacity hints for a growing container, not
ceilings — there is no analogue of the 64-aspect cap here.

The gap is allocation. A second vocabulary needs its own TagValue, and nothing
records which are taken.
[ADR-0106](../adr/0106-identity-fibonacci-tags-build-tag-retirement.md) §SD8
notes only that `TagValue(0)` was invalid under the fibonacci scheme and that
the runtime registry moved to a valid value. The precedent the vocab comment
cites for scope separation is a schema from a checkout that is **no longer in
this tree**, so its allocation is claimed by comment and nothing else.

Identity itself is settled by precedent rather than invention: facts rows carry
a `u64` `id` plus a `naturalKey` that is a blake3-16 digest over the
identifying tuple. A capability's identity becomes that, which also retires the
prototype's xxh3 slug hash and one dependency with it.

## 7. Store: a real tension, and the bridge

**Read.** The two obvious positions collide. The ClickHouse facts store targets
a **live server** (`http://localhost:8123/` by default) — that is how facts are
written. `chlocalpool` spawns `clickhouse-local --path <mktemp>`, pooled and
reaped: a stateless engine over external files that **cannot host** a
persistent table.

The reconciliation is that `factsschema/cborarrow` already converts facts rows
into an ArrowStream IPC payload using the generated typed Arrow builder, and
`leeway/ddl/arrow` is the Arrow mapping. So a serverless read path exists in
principle as *facts-shaped Arrow read through `file()`*, rather than as a table
of the port's own design.

**Not measured.** The pieces were read, the round-trip was not run. Proving it
is a milestone step, not an assumption.

## 8. Markdown: extract, do not decompose

The question was whether to store each capability's markdown as a parsed AST in
separate attributes.

**Read — prior art.** `anchor/codecdemo/labeledtextdoc.go` is boxer's own
answer to putting a text document into leeway. A document is a sequence of
*labelled text chunks*: the label is the membership, the prose is the prose,
and the derived structure is a word bag and word lengths. Not an AST. That maps
onto a capability body without adaptation — six labelled chunks, one per h1
section, which is exactly what the prototype's parser already produces.

**Read — corpus precedent.** The ADR ecosystem stores decision markdown as one
media-typed column (`content@text/markdown`,
[ADR-0123](../adr/0123-play-content-typed-detail-cells.md)) in its own table,
split out because the source is roughly 60× the metadata.

**Read — what the data actually contains.** Sampling the boxer catalog, the six
description sections are heterogeneous: `Vision and Scope` is genuine prose;
`Activities` and `Standards` are bullet lists of wikilinks with display labels;
`Business Capabilities` is prose with inline wikilinks. The AST's queryable
meaning is concentrated in the links — and links are relations, which are
already their own facts.

Three arguments against full decomposition:

1. **Round-trip.** The vault stays authoritative and the dump verb must
   regenerate it; the prototype has a round-trip test. A goldmark AST is not
   losslessly re-renderable — whitespace, list markers, link-title style — so
   storing AST instead of source would break the CQRS contract.
2. **Volume with no query behind it.** 2,625 capabilities × 6 sections × N
   nodes, to answer questions nobody asks. Nobody queries the third emphasis
   node in the second paragraph.
3. **The data-centricity invariant does not demand it.**
   [ADR-0148](../adr/0148-app-workingsets.md)'s rule targets app *state* and
   names blob-section opaque payloads as the anti-pattern. Prose in a
   text-typed section carrying `AspectHumanReadable` is modelled — the model is
   "this is human-readable text". A CBOR blob would not be.

## 9. The ledger

Estimated from the package line counts in §1, assuming the shape settled in
ADR-0168.

| Fate | Lines | What |
| --- | --- | --- |
| Ported | ~1,900 | vault parsing, lint, similarity — the last of these landed on 2026-08-27 as `boxer capmap similar` (ADR-0168 M12), three weeks after this line first claimed it |
| Deferred | 1,070 | the culler's tag-write path |
| Replaced by boxer | ~2,900 | its ClickHouse client, its WHERE-predicate validator and filter builder, its SVG treemap and layout fork, its colour scale, its table and detail renderers |
| Dropped | ~1,750 | the HTTP server, middleware, all 1,221 lines of HTML/JS/CSS, the vendored HTMX, and both SQL files |

The replacements are one-for-one with things boxer already has: `chlocalpool`
and the facts store; play's SQL editor and the nanopass passes; the imzero2
treemap widget, which carries its own squarified layout; the qualitative
palette; play's table and detail panes.

## 10. Corpus data

The vault cannot be committed as it stands. It carries a catalog derived from a
private checkout, whose name must not enter a public tree, and a public
process-framework catalog whose licence has not been checked against the repo's
gate. The boxer catalog — boxer describing its own toolbelt — is the part that
motivates having the tool here at all.

Placement is symmetric with `doc/adr/`, and there is precedent for a
git-ignored directory beneath `doc/`: doclint skips git-ignored trees, and its
own source comments describe exactly that arrangement.

## 11. Questions left open

Each is deferred with a trigger rather than left implicit.

- **`bool` → `boolArray`.** The one remaining promotion candidate. No
  capability data needs it. Trigger: a facts writer with a genuine array of
  booleans.
- **`anchor`'s `text` co-section, and the `symbolArray` low-cardinality
  disagreement.** One question, since both are "make the two schemas agree".
  Trigger: a consumer that wants `wordBag` — the compression-similarity ranker
  is the obvious candidate, since it recomputes exactly that at query time.
- **Graph value-aspects on `foreignKey`.** Using them means changing
  `factsschema` for semantics no consumer reads yet; `AspectLinking` already
  states the linking intent. Trigger: a reader that dispatches on the graph
  aspects.
- **The triage/culling workflow.** A UI that mutates repo files is a distinct
  security posture. Trigger: the read path proving the corpus is worth curating
  at that rate.
- **A treemap panel for play.** The widget exists and had never been wired to a
  play panel; one was proposed for profiles and dropped, on the reasoning that
  an icicle answered the same question while preserving path order. A
  capability map is the opposite case — a size-and-maturity hierarchy where
  path order does not matter — which is the motivation that kill note said
  would be needed. As of 2026-08-05 this is no longer open here: a play Treemap
  panel is being decided in ADR-0166, written concurrently with this survey.

## 12. Composing the two screens from applets — what play and sqlapplet lack

> **2026-08-14.** Measurements and reads in this section are against the tree at
> that date, later than the rest of the page. The question is narrower than
> §9's ledger: taking the prototype's two working screens — its Capability
> Browser and its Culler — as the target, what would a host app need from
> `play` and `sqlapplet` to assemble each one out of embedded applets?

### 12.1 What already works

**Read.** An app can host an applet and draw its own chrome around it:
`sqlapplet.NewEmbedded(def, EmbedConfig{…})` returns a configured
`*play.PlayApp`, and `apps/adhocdemo` — the only embedder in the tree — draws a
top panel of its own and then calls `inner.Render()`. Identity, capability
grants, dataset binding, AutoRun/Live gating and tab attenuation all happen
inside `NewEmbedded`, so an embedder gets the same applet the launcher does.

Also present, and worth naming so the gap list is not read as a longer one than
it is: the treemap's drill, breadcrumb and legend (ADR-0166); markdown-typed
cells (ADR-0123); focus-scoped keyboard capture (ADR-0177); `PlayApp.SetSignal`
for pushing a value into an applet; `RequestRun` and Live re-run.

### 12.2 Placing panes: an applet cannot say where a tab goes

**Read.** play already builds a split layout rather than one leaf of tabs. Tabs
carry a zone — `TabZoneBody`, `TabZoneEditor`, `TabZoneTools`, `TabZoneSide` —
and the renderer splits body-below-editor, side-right-of-body,
tools-right-of-editor. `detail` is registered in `TabZoneSide`, which is why an
applet naming `tabs: [table, detail]` already shows both at once.

**The gap.** An applet's `tabs:` is a flat keep-list, so a document cannot say
*where*: it takes play's per-tab default zone. The Browser screen is
treemap-top-left, detail-right, table-bottom — three placements, and the third
does not exist, since the only above/below split is the editor's and an applet
removes the editor. `TabRegistry.Replace` already accepts a `TabSpec` carrying a
`Zone`, so the mechanism is there and the declaration is not.

- **G1** — a zone in `tabs:` (`treemap@body`, `detail@side`, …).
- **G2** — one more zone, below the body, for the layout the Browser uses.

### 12.3 Several applets in one window

**Read.** `PlayApp.Render()` claims `PanelCentralInside` and opens a `DockArea`
under a fixed id. Two instances in one frame would each claim the central panel,
so the composition "one applet per pane" is not expressible today; `adhocdemo`
hosts exactly one.

- **G3** — a render mode that draws into the caller's current UI scope instead
  of claiming the central panel.
- **G4** — dock-tab identity across instances. play's dock tab ids are package
  constants (`dockTabTable = 3`), so two instances in one window would present
  the same numeric tab ids to egui_dock. Instance-id salting is per-`PlayApp`,
  but vendored crates have bypassed salting before, so this is a risk to
  measure rather than a fact.

Note that G1–G2 and G3–G4 are alternatives, not a sequence: with zones, the
Browser is **one** applet whose panels are its panes, and nothing needs to host
two. G3 is what a screen made of genuinely independent applets would need.

### 12.4 State between panes, and between host and applet

**Read.** `SetSignal(name, value)` publishes through the same path panels emit
on, so a host can push. Nothing exported reads back: `SignalEnvI.Get` is
internal to the package, and `MainSnapshot()` returns the result batch, not the
selection. `PlayApp`'s whole exported surface is 28 methods (**measured**,
`grep -c 'func (inst \*PlayApp) [A-Z]'`) and none of them answers "what is
selected".

- **G5** — a host-side signal read. Without it an embedder cannot know which row
  a Table selected, so it cannot drive a second pane from it, and a button
  inside an applet cannot reach host code at all.
- **G6** — a host-side write for *prelude-bound* params. `SetSignal` reaches
  unbound `{name:Type}` slots only; a knob for a `SET param_catalog` value has
  to be left unbound instead, which also flips the applet to Live.

### 12.5 The knobs

**Read.** play registers three param widgets — `scalarTextWidget`,
`dateTimePairWidget`, `dateTimeRangeWidget` — and one marker comment,
`-- play: ungroup`. So every capmap knob (`catalog`, `level`, `tag`, `size_by`,
`color_by`, `show`) renders as a free-text field, where both screenshots show a
populated dropdown.

- **G7** — an enumerated param widget. ADR-0124 §O4 already contemplated a
  `-- play: range <lo> <hi>` marker, so the marker vocabulary is the established
  route; the values-from-the-data variant the screens use (`All domains`,
  populated by a query) is the harder half.
- **G8** — a reset gesture: both screens have a Reset that returns every knob to
  the document's declared default.
- **G9** — a predicate surface. Both screens carry a free `WHERE` bar, validated
  and canonicalised before it runs. Applets remove the `editor` chrome tab, so
  there is nowhere to type one. boxer has the validating half already — it is
  the same nanopass seam play's editor uses — so the gap is a small read-only
  input an applet can declare, not the machinery behind it.

### 12.6 Panels the screens need and play does not have

- **G10** — a record cursor: an ordered result, a current position, prev/next/
  skip, and a filmstrip of neighbours. Detail *consumes* a selection signal but
  cannot advance one, and nothing else in play holds a position in a result.
- **G11** — a card layout: the Culler's centre pane is a header, two segmented
  meters and three prose columns. Detail renders a row's columns; the meters
  exist as a widget, the composition does not.
- **G12** — treemap depth (the panel's `show` is `drill`/`full`, the Browser's
  is 1–4) and the toolbar's quantile colour scale (the panel has a legend, not
  the min/P25/median/P75/P90/P99/max readout). **Closed 2026-08-27**, §12.12.

### 12.7 Actions, and the write path

**Read.** `fsbroker.Service.handleWrite` writes the request payload to the one
path its handle was granted, and only for `HandleModeWrite`. `fs.dialog.bundle`
("pick folder", ADR-0026 §SD3) mints a handle with no path-relative operation,
so a granted folder buys nothing today. The same model is why mdedit "is not
told which file — the Powerbox hands it a handle, never a path", which is also
what makes the Browser's *Open in Obsidian* link unbuildable as drawn.

- **G13** — an action seam: a way for a panel gesture to reach host code. Today
  a panel can emit a signal and no host can read it (G5), so this is G5's
  consequence rather than a separate mechanism.
- **G14** — a vault-scoped write. Tagging one of well over a thousand
  competences per keystroke cannot go through a per-file save dialog. This is the decision ADR-0168
  deferred, and it is the only gap on this page that is a *policy* question
  rather than a missing feature.

**2026-08-14 — G13 and G14 are out of scope, decided.** The mutation surface is
the CLI: `boxer capmap load` and `dump` move the corpus between the vault and
the store, and editing stays where SD3 puts it. So no in-app write path is
built, no broker operation is added, and the *Open in Obsidian* link is dropped
rather than approximated — the Powerbox hands an app a handle and never a path,
so there is nothing to hand an editor. Everything above G13 stands.

### 12.8 What this implies for the shape

Splitting the list by screen makes the sequencing obvious, and the two halves
are unequal.

**The Browser is an applet.** G1, G2, G7, G8, G9, G12 — placement, enumerated
knobs, a reset, a predicate bar, treemap depth. None of them needs multi-applet
hosting, none is a new subsystem, and each is useful to every other book in the
tree. The lens documents themselves barely change.

**The Culler is not.** G5, G10, G11, G13, G14 — a cursor, a card, an action seam
and a write path — describe an app that *embeds* an applet for its list and
draws its own review surface, which is `adhocdemo`'s shape with a real screen in
place of its Regenerate button. Trying to express it as a SQL document would
mean putting a mutation behind a lens whose whole contract is that it reads.

### 12.9 What has since been built

**2026-08-14 — G1, G2, G7 and G8 are done**, in play and sqlapplet, and the
competence book uses all four.

| Gap | What landed |
| --- | --- |
| G1 | A `tabs:` entry is `<panel>[:<node>][@<zone>]`; `body`, `side` and `bottom` may be named (ADR-0132 Update 2026-08-14) |
| G2 | `TabZoneBottom`, split before the side zone so it spans the full width, plus `TabRegistry.SetZone` (ADR-0097 Update 2026-08-14) |
| G7 | `-- play: enum <slot> <value>[=<label>][,…]` and a dropdown widget ahead of the scalar tail (ADR-0124 Update 2026-08-14) |
| G8 | Reset, restoring the values the buffer was *loaded* with — captured at install, never recomputed from a prelude the knobs rewrite |

Two of the four turned out smaller than this survey implied, and one turned out
to have a boundary worth naming. G1 and G2 are one change: play already built a
split layout out of zones, and the missing half was a document's ability to name
one. G7's *declared* options are a day's work; the values-from-the-data variant
the screenshots actually show — `All catalogs`, populated by a query — is a
different feature with the same syntax, and is not built.

What that leaves for the reading screen is G9 (a predicate input) and G12
(treemap depth, and the toolbar's quantile readout).

### 12.10 G9 arrived from somewhere else, and the layout arrived for free

**2026-08-15.** Two more of the list are settled, and neither was done as capmap
work.

**G9 is built, under [ADR-0187](../adr/0187-play-sql-expression-parameters.md).**
That ADR was proposed and had M0–M4 land the same day: a single-line SQL field
in `widgets/sqleditor`, three grammatical categories, the `-- play: expr`
directive, a `splice-expr` rewrite step at order `-90`, two tiers for where a
filled expression's text lives, and the class ceiling. A `{p:Expr}` slot in a
`WHERE` is the free predicate bar this section asked for, and it is a general
play feature rather than a capmap one, which is what §12.8 predicted about this
half of the list.

The applet arm is the half that mattered here, and it is the safety one. The
splice lives in `play.Client`, which an applet shares, so before M4 a document
declaring an expression slot would have had it substituted and executed against
its **mint-time** classification only — and a spliced `url(…)` is egress from a
surface whose whole contract is that it reads. M4 re-classifies the substituted
body and, in an applet, refuses a raise with the witness. No document in any
book declares an expression slot yet, so nothing had ever triggered it; that is
why it was a latent hole rather than a live one.

**The Browser's layout needed no new host feature.** G1, G2 and the
`<panel>:<node>` tab form were enough: one document, `tabs: ["treemap:nodes",
"detail@side", "table@bottom", "network@bottom"]`, with the Treemap tab bound to
a `nodes` CTE and the Table tab to the query's own result. ADR-0168 §M11 records
what it cost. So of §12.8's six items for the reading screen, five are done and
G12 is the remainder — treemap depth, and the toolbar's quantile readout.

### 12.11 The four remaining lenses are lenses over empty columns

**2026-08-15, measured against the boxer catalog** under `doc/competences/` (79
competences; in-tree but git-ignored, ADR-0168 §SD7) rather than against the
reference vault, because it is the corpus anybody running this actually has.
Each of the prototype's remaining views returns **zero rows**:

| View | Predicate | Why it is empty |
| --- | --- | --- |
| `capabilities_gaps` | `maturity != 255 AND pain != 255` | all 79 carry `255`/`255` |
| `capabilities_stale` | `lifecycle_changed_at != 0` | no note carries `lifecycle:` |
| `capabilities_orphans` | `level > 1 AND length(parent_ids) = 0` | 78 of 79 have exactly one parent; the level-1 root has none, and the predicate excludes it |
| `capabilities_multi_parent` | `length(parent_ids) > 1` | the maximum parent count in the catalog is 1 |

There is no broken-parent variant hiding underneath either: all 22 distinct
`parent_ids` targets resolve to competences that exist. Two of the three
provider columns the views want fall the same way — `lifecycle_current` and
`days_since_change` both derive from the `lifecycle_*` arrays, which are
already exposed and already empty, and both are expressible in SQL over them
without a new column. The third, `words`, is real signal and landed with §M11.

ADR-0168 §Verification plan turns this into a decision rather than a backlog:
the corpus stays unassessed, so these four are not ported.

### 12.12 G12 is closed, and the ledger's last line is true

**2026-08-27.** The two things left on the reading screen's list landed in the
play Treemap panel (ADR-0168 M13), as §12.8 predicted they would — neither is
about competences, and every book with a treemap tab gets them. The nesting bar
is a ladder, `drill` · `3 deep` · `4 deep` · `full`, the middle rungs named by
the levels they show below the frontier; and the numeric colour legend carries,
under the bar, where the described colours sit — min, quartiles, P90, P99 and
max in the channel's unit, over how many cells said anything. The readout is
surveyed client-side from the built tree, so it costs no query and is never a
frame behind the picture. One boundary, worth naming: the Browser's colour is a
category (the level-2 branch), so it gets the swatch key, and the numeric
readout is what a book whose colour is a measure sees — a document cannot
switch a column between a string and a number by parameter, since the panel
classifies `color` by its Arrow type.

The same day closed the one line in §9's ledger that was not yet true. The
ledger counted the prototype's similarity ranker as *ported* when it was only
*read*: the parser accepted `similar:` frontmatter and the encoding carried its
score, but nothing in-tree produced one. `boxer capmap similar` (ADR-0168 M12)
does, over the zstd raw-dictionary approximation `analytics/similarity/compression`
already uses, and writes the result into the notes through a node-tree edit of
the frontmatter rather than a re-render, so a run changes one stanza per note
and nothing else. Measured on the reference corpus, an all-pairs pass over
~1,600 written notes is about a million comparisons and under two minutes of
wall clock on a workstation; the prototype, which built a fresh compressor per
pair and compared only the directory-backed notes, was not measured at that
scale because it could not reach it.

With that, §12.8's split holds as written: the Browser is an applet and has
everything it asked for; the Culler is not, and ADR-0168 §Deferrals settled that
it is not built.
