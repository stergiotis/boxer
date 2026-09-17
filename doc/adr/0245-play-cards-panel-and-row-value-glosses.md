---
type: adr
status: accepted
date: 2026-09-17
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-17
---

# ADR-0245: a `play` cards pane — a paged grid of cards from a query result, each slot glossed per row

**In one paragraph:** a new `play` result pane ("Cards") renders a result set
as a paged, responsive grid of uniform cards — hero, overline, title, subtitle,
body, facts, tags, footer — over a new `widgets/cardgrid` widget. A query names
the slots with `card_*` columns; every other column becomes a fact on the card
through its ordinary gloss resolution (ADR-0186). Because one result mixes
kinds — a picture on one card, a recording on the next — a slot's gloss may be
a **row value**: a companion column `<label>_gloss` holding the media type of
that row's value, which the Table, Detail and Chat read too. The catalog gains
`audio/wav`, whose block face is a static waveform with play/pause, so an
audio sample's hero is its waveform.

## Context

`play` shows a result as a grid of cells (Table), one row in depth (Detail), or
through a shape-specific pane (Timeline, Kanban, Chat, …). None of them answers
"show me the things": a result whose rows are *items with a face* — image
samples, recordings, documents, products — reads in the Table as a column of
`[image/png · 359 B]` descriptors and in Detail one item at a time. The
request: card-shaped items in a paged or scrollable view, with a title, a hero
"and so forth" laid out the way card UIs are, the hero and most other fields
carrying full gloss and media-type support, special care for wide and
otherwise unwieldy input, and a model that is natural to write in SQL. The
named test case is a mixed set of image and audio samples, the audio's hero
being its waveform.

Substrate facts that shape the design:

- **The faces exist.** The gloss catalog (ADR-0123, ADR-0186) renders a value
  as an inline face (one line, no cache behind it) or a block face (markdown,
  code, image, …). play's `glossBlock` already serves block faces to a pane
  that is not Detail, from an artifact cache that holds many rows — the Chat
  pane's arrangement (ADR-0239 (proposed) §SD6).
- **A gloss binds once per column.** ADR-0186 §SD1 binds parameters per
  column, not per cell, and every route to a binding — alias, directive, rule
  set, affinity — is keyed on the column. Nothing lets row 1 say `image/png`
  and row 2 say `audio/wav` for the same column.
- **Stored media is self-describing by a sibling column.** A table that keeps
  blobs of several kinds keeps a `mime` column beside the content column. The
  per-column alias can only reach that shape through one `if(mime = …, content,
  NULL)` projection per kind the author remembered to list.
- **There is no audio gloss.** ADR-0208 built `science/audio` and the
  `waveform.Player`, and tally plays recordings through it — by staging the
  bytes into a sealed file or a memfd and opening a `track` with a background
  peaks build and a window cache. That is the right weight for one open
  recording and the wrong one for a page of forty-eight heroes. The native WAV
  reader, however, takes an `io.ReaderAt`, so a WAV held in a cell needs no
  staging at all.
- **Detail uploads what it decodes.** The image block face retains and ships
  full-resolution pixels, boxed at draw time. One row makes that harmless; a
  page of cards multiplies it by the page size.
- **`Label` truncates to one line or not at all.** The bindings carry
  `Truncate()` and wrapping, and no row limit.
- **"Card" is taken.** The leeway Detail card (`CardDriver`,
  `Table2CardEmitter`, "the card path") already owns the word in `play`'s
  code. The pane is titled *Cards* because that is what a user calls it; the
  widget is `cardgrid` and the driver `CardGridDriver`, so the two do not
  differ by one letter.

Card anatomy is settled practice rather than something to invent — Material's
and Polaris's card, Apple's content cells and every gallery since agree on the
order *media, overline, title, subtitle, supporting text, metadata, actions*,
on a fixed media aspect so a grid keeps its rows, and on clamping text rather
than letting one item stretch the row. The decisions below are about mapping
that anatomy onto a result set and onto imzero2, not about the anatomy.

## Design space (QOC)

**Question.** How does one slot of one card learn how to render its value,
when the kinds vary by row?

**Options.**

