---
type: adr
status: accepted
date: 2026-10-29
reviewed-by: "p@stergiotis"
reviewed-date: 2026-10-29
---

# ADR-0347: Links into mdedit — one URL scheme every app follows, a location grammar, and the view mode as a launch option

## Context

mdedit ([ADR-0178](./0178-mdedit-markdown-editor.md)) is opened by other
apps through a launch request ([ADR-0135](./0135-app-launch-requests.md))
whose config names a document: its text, and since
[ADR-0342](./0342-chat-artefact-export-to-lading-and-the-handover-to-mdedit-by-snapshot.md)
§SD5 the lading snapshot it lives in. The window opens in the Split layout
at the top of the document, whatever the caller meant to show.

Three things the tree has are not joined up:

- **A location inside a document is already addressable, at heading
  granularity.** The markdown widget scrolls to a heading slug
  (`markdown.Input.ScrollToSection`), mdedit queues one target per frame
  from the caret, the outline and find, and `mdoutline.SectionAt` maps a
  byte offset to its section. Nothing finer than a heading can be named,
  and nothing outside the window can set the target.
- **The view mode is session state.** Split, Source, Read and Blocks are
  picked by the segmented control and by nothing else (ADR-0178, update of
  2026-09-01). A caller that wants a document *read* cannot say so.
- **Every markdown surface renders links the same way and follows none.**
  The chat transcript and artefact, play's detail cells and docs panel,
  the help center, tally's preview and mdedit's own preview all draw
  through one widget, whose `LinkClaims` seam lets a host take a link; only
  play's docs panel uses it. A link to a boxer document in any of them is a
  browser hyperlink to nowhere.

The nearest precedent is the documentation reference
([ADR-0164](./0164-documentation-regex-search.md) §SD5, `help/docref`): one
frozen string form per corpus, `help://`, `adr://` and `chdoc://`, with a
heading slug as the fragment, carried in facts rows and search hits so any
surface can navigate to the section it names. Obsidian's own vault links
name a heading (`[[note#Heading]]`) or a block (`[[note#^id]]`), where a
block id is a trailing `^id` on the block's last line; the wikilink
extension parses the reference but no parser in the tree reads the
definition.

The owner's answers (2026-10-29): the document is the snapshot triple; a
location is a heading fragment, a line number or an Obsidian block id, all
three implemented; a link opens a new window per click; the surfaces share
one URL scheme and handle it identically.

## Design space (QOC)

**Question.** How does a link name a document and a place in it so that any
app can show it in mdedit, in a chosen view mode?

**Options.**

- **O1 — A URL scheme and launch-config fields.** `mdedit://` names the
  snapshot document, a fragment the location and a query the mode; every
  surface claims the scheme and sends the same launch request.
- **O2 — Per-surface gestures.** Each app that wants to open a place in
  mdedit builds its own launch config and button, as the chat's Open in
  mdedit does.
- **O3 — An operation on an open window.** A `show` command under
  [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md)
  and a host-side find-or-open policy, so a click navigates an existing
  window.
- **O4 — Status quo.** Text and snapshot in the launch config; the reader
  scrolls.

**Criteria.** C1 one spelling, written by a person, a model or SQL, read
by every surface; C2 a location that survives edits where it can; C3 no new
host mechanism; C4 fits the launch-request boundary (typed, audited,
refusable); C5 marginal cost per surface.

**Assessment.**

| | C1 | C2 | C3 | C4 | C5 |
| --- | --- | --- | --- | --- | --- |
| O1 | ++ | + | ++ | ++ | + |
| O2 | −− | + | ++ | ++ | − |
| O3 | + | + | −− | + | − |
| O4 | − | − | ++ | + | ++ |

O3 needs the window reuse policy ADR-0135 §SD5 deferred; O1 does not, and
a later O3 can reuse O1's grammar unchanged.

## Decision

Adopt **O1**.

### SD1 — The URL

```
mdedit://<mount>/<snapshot>/<path>[?mode=<mode>][#<fragment>]
```

`<mount>` and `<snapshot>` are spelled as the lading SFTP tree spells them
(`sftptree.MountName`, `sftptree.SnapshotName`, ADR-0198 §SD9): the
authority and path of the URL are the file's path in that tree. `<path>` is
the document's path in the snapshot, percent-encoded as a URL path is. The
scheme, its parser and its formatter live in one leaf package,
`apps/mdedit/mdeditlink`, which is also where the launch request is built,
so a surface that claims the scheme follows it with one call.

