---
type: how-to
audience: engineer with a specific task
status: draft
# reviewed-by: "@<handle>"   # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD  # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# How to verify dock tabs against the lost-send class (tab walk)

Scripted screenshots systematically miss a whole bug class: a
`BOXER_PLAY_FOCUS_*` capture starts **on** the tab under test, so the
tab's uploads land from frame 1. The failure mode lives on the other path —
Go runs every tab body every frame, but the host discards inactive tabs'
buffers, so send-once state (texture uploads, one-shot ops, delta streams)
desynchronizes exactly when a tab is **activated by click**, or re-activated
after the ~10 s idle texture eviction. See the imzero2 skill's "Lost Sends
(Host-Skippable Regions)" pitfall for the invariant and the standard fixes.

The verification shape that catches it — walk the tabs *interactively*:

1. Launch the app live with the inspection port (pick a free port; 5719 may
   be held by a concurrent session):

   ```sh
   EGUI_INSPECTION=127.0.0.1:5799 HMI_BUILD=0 CLICKHOUSE_USER=default \
     BOXER_PLAY_SQL="<a query every pane can claim>" \
     BOXER_PLAY_AUTORUN=1 ./rust/imzero2/hmi.sh --launch play &
   ```

2. Attach the egui MCP driver (`attach`, port 5799) and wait for the result
   (`wait_for` on the status-bar row count).

3. For **each** tab, in an order where every tab is entered FROM another
   tab (the default-active tab must also be left and re-entered):
   - click the tab's title cell (`query_tree` the tab-bar row for cells;
     titles are painted, not accessible — locate positionally),
   - `wait_for` a tab-specific content marker,
   - assert the pane's content nodes have **non-trivial bounds** (the
     lost-send signature is a 0×0 content node where a texture should be).

4. Add the **eviction leg** for texture-bearing tabs (Map, World, imztop
   panels): activate, leave for ≥ 12 s (past the 600-frame LRU), re-enter,
   and re-assert content bounds. Interact once (hover) to confirm readouts
   still track.

5. One-shot settings (`BOXER_PLAY_MAP_ZOOM`, …) are verified the same
   way: set the knob, launch with a *different* tab focused, click into the
   target tab, and assert the setting took effect (camera zoom via the tab's
   own readouts).

Caveats: the MCP `screenshot` tool terminates this windowed client — verify
via tree reads (`get_node` bounds, label values), and use the
`BOXER_PLAY_SCREENSHOT` SVG path when a visual record is needed. Kill
only your own instance afterwards (match the inspection port in
`/proc/<pid>/environ`); concurrent sessions share the default port.
