---
type: explanation
audience: whoever decides whether the app host (`public/keelson/runtime/app`, `windowhost`) gains lifecycle hooks for work that outlives a frame — a problem statement, read before any ADR that would add them
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** A problem statement, not a decision.
> Written 2026-10-11 from the chat app's coordinator as it landed with the
> agent harness (ADR-0308 §SD9, ADR-0303 §SD9). Nothing here proposes an
> option; it says what is missing and what the missing pieces cost today.

# The app host has no hook for work that outlives a frame

## The contract today

An app is `Mount` once, `Frame` per render pass per open window, `Unmount`
once (`public/keelson/runtime/app/app.go`). The mount context gives a
`Cancel() <-chan struct{}`, a bus client, storage and ids. Closing a window
and shutting the process down take the same edge (`windowhost.reapWindow`,
ADR-0188): close the stop channel, save the workingset, call `Unmount` on
the render goroutine, then close the instance's bus client. There is no
step between `Unmount` returning and the bus client closing.

`Manifest.BackgroundTickHz` is declared and documented ("unfocused tick
rate; 0 = no tick"). No host reads it. `SurfaceHeadless` is declared and
unused (ADR-0302).

`bgjob.Runner` is the shape every app uses for a long call: it runs the
function under `context.WithCancel(context.Background())`, so the job is
tied to nothing the host owns, and the app polls it in `Frame` to land the
result on the render thread.

Watchbill is not this. It runs durable jobs that need no person, claimed by
polling a ClickHouse row, re-run from the top when abandoned, with no result
channel back to the enqueuer. ADR-0303 rejected it for the chat turn as
option O5, and the reasons hold: a turn blocks on the person, returns a
large value to the render thread, and must never re-send a call that may
have left the process.

## What a chat turn needs that the contract does not give

A turn is one in-process call that can take minutes, waits on the person
mid-way (ask, approve, endorse), and writes a journal entry before and after
every act (ADR-0308). Four things follow, and each is hand-built in
`apps/chat` today:

1. **Finish the tail after cancel.** When the person stops a turn, the
   model call must die but the journal must still record that it was
   stopped. `journalCtx` (`chat_journal.go`) is `context.WithoutCancel` on
   every journal write; the client bounds each write with its own timeout.

2. **Retire with a drain.** When the window moves on (new conversation,
   resume of another run, window close) the old run must be closed in the
   journal, but not before the turn that is still writing has left. The
   coordinator carries `turns`, `retired`, `closed`, `closeOnce`;
   `retire()` closes now if idle and otherwise leaves the close to the last
   `leaveTurn`; `enterTurn` refuses after retirement; `awaitClosed` lets a
   resume wait for the late close (ADR-0308 §SD9, one writer per run).
   `App.retiring` keeps those coordinators by run id until they are closed.

3. **Land a result on the render thread, once.** `drain()` in `Frame` polls
   the turn runner; `runAction`/`drainAction` carry an `after func()` that
   runs only when the action succeeded; `deleting` names the run a delete is
   tombstoning so a turn or resume refuses to touch it.

4. **Resume.** Nothing restores in-flight work at host start. Resume is a
   person's click in the conversations panel that reopens the writer, folds
   the journal, settles open calls as unknown, and adopts the run. That is
   by design for now (ADR-0303 §SD9: a run nobody started has no principal),
   but it means an open run after a crash is invisible until someone looks.

## Where it breaks, or nearly does

- **Bus closed under the tail.** `Unmount` cancels the turn and calls
  `retire()`. The close that `retire()` schedules runs after `Unmount`
  returns, on the per-instance bus client the host is about to close. A
  late close or tail write gets the closed-client error that `AppI`'s doc
  says a goroutine outliving `Unmount` will see. The run is then left open
  and the next resume settles it as unknown. The chat tests cover the
  in-window retire; nothing covers the window-close race, because nothing
  can wait for it.
- **Every app re-implements the same four things**, differently. The flags
  above are the second such set in the tree (watchbill's worker has its own
  `stopping`, run map and settle-with-background-context). Both review
  rounds found their worst chat defects here: a cancelled context poisoning
  the writer, a closed run that could not resume, a delete racing a turn.
- **A turn is not a keelson task.** `bgjob` is started with no task handle,
  so the turn does not appear in the task monitor, cannot be cancelled from
  there, and its progress is copied by hand from the coordinator each frame.
- **No background tick** means a window that is collapsed or unfocused
  still has to be rendered for its result to land. The declared
  `BackgroundTickHz` would be the hook; it does nothing.

## What a hook would have to provide

Stated as requirements, not as a design:

- A **retire step with a deadline** between `Unmount` and the bus close:
  the host asks the app to finish, waits off the render goroutine, bounded
  per window and by the process shutdown grace, then unloads. Chat's
  `retire`/`turns`/`closed`/`awaitClosed` and the window-close race go away.
- A **cancel with acknowledgement**: `Cancel()` plus a way for app
  goroutines to register, so the host can wait for them rather than a
  channel nobody answers. `bgjob` would derive from the mount context and
  take the task handle, so a turn is a task.
- A **completion hook on the render thread**: the host calls back when a
  background call settles, with the result, once. Replaces the per-runner
  `drain*` and `afterAction`.
- A **background tick** that honours `BackgroundTickHz`, so results land
  without a render.
- **Resume on boot** stays out until a principal for an unattended run
  exists (ADR-0303 §SD9, ADR-0308 §SD11). The hook that would carry it is
  the retire step's mirror: the host lists the app's open runs and offers
  them. That is a later ADR.

## What to read

- `public/keelson/runtime/app/app.go`, `manifest.go` (`BackgroundTickHz`),
  `EXPLANATION.md`
- `public/keelson/runtime/windowhost/windowhost.go` (`reapWindow`)
- `public/keelson/runtime/bgjob/bgjob.go`
- `apps/chat/chat_journal.go` (`journalCtx`, `retire`, `enterTurn`,
  `leaveTurn`, `closeJournal`, `awaitClosed`), `chat.go` (`retiring`,
  `deleting`, `drain`), `chat_actions.go` (`runAction`, `drainAction`),
  `chat_resume.go`, `chat_lifecycle_test.go`
- ADR-0188 (window reap), ADR-0269 §SD6 (grants and instances do not
  survive the process), ADR-0303 §SD9 and O5, ADR-0308 §SD9 and §SD11
- `doc/explanation/watchbill-architecture.md` for what watchbill is instead