### SD2 — The fragment names a location

Three forms, told apart by their first character:

| Fragment | Names | Granularity in the preview |
| --- | --- | --- |
| `<slug>` | the heading whose `markdown.SlugHeading` slug it is; an explicit `{#anchor}` counts | the heading |
| `^<id>` | the top-level block that ends with the Obsidian block id ` ^id` | the block |
| `L<n>` | line `n`, 1-based | the top-level block holding the line |

A heading slug is lower-case, so a capital `L` cannot collide with one. A
fragment that resolves to nothing opens the document at the top and says so
on the status line; it never refuses the open. In Source and Blocks mode the
same location moves the caret to the line, the heading or the block's
start, which is what those modes have in place of a scroll.

### SD3 — The view mode and the location are launch options

`launchcfg.MdeditLaunch` gains `Mode` (`split`, `source`, `read`, `blocks`;
empty is Split, the zero value) and `Location` (the fragment as written).
They are applied when the window mounts, and the location is applied again
when a snapshot read replaces the text the launch carried, since the
location refers to the snapshot's bytes. The vocabulary terms are
`mdeditLaunchMode` and `mdeditLaunchLocation`.

### SD4 — Block ids are parsed, and scroll targets gain a byte offset

The Obsidian layer gains `FeatureBlockId`: a trailing ` ^id` on a block's
last line is stripped from the rendered text and recorded as the block's
`id` attribute, the attribute goldmark's HTML renderer already emits; a
line holding only `^id` names the block above it and is dropped. The
markdown widget records the id on the block (`BlockInfo.Id`) and resolves a
`^id` scroll target to it. The widget also takes a byte offset as a scroll
target (`Input.ScrollToOffset`), resolved to the top-level block holding
it, which is what a line number becomes. The source pane cannot be scrolled
to an offset, as find's own tooltip records; the caret is set and the pane
follows it when it next renders.

### SD5 — Every surface claims the scheme and does the same thing

A surface that renders markdown passes `mdeditlink.Claimed` as its link
claim and follows a click with `mdeditlink.Open`, off the render thread:
the chat transcript and artefact, play's detail cells and docs panel, the
help center, tally's preview, and mdedit's own preview and Blocks mode. A
surface that follows the link declares `windowhost.open` in its manifest,
as any launch caller does. There is no per-surface behaviour: one window
per click, as ADR-0135 §SD5 decides for every launch, with no reuse.

### SD6 — mdedit offers the link

A document read from a snapshot carries a **Copy link** gesture beside the
rclone command: the URL of the document at the caret's section, in the
window's current mode. A document not from a snapshot has no stable
identity and offers none.

### SD7 — Deferred, recorded

- **Window reuse.** A link clicked twice opens two windows. The trigger
  for ADR-0135 §SD5's deferred policy is a person following a chain of
  cross-links and collecting a window per hop.
- **Same-document fragments** (`#slug` alone) and **relative links inside
  a snapshot** are not claimed; they stay hyperlinks. The trigger is a vault
  of cross-linked notes read in mdedit.
- **Block ids below the top level.** A `^id` on a list item inside a list
  is recorded on the item but only a top-level block is a scroll target.
- **A line inside a long block.** The preview scrolls to the block; the
  seam reports no geometry below that.
- **mdspan and mdextract** do not read block ids; a `[[note#^id]]`
  reference is stored with its fragment as written, which is what resolves
  against the attribute once the facts side wants it.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `launchcfg.MdeditLaunch` | `Mode`, `Location` | the generated `launchcfg.out.go` and its golden; the vdd cohort |
| `mdedit://` | new scheme, SD1 and SD2 | `mdeditlink` parse and format tests |
| `obsidian.FeatureE` | `FeatureBlockId`, in `FeatureAll` | the FeatureAll tests; the widget's default feature set |
| `markdown.Input` | `ScrollToOffset`, `ScrollToOffsetSet`; `ScrollToSection` accepts `^id` | `BlockInfo.Id` |
| manifests | `windowhost.open` on the help center | the capability tables |

## Alternatives

- **Byte offsets as the location.** Precise and brittle: one edit above
  moves every offset below it, and the preview cannot place one below block
  granularity anyway. A line number reads the same and a person can write
  it.
