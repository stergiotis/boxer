---
type: adr
status: proposed
date: 2026-07-15
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not
> implement as if accepted.

# ADR-0123: `play` content-typed detail cells — `label@mime`

## Status

Proposed, pre-human-review.

The pane is built: `play_detail_rich.go` with its tests, the `cellRaw` /
`renderRichCell` seam in `renderDetailSection`, the `executed` handoff into the
Detail tab, a `widgets/imagedecode` package extracted from
`help.FSImageResolver` (§SD5), and one snippet-library entry.

Verified per §Validation — unit suites green, and a live run against a
literal-only `SELECT` confirmed all six renderers (markdown heading / italic /
inline code / link / list / blockquote; pretty-printed and highlighted JSON;
highlighted SQL; untruncated multi-line plain text; a `unhex`-supplied PNG
decoded and drawn pixel-exact at native size) plus all four diagnostic paths and
the slash gate leaving `dot_done@success` alone.

The separator claims measured against `clickhouse-local`: `` AS
notes@text/markdown ``, `` AS notes@textmarkdown `` and `` AS notes/markdown ``
each fail with a syntax error naming the offending character's position.

## Context

`play`'s Detail pane forks on schema shape (`play_detail.go:164`). A
leeway-shaped result goes to the card driver, which knows each column's type
from the leeway encoding in its physical name. Everything else — an ordinary SQL
result — falls to `renderAdHocDetail`, which groups columns by prefix into four
sections and renders every cell the same way: `c.Label(val).Truncate()`
(`play_detail.go:229`).

That is the right default for a number or a name, and wrong for the cases where
a column holds a document. A markdown field renders as one clipped line of
asterisks; a stored PNG renders as the first few dozen characters of its hex
dump. The pane has the vertical room to show either properly, and no way to be
told which columns deserve it.

Substrate facts that shape the design:

- The renderers already exist. `widgets/markdown` parses once into a retained
  `Doc` and renders many times (`Parse` → `Doc.Render(ids)`); `widgets/codeview`
  highlights SQL, JSON and Go into a retained `CodeViewJob`.
- `c.Image` takes **decoded** RGBA8 pixels (`[]uint32`, `0xRRGGBBAA`, row-major)
  — the bindings carry no encoded-bytes path. The only bytes-to-pixels pack loop
  in the tree is inside `help.FSImageResolver.LoadImage`
  (`public/keelson/runtime/help/image.go:60`), where it is welded to an `fs.FS`.
- `codeview`'s retained holders are **not** a render cache. `BuildRetained`
  interns the *already-serialized* buffer (`unique.Make(string(raw))`,
  `fffi2_typed_impl.go:170`), so the highlighter and the buffer construction run
  on every call; the intern dedups the resulting bytes and the Rust-side
  registration, nothing earlier.
- `formatCell` hex-encodes `Binary` / `LargeBinary` (`play_format.go:62`).
  Calling it on an image column costs two bytes of string per byte of blob,
  every frame.
- Naming conventions over result columns are the house pattern, and there are
  three of them to not collide with: ADR-0116's `section:column` handle, whose
  `splitHandle` claims any identifier with **exactly one colon**
  (`lwsql.go:316`); ADR-0121's `cond_N`; and ADR-0122's `lane` / `title` /
  `dot_<label>@<token>`. The Map pane likewise requires `mercator_x` /
  `mercator_y` by name.
- ADR-0122 §SD2 already measured the separator question against
  `clickhouse-local`: `@`, `~`, `/`, `!` and `?` all parse when backtick-quoted
  and raise a syntax error unquoted, so a forgotten backtick fails loudly. `#`
  does not — it opens a ClickHouse line comment, so the typo yields a
  plausible-looking column and no diagnostic.

## Decision

A result column named `` `<label>@<mime>` `` renders its cell as `<mime>` in the
ad-hoc Detail pane, instead of as a truncated one-line label:

```sql
SELECT
  body      AS `notes@text/markdown`,
  thumbnail AS `shot@image/png`,
  payload   AS `req@application/json`,
  stmt      AS `q@application/sql`,
  stack     AS `trace@text/plain`
FROM t
```

Detection is not offered. A `String` column whose text happens to open with `#`
is not thereby markdown, and a pane that guesses is a pane that is confidently
wrong on somebody's data. The declaration costs one `AS` per query and says what
it means — the §SD1 reasoning of ADR-0122, reused.

### SD1 — Scope: the ad-hoc path, and only it

