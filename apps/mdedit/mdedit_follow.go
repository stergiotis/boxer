package mdedit

// Follow-the-file. While a document opened from a file is unmodified, the
// buffer follows the file on disk: an external write reloads it, so mdedit
// reads well beside a pipeline or another editor. The moment the buffer holds
// unsaved edits the follow goes passive — a badge says the disk moved on, and
// nothing destructive happens.
//
// The broker side is the read-handle watch seam (fsbroker.handleWatch): the
// read handle openFile now KEEPS is asked to watch its own file, the broker
// watches the parent directory and filters to the file, and events arrive on
// fs.handle.{uuid}.event under a narrow Sub cap granted at Resolve. The
// ordering rules are the ones capdemo demonstrated — subscribe before watch —
// plus one this app adds: with handle uuids stable per (app, path, op),
// re-opening the SAME file mints the same uuid, so the old handle's close is
// sequenced BEFORE the new dialog rather than racing it.
//
// Threading is the app's standing shape: the event handler runs on the
// broker's goroutine and only sets mu-guarded flags; a per-frame drain
// debounces them (a save is a burst — truncate+write, or temp-write+rename —
// that must become ONE re-read) and applies results on the render thread.
// One wrinkle is the host's reactive repaint: an event arriving with no input
// would sit unseen, so renderBody keeps a low-rate repaint tick alive while —
// and only while — a follow is active. The tick lives THERE rather than in
// drainFollow so everything in this file is callable from plain tests, where
// no FFI sink exists to accept a repaint request.

import (
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
)

const (
	opUnwatch = ".unwatch"

	// followDebounce coalesces the burst a save is made of into one re-read.
	followDebounce = 250 * time.Millisecond

	// followTickSecs is the reactive host's polling cadence while a follow
	// is active — the price of hearing an event that arrives with no input.
	followTickSecs = 0.25

	// followTeardownTimeout bounds the unwatch/close pair. Shorter than the
	// broker's own HandleOpTimeout on purpose: teardown runs on paths that
	// must not hang (ahead of a new dialog, inside Unmount), and against a
	// live in-process broker it is effectively instant.
	followTeardownTimeout = 2 * time.Second
)

const (
	tipDiskChanged = "The file changed on disk while this buffer holds unsaved edits, so nothing was reloaded. Save to overwrite the disk version, or copy your work out and Open to take the disk's."

	tipDiskGone = "The file was deleted or renamed on disk. The buffer is untouched; Save as gives it a new home."
)

// followState is everything the follow owns that crosses goroutines — ALL of
// it mu-guarded: the event handler writes from the broker's goroutine, the
// re-read goroutine reports here, and the render thread drains.
type followState struct {
	// handle is the kept read handle's subject prefix; "" means no follow.
	handle string
	// unsub cancels the event subscription.
	unsub func()

	// changeAt/changeOk is the pending, coalesced "the file moved" signal.
	changeAt time.Time
	changeOk bool
	// gone marks a Delete/RenameFrom; closed marks the broker ending the
	// stream.
	gone   bool
	closed bool

	// rereading is the single-flight guard for the re-read goroutine —
	// separate from fileBusy, so Open/Save stay usable while a reload runs.
	rereading  bool
	reloadDone bool
	reloadText string
	reloadErr  error
}

// startFollow subscribes to the handle's event stream and asks the broker to
// watch the file. Runs on the open goroutine — bus round-trips. Subscribe
// comes BEFORE watch (the capdemo ordering) so no early event slips between
// the two.
func (inst *App) startFollow(handle string) (err error) {
	unsub, err := inst.bus.Subscribe(handle+"."+fsbroker.HandleEventOp, inst.handleFollowEvent)
	if err != nil {
		return
	}
	reply, err := inst.bus.RequestWithTimeout(handle+".watch", nil, transferTimeout)
	if err != nil {
		unsub()
		return
	}
	wr, err := fsbroker.UnmarshalWatchReply(reply)
	if err != nil {
		unsub()
		return
	}
	if !wr.Started {
		unsub()
		err = errFile(wr.Reason)
		return
	}
	inst.mu.Lock()
	inst.follow = followState{handle: handle, unsub: unsub}
	inst.mu.Unlock()
	return
}

// handleFollowEvent runs on the BROKER's goroutine and only files flags —
// never a c.* call, never a bus request (the pump's publishes are synchronous,
// so blocking here stalls the broker).
func (inst *App) handleFollowEvent(msg *app.Msg) {
	ev, err := fsbroker.UnmarshalWatchEvent(msg.Payload)
	if err != nil {
		return
	}
	inst.mu.Lock()
	f := &inst.follow
	switch ev.Kind {
	case fsbroker.WatchEventModify, fsbroker.WatchEventCreate,
		fsbroker.WatchEventRenameTo, fsbroker.WatchEventAttrib,
		fsbroker.WatchEventOverflow:
		// Overflow means events were lost — and the rescan of a one-file
		// watch IS a read, so it lands in the same lane. A change also
		// supersedes a pending gone: delete-then-recreate is the file coming
		// back.
		f.changeAt = time.Now()
		f.changeOk = true
		f.gone = false
	case fsbroker.WatchEventDelete, fsbroker.WatchEventRenameFrom:
		f.gone = true
	case fsbroker.WatchEventClosed:
		f.closed = true
	}
	inst.mu.Unlock()
}

