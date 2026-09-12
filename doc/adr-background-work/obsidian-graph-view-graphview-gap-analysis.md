---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Compiled 2026-09-11 against the
> `widgets/graphview` package as it stood at
> [ADR-0224 §SD1–SD11](../adr/0224-graphview-go-graph-widget-painter-lane.md)
> and Obsidian's public help page for the Graph view, and **re-baselined
> 2026-09-12** against the package as it stands. Two things landed between the
> dates: ADR-0224 §SD12 reported the secondary click this page asked for, and
> [ADR-0225](../adr/0225-graphview-navigation-layer-and-radial-layout.md) built
> the focus-and-radius helper the local-graph rows called for — which this page
> was the second analysis to ask for, and the argument that earned it.
> Nothing here is a decision; it is the inventory a later ADR update or SD would
> pick from. Provenance: the vendor's documentation page only — no application
> code was read.

# Obsidian Graph view → graphview: feature gap analysis

## 1 Question and scope

Obsidian's Graph view is not a library but a **consumer expectation**: this
repository ingests Obsidian vaults into `boxer.facts` and answers the link
graph, tags and properties in SQL
([how-to](../howto/markdown-facts-obsidian-queries.md)), so a vault owner
who opens that graph in graphview will measure it against the view they
already know. The page tabulates every setting and interaction on the
help page against the widget's contract, using the framing and legend of
the [NetChart analysis](./netchart-graphview-gap-analysis.md): under SD1
the caller declares the node and edge set every frame, so anything
Obsidian does by *filtering* is the caller's; only the visual affordance
can be a widget gap. Legend: **✓** covered, **≈** different shape or
partial, **caller** the caller does it under SD1, **gap** not available.

## 2 Settings and interactions

| Obsidian | graphview | Status | Note |
|---|---|---|---|
| Filters › Search files (query) | caller filters the SQL result | caller | The tags / properties queries in the how-to are the filter language. |
| Filters › Tags, Attachments (show as nodes) | caller adds tag / attachment nodes | caller | A "kind" encoding for those nodes is colour only; node shapes are a NetChart §6 gap. |
| Filters › Existing files only | caller drops unresolved targets | caller | The how-to's graph query already does this by default. |
| Filters › Orphans | caller drops degree-0 nodes | caller | |
| Groups › colour by search query | `NodeSpec.Color`; `NodeSpec.Auras` + `AuraStyle` | ✓ | Obsidian recolours the node; graphview can do that or draw the group as an aura blob (SD11), which is strictly more. Nodes in no group are drawn normally in both. |
| Display › Arrows (toggle) | arrow head always painted at `To`, sized by `TipSize` | ≈ | No off switch; an undirected vault view draws arrows it should not. Confirms the decoration-kinds gap (NetChart §6). |
| Display › Text fade threshold (labels fade in with zoom) | `LabelsAlways`, else hover / selected / pinned | **gap** | A label level-of-detail threshold on on-screen node size or zoom. Confirms the `nodeDetailMinZoom` gap (NetChart §6); with SD7's fixed-size labels it is the only way to keep a vault-sized graph readable. |
| Display › Node size (slider) | `Style.NodeRadius`, `NodeSpec.Radius` | ✓ | |
| Node size grows with incoming link count | caller maps in-degree to `Radius` | caller | One `groupUniqArray` over the backlinks query. `nav` builds an adjacency and could answer degree directly, but publishes none of it — the graph-query gap of the Cytoscape reading. A radius-aware repulsion (NetChart §3) keeps large hubs from overlapping. |
| Display › Link thickness (slider) | `Style.EdgeWidth`, `EdgeSpec.Width` | ✓ | |
| Display › Animate (time-lapse by creation date) | caller grows the declaration frame by frame | caller | Bling; noted because the incremental placement near a neighbour (SD1 reconciliation) is what makes it look right for free. |
| Forces › Center force | `ForceParams.CenterGravity` (`LayoutForceDirectedCG`) | ✓ | |
| Forces › Repel force | `ForceParams.CRepulse` | ✓ | |
| Forces › Link force | `ForceParams.CAttract` | ✓ | |
| Forces › Link distance | `ForceParams.KScale` | ≈ | One global ideal length; per-edge length is a NetChart §3 gap, not needed for parity here. |
| Hover: highlight the note's connections, dim the rest | hovered node and its own marker / label are highlighted; incident edges are not, and nothing is dimmed | **gap** | Two pieces: a neighbourhood highlight (incident edges + neighbours) and **per-node / per-edge opacity** for the rest. Confirms the opacity gap (NetChart §4, §6) — the single most recurring primitive across analyses. |
| Click: open the note | `EventKindNodeClick` under `NodeClicking` | ✓ | The caller opens the document. |
| Right-click: context menu | `EventKindNodeSecondaryClick`, and the edge and background kinds beside it | ✓ | Closed by ADR-0224 §SD12 — the trigger; the menu is the caller's. |
| Zoom: wheel, `+` / `-` keys | wheel and pinch, `ZoomSpeed`; `SetCamera` | ≈ | Keyboard zoom is the caller's via `Camera` / `SetCamera` if it reads the keys itself. |
| Pan: background drag, arrow keys (Shift accelerates) | background drag; `SetCamera` | ≈ | As above for the keyboard. |
| Node drag | on by default; `NoDragging`, `PinOnDrag` | ✓ | |
| Local graph: notes connected to the active note | `nav` in `ModeFocus`, or `ModeManual` with one expansion | ✓ | Closed by ADR-0225 §SD2: the helper owns the universe and derives the subset. |
| Local graph › Depth (hops) | `nav.Expand(id, depth, dir)`; `Options.FocusRadius` and `FocusTailRadius` in focus mode | ✓ | Closed by ADR-0225 §SD2. This page asking for it second is what made the helper worth building rather than leaving to each consumer. |
| Local graph › Incoming / outgoing links | `nav.DirectionIn` / `DirectionOut` / `DirectionBoth` per expansion | ✓ | Closed by ADR-0225 §SD1 and §SD2: the adjacency keeps the direction per entry, so one walk serves all three. |
| Local graph › Show neighbour links (edges among the neighbours) | always declared: `nav` emits every universe edge whose ends are both visible | ≈ | Not a toggle. A vault owner who turns this off in Obsidian has no equivalent here; dropping those edges means dropping them from the universe, which also changes what the walk reaches. |
| Local graph honours the global settings | one `Options` value per `View` | ✓ | Two views share an `Options` by value. |

