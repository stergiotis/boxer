---
type: explanation
audience: package maintainer
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as
> authoritative.

> **Provenance.** Status-quo measurement plus design-space survey, compiled
> 2026-08-01, ahead of any decision. Three tiers, marked throughout: (a)
> claims about this repository were measured against the working tree on the
> compile date — the registry counts in §2 come from a throwaway probe that
> booted the real registry, not from grepping; (b) the desktop-entry
> measurements in §5.1 were taken from `/usr/share/applications` on one
> Fedora workstation and are a sample, not a census; (c) the external prior
> art in §5.2–§5.5 comes from the sources listed in §11, read for this
> survey but summarised rather than quoted. Nothing here is a decision.

# Organizing and launching keelson apps

## 1 Question and scope

The registry has grown past the point where one flat category list carries
it. This page measures where it actually stands, sets out what "organize"
is being asked to do, surveys how launchers elsewhere answer it, and costs
the options — including the one the title of the prompting question asks
about directly: **is a tagging/labelling system necessary?**

Scope is *finding and starting* an app: the launcher surfaces, the manifest
metadata behind them, and the CLI selection path. Out of scope: what happens
after an app opens (windowing, workingsets, composition — the last of these
is [app-composition-survey.md](./app-composition-survey.md), which asks a
different question about the same objects).

## 2 The status quo, measured

### 2.1 The inventory

Booting the registry with the applet corpus minted gives **34 registrations
across 6 categories**:

| Category | Count | What is in it |
| --- | --- | --- |
| Applets | 13 | every ADR-0132 SQL applet, from all three books |
| Tools | 8 | godepview, play, imztop, imzrt, splashscreen, sqlappletcreator, regex explorer, sccmap |
| Demos | 6 | capdemo, taskdemo, idsshowcase, leewaywidgets, logdemo, widget gallery |
| Runtime | 5 | adhocdemo, capinspector, configview, helphost, logviewer |
| Data | 1 | fibscope |
| Science | 1 | terrainscope |

Two things stand out. **The largest bucket is 38% of the registry**, and it
is the only one that grows without a Go file being written — an applet is a
committed markdown document, so the corpus grows at authoring speed. The 13
break down as 6 topology + 4 Go-dependency + 3 keelson-introspection. And
**two buckets hold one app each**, which is what a classification scheme
looks like when the axis does not fit the population.

### 2.2 One classification field

`app.Manifest` (`public/keelson/runtime/app/manifest.go`) carries exactly
one classification slot:

```go
// Category groups apps in interactive shells. Empty means "uncategorised".
Category string
```

Single-valued, free-form, no registered vocabulary, no validation. The
launcher's ordering preference is a hardcoded list in the window host
(`preferredCategoryOrder = ["Runtime", "Tools", "Demos"]`), with unknown
categories sorted alphabetically after it and empty ones collected into an
`"Other"` bucket. Nothing rejects a typo'd category; it becomes a new
bucket.

### 2.3 Two launcher surfaces, deliberately unequal

- **`Apps ▾` menu** — per-category submenus, alphabetical within each. **No
  search**, for a recorded reason: egui's `menu_button` closes on any click
  outside a menu `Button`, focus clicks on a `TextEdit` included, so an
  in-bar field fought the menu.
- **Empty-state pane** — shown when no windows are open. Has the search
  field. Empty query renders the same category sections; a non-empty query
  flattens to a single list.

So the surface a user reaches *while working* has no search, and the surface
with search is the one that disappears as soon as anything is open.

### 2.4 What search matches

`filterManifests` is a case-insensitive substring test over **`Display` and
`Category` only** — deliberately, with `Id` excluded (every entry would
match "github") and `Title` excluded as a longer-form variant. Consequences,
none of them bugs but all of them limits:

- No fuzzy or subsequence matching: "gdv" does not find "Go dependency
  explorer", and "dependency" does not find `godepview` by its id.
