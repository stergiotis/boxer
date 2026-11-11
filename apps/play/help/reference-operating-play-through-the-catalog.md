---
type: reference
audience: agents operating the window through the catalog, and the person reading what they did
status: draft
title: Operating play through the catalog
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Operating play through the catalog

Play's operations catalog is what an agent reaches the window by: queries
that read the window as it stands and commands that change it, under a
task grant the person approved. The person's own gestures run through the
same handlers, so what an agent does and what a click does cannot drift.
This page lists every operation by name, says when each is unavailable or
refused, draws the two machines the window moves through, and states the
tables play reads and the datasets it publishes. The catalog itself carries
the arguments and result fields of every operation; this page is the map,
not the schema.

A test keeps the operation list below equal to the catalog in code, in
both directions.

## How a call fails

Four answers come back in place of a result, and each says what to do:

- **Refused** — the call cannot run as made, and the reason names the
  remedy: *the buffer is empty*; *parameters need a value first: from, to*;
  *no pane sankey*. Change the arguments or the window and call again.
- **Conflict** — the window moved under the call, and the reason says
  what to read again: *no parameter from in the buffer yet; slots follow a
  new buffer after a short debounce, read get_state again*; *no node recent
  in the buffer; it has main*. A command that named a base revision of a
  resource another writer has since moved is a conflict too.
- **Refused for a destination** — a run or a read the grant does not cover.
  The refusal names the destination the grant would have to list:
  `clickhouse:<host>` for the endpoint, `keelson:<table>` for an
  introspection table or a dataset alias, `keelson-bundle:<alias>` for a
  bundle's datasets, `http:basemap` for the Map pane's tiles. The remedy is
  a grant that lists it, not a different statement.
- **Unavailable** — the catalog declares the operation cannot run now.
  Only `run` and `query` have such a gate, and it is one reason: *a run is
  in flight*. `cancel_run` ends the run, or wait for it to land.

Every operation refuses with *the window has not mounted* before the
window's first frame.

## Reading the window

| Operation | Kind | What it does | Unavailable or refused when |
| --- | --- | --- | --- |
| `get_state` | query | the buffer, the parameters with their controls and defaults, the signals, Live, the main result's phase and notice, the destination a run needs, the bundle and the datasets followed | never, once mounted; read it first |
| `describe_result` | query | columns, handles, types and row count of a result: the one the panels draw, a pane's, or a drawn node's | the pane or node is not drawn (*not drawn: show_pane …*); a result id that is not the one held |
| `sample_rows` | query | up to 50 rows of a result as cell text, NULL kept apart from empty text | as `describe_result`; rows and an offset both given; a result id that is not the one held |
| `list_panes` | query | the panes in dock order: whether each can draw what it is fed and why not, its last status line, the node feeding it, the signals it writes, and the operations that read and drive it | never, once mounted |
| `get_query_graph` | query | the buffer's split: nodes, what each reads, the panes each feeds, the observed node and the bindings | the window has no query graph yet |
| `get_diagnostics` | query | whether the buffer parses and the server's verdict when not, skipped rewrites, unresolved leeway handles, the security class, the split, dropped emits, gloss directives that failed, the last run's full error | never, once mounted |
| `list_history` | query | this window's runs newest first, with the SQL and the signal values each shipped | never, once mounted |
| `get_detail` | query | one row as the Detail pane draws it: attributes by name, values, labels, SQL handles, canonical identity and temporal attributes | no row is selected and none is passed; the row is past the result |
| `get_canonical` | query | one leeway row's content digest, wire fingerprint and the CBOR items both are taken over | the result is not leeway-shaped; the row did not read as leeway; the row is longer than the byte bound |
| `get_schema` | query | the Schema pane's reading of the result it is fed: sections, membership channels, value columns, handles and canonical types | the pane is not fed a result |
| `list_glosses` | query | the glosses a column can render through, with their parameters and the spellings that declare them | never, once mounted |
| `complete_sql` | query | what may stand at a byte offset of a statement, as the Completion pane would offer | the offset is past the statement; listings no pane has probed yet are named under *not_ready* rather than refused |
| `list_snippets` | query | the snippet libraries' worked queries, or those matching words | no snippet library is registered |
| `read_snippet` | query | one snippet: what it does and its SQL | the snippet is not in a library |
| `list_functions` | query | the functions boxer adds to a query in play: server UDFs, client macros, host functions, where each runs and whether the endpoint has it | never, once mounted |
| `sql_flow` | query | a statement's clause graph or its column lineage, derived locally | the statement is empty or does not parse; the lens is neither *statement* nor *lineage* |

