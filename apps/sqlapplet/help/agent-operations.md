---
type: reference
audience: end-user
status: draft
title: Operating a SQL applet as an agent
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Operating a SQL applet as an agent

Every window minted from an applet document serves an operations catalog
(ADR-0269). The catalog is play's: the window holds an attenuated SQL
playground whose buffer is the document's SQL, and the operations it
offers are play's own, under play's names, served by that playground with
play's availability and agent limits. This page is shared by every applet
book; the applet's own page beside it says what the SQL reads, and
`keelson('operations')` lists the catalog with its arguments.

## What a minted applet serves

The subset is the one ADR-0288 §SD8 declares for an operable bundle view,
and play declares it beside its catalog (`play.OperableOperations`), so
what is offered here and in a bundle view is one list.

Reading the window:

- `get_state` reads the buffer, the parameters, the signals, Live and the
  main result's phase.
- `describe_result` describes a result's columns, handles, types and row
  count; `sample_rows` reads up to 50 of its rows as cell text.
- `list_panes` lists the panes with whether each can draw what it is fed
  and why not, and names the operations that read and drive each.
- `list_history` lists this window's runs, newest first, with the SQL each
  shipped and the signal values it sent.

Driving the window:

- `run` runs the buffer, or one statement of it, under the agent limits;
  `cancel_run` stops the run the task started while it is in flight.
- `set_param` sets one parameter in its tier; a change re-runs when the
  applet is Live.
- `show_pane` raises a pane.

Per pane, when the document shows it: the pane's reading, its options and
its selection. Each selection command writes `selection_key`, which a
query filtering on it re-reads.

| Pane | Operations |
| --- | --- |
| table | `get_table`, `set_table_options` |
| projection | `get_projection` |
| timeline | `get_timeline`, `set_timeline_options`, `set_timeline_window` |
| map | `get_map`, `set_map_options`, `set_map_view` |
| vectorfield | `get_vectorfield`, `set_vectorfield_view` |
| world | `get_world`, `set_world_options` |
| kanban | `get_kanban` |
| chat | `get_chat_pane` |
| cards | `get_cards`, `set_cards_options` |
| network | `get_network`, `set_network_options`, `select_network_node` |
| graphview | `get_graphview`, `set_graphview_options`, `select_graphview_nodes` |
| sankey | `get_sankey`, `set_sankey_options`, `select_sankey_node` |
| dist | `get_dist`, `set_dist_options` |
| icicle | `get_icicle`, `set_icicle_options`, `select_icicle_frame` |
| series | `get_series`, `set_series_options` |
| treemap | `get_treemap`, `set_treemap_options`, `select_treemap_node` |
| chart | `get_chart`, `set_chart_options` |
| files | `get_files`, `set_files_options`, `select_files_path` |

The resources these read and write are play's: `sql`, `params`, `signals`,
`result`, `panes`, `followed_datasets`, `bundle`, and one per pane
(`table`, `projection`, `timeline`, `map`, `vectorfield`, `world`, `cards`,
`network`, `graphview`, `sankey`, `dist`, `icicle`, `series`, `treemap`,
`chart`, `files`), each the pane's settings and selection.

## What is absent, and why

The document is the author's (ADR-0132 §SD3), so nothing that changes it
is offered: no `set_sql`, no `bind_dataset`, no `publish_result`,
`list_bundles` or `open_bundle`. A changed document is a new applet,
written in play and saved through the applet creator; a changed dataset is
a new bundle, published in play. The authoring tools are absent for the
same reason: explain, trace, flow, completion, the Projection's compute,
cancel, explain-clusters and publish, and `get_detail`, which reads the
window's side panel. Of play's diagnostics only `list_panes`' verdicts and
the last run's error travel.

A pane the document's `tabs:` pruned is declared and reported unavailable
rather than left out, since the catalog is one static value every minted
manifest shares; the refusal names the pane and says the document's tabs
do not name it. Before the window mounts, every operation is unavailable
with that one reason. A `run` is judged by play's agent limits, which fail
a run and never change it, and the agent's run is stamped as the task's in
the window's history.

## How a bundle binds datasets

An applet whose SQL reads ad-hoc datasets declares their aliases in its
frontmatter (`datasets:`, with `datasets_hint:` for how to produce them).
The window follows each alias (ADR-0240 §SD6): a dataset published under
it binds, a retract unbinds, and the result panes show a notice naming the
waiting aliases until they bind, then the window re-runs by itself. A
window binds; it never publishes.

A bundle is an applet document published together with the datasets it
reads (ADR-0288 §SD1). Inside the document a dataset is named by its local
name, and the service mints the global alias as `<bundle>__<local>`, so
the document is never rewritten. play opens a bundle by launch config or
`open_bundle`, binds the datasets under their local names on the
introspection endpoint, and follows the bundle through republish and
retract; a receiver embeds one as a bundle view, which offers this same
subset under a `bundle_` prefix with a `bundle_view` argument
(`bundle_list_views` lists the views). A grant names the bundle or the
global alias, never a local name: two windows' `keelson('stats')` are two
datasets, and play judges a run by each bound name's global alias.