- No synonym channel. Typing "processes" finds nothing, though
  *Component processes* exists; typing "cpu" or "memory" finds neither
  imztop nor imzrt.
- The prose is not searched, though 18 of the 34 registrations carry a
  `Help` corpus — all 13 applets plus capinspector, helphost, logviewer,
  godepview, and play.

### 2.5 The headless path is already data-centric — the GUI path is not

The CLI selects apps by **SQL predicate over the manifest relation**:

```sh
imzero2 demo --launch play                              # sugar for subject_alias = 'play'
imzero2 demo --launch "subject_alias IN ('play','widgets')"
imzero2 demo --launch "category = 'tools'"
```

`--launch` is a WHERE-clause body; the runtime wraps it as
`SELECT id FROM table WHERE <expr>` and evaluates through `clickhouse-local`
(with a bare-alias fast path that skips CH entirely for the appliance boot
case). The same inventory is also an introspection table,
`keelson('apps')` — 16 columns including `category`, `launch_kind`,
`workingset`, `has_help`, `caps`, `registration`.

This is the sharpest asymmetry in the status quo. **Headless selection is a
query over data; interactive selection is a hardcoded tree plus a substring
test.** The repository's data-centricity invariant points one way and the
launcher goes the other.

A related duplication: the inventory is serialised by *two* schemas that
have drifted — `keelson('apps')` (`preferred_width`, `caps` as a string
list) and the carousel's `listSchema` (`pref_w`, `caps` as a count, plus
`subject_alias` and `legacy_code`, which the introspection table lacks).
Any new classification field would have to be added to both, in two
spellings.

### 2.6 Grouping information that already exists and is discarded

`sqlapplet.MintManifests` builds one manifest per applet document. The
`AppletDef` it mints from carries `BookID` — `"sqlapplet"`, `"topology"`,
or `"godep"` — the book that contributed the document. `manifestFor` does
not copy it:

```go
Category: "Applets",   // hardcoded, identical for all 13
```

The three-way grouping is authored, committed, and thrown away at mint time
because the manifest has one slot and it is already spent. The applet
frontmatter carries `type:` and `audience:` too (help metadata, currently
read by the help facility rather than the launcher), so there are further
per-applet signals with nowhere to land.

### 2.7 What is absent entirely

No recents. No pinning or favourites. No usage history: `keelson('windows')`
reports live windows but carries no open-time, and nothing records that an
app was ever launched. No per-user ordering of any kind — the list is the
same on frame one as on frame one million.

## 3 Three jobs, one field

"Organize the apps" is three separable jobs, and conflating them is what
makes the single `Category` field feel simultaneously over- and
under-powered:

- **Placement** — where an entry sits when the user is *browsing* a
  structure. Wants few buckets, stable order, exactly one home per entry.
- **Retrieval** — what the user can *type* to reach an entry they already
  have in mind. Wants many aliases per entry, no structure at all,
  duplicates harmless.
- **Ranking** — which entries come *first* when several match. Wants
  observed behaviour, not authored metadata.

`Category` does placement adequately, is pressed into service for retrieval
(it is half the search predicate), and does nothing for ranking. The two
one-app buckets in §2.1 are a placement symptom; the "processes"/"cpu"
misses in §2.4 are a retrieval symptom; the absence of any recents list is
the ranking gap.

## 4 Diagnosis: the axis is provenance, not subject

The `Applets` bucket does not describe what those 13 things are *for*. It
describes *how they were built* — a markdown document rather than a Go
package. The consequence is concrete and testable:

> The Go dependency explorer (`Tools`), the four Go-dependency applets
> (`Applets`), and the repo code-exploration demo (`Tools`) are five ways of
> looking at the same subject. No launcher view groups them, and no search
> query returns them together, because their category records the
> implementation technique instead of the topic.

