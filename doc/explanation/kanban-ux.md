---
type: explanation
audience: contributors working on or embedding the kanban widget
status: draft
---

> **Status: draft — pre-human-review.** A clean-room synthesis of kanban board
> patterns and where boxer's `kanban` widget sits against them. This page
> explains the design *space*; it decides nothing. The authoritative record of
> what the widget actually does is its code and package doc
> (`public/thestack/imzero2/egui2/widgets/kanban`); where this page and the
> code disagree, the code wins.

# Kanban UX: the design space, and where our widget sits

Boxer ships a kanban board widget. This note captures the board patterns that
have stabilised across the tool ecosystem — the parts that follow from the
problem rather than from any one product — so a reader can judge the widget's
shape and its deliberate omissions without re-deriving the field.

## Clean-room boundary

Everything here is drawn from *public patterns, principles, and algorithms*:
design heuristics, the group-by-parent display pattern, the freely-published
fractional-indexing idea. No product's source, schema, or proprietary spec was
consulted or copied; the widget is derived from these requirements
independently. Where an algorithm is named (fractional indexing), it is the
generic, openly-described method, not any vendor's particular variant.

## What a board is made of

Four parts recur regardless of implementation:

- **Columns are a status field.** The columns are the distinct values of one
  single-select attribute (To Do / Doing / Done); a card "is in a column" by
  carrying that value. Nothing structural distinguishes a column beyond being a
  value of the status field.
- **Cards are work items with an anatomy.** The recurring fields are title,
  owner/assignee, labels, due date, priority, a blocked/flag marker, an
  estimate, and links to related items. A board shows a subset; the item carries
  the rest.
- **WIP limits are the defining constraint.** A per-column cap is what makes a
  board *kanban* rather than a list-with-columns. It is a constraint that
  exposes flow problems, not a target: visible as `count / limit`, respected,
  and flagged when exceeded.
- **Swimlanes are a second grouping axis.** Columns partition by status;
  swimlanes partition *orthogonally* by some other attribute — priority, type,
  owner, or a parent relationship. The important property is that *which*
  attribute is nothing special: grouping is a chosen axis, not a fixed layout.

## Ordering is a rank, not an array position

A card's position within a column is data. The array-index representation
("the third card in Doing") forces a renumber on every move and does not
survive concurrent edits. The robust representation is a **per-card rank** —
a value you can always place *between* two neighbours. Fractional indexing
gives this with strings: because a string always exists lexicographically
between two others, an insert or reorder is O(1) and touches only the moved
card, and the same property makes concurrent reorders mergeable. This is a
property of the ordering problem, not of any board; a board that stores order
as array position has taken on a debt that comes due at persistence and
collaboration.

## Sub-items are a solved problem

The most common source of confusion — "how do sub-tasks live on a board?" —
has a settled answer, and it is not a choice between competing card layouts:

1. **A sub-item is a first-class work item** with a parent link and its *own*
   status. It is scheduled — placed in a column — independently of its parent.
2. **Its presentation is a view mode over one flat model**, not a distinct data
   shape. The same `{item, parent, status}` data renders three ways: *flat*
   (children as ordinary cards with a parent reference), *grouped by parent*
   (a swimlane per parent, children in the columns), or *rolled up* (the parent
   only, children collapsed into a progress indicator).
3. **A rollup on the parent ties scattered children together.** When children
   are spread across columns (or across owner swimlanes), a `k / n done`
   indicator on the parent restores the at-a-glance sense of the whole.

The consequence: the data model needs only a parent link and a status per item;
everything else is a rendering choice the viewer toggles. Attempts to encode the
presentation into the data (a "checklist" type distinct from a "card" type)
create the very fork that view-modes dissolve.

## Interaction expects a keyboard, not only a pointer

Pointer drag — grab a card, a ghost tracks the cursor, an insertion line marks
the drop — is table stakes. It is no longer sufficient on its own: a
keyboard-driven move (focus a card, grab it, move with arrow keys, drop) is an
accessibility requirement and an expected affordance, not a nicety. A command
palette, text filtering, and multi-select follow, in decreasing order of how
universally they appear.

## Where our widget sits

The `kanban` widget implements the load-bearing parts and defers a legible set
of extras. It is a composite, Model-driven widget (the caller hands over a
`Model` of columns and cards; the widget owns layout, drag, moves, and
rollups), which keeps the whole-board operations — hit-testing a drag, computing
a rollup, applying a move — as functions over data that can be unit-tested.

On par with the field:

- Columns as a status field; one-level sub-items scheduled independently.
- Swimlanes as a **general grouping axis** — by parent, or by any caller-supplied
  key (owner, priority, label), with an "unassigned" catch-all.
- Parent (and per-lane) **rollups**.
- **Pointer drag-and-drop** with a precise insertion index.
- Design-system-tokenised styling.

Deferred, roughly in priority order:

| Capability | State | Note |
| --- | --- | --- |
| **WIP limits** per column | absent | The defining mechanic; cheap to add (a per-column cap, a `count / limit` header, an over-limit flag). The largest gap. |
| **Keyboard** move + focus nav | absent | Accessibility and a baseline expectation; the non-pointer move path. |
| **Fractional-rank ordering** | absent | Order is array position today — fine while ephemeral, a debt once persisted or shared. |
| **Rich card content** | partial | Title, subtitle, an accent, and up to 3 legend-backed dots (a compact stand-in for labels/flags — a board-wide legend gives each dot a label and a hover tooltip). A card-body render hook for avatars, due dates, and other richer content is still absent. |
| **Grouped-mode drag / reparent** | absent | Grouped views are read/select; the flat view owns moves. |
| **Filter / search**, collapsible lanes, multi-select | absent | Common conveniences, secondary to the above. |

Deliberate non-goals — omitted by design, not pending:

- **Nesting beyond one level.** The parent/child relationship is exactly one
  deep. Deeper hierarchies are a different widget's problem.
- **Real-time / CRDT collaboration.** A local, single-viewer widget; concurrent
  editing is out of scope (and is where fractional-rank ordering would first
  earn its keep).
- **In-widget card create/edit.** The caller owns the data; the widget reports
  moves and selection, and does not mutate content beyond a card's column and
  order.

## Sources

Board UX and WIP: [IxDF — Kanban Boards](https://ixdf.org/literature/topics/kanban-boards),
[Businessmap — board features](https://businessmap.io/blog/best-kanban-board-features).
Sub-items and hierarchy:
[Atlassian community — swimlanes by parent](https://community.atlassian.com/t5/Jira-Software-questions/How-do-I-keep-sub-tasks-in-their-parent-item-s-swimlane/qaq-p/1593240),
[GitHub Blog — introducing sub-issues](https://github.blog/engineering/architecture-optimization/introducing-sub-issues-enhancing-issue-management-on-github/),
[GitHub Docs — parent/sub-issue progress](https://docs.github.com/en/issues/planning-and-tracking-with-projects/understanding-fields/about-parent-issue-and-sub-issue-progress-fields).
Ordering:
[Manuk Minasyan — kanban position management](https://www.manukminasyan.com/blog/kanban-boards-position-management),
[fractional indexing vs LexoRank](https://aexylus.com/utils/fractional-indexing-vs-lexorank).
Data model:
[Baserow — kanban view (status = single-select)](https://baserow.io/user-docs/guide-to-kanban-view).
Accessible interaction:
[ServiceNow Horizon — visual board](https://horizon.servicenow.com/workspace/components/now-visual-board),
[Kanboard — keyboard-first board](https://kanboard.io/).