// drainFollow moves the follow's pending state onto the render thread, once
// per frame. Debounce and single-flight live here; the reactive-repaint tick
// that keeps this being CALLED while the window is idle lives in renderBody,
// beside the frame that needs it — this function stays free of c.* calls so
// the whole follow state machine is drivable from plain tests.
func (inst *App) drainFollow() {
	if !inst.followActive {
		return
	}
	inst.mu.Lock()
	f := &inst.follow
	handle := f.handle
	reloadDone, text, rerr := f.reloadDone, f.reloadText, f.reloadErr
	f.reloadDone = false
	gone, closed := f.gone, f.closed
	f.gone, f.closed = false, false
	due := false
	if f.changeOk && !f.rereading && time.Since(f.changeAt) >= followDebounce {
		f.changeOk = false
		f.rereading = true
		due = true
	}
	inst.mu.Unlock()

	if reloadDone {
		inst.applyReload(text, rerr)
	}
	if gone {
		inst.diskGone = true
	}
	if closed {
		// The broker ended the stream — the handle was closed under us or
		// the parent directory vanished. Following is over; the local state
		// says so and the teardown below finds nothing left to unwatch.
		inst.stopFollow()
		return
	}
	if due && handle != "" {
		go inst.rereadFollowed(handle)
	}
}

// rereadFollowed fetches the file's current bytes through the kept read
// handle, on its own goroutine.
func (inst *App) rereadFollowed(handle string) {
	body, err := inst.bus.RequestWithTimeout(handle+opRead, nil, transferTimeout)
	text := ""
	if err == nil {
		if reason, isErr := readReplyError(body); isErr {
			err = errFile(reason)
		} else {
			text = string(body)
		}
	}
	inst.mu.Lock()
	inst.follow.rereading = false
	inst.follow.reloadDone = true
	inst.follow.reloadText = text
	inst.follow.reloadErr = err
	inst.mu.Unlock()
}

// applyReload lands a re-read on the render thread. The three-way split is
// the feature's whole contract:
//
//   - disk == checkpoint: our own save landing, or an identical external
//     write — indistinguishable and equally harmless. Not a foreign change;
//     clear any stale badge and do nothing. rebindBuffer's no-op on equal
//     text is the second net under this one.
//   - buffer clean: the reader has nothing at stake — follow. Same trio as
//     an Open (rebind, checkpoint, let the autosave carry it).
//   - buffer dirty: nothing destructive. The badge says the disk moved on,
//     and the tooltip says what the reader's options are.
func (inst *App) applyReload(text string, err error) {
	if err != nil {
		inst.status = "reload failed: " + err.Error()
		inst.logger.Warn().Err(err).Msg("mdedit: follow re-read failed")
		return
	}
	inst.diskGone = false
	switch {
	case text == inst.saved:
		inst.diskChanged = false
	case !inst.dirty():
		inst.rebindBuffer(text)
		inst.saved = text
		inst.persistedSrc = ""
		inst.diskChanged = false
		inst.status = "reloaded from disk"
	default:
		inst.diskChanged = true
	}
}

// stopFollow ends the follow from the render thread: the local state drops
// NOW (no more badges, no more repaint ticks), the bus half runs on its own
// goroutine. NOT the teardown openFile uses — a re-open of the same file
// must sequence the close BEFORE its dialog (see the file header), so
// openFile snapshots the state itself and calls teardownFollow inline.
func (inst *App) stopFollow() {
	inst.followActive = false
	inst.diskChanged = false
	inst.diskGone = false
	inst.mu.Lock()
	f := inst.follow
	inst.follow = followState{}
	inst.mu.Unlock()
	if f.handle == "" {
		return
	}
	bus := inst.bus
	go teardownFollow(bus, f.handle, f.unsub)
}

// teardownFollow is the bus half of ending a follow: unwatch, unsubscribe,
// close — bounded, best-effort, callable from any goroutine. Close is last
// and matters most: it is what stops the broker-side pump, which outlives
// the app's bus client otherwise (cap revocation silences no pump).
func teardownFollow(bus app.BusI, handle string, unsub func()) {
	if handle == "" {
		return
	}
	if bus != nil {
		_, _ = bus.RequestWithTimeout(handle+opUnwatch, nil, followTeardownTimeout)
	}
	if unsub != nil {
		unsub()
	}
	if bus != nil {
		_, _ = bus.RequestWithTimeout(handle+opClose, nil, followTeardownTimeout)
	}
}