The same holds for topology: six applets read the ADR-0126 topology tables;
`capinspector` and `imztop` look at adjacent runtime structure; they sit in
three different buckets. Meanwhile `Tools` has become the bucket for "a Go
app that isn't a demo and isn't runtime plumbing" — it holds a SQL IDE, a
process monitor, a splash screen, and a regex tester.

This is worth stating plainly because it changes what the fix is. If the
problem were *too many entries*, the answer would be more buckets or
deeper nesting. The problem is that **one axis is being asked to carry
several independent ones** — provenance (Go app / SQL applet / demo),
subject (Go deps / topology / observability / geo), and maturity (demo /
tool / runtime service) are all real and all orthogonal, and any single
string can encode only one at a time.

## 5 Prior art

### 5.1 The desktop-entry answer: two fields, not one

freedesktop's Desktop Entry Specification splits precisely along the §3
seam:

- **`Categories`** — "a list of strings used to classify menu items". It is
  **multi-valued**, drawn from a registered vocabulary, with an `X-` prefix
  as the extension escape hatch. The spec's guidance is to list every
  category that clearly applies and none that only vaguely applies.
- **`Keywords`** — a separate, semicolon-separated, translatable list added
  *specifically* for search. GNOME added it because matching on `Name`
  alone was not enough and `Comment`/`Description` were the wrong shape.

Measured on one Fedora workstation's `/usr/share/applications` (215 entries;
tier (b) — a sample, not a census):

| | Entries | Distinct tokens | Mean tokens/entry |
| --- | --- | --- | --- |
| `Categories` | 182 | 67 | 2.96 |
| `Keywords` | 133 (62%) | 667 | ≈5 |

Two numbers do the arguing. **Real entries carry ~3 categories each, not
one** — an ecosystem that had a single-valued category field would have had
to pick, and the fact that nobody had to is why the menus work. And the two
layers behave completely differently: 67 category tokens across 182 entries
is heavy reuse (a working controlled vocabulary), while 667 keyword tokens
across 133 entries is near-unique-per-entry (a free-text retrieval aid that
nobody governs, because nobody has to — keywords are never *displayed as
structure*).

That last clause is the whole reason the split is cheap. Vocabulary
governance is only owed on terms a user *sees and navigates*. Terms that
exist solely to be matched against can be as messy as their author likes.

### 5.2 The launcher school: type-first, categories demoted

Spotlight, Alfred, Raycast, and the editor command palettes converge on the
same shape: a single text field is the primary surface, results are ranked
rather than grouped, and category survives only as a *scope prefix* or a
tiebreaker label. Alfred's identity is that fuzzy matching plus learned
ordering gets you there in three keystrokes; Raycast is the same idea with a
richer action layer. None of them asks the user to navigate a category tree
as the main path.

Note the fit with §2.3: this repository's search-bearing surface is the one
that vanishes when a window opens, which is the exact inverse of the
convention.

### 5.3 Ranking: frecency

The ranking layer these launchers rely on is *frecency* — frequency and
recency folded into one score. The mechanics are well-trodden: Firefox's
Places system uses exponential decay with roughly a one-month half-life
(a visit a month old is worth half a fresh one); `fre` decays per-item
weights exponentially so ordering shifts smoothly rather than in steps;
`fzf`'s frecency option exposes the half-life directly, with the guidance
that short half-lives suit fast-moving work and long ones suit reference
material.

The relevant property for this repository is that **frecency needs no
authored metadata at all**. It is derived from observed launches, so it
costs no curation and cannot drift out of date — it is the one layer that
gets *better* as the corpus grows rather than worse.

### 5.4 Facets beat deeper hierarchy

The information-architecture literature is consistent that faceted
classification — several shallow independent dimensions — navigates better
than one deep hierarchy, precisely because each facet considers one aspect
and does not have to anticipate combinations. Hierarchies force the designer
to pick the one axis users will think along; facets let the user pick. The
practical pattern in e-commerce and elsewhere is hybrid: a shallow hierarchy
for the top-level browse, attributes for narrowing.

