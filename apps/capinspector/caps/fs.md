---
type: explanation
audience: contributors
status: draft
---

> **Status: draft — pre-human-review.** Rendered by the capability
> inspector for the fs.* cap. Refine if the Powerbox grows additional
> handle ops past the M2.6 / fs.watch shape.

# fs.* Powerbox

User-mediated filesystem access. The path is never exposed to the
app — the app addresses the file via an opaque handle subject the
broker mints on resolve.

**Dialog flow (read / write / bundle):**

1. App publishes `fs.dialog.read` (or `.write` / `.bundle`).
2. `fsbroker.Service` queues a `PendingRequest` and replies are
   suspended on the request inbox.
3. `pickerbridge.Bridge` (running every frame as an overlay above the
   window host body) sees the pending request and drives the egui
   file picker.
4. On user selection, `Service.Resolve(reqId, path)` mints a uuid,
   registers `(uuid, path, mode)`, augments the requesting client's
   caps with `fs.handle.{uuid}.>` (Pub direction for read/write/bundle
   handles), and replies with
   `DialogReply{Granted: true, HandleSubjectPrefix: "fs.handle.{uuid}"}`.
5. App publishes `fs.handle.{uuid}.read` to fetch the file contents
   over the same bus.

User clicks Cancel → `Service.Cancel(reqId)` → `DialogReply{Granted:
false, Reason: ...}` — the app receives a reply on the same inbox so
it doesn't time out.

**Watch flow (fs.dialog.watch — streaming directory changes):**

1. App publishes `fs.dialog.watch`. Same queueing path as the other
   dialogs; the picker overlay opens with the "Pick folder to watch"
   header.
2. On user selection, `Service.Resolve` mints a `HandleModeWatch`
   handle and augments the app's caps with `fs.handle.{uuid}.>` at
   `CapDirectionBoth` — the Sub direction is the wrinkle that lets the
   app subscribe to the streaming event subject.
3. App subscribes to `fs.handle.{uuid}.event` *before* starting the
   watch so early kernel events aren't missed.
4. App publishes `fs.handle.{uuid}.watch` with an optional
   `WatchRequest{PollFallback, PollIntervalMs, Recursive}` payload.
   The broker picks the backend via `statfs` on the path; reply names
   which one was chosen.
5. Each filesystem change appears as a `WatchEvent{Kind, Name, Cookie,
   Ts}` payload on the event subject. `Kind` covers Create / Delete /
   Modify / Attrib / RenameFrom / RenameTo / Overflow / Closed.
   `Name` is the basename relative to the watched directory in
   single-level mode, or a forward-slash relpath ("sub/file.txt",
   "deep/nested/leaf.txt") in recursive mode. Absolute paths stay
   inside the broker either way.
6. Teardown: `fs.handle.{uuid}.unwatch` stops the stream and keeps the
   handle alive; `fs.handle.{uuid}.close` evicts the handle and tears
   down any active watch implicitly.

**Backends within `fsbroker` (per-watch strategy, not Powerbox
alternatives):**

- **inotify** — primary on Linux. `unix.InotifyInit1(IN_NONBLOCK|IN_CLOEXEC)`
  + `InotifyAddWatch` with a fixed event mask. In single-level mode
  one watch covers the root path. In recursive mode an initial
  `filepath.WalkDir` walks the subtree and `InotifyAddWatch`es every
  directory it finds; a `wd → relDir` map translates kernel events
  back into watch-root-relative paths, and new subdirectories appearing
  via `IN_CREATE+IN_ISDIR` / `IN_MOVED_TO+IN_ISDIR` are AddWatched
  dynamically. Symlinks are not followed; `max_user_watches` exhaustion
  is best-effort skipped.
- **poller** — `time.Ticker` + `os.ReadDir` (or `filepath.WalkDir` in
  recursive mode) + mtime/size diff against a snapshot. Auto-selected
  via `Statfs.Type` for PROC / SYSFS / NFS / FUSE / CIFS
  (inotify-blind filesystems). Forced via `WatchRequest.PollFallback`.
  Default 500ms interval, clamped to a 100ms floor.

`IN_Q_OVERFLOW` from the kernel surfaces as a `WatchEventOverflow`
payload — the app's signal to rescan from scratch. Channel-full inside
the broker has the same effect.

**Where to look in the code:**

- `runtime/fsbroker/service.go` — broker (dialog queue + handle ops)
- `runtime/fsbroker/watcher.go` — inotify + poller backends
- `runtime/fsbroker/pickerbridge/bridge.go` — picker overlay driver
- `imzero2/egui2/widgets/filepicker/` — the egui widget
- `capdemo/` — first consumer; see `runPick` for reads and
  `runWatchPick` / `handleWatchEvent` for streaming watches

**ADR reference:** §SD7 (Powerbox pattern); 2026-05-14 amendment
"fs.watch: streaming filesystem-change notifications over the bus".