## 3 Reading

Of the three widget gaps this page found, one closed: the secondary click is
ADR-0224 §SD12. Two remain, and both were confirmations of NetChart rows rather
than new findings — which was the useful result then and is the useful result
now: a consumer with a concrete expectation lands on the same primitives a
vendor surface did.

**Small.** The label level-of-detail threshold (NetChart §6,
`nodeDetailMinZoom`). An arrows-off switch is still a degenerate case of the
decoration-kinds row, and Ogma's `shape.head: none` is the same request from a
third direction.

**Medium.** Hover neighbourhood highlight with the rest dimmed — the one
interaction a vault owner will notice missing first. It reduces to the
**opacity** primitive plus a per-frame neighbour set, and the later Ogma
reading sharpened opacity into a pair: a fade and a non-pickable flag, since a
dimmed node that still answers the pointer is a bug. Every analysis in the
series now names that pair as the first primitive to add.

**Closed by the navigation helper.** The local graph — its depth, its direction
and its subset — is ADR-0225's `nav`, and this page is why it exists in the
shape it does: two independent consumers asking for a focus-and-radius walk is
the threshold the NetChart reading set for building one. The one row that did
not survive the translation is the neighbour-links toggle, which `nav` does not
offer because its edge rule is unconditional.

**Caller-side, no widget change.** Filters, group membership, node size by
in-degree and the time-lapse are all SD1 declaration work, with the caveat that
`nav` knows the degree and will not say.

Nothing here needs IDL, Rust or fetcher work.

## 4 References

- [ADR-0224](../adr/0224-graphview-go-graph-widget-painter-lane.md) — the
  widget and its design decisions; SD1, SD7 and SD11 carry the rows above,
  and SD12 closed the secondary-click row.
- [ADR-0225](../adr/0225-graphview-navigation-layer-and-radial-layout.md) —
  the `nav` package the local-graph rows became.
- [netchart-graphview-gap-analysis.md](./netchart-graphview-gap-analysis.md) —
  the model for this page and the rows it confirms.
- [How to ingest a markdown vault and query it like Obsidian](../howto/markdown-facts-obsidian-queries.md)
  — the graph, backlinks and tags queries a caller would feed the widget.
- Obsidian Help, "Graph view" (Settings: Filters, Groups, Display, Forces;
  time-lapse; Local graph), public help page, read 2026-09-11. The
  `help.obsidian.md/plugins/graph` address redirects to the vendor's main
  domain; the content was reachable there.