- **O1 — Alias only, coalesce by label.** Several columns may share a gloss
  label — `` `card_hero@image/png` ``, `` `card_hero@audio/wav` `` — and the
  slot takes the first non-null. The Chat pane's "one column per attachment
  kind" (ADR-0239 (proposed) §SD1).
- **O2 — A row-value companion, cards only.** `card_hero_gloss` holds the
  row's media type; only the Cards pane reads it.
- **O3 — A row-value companion, general.** For any column labelled `L`, a
  text column `L_gloss` holds per-row media types; the Cards pane reads it
  first, and the grids, Detail and Chat follow in a later milestone.
- **O4 — One structured column per card.** A `Tuple` / `Map` / JSON `card`
  column carrying slots and types together.

**Criteria.**

- **C1 — Per-row mixtures**, including a kind the query author did not list.
- **C2 — Fit to stored `(content, mime)` data**: projections needed per query.
- **C3 — Loud failure**: an unknown or misspelt type surfaces on the card.
- **C4 — Pane coherence**: selecting a card shows the same rendering in Detail.
- **C5 — Cost to ADR-0186's bind-once-per-column.**

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −  | ++ | ++ | ++ |
| C2 | −− | ++ | ++ | −  |
| C3 | −− | ++ | ++ | +  |
| C4 | ++ | −− | +  | −− |
| C5 | ++ | +  | −  | −− |

O1 fails the requirement itself: a kind with no projection yields a NULL in
every listed column and the hero silently vanishes. O4 is self-describing but
play cannot read a struct or a map through the gloss cell accessor, and SQL
authors would build the tuple by hand. O2 meets the request and leaves Detail
showing the selected card's hero as a hex blob. O3 pays for coherence with a
second binding tier, bounded as SD2 describes.

## Decision

We will add a **Cards** result pane over a new **`widgets/cardgrid`** widget,
claimed by `card_*` column names matched on the gloss label, with unclaimed
columns rendered as facts through their glosses; add **row-value glosses**
(`<label>_gloss`, O3) as a binding tier above the alias; and add **`audio/wav`**
to the catalog's content family with a static-waveform block face and
play/pause. No IDL, opcode or Rust change.

### SD1 — Panel contract: `card_*` slots, matched on the gloss label ✓

One required channel (`main`). The pane claims a schema carrying at least one
of `card_title` and `card_hero`, by name, the name being the gloss label so
`` `card_body@text/markdown` `` claims `card_body` (ADR-0239's rule).

| column | meaning | rendering |
| --- | --- | --- |
| `card_hero` | the item's face | block face in a fixed-aspect box (SD4) |
| `card_overline` | kind, category, source — the eyebrow | one line, small, weak |
| `card_title` | the item's name | up to two lines, strong |
| `card_subtitle` | second line | one line |
| `card_body` | supporting text | block face when it has one, else wrapped text; clamped |
| `card_tags` | `Array(String)` or a string | badges, one line, `+n` overflow |
| `card_tone` | a design-system token, per row | accent edge on the card |
| `card_footer` | timestamp, provenance | one line, small, weak |
| `<label>_gloss` | per-row media type of `<label>` (SD2) | — |

The six slots that render through a gloss — hero, overline, title, subtitle,
body, footer — may carry a companion; `card_tags` and `card_tone` are
vocabularies of their own, and a companion for one of them is a reject.

No type requirement on the text slots, for ADR-0122 §SD1's reason: they are
read through the gloss cell accessor, which is total. `card_tone` takes the
token vocabulary ADR-0122 §SD2 resolves; being a row value it cannot be
checked in `AcceptForChannel`, so an unknown token draws neutral and is
counted in the status line.

**The prefix is a reserved namespace.** An unknown `card_*` column — 
`card_titel` — is a rejection naming the column and the slots, as is a slot
claimed twice. That is what the prefix buys over Kanban's bare `title`: the
contract can tell a typo from a fact.

**Every other column is a fact.** Unclaimed columns render as `label  value`
lines through their ordinary resolution — alias, directive, rule set,
affinity, row value — using the **inline face only**, in schema order, NULLs
skipped, capped at a per-density count with `+k more`. Detail is where the
rest is read; a click gets there (SD5). So

```sql
SELECT name    AS card_title,
       kind    AS card_overline,
       content AS card_hero,
       mime    AS card_hero_gloss,
       note    AS `card_body@text/markdown`,
       length_ms AS `length@gloss/duration;unit=ms`,
       sample_rate, channels
FROM samples
```

is a complete card: three facts, one of them glossed, no further ceremony.

### SD2 — Row-value glosses: `<label>_gloss` ✓

For a column whose gloss label is `L`, a text column named `L_gloss` is its
**companion**: each row's value is a media-type token in the alias's syntax
(`image/png`, `gloss/duration;unit=ms`), NULL or empty meaning "no row-level
declaration". The companion is claimed — it is not a fact and takes no gloss
itself.

**Precedence per value**, extending ADR-0186 §SD3 at the top: row value ›
alias › directives › rule sets › affinities. The most specific statement wins,
and a row with no value falls through to whatever the column resolves to, so
the two spellings compose: alias the common kind, override the odd row.

**Binding is per distinct token, not per cell.** A token binds through
`Catalog.BindToken` once per result and is cached with its `Accepts` verdict
for the column's value kind, as a synthesized column resolution whose source
reads `row value: <column>`. Everything downstream of a column resolution —
inline faces, `glossBlock`, the artifact cache — takes it unchanged. The
distinct tokens of one companion are capped; past the cap the remaining rows
render plain with the reason. That bounds what a column of garbage can cost,
which bind-once-per-column bounded by construction.

**The gate.** Inside the `card_` namespace intent is unambiguous, so any
non-empty value that does not bind — `png`, `image/pgn`, `;unti=K` — shows
the reason on that card: in the hero or body box for those slots, and as a
fact of its own, ahead of the result's, for a one-line slot — loud without
taking the slot's line. Outside it (SD8's
general milestone) ADR-0123 §SD2's slash gate applies per value: a token
without a slash is not a declaration and is silent, because a table may
simply have columns named `lip` and `lip_gloss`; one *with* a slash that does
not bind marks its grid cell in the warning tone.

