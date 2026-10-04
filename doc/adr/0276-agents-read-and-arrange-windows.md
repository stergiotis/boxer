---
type: adr
status: proposed
date: 2026-10-04
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0276: Agents read windows from keelson and arrange them under a task grant

## Context

[ADR-0275](./0275-imzero2-window-arrangement-rust-reports-go-decides.md) gave the person window management. Each frame Rust reports every window's geometry, stacking and content need, and Go computes arrangements, which the shell offers in a Window menu. The owner wants the chat app's coordinator to use the same facilities: discover the open windows, enumerate them with their state, and arrange them.

Two existing contracts bound the design:

- [ADR-0269](./0269-app-operations-a-command-query-contract-agents-drive-under-a-task-grant.md) gives an agent nothing except under a task grant the person approves. A grant names windows, a mode for each (observe, suggest, act) and a budget. Its `runtime.agent.list` shows only the task's own windows, and titles are untrusted text (§SD7). Agents get no synthetic input (§SD11).
- [ADR-0253](./0253-introspection-table-reads-as-a-bus-capability.md) makes reading an introspection table a bus capability, granted per table. `keelson('windows')` already lists the open windows, without geometry.

The owner's answers to the policy questions (2026-10-04):

- **Enumeration** goes through the keelson table, with all the window state the host has modelled there.
- **Arranging** may touch the whole desktop, in act mode.
- **Further verbs:** an agent may raise and place a window of its task, and arrange a chosen subset of windows.

## Decision

Window state becomes keelson data that any app can read under ADR-0253's per-table grant. The verbs that change the desktop become `runtime.agent.*` services, which inherit ADR-0269's grant, record and taint handling.