The leeway card path is untouched. A leeway column's rendering is already
declared upstream, in the mapping — `canonicalShreds` routes string-likes by an
`lw` vocabulary (`symbol|text|blob|…`), and the physical name carries the result.
A second, name-based declaration over the same column would be a competing source
of truth for a question already answered. Non-leeway results have no such
channel, which is the whole reason they need one.

The Table tab is untouched for a duller reason: a cell there is one line high.
Detail is the pane with room for a block.

### SD2 — The gate is the slash, not "parses as a media type"

`mime.ParseMediaType` does **not** require a slash. Measured:

```text
ParseMediaType("text/markdown") -> "text/markdown", nil
ParseMediaType("success")       -> "success",       nil     ← no error
ParseMediaType("example.com")   -> "example.com",   nil     ← no error
ParseMediaType("a/b/c")         -> "",              error
```

So "the token after `@` parses cleanly" is not usable as the discriminator. Had
it been the rule, ADR-0122's own `` `dot_done@success` `` would resolve to a
media type named `success`, fail the vocabulary lookup, and paint an "unknown
content type" diagnostic into the Detail pane of every board query. The rule is
therefore:

| column name | outcome |
| --- | --- |
| no `@` | plain. Unchanged from today. |
| `@` present, no `/` after it | plain, **no diagnostic**. `user@example.com` and `dot_done@success` are ordinary columns. |
| `/` present, parses, known type | rendered as that type. |
| `/` present, parses, unknown type | plain, **with the reason shown inline**. |
| `/` present, fails to parse | plain, with the parse error shown inline. |

The last two rows are ADR-0122 §SD2's own principle applied one level up: a
convention whose typo mode is a wrong-but-plausible render is not worth having.
`` `notes@text/markdwn` `` must not quietly become plain text. But the diagnostic
is scoped to declarations — a token with a slash in it — so it cannot fire on a
column that never meant to declare anything.

Past the gate, `mime.ParseMediaType` earns its keep: it case-folds
(`TEXT/Markdown` → `text/markdown`) and splits parameters, so
`` `notes@text/markdown; charset=utf-8` `` works without a hand-rolled parser.

**On `;base64`.** It is not a MIME parameter. `ParseMediaType("image/png;base64")`
fails with `invalid media parameter`, because a parameter is `key=value` and
`base64` is a bare token — the spelling is a data-URI-ism, not a media-type one.
Recorded because it is the obvious next request (§Deferred): the form would have
to be `;encoding=base64`.

### SD3 — A closed vocabulary

| declared type | rendered by |
| --- | --- |
| `text/markdown` | `widgets/markdown` — parsed and rendered, not highlighted |
| `text/plain` | a wrapped label, untruncated |
| `application/json` | `codeview.BuildJson`, after `json.Indent` |
| `application/sql` | `codeview.BuildSql` |
| `text/x-go` | `codeview.BuildGo` |
| `image/png`, `image/jpeg`, `image/gif` | `image.Decode` → `c.Image` |

Closed, because an open one has no way to be wrong out loud.

The image set is not a judgement about formats; it is `help/image.go`'s existing
set, which blank-imports exactly png / gif / jpeg and records why: webp and avif
add roughly a megabyte to every binary that links them. Matching it means the
decode helper is shared (§SD5) and the binary cost is unchanged.
`image/svg+xml` has no Go decoder in the tree and rejects with that reason.

Markdown parses with `GFM | Callout | Highlight | Comment`. Wikilinks and embeds
are dropped from the default set: there is no vault behind a database cell, so
`NoopResolver` would resolve them to `/page` URLs that go nowhere. Frontmatter is
dropped because a cell is not a note — a leading `---` is content here.

**Known limitation, not fixed here:** the markdown widget's renderer drops
tables and math even though the parser recognises them (its package doc says so).
A GFM table in a declared markdown cell therefore renders as nothing. That is an
upstream gap; this ADR notes it rather than growing to fix it.

### SD4 — Raw cells, not `formatCell`

The rich path must never call `formatCell`. On a `Binary` column it hex-encodes
(`play_format.go:62`), so a one-megabyte PNG becomes a two-megabyte string every
frame — and today's section loop calls it merely to test the `val == ""` skip.

`cellRaw(rec, col, row)` reads `String`, `LargeString`, `Binary`, `LargeBinary`
and `FixedSizeBinary`, and falls back to `formatCell` for anything else, so
`` SELECT 42 AS `x@text/markdown` `` is odd but total. ClickHouse `String` is
byte-arbitrary and arrives as Arrow `String` or `Binary` depending on
`output_format_arrow_string_as_string`; both reach the same bytes.