## Changing the buffer, the parameters and the signals

| Operation | Kind | What it does | Unavailable or refused when |
| --- | --- | --- | --- |
| `set_sql` | command, document | replaces the SQL buffer; the parameter slots follow after a short debounce | never, once mounted |
| `set_param` | command, document | sets one parameter in its tier: a pinned one rewrites its SET line, a live one writes its signal | the slot is not in the buffer yet (conflict: read `get_state` again); an enum value the slot does not offer; a value outside a range control's shape |
| `set_signal` | command, document | sets one signal with the task as its writer; `selection` is a row of a node's result, and `selection_node`, `selection_id` and `selection_key` follow it from that row | a signal a pane publishes and does not take back (`tl_from`, `vp_min_x`, `gv_selection`, …), naming the pane's command instead; a selection that is not a row number, names a node not on screen, or a row the result lacks; `node` with any signal but `selection` |
| `delete_signal` | command, document | deletes one held signal, so a parameter reading it falls back to unfilled or its default | nothing holds the signal; a signal a pane publishes |
| `set_run_options` | command, document | sets the conditions rewrite, which adds one column per WHERE condition | the endpoint offers no conditions rewrite |

## Running

| Operation | Kind | What it does | Unavailable or refused when |
| --- | --- | --- | --- |
| `run` | command, run, async | runs the buffer, or one statement of it, under the agent limits; the call stays running until the result lands and answers with the result's state | unavailable while *a run is in flight*; refused when the buffer is empty, when parameters need a value first, when both `subquery` and `statement` are given, when the window's class ceiling would block the substituted body, and when the agent limits refuse (below) |
| `query` | command, run, async | `set_sql`, `run` and `sample_rows` in one call: replaces the buffer, runs it under the agent limits and answers with the first rows | as `run`; a run the checks refuse leaves the buffer as it was |
| `cancel_run` | command, run | stops the run this task started while it is in flight; a run asked for in this frame and not started is withdrawn | no run is in flight; the run is the person's or another task's (*only the person cancels it*) |
| `observe_node` | command, document | makes every panel without a binding draw one node's result, or the sink's again; the node's statement is checked at the call as `run` checks the buffer | the node is not in the buffer's split; the class ceiling or the agent limits refuse the node's statement |
| `check` | external read | runs one assertion over the tables the main result's statement read, under the agent limits, and returns its one row and two digests | the window holds no result; the assertion does not parse, carries a format clause, names a table the statement did not read, or returns no row or more than one |
| `validate_sql` | external read | grammar, canonical form, play's rewrite and whether `run` would accept a statement, without running it | the statement is empty or longer than 16 KiB |
| `trace_rewrite` | external read | every client-side pass over a statement in order, with outcome, time and error, and the body that would ship | as `validate_sql` |
| `explain_sql` | external read | the server's EXPLAIN of a node or a statement: syntax tree, plan, pipeline, estimate or index use | the statement routes to the host's introspection plane, which has no EXPLAIN; the window has no endpoint; the agent limits refuse the statement |
| `list_tables` | external read | the endpoint's tables with engine, row count, comment, and which are leeway tables | the window has no endpoint; the grant does not list it |
| `describe_table` | external read | one table's columns, or a leeway table's sections, handles and membership channels | no table named; no such table; as `list_tables` |
| `lookup_docs` | external read | a name's documentation from the endpoint (`system.functions`, `system.documentation`, the bodies of its SQL UDFs) | the window has no documentation source, or its source does not answer agents; nothing named and the Docs pane shows nothing |
| `endpoint_functions` | external read | which user-defined functions the endpoint carries, what this build declares and it lacks, and the functions no roster declares | the window has no endpoint |