- **SD1 — `keelson('windows')` models a window's state.** The table keeps its columns and gains:
  - the outer rect `x`, `y`, `w`, `h` (logical points, viewport top-left origin);
  - `need_w` and `need_h` (ADR-0275 §SD4);
  - `stack` (the stacking rank: larger is further front, 0 unknown);
  - `collapsed`, `maximized`, `active` (the shell's active window), and `shown` (whether the window reported geometry last frame; a window opened this frame has not);
  - `agent_tasks` (the tasks whose grant holds the window: a window can sit in several tasks' grants, observers included).

  A one-row `keelson('desktop')` holds:
  - the work area `work_x`, `work_y`, `work_w`, `work_h`, and `work_shown` (false until a frame has shown a window);
  - the active window's key;
  - the arrangement in progress, empty when none.

  The render goroutine copies these into a snapshot under the host's lock once per frame, the way `keelson('frame_times')` is served. A read therefore sees the last completed frame.
- **SD2 — The chat reads windows by SQL.** The chat's manifest declares the `keelson.query` grants for `windows` and `desktop`. The coordinator gets a `query_windows` tool that takes a `SELECT` over those two tables, gated by `keelsonquery`'s default-deny gate. Titles in a result are wrapped as untrusted, as `list_windows` already does. `list_windows` stays: it is the task's view, with operations and load state.
- **SD3 — Three verbs.**
  - `runtime.agent.arrange` takes a command (an ADR-0275 arrangement) and optionally the window keys to arrange. Without keys it arranges every windowhost window. With keys it lays out only those windows in the work area and leaves the others where they are.
  - `runtime.agent.raise` brings one window to the front.
  - `runtime.agent.place` sets one window's outer rect, through the one-frame placement of ADR-0275 §SD3.

  Each verb replies once the change is queued. The geometry settles over the following frames, and the agent reads the result back from `keelson('windows')`.
- **SD4 — Authority.**
  - A grant gains a **desktop mode**. The person sees it in the request dialog and decides it like a window's mode. `arrange` requires desktop mode act.
  - `raise` and `place` require act mode on an entry for that window.
  - All three are refused under observe and suggest, with the remedy ADR-0269 already gives (widen the grant).
  - Each accepted call is an action record (ADR-0269 §SD9) with effect `view`.
  - These are acts of the host on window geometry, not input to an app, so ADR-0269 §SD11 is unaffected.
- **SD5 — The windowhost API.** Off the render thread, the host gains:
  - an arrangement over a subset of keys, beside `Arrange`;
  - `Place(key, Rect)`;
  - `Raise(key)`.

  Each queues under the host's lock and is applied by the next `Frame`, the way `pendingRaise` and `pendingArrange` are. The subset arrangement uses the same geometry with the other windows left out.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `keelson('windows')` schema | Columns added (SD1) | Readers selecting `*` (play's catalog, the app center) see more columns |
| `keelson('desktop')` | Added | Introspection catalog; `keelson.query` subject for it |
| `runtime.agent.arrange`, `.raise`, `.place` | Added | `agent.Service` dispatch, wire types, `HostI`, `agent.Client`, the dispatch test host and the chat's test host |
| Grant request | Desktop mode added (SD4) | Request dialog in host chrome; grant records; `keelson('agent_grants')` `desktop` column |
| Chat manifest | `keelson.query` grants for `windows`, `desktop` | `query_windows` tool, coordinator prompt |
| Exported Go API (`windowhost`) | Subset arrange, `Place`, `Raise`, the geometry snapshot | — |

## Alternatives

- **Add geometry to `runtime.agent.list`.** That list is the task's view by design (ADR-0269), and the owner asked for the desktop to be keelson data. A second, task-scoped copy of the same state would drift from the table.
- **A windowhost bus service outside the agent family.** It would need its own grant check, records and taint handling, all of which `runtime.agent.*` already has.
- **Arranging by synthetic drags.** Ruled out by ADR-0269 §SD11. It would also act through the person's pointer.
- **Arrange in suggest mode, confirmed by the person.** It keeps the person in every arrangement, but the owner chose act under an explicit desktop mode. The grant dialog is where the person decides.
- **Let an agent read windows without a grant.** Titles can name the person's documents; ADR-0253's per-table grant is the existing way to say who may read them.

## Consequences

### Positive

- One table serves the person (play, the app center) and the agent; nothing about a window's state is visible to one and not the other.
- The verbs are thin: placement and arrangement already exist (ADR-0275), so the new code is grant checks, wire types and queues.

### Negative

- An app granted `keelson.query` for `windows` reads every window's title, including windows outside any task. The grant names the table, not a subset of rows.
- A verb's reply says the change was queued, not where the windows ended up. An agent that needs the outcome reads the table a frame or more later, and an arrangement can take several frames (ADR-0275 §SD4).
- Desktop mode is a new item in an already dense request dialog.

### Neutral

- A window with short content ends shorter than the cell it was placed in (ADR-0275, update 2026-10-04). An agent placing such a window sees the shorter height in `h`.

## Verification plan

- Unit tests in `agent` cover the refusals: arrange without desktop act, and raise or place without act on the window. They also cover a record per accepted call. Tests in `providersgui` cover the new columns and the desktop table against a fake host.
- A chat scene drives the coordinator through a scripted model: it reads `keelson('windows')`, arranges, and reads again. Chat already has scripted-model scenes. It lives in the integration lane beside ADR-0275's `TestSceneWindowArrangements`.

## Status

Proposed 2026-10-04.

- **M1 — The keelson tables (SD1).** ✓ Without `agent_tasks`, which needs the agent service and comes with M3. The viewport size is not in `keelson('desktop')`: no host report carries it, and the work area is what arrangements use.
- **M2 — The windowhost API (SD5).**
- **M3 — The agent verbs and desktop mode (SD3, SD4), and `agent_tasks`.** ✓ `keelson('agent_grants')` gains a `desktop` column.
- **M4 — The chat's `query_windows` tool and grants (SD2).**
- **M5 — The scripted chat scene.**