Applied here: the fix for §4 is *not* `Applets/Go`, `Applets/Topology`,
`Tools/Go`. That nests provenance above subject and re-commits the same
error one level down.

### 5.5 Where free-form tagging goes wrong

The folksonomy literature catalogues the failure modes: synonymy, polysemy,
word-form variation, inconsistent description depth, misspellings, and
single-use tags meaningful to nobody. There is no way to hold consistency
over time without a thesaurus. The recurring remedy is hybrid — a controlled
vocabulary for the navigable structure, free tags underneath.

One qualification matters for this repository, and it cuts both ways. Most
of that literature studies *many taggers disagreeing*. A single-curator
corpus does not have that problem. What it does have is the other one:
**drift across time**, where the same curator picks `geo` in spring and
`geospatial` in autumn, with nothing to notice. So the folksonomy risk here
is smaller than the literature implies, but it is not zero, and the thing
that would contain it (a registered vocabulary with a validation gate) is
the same thing either way.

## 6 So — is a tagging system necessary?

Splitting the question is the whole answer.

**Is one string enough? No — demonstrably.** §2.1's two singleton buckets,
§2.6's discarded book id, and §4's five-Go-tools-in-three-buckets are three
independent symptoms of one field carrying three axes. This part is not a
judgement call; the evidence is in the tree.

**Does the fix have to be free-form tags? No.** Three distinguishable
mechanisms often get bundled under "tagging", and they have very different
cost/benefit here:

| Mechanism | Job (§3) | Verdict | Why |
| --- | --- | --- | --- |
| Multi-valued **controlled** category list | placement | **Warranted** | Fixes §4 directly; freedesktop's evidence is that ~3/entry is the natural multiplicity; a closed set means it can be validated at `Register` and the launcher's bucket list stops being a surprise. |
| A **keywords / synonym** field | retrieval | **Warranted, and cheapest** | Fixes §2.4 with no vocabulary decision at all, because keywords are never rendered as structure (§5.1). No governance owed. |
| **Free-form user tags** (folksonomy) | — | **Not warranted now** | At n=34 with one curator it buys what keywords already buy, and adds the §5.5 drift risk plus a governance surface. Revisit if runtime-authored applets (ADR-0132 "O4") make the corpus grow outside review. |
| **Frecency / recents / pins** | ranking | **Warranted, orthogonal** | Needs no metadata, so it composes with any of the above and is the only layer that improves with corpus growth. |

So: *labelling* yes, *tagging* — in the free-form folksonomy sense — no, not
yet. And the labelling that is warranted splits into two fields with
different rules, not one field used two ways.

**The scale caveat, stated honestly.** 34 entries is not a large inventory,
and none of this is urgent on today's numbers. What makes it worth deciding
now rather than at 80 is that the growth is *asymmetric*: applets grow per
committed markdown file while the schema decision (`Manifest` is exported
`public/` API, consumed by two serialisation schemas and two launcher
surfaces) gets more expensive to change with every consumer added. The
metadata decision wants to lead the corpus, not trail it.

## 7 Options, costed

Criteria: **C1** survives corpus growth · **C2** owes no vocabulary
governance · **C3** consistent with data-centricity (one relation, both
paths) · **C4** one place to change when the vocabulary moves · **C5** cost.

| | C1 | C2 | C3 | C4 | C5 |
| --- | --- | --- | --- | --- | --- |
| **O1** Status quo | ✗ | ✓ | ✗ | ✓ | — |
| **O2** Better retrieval: `Keywords` + fuzzy match + search the help corpus | ✓ | ✓ | ~ | ✓ | low |
| **O3** Multi-valued controlled `Categories` (facets), replacing the single string | ✓ | ✗ registered set | ~ | ✓ | medium |
| **O4** Free-form tags | ~ | ✗ drift | ~ | ✗ | medium |
| **O5** Frecency + pins over a launch fact | ✓ | ✓ | ✓ facts | ✓ | medium |
| **O6** SQL-native launcher: the GUI filter becomes a predicate over the same relation `--launch` already queries | ✓ | ✓ | ✓ | ✓ | high |