### The agent limits

A run an agent's work causes — `run`, `query`, `observe_node`, a Live
rerun after an agent's input, a `check`, an `explain_sql` — is judged on
the statement about to be sent, after the client-side rewrites, and fails
before sending when:

- the statement cannot be classified, or is not a plain read (the refusal
  names the witness: a mutating function, a DDL keyword);
- a `keelson()` table or dataset it names is not in the grant as
  `keelson:<table>`, as the dataset's global alias, or as its bundle's
  `keelson-bundle:<alias>` (a task runs without an entry on a dataset it
  published itself);
- the endpoint is not in the grant as `clickhouse:<host>`, unless the run
  goes to the host's introspection engine.

The limits fail a run; they never change it. The person's own Run is not
judged by them. A window opened with a security class ceiling (an applet's
promise) refuses, before the limits, a body whose substituted parameter
values would raise the class, naming the parameter.

### The write gates

Three gates sit between a statement and a write, and none of them is an
operation:

- Without `BOXER_PLAY_ALLOW_WRITES` the run path sends `readonly=2`, so the
  server refuses any INSERT or DDL that reaches it; a gated INSERT lands in
  the status line (`get_state` reports it as the result's notice) with the
  variable to set. An agent's run is sent `readonly=2` whatever the
  variable says.
- `BOXER_PLAY_APP_WRITES=off` stops play's own writes: the Series verdicts
  in `boxer.tslabels`. An agent's call that would cause one needs the
  endpoint in its grant, as a run does.
- A result labelled confined (`get_state` and `describe_result` say so)
  is written only to an endpoint declared as allowed to see sealed data;
  `publish_result` and `publish_projection` carry the label with the
  datasets.

## Panes

`show_pane` raises a pane; `list_panes` says what each draws and names
the operations that read and drive it. A pane that draws a result can be
fed from one node of the buffer's split with `bind_pane`; a pane that
reads named CTEs off the split (Timeline, Network, Sankey, Kanban, …)
cannot be bound, and the refusal names the CTEs to write with `set_sql`.

| Operation | Kind | What it does | Unavailable or refused when |
| --- | --- | --- | --- |
| `show_pane` | command, view | raises a pane by id | no pane of that id |
| `bind_pane` | command, document | feeds a panel from one split node, or from the main result again | the pane draws no result; the pane reads its CTEs by name; the node is not in the buffer (conflict, naming the nodes it has) |
| `get_table` | query | the Table pane's sort, page, display options, each column as the header captions it, and on request the page's rows | the Table has not drawn a result; the grouping has not drawn yet |
| `set_table_options` | command, document | sort, group and fold, turn the page, rows per page, raw cells, the leeway display options | no column of that name; a page past the last; `descending` without a sort; expand or collapse before a grouping has drawn; leeway display options on a result that is not leeway-shaped; per-attribute granularity with a sort or a grouping |
| `get_chart` | query | the Chart pane's reading, axis, series and what its caps left out | the pane is not drawn |
| `set_chart_options` | command, document | the mark (bar, line, scatter, heatmap) or the log axis; drawing only | a mark this chart does not offer; a log axis when a value drawn is zero, negative or missing |
| `get_dist` | query | per series the count, quantiles, band width and Wasserstein-1 distance to the baseline; the views the result admits | the pane is not drawn |
| `set_dist_options` | command, document | the view: ecdf, shift, boxen or histogram; drawing only | a view this result does not admit |
| `get_series` | query | the time range, the Δt class with the finding and its scaffold SQL, each lane's range, smoothing and envelope, the score and span overlays, the change point scaffolds | the pane is not drawn |
| `set_series_options` | command, document | smoothing, its half-width, or the envelope; drawing only | a half-width outside the kernel's range, which the refusal names |
| `get_timeline` | query | mode, events and skipped rows, lanes, extent, brushed window, selected row and bands | the pane is not drawn |
| `set_timeline_options` | command, document | the now line | never, once the pane exists |
| `set_timeline_window` | command, document | sets or clears the brushed window `tl_from` and `tl_to` carry | `from` not before `to`; one of the pair missing; `clear` with a window |
| `get_treemap` | query | the whole and the children of a container, the colour channel, the pinned leaf, the drill, and what the build dropped | the pane is not drawn; no node at the path |
| `set_treemap_options` | command, document | what the fill encodes and how deep the cells nest | a depth outside the range; a colour channel the result lacks (no `color` column) |
| `select_treemap_node` | command, document | pins a leaf, or drops the pin; its key becomes `selection_key` | the treemap has no cells; no leaf at the path or label; `clear` with a path |
| `get_icicle` | query | the frames under a path or every frame by its own value, prune, settings, the pinned frame | the pane is not drawn; no frame at the path |
| `set_icicle_options` | command, document | orientation, sibling order, colouring, prune threshold, labels | a value outside the option's set |
| `select_icicle_frame` | command, document | pins a frame, or drops the pin; its key becomes `selection_key` | the icicle has no frames laid out; no frame at the path or label |
| `get_kanban` | query | each lane's card count and dot sums, the lanes CTE's verdict, rows past the cap, a lane's cards | the pane is not drawn; the board has no such lane |
| `get_cards` | query | the page the Cards pane drew: each card's text, facts and warnings, the hero's media type | the pane is not drawn |
| `set_cards_options` | command, document | the page, cards per page, density, hero aspect | a page past the last; a page size, density or aspect outside the set; a hero aspect when the cards have no `card_hero` |
| `get_world` | query | the country and value columns taken, the legend's range, how many rows resolved, each country with its value | the pane is not drawn |
| `set_world_options` | command, document | the value column (auto, presence or a numeric column) and the projection | a column before the pane has drawn; a projection outside the set |
| `get_vectorfield` | query | the field's description, the step on display, per-step speeds in the settled view, the phases' errors, the sites | the field has not been described yet (the reading says why) |
| `set_vectorfield_view` | command, view | frame the field or a box, show a step or a time, start or stop playback, set its speed | the map has not laid out yet; both `fit_field` and `bounds`; both `step` and `time`; a rate outside 0.25–4 |
| `get_network` | query | counts and cap, the layout's error, the highlighted vertex, a page of vertices and edges | the pane is not drawn |
| `set_network_options` | command, document | the layout direction | a direction outside the set |
| `select_network_node` | command, document | highlights a vertex, or drops the highlight; its id becomes `selection_key` | the pane has no vertices; no vertex of that id |
| `get_graphview` | query | counts and caps, each setting and who decided it, settle state, selection and hover, notes, a page of vertices with graph metrics | the pane is not drawn |
| `set_graphview_options` | command, document | layout, orientation, size channel, auras, hold, basemap, pause; fit, relayout and settle act once | the pane has no graph; basemap on a graph whose vertices carry no lat and lon; a layout outside the set |
| `select_graphview_nodes` | command, document | selects vertices, or none; `gv_selection` carries them and `selection_key` the last | the pane has no vertices; an id the graph lacks |
| `get_sankey` | query | the conserved total, each node's in- and outflow, each flow's share, the mode used, unbalanced nodes, what the build summed, dropped or capped | the pane is not drawn |
| `set_sankey_options` | command, document | the mode: auto, sankey or alluvial | a mode outside the set |
| `select_sankey_node` | command, document | pins a node or a ribbon, or drops the pin; a pinned node's id becomes `selection_key` | the pane has no diagram; no node of that id; no ribbon between the source and target |
| `get_files` | query | a directory of the tree the Files pane interned, or the paths a filter matches, with sizes, times and counts | the pane has not interned a result; the tree has no such directory |
| `set_files_options` | command, document | directory, mode, quick filter, hidden names, order | a mode or sort outside the set; a directory the tree lacks |
| `select_files_path` | command, document | selects one path as a click does; `selection_key` is written, and `selection` when a row named the path | the tree has no such path; the root, which the browser does not select |
| `get_chat_pane` | query | the transcript the Chat pane folded: messages in order, senders, replies, flags, reactions, what the fold skipped | the pane is not drawn |
| `get_map` | query | the Map pane's table source, render and colour expression, sampling, the raster on screen and its errors, camera, selected area and the Timeline window it filters by | the pane is not drawn |
| `set_map_view` | command, view | centre and zoom, or a lat/lon box; the raster is fetched once for the view and the `vp_*` signals written | the map has not laid out yet; both a centre and bounds; a centre or bounds outside their ranges; the grant does not list `http:basemap` |
| `set_map_options` | command, document | time column, render, sampling, ladder, cache, readout; select or clear the area | the table source and colour expression, which are the person's; an area and `clear_area` together; a value outside its range |

## Datasets, bundles and the projection

| Operation | Kind | What it does | Unavailable or refused when |
| --- | --- | --- | --- |
| `bind_dataset` | command, document | binds an ad-hoc dataset alias in this window, so the buffer reads it with `keelson('<alias>')`; the window waits for a dataset not yet live and follows republishes | an alias that is not a bare identifier; a dataset handle in place of its alias; the window has no bus |
| `list_datasets` | external read | the datasets bound here: the alias to read, the destination a run needs, the columns, and why a waiting alias waits | the window has no bus |
| `list_bundles` | external read | the live ad-hoc bundles with publisher, local names and each column summarised, and this window's last publish | the window has no bus; the catalog could not be read |
| `open_bundle` | command, document | opens a bundle: its document replaces the buffer, its datasets are bound under the names the document reads, the window follows its republishes and runs | the host links no applet document parser; the host serves no introspection endpoint; an alias that is not a bare identifier |
| `publish_result` | command, consequential | publishes the main result as a bundle: the rows as one dataset and an applet document reading them; the person confirms unless the grant lists `publish:<prefix>` | the result is loading, failed or a prefix cut by the row cap; a publish from this window is in flight; the bundle alias or local name is not a bare identifier; the window has no bus |
| `compute_projection` | command, view | computes the Projection pane over the result it is fed: neighbour graph, clusters, 2-D layout; the pane is raised | the window has no Projection pane; the pane cannot draw this result; a projection is running; too few rows |
| `get_projection` | query | the run's status, clusters and sizes, the layout, a page of points | never, once mounted; before a run it says so |
| `explain_clusters` | query | per cluster a SQL rule over the result's columns with precision and recall, and what sets it apart | no projection to explain; a cluster outside the range |
| `get_archetypes` | query | per cluster what its rows typically hold, the extremes and the rows that break the pattern | no projection to read; a cluster outside the range |
| `cancel_projection` | command, view | stops the projection run this task started | no projection is running; the projection is the person's or another task's |
| `publish_projection` | command, consequential | publishes the finished projection as `keelson('<name>')` and `keelson('<name>_rules')` and binds both here; the person confirms unless the grant lists `publish:<prefix>` | no projection to publish; the result it was computed over is gone or moved; the layout has not been drawn (*show_pane projection, then publish*); a publish is in flight; the name is not a bare identifier within 32 bytes |

## The two machines

Two operations per machine come with it: `*_state` reads the current
state, the transitions allowed from it and the latest ones taken;
`*_machine` reads every state and every allowed transition. Both read
without a grant entry.

### The main result: `query_state`, `query_machine`

| Operation | Kind | What it does | Unavailable or refused when |
| --- | --- | --- | --- |
| `query_state` | query | the main result's current phase, the edges out of it, the last 16 transitions | never, once mounted |
| `query_machine` | query | every phase of the main result's lifecycle and every edge | never, once mounted |

The phases, as `get_state` reports them in `result.phase`:

| Phase | Meaning | Moves to |
| --- | --- | --- |
| idle | no run has completed yet | `running` on Run |
| running | a run is in flight; `result.progress` and `result.run_by` say whose | `rows`, `empty` or `failed` when it lands |
| rows | the last run returned rows | `running` on Run; `rows (stale)` on an input change |
| empty | the last run returned no rows | `running` on Run; `empty (stale)` on an input change |
| failed | the last run failed or was cancelled; `result.error` has the text | `running` on Run; `failed (stale)` on an input change |
| rows (stale), empty (stale), failed (stale) | the result shown is of a buffer or signal values that have since moved | `running` on Run; back to the fresh twin on revert |

*Run* is the Run button, Ctrl+Enter, `run`, `query`, or a Live rerun. An
*input change* is a buffer edit (`set_sql`, typing, `set_param` on a
pinned slot) or a move of a signal the last run read (`set_signal`,
`delete_signal`, `set_param` on a live slot, a pane publishing one).
*Revert* is the buffer and the signals reading as the last run shipped
them again. A cancel settles as `failed`, not `idle`. A run that finishes
within one frame may skip `running`.

### The projection: `projection_state`, `projection_machine`

| Operation | Kind | What it does | Unavailable or refused when |
| --- | --- | --- | --- |
| `projection_state` | query | the projection's current phase, the edges out of it, the last 16 transitions | never, once mounted |
| `projection_machine` | query | every phase of the projection's lifecycle and every edge | never, once mounted |

| Phase | Meaning | Moves to |
| --- | --- | --- |
| idle | no projection over the result the pane is fed | `extracting` on Compute |
| extracting | features are being extracted from the rows | `running` when they are ready; `cancelling` on Cancel; `failed` |
| running | the neighbour graph, clusters and layout are being computed | `done`; `cancelling` on Cancel; `failed` |
| cancelling | a cancel is draining the run | `cancelled` when drained; `failed` |
| done | a projection is held; `get_projection`, `explain_clusters`, `get_archetypes` and `publish_projection` read it | `idle` when the fed result changes (any run in this window); `extracting` on Recompute |
| failed | the run failed; `get_projection` has the error | `idle` when the fed result changes; `extracting` on retry |
| cancelled | the run was cancelled | `idle` when the fed result changes; `extracting` on retry |

*Compute* and *Recompute* are the Compute projection button or
`compute_projection`; *Cancel* is the pane's Cancel button or
`cancel_projection`. A new main result drops a held projection unless
`publish_projection` kept it as datasets.

## Preconditions a refusal names

The reasons recur across operations, so they are listed once:

- **The window has not mounted** — before the first frame; wait and call
  again.
- **The pane is not drawn** — a lazy pane draws only while its tab is in
  front, so its `get_<pane>` reads the last draw or refuses with
  `show_pane <id>`; `list_panes` says which panes are lazy and visible.
- **The pane cannot draw what it is fed** — its `list_panes` row carries
  the reason (a missing column, the wrong shape); the remedy is the SQL,
  through `set_sql`.
- **The result is not held** — `describe_result`, `sample_rows` and
  `check` take a result id; one that is not the result now held is
  refused as *another result is held now*, so a sample never mixes two
  runs. Read `get_state` for the id.
- **The result is not leeway-shaped** — `get_canonical` and the leeway
  display options need leeway columns; `describe_result` says whether
  they are.
- **A dataset is not bound** — a run naming an alias the window does not
  hold waits or fails; `list_datasets` says which aliases wait and why,
  `bind_dataset` adds one, and the grant must list it.
- **The revision moved** — a command made over a base revision another
  writer has moved since is a conflict: read the resource again and
  repeat the call.
- **The buffer changed since the split** — `get_query_graph` and
  `bind_pane` name the nodes of the last run's split; a buffer changed
  since splits again on the next run.
- **Only the person cancels it** — `cancel_run` and `cancel_projection`
  stop this task's own work and nothing else's.
- **The value is not in the set** — an option command names the values it
  takes (*mode is auto, sankey or alluvial*, *density is S, M or L*,
  *page_size is 50, 100, 500, 1000 or 10000*); pass one of them.

## What play reads and publishes

**Tables it reads.** The endpoint's tables, under the grant's
`clickhouse:<host>`. Introspection tables through `keelson('<table>')`,
each under `keelson:<table>`, served by the host's introspection engine.
For `lookup_docs` and the Docs pane, `system.functions` and
`system.documentation` of the endpoint. The snippet libraries are files of
this help book and of the libraries other apps register; no table.

**Tables it writes.** `boxer.tslabels`, for the verdicts the person records
on the Series pane's spans; no operation records one, and
`BOXER_PLAY_APP_WRITES=off` stops it.

**Datasets it publishes**, all ad-hoc, all alive for the life of the
window that published them:

- `publish_result`'s bundle: one dataset of the main result's rows under
  the local name the document reads (`result` unless given), and the
  document.
- `publish_projection`'s pair: `<name>` with one row per entity and
  `<name>_rules` with one row per cluster rule, bound in the same window.
- The fixtures the Series and Cards panes offer when a result is empty:
  `fixture_series` and `fixture_truth`, `fixture_cards`. They are
  ordinary datasets with a scaffold query the person can read and change;
  nothing in play knows it is looking at a fixture.

## The Series pane's honesty lines

What the Series pane refuses to do is as much a part of its reading as
what it draws, and `get_series` reports each:

- **No interpolation, no fill, no resampling.** A null value breaks the
  line and a gap in the time grid breaks the line. Smoothing and the
  envelope are drawing only; every reading and every analysis uses the
  raw values, and the envelope keeps every extreme.
- **An irregular grid is a finding, not an error.** The pane classifies
  the Δt as regular, regular with gaps, irregular, not ordered by time, or
  too short to classify. An irregular or gapped series still charts,
  time-true; only the analysis tier refuses, and the refusal carries the
  SQL that would fix it — add ORDER BY, add WITH FILL, add GROUP BY —
  with the measured step filled in. `get_series` returns that scaffold;
  apply it with `set_sql`, never silently.
- **One algorithm per name.** A score lane names its detector and window,
  and the caption says whether it is causal (each value uses only what
  came before, so replaying is the backtest) or two-sided (every value
  sees the whole series, so it is not what an alert would have known).
  Beside it the pane draws a moving-average residual at the same window
  as the baseline a detector has to beat, and shades the warm-up region
  where a detector reports nothing yet.
- **Change points are spelled in SQL.** `get_series` lists the pane's
  Change points menu under `changes`: one scaffold per question (shifts in
  level, in level or spread, despite outliers, at a known time scale, what
  a monitor would have alarmed on, the probability a change just
  happened), each with the method's assumption in `about`. Put the current
  query into the scaffold's `base` and apply it with `set_sql`; the result
  arrives as the `spans` and `scores` overlays, and `sample_rows` on those
  nodes reads the segments' `level`, `spread`, `samples` and `shift`.
  `span_kind` says whether a span is a flagged extent, a segment (the
  boundaries are the changes, and no segment is a detection) or an alarm
  (from the estimated change to its detection). Every method reads the
  samples as independent: say so when reporting a change count, and read
  it against the `tsPeltMeanPath` table before calling it a finding.
- **A threshold is drawn only when the method supplies one.** `threshold`
  and `threshold_is` carry the level and the condition it holds under;
  repeat the condition with the number.
- **Verdicts are the person's.** Spans carry recorded verdicts; no
  operation records one.