It returns a **string, not a `[]byte`** — the zero-copy shape for both families.
`String.Value` returns a substring of the array's backing string and
`Binary.ValueString` is documented as allocation-free, so the per-frame
empty-check costs a length read. The string therefore aliases Arrow memory and
must not outlive the frame; the cache stores only what it derives from it
(§SD5), never the string itself.

### SD5 — Caching on `(executed, row)`, and a shared decoder

Every renderer here needs a cache, and the interning in `codeview` is not one
(§Context). `markdown.Parse` builds a segment tree and exists to be hoisted;
decoding a PNG per frame is not arguable.

The cache clears whenever `(executed, row)` changes. The Detail pane shows one
row, so the working set is that row's columns — bounded by construction, needing
no LRU. `executed` is the same freshness token the pager, the World pane and
`KanbanDriver`'s fold already key on.

The bytes-to-RGBA8 loop moves out of `help.FSImageResolver.LoadImage` into a
small `widgets/imagedecode` package that both callers use. Not into `bindings`,
despite the pixel format being the `Image` opcode's own contract: the png / jpeg
/ gif blank-imports would then land in every imzero2 binary, which is the cost
`help/image.go` went out of its way to avoid.

### SD6 — Caps

Images resolve their dimensions with `image.DecodeConfig` — header-only, cheap —
and reject over a pixel budget *before* `image.Decode` allocates. This is not
hypothetical tidiness: a 30000×30000 PNG is a ~40 KB file and a ~3.6 GB
decode. Text sources cap by byte length and fall back to plain with a note.
Rendering bounds the image with `FitAspectMaxE` inside a fixed box.

### SD7 — Deferred

- **base64, `data:` URIs, and path/URL sources.** Raw bytes first; ClickHouse
  `String` already holds a blob verbatim. `;encoding=base64` is the shape if it
  is wanted (§SD2). A path or URL source would put a fetcher, a sandbox policy
  and a timeout into a detail pane, and is a separate decision.
- **The Table tab**, and the leeway card path (§SD1).
- **`text/markdown` as source**, i.e. `codeview.BuildMarkdown` — the
  show-me-the-source variant of a type that already renders. Needs a second
  spelling; no one has asked.
- **webp / avif / svg**, and GFM tables inside markdown cells (§SD3).

## Alternatives

**Sniffing the bytes.** Ruled out by the request, and independently: a text
column that opens with `#` is not markdown, and the failure mode is a pane
confidently mis-rendering data it was never told about. Magic-byte detection for
images alone would be reliable but would answer only the easy half.

**Arrow field metadata.** `arrow.Field` carries a metadata map, which is the
textbook place for this. ClickHouse's `ArrowStream` output populates none of it,
and `play`'s user drives SQL — a channel they cannot write to is not a channel.

**A `keelson` registry table mapping `table.column` → mime.** A second source of
truth, and one that cannot name what `play` actually renders: results are
arbitrary expressions, and `` concat(a, b) AS x `` has no registry key. It is,
however, the natural home if a declaration should ever outlive a single query —
noted for whoever wants that.

**A `md_` / `img_` prefix**, mirroring `dot_`. It needs no backticks, which is a
real advantage. It was not taken because it scales by inventing a prefix per
type, and it buries the label behind the type name — `md_notes` reads as a
column about markdown rather than a column of notes.

## Consequences

A third convention now rides on result column names, alongside ADR-0116's
handles, ADR-0121's `cond_N` and ADR-0122's board columns. They are mutually
non-colliding by construction — a MIME type contains no colon, so `splitHandle`
passes `` `notes@text/markdown` `` through untouched; and the slash gate keeps
this convention off `dot_done@success`. That is three conventions deep on one
namespace, and the next one should probably ask whether the namespace is the
right place at all.

`play` grows a dependency on `image/png`, `image/jpeg` and `image/gif` decoders.
The Detail pane can now spend real time on a frame — bounded by §SD6, but a
large declared markdown cell is more work than a truncated label was.

## Validation

- Unit: the name parser — the `@` gate, the slash gate, parameters, case
  folding, and `dot_done@success` / `user@example.com` left alone.
- Unit: `cellBytes` across the Arrow string and binary types, and its fallback.
- Unit: vocabulary resolution, including the two diagnostic paths.
- Live: a `values()`-literal query carrying a markdown cell, a JSON cell and a
  PNG blob via `unhex('89504e47…')`, screenshotted per the `play` recipe.