Notes on the two that need them. **O3**'s C2 cost is real but bounded — a
registered set of perhaps a dozen tokens with an `X-`-style escape hatch,
validated at `Register`, is the freedesktop shape and it has held for two
decades. **O6** is the option the repository's own invariant argues for and
the one §2.5 exposes as missing; it is also the one that would let the two
drifted schemas of §2.5 collapse into one. Its cost is that a GUI filter
box cannot shell out to `clickhouse-local` per keystroke, so it needs either
the in-process introspection engine on the query path or a deliberately
narrower predicate language — which is a genuine design question, not a
detail.

## 8 A layered shape, if one is wanted

Ordered so each layer is independently useful and none blocks the next:

1. **Retrieval first.** Add a `Keywords []string` to `Manifest`; extend the
   match to fuzzy/subsequence over `Display` + `Keywords` (+ optionally the
   help corpus body); carry the applet `BookID` and frontmatter `type:` into
   keywords so §2.6's discarded grouping starts paying. No vocabulary
   decision, no ADR-worthy contract change beyond the field itself. This
   alone answers most of §2.4.
2. **Placement second.** Decide the facet question: either promote
   `Category` to a multi-valued registered list, or keep it single-valued
   for placement and add a separate registered `Facets`/`Topics` list.
   Either way the vocabulary becomes a named registry — which is what makes
   this Tier-1 (§9).
3. **Ranking third, and only if measured.** A launch record in `boxer.facts`
   (the ADR-0148 workingset write path already writes per-app records on
   close, so the machinery exists) plus frecency ordering and explicit pins.
   Worth doing when the list is long enough that ordering matters; at 34 it
   plausibly is not yet.
4. **Fold the surfaces.** Whatever lands, the `Apps ▾` menu and the
   empty-state pane should read the same predicate — today they share a
   grouping function but only one of them can search (§2.3).
5. **Record the free-tag deferral** rather than leaving it implicit, with
   the ADR-0132 "O4" runtime-authoring trigger named as the condition that
   would reopen it.

## 9 What this touches

This is a **Tier-1** change under CODINGSTANDARDS § Design Before Code — an
ADR before code — on two independent counts: `app.Manifest` is exported Go
API under `public/`, and a registered category vocabulary is a *named
registry*. An inventory of what moves with it:

| Surface | Change | Moves with it |
| --- | --- | --- |
| `app.Manifest` | new field(s) | every registration site (21 `app_register.go` + the applet minter) |
| `Manifest.Validate` | vocabulary gate | registration-time failures become possible where none were |
| `keelson('apps')` | new column(s) | ADR-0094 provider, its tests, any applet querying it (`runtime-apps.md` does) |
| carousel `listSchema` | new column(s), second spelling | `--launch` predicates; column order is documented as stable |
| `windowhost` launcher | grouping + filter | `preferredCategoryOrder`, `groupByCategory`, `filterManifests`, both render paths, the category tests |
| sqlapplet frontmatter | new key(s) | 13 committed documents, the authoring how-to, the store's validation gate |
| `boxer.facts` (only if §8.3) | launch records | facts schema — Tier-1 on its own |

## 10 Open forks for the dialogue

1. **One multi-valued field or two single-purpose ones?** freedesktop puts
   toolkit, desktop environment, main category, and sub-category all in one
   `Categories` list and lets consumers filter. Splitting `Category`
   (placement, one value) from `Topics` (subject, many) is more explicit and
   less flexible. This is the central fork.
2. **Who owns the vocabulary?** A Go `var registeredCategories` is the
   cheapest and matches ADR-0009's env-registry precedent. A facts-backed
   vocabulary is more data-centric and much heavier.