- **A document fact id** (ADR-0217's `mdDoc`) **as the document.** It names
  a sent copy, not the document, and carries no images; the snapshot triple
  is immutable and already what the window reads.
- **A disk path as the document.** The fs broker opens files through a
  dialog only (ADR-0026 §SD7); a link cannot open one without the person
  picking it.
- **Extending `docref`.** Its three schemes are documentation corpora
  with a slug fragment; a lading document is another corpus with a richer
  fragment, and `docref` is frozen for rows already written. A fourth
  scheme there would have carried the same grammar under another package's
  lifecycle.
- **A `show` operation instead of launch options.** Deferred, not
  rejected (SD7): it needs an open window to address.

## Consequences

### Positive

- A model, a SQL row or a person can write a link to a place in a
  document, and every markdown surface opens it the same way.
- The view mode a caller wants is part of the opening intent, where
  ADR-0135 §SD4 puts it.
- Block ids read from a vault render as Obsidian renders them, with the
  marker hidden.

### Negative

- One window per click. The reuse policy stays deferred with a named
  trigger.
- Two more vocabulary terms, append-only.
- A `^id` that is not meant as a block id — prose ending in a caret word —
  is now stripped from the rendered text. The grammar (`[A-Za-z0-9-]+`,
  preceded by whitespace, ending the block) is Obsidian's, and the Source
  pane shows the bytes unchanged.

### Neutral

- The link's authority and path are the SFTP tree's path, so a reader with
  the rclone mount finds the file at the same relative place.

## Migration — Tier 1

Nothing breaks. Launch configs without the new terms decode as before and
open in Split at the top.

## Verification plan — Tier 1

- **Lane.** The default `go test` lane: `mdeditlink` round-trips and
  rejects malformed URLs and fragments; the obsidian package renders block
  ids as `id` attributes and strips the marker; the markdown widget records
  `BlockInfo.Id`, scrolls to an offset and to a `^id` under the headless
  scene harness; mdedit applies a mode and resolves each fragment form to a
  caret and a scroll target; the launch config golden.
- **What would fail.** A fragment form that opens nothing without saying so;
  a block id left in the rendered text; a launch config whose mode or
  location is dropped on the snapshot replacement; a surface whose claim and
  follow differ from the shared pair.

## Status

Accepted (2026-10-29). Implemented with the decision: the block-id
extension, the widget's offset and block-id targets, the two vocabulary
terms, `mdeditlink`, mdedit's launch options and Link gesture, and the
claims on the chat, play, help-center, tally and mdedit surfaces.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## Updates

### 2026-10-29 — review fixes

An independent review of the implementation found one bug and several
edges, all fixed with the decision unchanged:

- A location applied in a mode with a caret was overwritten in the same
  frame by the caret tracking, which read the stale cursor; the reported
  caret is now set to the target with its baseline section.
- A lone `^id` line under a heading renamed the heading's anchor; it is
  left as text. The lone form needs blank lines around it (a paragraph of
  its own); a `^id` line continuing a paragraph is that paragraph's text.
  An id lands on one node — the block it closes, else its paragraph — so
  the HTML carries it once.
- `Parse` refuses a path `fs.FS` would refuse (empty, `.` or `..`
  segments, a leading slash) instead of opening a window that cannot read.
- When a section and an offset target are both set, the section wins and
  the offset is ignored, as the widget's doc says.
- A link-launched window waits for the snapshot's text before resolving
  the location, rather than judging the empty handed text first; and it
  offers its own link only once that text is in the buffer.
- The chat claims the scheme only with a bus, as the other surfaces do.

## References

- [ADR-0135](./0135-app-launch-requests.md) — the launch request and the
  deferred window reuse policy (§SD5).
- [ADR-0178](./0178-mdedit-markdown-editor.md) — mdedit; the view modes and
  the handover.
- [ADR-0342](./0342-chat-artefact-export-to-lading-and-the-handover-to-mdedit-by-snapshot.md)
  — the snapshot triple in the launch config.
- [ADR-0334](./0334-mdedit-blocks-over-the-source.md) — block source
  ranges, which the offset target resolves against.
- [ADR-0164](./0164-documentation-regex-search.md) §SD5 — `docref`, the
  precedent for a frozen reference string.
- [ADR-0198](./0198-fs-snapshot-store.md) §SD9 — the SFTP tree whose
  names the URL reuses.