### SD3 — The widget: columnar model, host-owned state, host-drawn blocks ✓

`widgets/cardgrid` follows `chatview` and `tree`: a columnar `Model` (one
slice per slot over card ordinals, facts and tags as ragged co-arrays), a
host-owned `State` (selected ordinal, density, hero aspect), a `Result`
reporting the frame's click. The widget never sees a gloss or an Arrow array:
hero and body arrive through `Block` callbacks — the contract `chatview` and
the leeway card already have, restated locally and widened by what a
fixed box needs: the content's size (the widget centres it), a *pending*
and a *reason* shape the widget draws itself so they look the same
whoever the host is, and a click report — and every other slot as text plus
a colour. The model holds **one page**; paging is the
host's (SD5).

A card's click sense is emitted *first* and its slots after it, so the sense
sits behind the content: labels are not selectable and let the click
through, a block's own controls — a play button — sit in front and keep
theirs, and a widget that senses clicks without being a control (an image
does) reports the click for the card. A click-sensing frame *around* the
content would have swallowed the button's clicks.

### SD4 — Layout: a uniform grid, and what unwieldy input does to it ✓

Cards are laid out in `n = max(1, ⌊(W + gap) / (minW + gap)⌋)` columns,
stretched to fill, `minW` from a three-step density (S / M / L). Every card on
a page has **the same height, derived from the schema and the density alone**:
a slot present in the schema reserves its budget whether or not a row fills
it; a slot absent from the schema reserves nothing. Every card and every slot in it is an allocated rect with a hard paint clip
(the treemap's fixed-cell recipe), so the whole page is placed from computed
rects before anything is laid out, and content that outgrows its budget
cannot reach a neighbour. A grid whose rows depend on content is the masonry
alternative below — so uniformity is both the convention and the cheap path.

Overline, title and subtitle are one flowing block inside their combined
budget rather than three fixed rows: a one-line title is then followed
directly by its subtitle, and the slack falls below the block instead of
opening a hole inside it.

What each kind of bad input meets:

| input | treatment |
| --- | --- |
| hero of extreme aspect (panorama, strip), or tiny | contained in the hero box (16:9 default; 4:3, 1:1), letterboxed on a subtle fill; never cropped, never stretched, never scaled past native size |
| hero NULL on this row | the box stays, with a muted placeholder glyph — the row keeps its line |
| huge image | header-only pixel budget before decode (ADR-0123 §SD6); then **reduced to a fixed thumbnail bound and only the thumbnail retained** (`x/image/draw`), laid out by the source's own size, so a page costs thumbnails, not originals |
| animated GIF | first frame |
| oversized or undecodable media | first line of the reason in the hero box, weak; the card still draws |
| long title, no break opportunity (a path, a hash) | wrapped anywhere, clipped at two lines, full text on hover |
| multi-line or megabyte text in a one-line slot | newlines folded to spaces and the string cut to the slot's character budget **in Go, once per fold** — a long cell is not shipped to the renderer every frame to be truncated there |
| body longer than its budget | cut by lines and by runes with an ellipsis, then clipped; a block-faced body scrolls inside its area (ADR-0186's card doctrine) |
| invalid UTF-8, control characters | `EnsureUTF8`; C0 controls other than tab and newline dropped |
| hundreds of columns | the fact cap, `+k more` |
| nested values in a fact | the inline face or `formatCell`, truncated, full on hover |
| many rows | the pager; only the page is folded, so no card cap is needed |

Line clamping is an estimate backed by a clip: with no row limit on `Label`,
a two-line title is cut in Go to the runes two lines are estimated to hold —
on the wide side, so the ellipsis usually lands before the clip does — inside
a fixed-height clipped region. Where the estimate is wrong the text is cut
at the region's edge without one. If that reads badly in practice, the fix
is a `maxRows` on the label in the IDL — a Tier 1 change recorded here as the
follow-up, not taken on speculation.

### SD5 — Paging, the artifact budget, and selection ✓

The pane pages with `widgets/pager` (12 / 24 / 48 / 96 cards) and scrolls
vertically within the page. **The page is the artifact cache's working set**:
the cache is keyed on (result, row, column) like Chat's and dropped on a page
turn, so decoded memory is bounded by page size × thumbnail size with no LRU.

Artifacts are built **on the frame thread under a per-frame time budget**:
cards whose artifacts are not built draw a skeleton and the pane requests a
repaint until the page is complete. A page turn is therefore never one long
frame, only a fill-in over several. One very large image still costs its own
decode in one frame; moving builds off the frame thread is the recorded
deferral, and a profile of a 96-card page of large images is what would
move it.

Selection carries both ways, as Kanban's does (ADR-0122 §SD3) and for the same
reason — a card paints its selection. A click emits `selection` for the Arrow
row; a cursor moved elsewhere selects the card and **turns the page to it**.
Arrow keys, Home and End move the selection within the grid (focus-scoped,
ADR-0177; ← and → walk the reading order, so a row's end leads to the next
row); Space toggles playback on an audio hero.

A second pager in one app needed its id stack salted apart from the Table's:
a pager derives the same ids from whatever stack it is given, and the
collision is silent — one of the two stops hearing its clicks.

### SD6 — `audio/wav`: a content gloss whose block face is a waveform ✓

`audio/wav`, `audio/x-wav`, `audio/wave` and `audio/vnd.wave` join the content
family in `public/hmi/gloss`, accepting bytes and text. Four members rather
than one with aliases: IANA registers only the last, a table's `mime` column
holds whichever its ingester wrote, and a declaration should read back as it
was written; `gloss.IsWAVMediaType` is what a host keys its face on.

- **Inline face**: `[audio/wav · 0:03 · 44.1 kHz · 2 ch]` from a header read
  bounded on the input like `application/cbor`'s walk — if `fmt ` and `data`
  are not within the first few kilobytes the face is the image family's
  descriptor, type and size. The length shown is what the bytes hold, not
  what the header promises; a truncated file takes the warning tone.
- **Block face**, bound in play: a static min/max overview — `peaks` over
  `wavfile` over the cell's bytes, reduced to the face's column count, only
  the reduced columns retained (`peaks.Overview`) — drawn by a new
  `waveform.RenderThumbnail` (the minimap's drawing without a `Player` behind
  it), with play/pause, a position readout, and click-to-seek. It serves the
  card hero, the ad-hoc Detail pane and the Chat bubble alike. The leeway
  card does not get it: its values reach a face as marshalled text, which
  cannot carry a recording, so the face says the bytes are not a WAVE file.
- **Playback** follows tally: `pulsesink` where a pulse socket opens,
  `sink.Null` and a visible "no output device" otherwise; no bus capability,
  because ADR-0208 §SD6 left that to a keelson decision that has not been
  made. On play the cell's bytes are **copied** — the source outlives the
  frame, the Arrow view does not. The app owns **one now-playing session**
  shared by every pane, so starting a second recording stops the first, and
  it closes on a result change. The device is opened off the frame thread
  and nothing holds the session's lock while it is: the faces read the
  session's state every frame.
- **Caps**: a byte limit on a cell offered for decode, with the reason shown
  past it, like the text limit.

Not the full `waveform.Player`: zoom, regions and lanes need a `track`, and a
`track` needs a reopenable staged source. An "open in player" affordance from
the block face is deferred.

### SD7 — Registration and fixture ✓

A built-in tab with `shapeContract` and the selection signal, dock id 32
(next free), so the strip, the Panes menu and the derived
`BOXER_PLAY_FOCUS_CARDS` knob follow from the registry.

The fixture follows the series fixture lab (ADR-0163): play publishes an
ordinary ad-hoc dataset, `fixture_cards`, and gets out of the way — the help
corpus's "Cards" snippet is the query above against `keelson('fixture_cards')`.
The rows are procedural and chosen for the SD4 table, not for looks: PNGs
including a panorama, a strip, an icon, one over the pixel budget and one
corrupt; WAVs including a sweep, a burst, silence, stereo, 8-bit and a
truncated file; a row with a NULL hero; a row whose `mime` is misspelt; a
title that is one long path; a markdown note with a table in it. The pane
offers it wherever it has nothing to draw — no result, a rejected schema, a
failed query — because a buffer restored from an earlier session may name a
dataset that did not outlive the process that published it; publishing again
binds the alias without writing the query a second time.

### SD8 — Milestones

- **M1 — widget.** ✓ `widgets/cardgrid`: model, layout, clamps, selection,
  keyboard; a gallery demo over a synthetic model that includes the SD4 cases.
- **M2 — pane.** ✓ Contract and rejects, page fold, facts through glosses,
  column-level glosses on hero and body, thumbnails, the frame-budgeted cache,
  selection both ways, registration.
- **M3 — row values in the pane.** ✓ `<label>_gloss` per SD2, for slots and facts.
- **M4 — audio.** ✓ The `audio/wav` gloss, `waveform.RenderThumbnail`, the block face
  in Detail and as hero, the now-playing session.
- **M5 — fixture and docs.** ✓ `fixture_cards`, the snippet, a tour scene,
  `features.md` / `snippets.md`.
- **M6 — row values elsewhere.** ✓ The per-row Table grid (cell text, width
  seed, hyperlink cells), the ad-hoc Detail pane and the Chat bubble read the
  companion through one app-level resolution, and the Glosses tab names a
  column's companion; recorded as a dated Update on ADR-0186. The leeway
  paths — the per-attribute grid and the leeway card — do not: a leeway
  column's name is its physical encoding, so `<label>_gloss` has nothing to
  pair with there, and a rule is that path's route to a gloss (ADR-0186 §SD3).

### SD9 — The un-glossed cell is read by its head ✓

Found while implementing, and taken here because this pane makes it the
common case rather than a corner: a result that carries media costs the
*other* panes more than it costs this one. The Table grids and the ad-hoc
Detail pane format an un-glossed cell per visible cell per frame with no
cache behind them, and a ClickHouse `String` holding a recording arrives as
an Arrow `String` as readily as a `Binary` — so each frame validated and
copied every blob on the page to show its first few dozen characters. Over
the fixture (18 rows, about 1.3 MB of media; 2026-09-17, one desktop run)
the hidden Table tab wrote about 2.5 MB of cell text per frame and the Go
side of a frame took about twice as long as it did afterwards.

Two bounds, both on display text only: `formatDisplayCell` reads a long text
value by its head and names its size, for the one-line cells of both grids
and the ad-hoc Detail rows; and `gloss.FormatArrowElem` spells out at most
`FormatBinaryMaxBytes` of a binary value in hex — identifiers, hashes and
packed addresses fit under it whole. Sorting, selection keys and the glosses
read the value itself and are untouched. M6 removes the cost for a column
whose rows are glossed — an inline face is a descriptor — and this removes it
for one that is not.

### SD10 — Deferred, and why

- **Masonry and continuous scroll** — see Alternatives; the widget's input
  does not change if it is taken up.
- **Compressed audio** (FLAC, MP3, Opus): ffmpeg over a memfd is tally's
  route and costs a process per hero; wanted on Detail's single face first.
- **Off-thread artifact builds** (SD5), **`maxRows` on `Label`** (SD4), **the
  full player from a cell** (SD6).
- **Actions on a card** — links, buttons, a `card_url`. A result is read-only
  (ADR-0122 §SD3); `gloss/url` on a fact already gives a link.
- **Grouping** (`card_group` section headers) and **client-side sort /
  filter**: `ORDER BY` and `WHERE` are the mechanism.
- **`image/svg+xml`, webp, avif** — ADR-0123 §SD7's list, unchanged.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| result-column convention (ADR-0123 §SD2, ADR-0186 §SD3) | + reserved `card_*` labels; + `<label>_gloss` companion as the top binding tier | `features.md`, `snippets.md`; the Glosses tab's source column; an ADR-0186 Update at M6 |
| `public/hmi/gloss` catalog | + the four WAVE spellings in the content family, registered after `application/cbor` so its pinned prefix does not move | the presentation family's pinned offset; the `gloss(…)` round-trip over the default catalog |
| `widgets/waveform` (exported API) | + `RenderThumbnail` | a headless scene test |
| `science/audio/peaks` (exported API) | + `Overview`, `OverviewE` | — |
| `widgets/imagedecode` (exported API) | + `DecodeThumbnailRGBA8`, `ThumbnailSize` | — |
| `widgets/pager` (exported API) | + `GoToIndex` | — |
| `gloss.FormatArrowElem` | binary values past `FormatBinaryMaxBytes` render as head plus size (SD9) | any consumer that read a large blob's full hex from display text |
| sqlapplet's result-tab policy | + `cards` (and `chat`, which it had not been told about) | `TestTabPolicyCoversEveryRegisteredTab` |
| `widgets/cardgrid` (new exported API) | new package | demo registry entry, `package_props.go` |
| play tab roster | + Cards, dock id 32 | Panes menu, tab marks, `doc/env-vars.md`, the play tour's `08_cards` scene |

## Alternatives

- **O1, O2, O4** — assessed in the QOC above.
- **Masonry with continuous scroll.** Natural heights suit mixed-aspect photo
  sets. Rejected for v0: it needs per-card measurement before layout, scroll
  virtualisation and a byte-budgeted LRU, where the paged uniform grid bounds
  its memory by construction — and a grid that keeps its rows is the more
  legible instrument for comparing items, which is what a query result is for.
- **Unprefixed slot names** (`title`, `hero`), as Kanban and Chat use.
  Rejected: with unclaimed columns becoming facts, a bare `title` could not be
  told from a fact named title, and a typo could not be told from either.
- **Explicit `card_field_<label>` facts only.** Rejected: it makes the common
  query longer to protect against showing a column the author selected.
- **Sniffing the hero's bytes.** Magic numbers would identify PNG and RIFF
  reliably. Rejected as in ADR-0123: it answers the easy half, and the row
  value makes the declaration as cheap as selecting the `mime` column.
- **A `waveform.Player` per audio hero.** Rejected: a staged source, a
  background build and a window cache per card (Context).
- **Extending the leeway card emitter into a grid.** Rejected: it lays out one
  row's sections and attributes; a gallery of rows shares its name and
  nothing of its structure.

## Consequences

### Positive

- Item-shaped results get a pane, written in the SQL the author already has —
  often one `AS` per slot and the `mime` column.
- A card can carry any mixture of kinds, and says so itself when a kind is
  unknown.
- Audio becomes a catalog member, so Detail and Chat gain a waveform face
  without being touched.

### Negative

- A fourth convention on result column names, and the first whose meaning
  lives in another column's *values*. A result that already has `x` and a text
  `x_gloss` changes meaning; the slash gate limits that to values that look
  like media types.
- Bind-once-per-column is gone for companion columns; the token cap is what
  stands in for it.
- play links the pulse client and the WAV reader, and can now make sound.
- Frame-budgeted builds mean a page fills in rather than appearing; a single
  oversized decode still lands in one frame.

### Neutral

- Two things in `play` are now called a card; the code names keep them apart.
- Clamping is an estimate backed by a clip until a label row limit exists.
- A large un-glossed value no longer shows whole in a grid cell or an ad-hoc
  Detail row (SD9); it never fitted either, and the raw value is a gloss or a
  query away.

## Migration — Tier 1

- **Breaks.** Nothing at rest, in SQL or in Go. A result with columns `L` and
  text `L_gloss` whose values contain a slash reads differently; display text
  of a binary value past `FormatBinaryMaxBytes` is its head and size (SD9).
- **Path.** Additive; hosts embedding `PlayApp` inherit the tab and the gloss.
- **Regeneration.** None — no IDL, codegen or FFI change.
- **Old shape.** Kept: aliases, directives, rule sets and affinities resolve
  as before beneath the row value.

## Verification plan — Tier 1

- **Lane.** Default `go test`: the recognizer against every reject and the
  label matching; the page fold as a golden over `fixture_cards`; row-value
  precedence, the token cap and both gates; the text pre-clamp (newlines,
  budget, UTF-8, controls); the thumbnail scaler's bounds; the `audio/wav`
  inline face over good, truncated and header-far files; the reduced peaks of
  a known signal; playback against `sink.Null` with a manual clock. The
  widget's column count and uniform height as table tests; `RenderThumbnail`
  under the headless scene harness, with a scripted click. The screenshot
  tour: a `cardgrid` gallery demo, and the play tour's `08_cards` scene over
  `fixture_cards` — published, run, and a card clicked so Detail follows.
- **What would fail.** `` `card_body@text/markdown` `` no longer claiming
  `card_body`; `card_titel` accepted; a row value losing to an alias; a
  misspelt row value rendering silently; a card's height depending on its
  content; a retained artifact larger than the thumbnail bound.
- **Gap.** Audible output, seek latency and the skeleton fill-in are checked
  by hand; the per-frame cost of a 96-card page is not benchmarked, the page
  size selector being the mitigation. Keyboard movement is checked by hand
  with real key events: the inspection seam's synthetic key press does not
  reach a key-capturing frame (it does not move the tree widget either), so
  no scripted lane covers it.

## Status

Accepted 2026-09-17. The design dialogue of 2026-09-17 settled four forks: row-value
glosses, general rather than cards-only (SD2, O3), on the requirement that
each card carry its own mixture and describe itself; unclaimed columns as
facts (SD1); WAV with play/pause for the first audio cut (SD6); a uniform
paged grid (SD4, SD5). M1–M6 are built, with what implementation changed
folded in above: the flowing head block and the behind-the-content click
sense (SD3, SD4), four WAVE spellings rather than one with aliases (SD6), the
fixture offered in every empty state (SD7), and the bounded un-glossed cell
(SD9). Verified through the unit suites, the gallery tour, the play tour's
`08_cards` scene, and a live run (selection by click and by key, the hero's
own button keeping its clicks, playback through the output device). From
here, changes land as dated entries under `## Updates`.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0123](./0123-play-content-typed-detail-cells.md) — `label@mime`, the slash gate, image caps.
- [ADR-0186](./0186-play-gloss-catalog.md) — the gloss catalog, faces, binding precedence.
- [ADR-0239](./0239-play-chat-panel-and-chatview-widget.md) (proposed) — label-matched contract, host-drawn blocks, the many-row artifact cache.
- [ADR-0122](./0122-play-kanban-panel.md) — named columns over detection, tone tokens, two-way selection.
- [ADR-0208](./0208-audio-waveform-player-widget.md) — `science/audio`, peaks, sinks, the player this does not embed.
- [ADR-0163](./0163-play-timeseries-workbench.md) — the fixture-lab pattern.
- [ADR-0097](./0097-play-reactive-query-graph.md) — `PanelI` and channel negotiation.
- [ADR-0177](./0177-imzero2-focus-scoped-keyboard-capture.md) — focus-scoped keys.