3. **Does `Applets` survive at all?** If subject facets land, provenance
   might be better as a boolean-ish `source` column (queryable, not
   displayed) than as a bucket the user browses.
4. **Is O6 the real destination**, with O2/O3 as the increments toward it —
   or is a predicate box the wrong affordance for a launcher regardless of
   how well it fits the invariant?
5. **How much prose should search reach?** Applet help books are
   substantial; indexing them makes search excellent and makes the ranking
   question urgent at the same time.
6. **Does any of this wait for growth?** A defensible answer is "add
   `Keywords`, fix the fuzzy match, and revisit facets at 60 registrations".
   §6's asymmetry argument is the counter, not a refutation.

## 11 Sources

External material consulted for §5 (tier (c) — summarised, not quoted):

- [Desktop Entry Specification](https://specifications.freedesktop.org/desktop-entry/latest-single/)
  and [Desktop Menu Specification — extensions to the desktop entry format](https://specifications.freedesktop.org/menu-spec/1.1/desktop-entry-extensions.html)
  — the `Categories` field, its registered vocabulary, and the `X-` prefix rule.
- [GNOME Goal: add keywords to application desktop files](https://wiki.gnome.org/Initiatives/GnomeGoals/DesktopFileKeywords)
  and [Writing a Search Provider](https://developer.gnome.org/documentation/tutorials/search-provider.html)
  — why `Keywords` was added as a field distinct from `Categories`.
- [Frecency](https://en.wikipedia.org/wiki/Frecency) ·
  [Firefox URL-bar ranking](https://firefox-source-docs.mozilla.org/browser/urlbar/ranking.html) ·
  [`fre` — command-line frecency tracking](https://github.com/camdencheek/fre) ·
  [fzf frecency discussion](https://github.com/junegunn/fzf/discussions/4543)
  — decay mechanics and half-life guidance.
- [Faceted search](https://en.wikipedia.org/wiki/Faceted_search) ·
  [NN/g — Taxonomy 101](https://www.nngroup.com/articles/taxonomy-101/) ·
  [Hedden — faceted classification and faceted taxonomies](https://www.hedden-information.com/faceted-classification-and-faceted-taxonomies/)
  — facets vs deep hierarchy.
- [Folksonomies: why do we need controlled vocabulary?](https://www.webology.org/2007/v4n2/editorial12.html) ·
  [TaxoFolk — a hybrid taxonomy-folksonomy classification](https://www.researchgate.net/publication/233701317_Taxo_Folk_A_hybrid_taxonomy-folksonomy_classification_for_enhanced_knowledge_navigation)
  — folksonomy failure modes and the hybrid remedy.
- Launcher comparisons (Spotlight / Alfred / Raycast) were read as
  background for §5.2; they are consumer reviews rather than design
  documents, and no specific claim rests on them alone.

## References

- [ADR-0026 — app runtime and capability subjects](../adr/0026-app-runtime-and-capability-subjects.md)
  — the `AppI`/`Manifest`/`Registry` foundation this survey's subject lives in.
- [ADR-0132 — sqlapplet, SQL-defined applets](../adr/0132-sqlapplet-sql-defined-applets.md)
  — §SD2 mints one manifest per document under `Category: "Applets"`, and
  records the gallery fallback "if it ever grows past what the Apps menu
  comfortably lists". §2.1 is that trigger being approached.
- [ADR-0094 — introspection tables](../adr/0094-keelson-introspection-tables.md)
  — `keelson('apps')`, the inventory-as-relation this survey leans on.
- [ADR-0135 — app launch requests](../adr/0135-app-launch-requests.md)
  — `windowhost.open` and `LaunchKind`, the argument-carrying open path.
- [ADR-0148 — app workingsets](../adr/0148-app-workingsets.md)
  — the per-app record write path a launch-history fact would follow.
- [app-composition-survey.md](./app-composition-survey.md)
  — the adjacent question: what to do with apps once several are open.
