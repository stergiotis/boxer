package mdedit

// File I/O through the fs Powerbox (ADR-0178 M4).
//
// Three gestures — Open, Save, Save as — over `fsbroker`'s two dialog
// subjects. The broker's grant model shapes all of them, and it is worth
// stating what it gives and what it withholds, because both are visible to
// whoever uses this.
//
// **The app never learns the path.** `DialogReply` carries a handle subject
// prefix and — since the Powerbox widening ADR-0178 had recorded as a
// deferral — the file's BASENAME; `handle.path` stays inside the broker
// ([ADR-0026](../../doc/adr/0026-app-runtime-and-capability-subjects.md) §SD7).
// So the editor can now title what it has open and tell two documents apart,
// while still never seeing a directory or a location. The name is the
// broker's own truth (`filepath.Base` of the resolved path), not an echo of
// the suggestion this app put in the save dialog — the user may have typed
// something else entirely, and echoing the suggestion back would be a guess
// presented as a fact.
//
// **A read handle can never write.** The broker refuses it by mode, by design,
// so opening a document does not give the app anywhere to save it. The first
// Save after an Open therefore raises a dialog — the portal-style behaviour
// ADR-0178 chose over asking fsbroker for a read-write mode — and every save
// after that reuses the granted handle silently.
//
// **The cap set stays narrow.** The manifest declares the two dialog subjects
// only. `fs.handle.{uuid}.>` is granted by the broker at the moment the user
// approves a dialog, and revoked on close, so a static `fs.handle.>` in the
// manifest would widen the app's standing authority to every handle the broker
// ever mints in exchange for nothing.
//
// Everything here runs OFF the render goroutine: each gesture is one or two
// blocking bus round-trips, and one of them is a human deciding in a file
// picker. Results come back through the guarded fields drainAsync moves onto
// the render thread.

import (
	"strings"
	"unicode"

	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

// Handle ops. The broker splits `fs.handle.{uuid}.{op}` on dots, so these are
// appended to the prefix the dialog reply carried.
const (
	opRead  = ".read"
	opWrite = ".write"
	opClose = ".close"
)

const (
	// defaultFileName is the save dialog's suggestion when the document gives
	// nothing better to go on.
	defaultFileName = "untitled.md"

	// maxSuggestedNameLen bounds a name derived from a heading. A heading can
	// be a sentence; a filename should not be.
	maxSuggestedNameLen = 48

	// dialogTimeout and transferTimeout come from the broker rather than being
	// chosen here: which of its requests waits on a person is a property of
	// the subject, and fsbroker is what knows it. Named locally only so the
	// call sites below read as prose.
	dialogTimeout   = fsbroker.DialogTimeout
	transferTimeout = fsbroker.HandleOpTimeout
)

// Hover help for the file row.
const (
	tipOpen = "Open a document from a file. The system file picker chooses it — this app never sees your filesystem, only the one file you pick. The current document is replaced, so save it first if it matters."

	tipSave = "Save to the file this document is bound to, asking where to put it the first time. Opening a file does NOT bind it for saving: the Powerbox grants read and write separately and refuses a write on a read handle, so the first save after an open asks once and then stays quiet."

	tipSaveAs = "Save to a different file, and bind the document to that one from now on."

	tipFileBound = "The file this document saves to, by name only. The Powerbox reveals the basename and keeps the path, so the badge can say which file — never where it lives."

	tipFileRead = "The file this document was opened from, by name only — the Powerbox reveals the basename and keeps the path. Opening does not bind for saving; the first Save still asks where."
)

// fileOpE distinguishes what a finished file goroutine was doing, so the drain
// applies the right thing.
type fileOpE uint8

const (
	fileOpNone fileOpE = iota
	fileOpOpen
	fileOpSave
)

// fileResult is one completed file gesture, handed from the goroutine to the
// render thread. Assembled off-thread and read under inst.mu.
type fileResult struct {
	op fileOpE
	// text is the document read by fileOpOpen.
	text string
	// saved is the snapshot that actually landed, for fileOpSave. The
	// checkpoint is what reached the disk, not what the buffer holds when the
	// reply arrives — the reader may have typed while the write was in
	// flight, and that typing is genuinely unsaved.
	saved string
	// handle is the write-handle prefix to reuse for later saves.
	handle string
	// displayName is the basename the broker named in the dialog reply —
	// which file, never where. Set only when a dialog actually ran: a silent
	// save through the existing handle learns nothing new about the name and
	// must not clobber it.
	displayName string
	// cancelled separates "the user pressed Cancel" from "it failed". One is
	// not an error and must not be logged or coloured like one.
	cancelled bool
	// dropHandle asks the render thread to forget the bound file, set only
	// when the BROKER refused a write — the handle is then demonstrably not
	// somewhere to save. An explicit flag rather than reading handle == "":
	// a "Save as" whose dialog failed also carries no handle, and clearing
	// the binding there would throw away a file that is still perfectly good
	// because the reader changed their mind about saving elsewhere.
	dropHandle bool
	// followOk says the opened file is now being followed on disk
	// (mdedit_follow.go); followErr is why not. Following is an enhancement,
	// not the gesture — its failure never fails the open.
	followOk  bool
	followErr error
	err       error
}

// ---------------------------------------------------------------------------
// Pure helpers
// ---------------------------------------------------------------------------

// suggestedFileName is what the save dialog pre-fills, derived from the
// document's first heading.
//
// Advisory in the strongest sense: the broker never derives the write path
// from it and the user may replace it outright. That is also why the editor
// does not remember it as "the filename" afterwards — it is what this app
// asked for, not what the user chose.
func suggestedFileName(headings []markdown.HeadingInfo) (name string) {
	base := sanitiseFileBase(firstHeadingText(headings))
	if base == "" {
		return defaultFileName
	}
	return base + ".md"
}

// firstHeadingText is the document's working title: the first heading that
// has text, or "".
func firstHeadingText(headings []markdown.HeadingInfo) (title string) {
	for _, h := range headings {
		if h.Text != "" {
			return h.Text
		}
	}
	return ""
}

// sanitiseFileBase reduces a heading to something that can be a basename:
// letters, digits and single hyphens, trimmed and bounded.
//
// Deliberately conservative rather than merely legal. The picker treats the
// hint as a basename and reduces any directory component away, so a `/` here
// would silently lose everything before it; dropping every separator up front
// means the suggestion says what it looks like.
func sanitiseFileBase(title string) (base string) {
	var b strings.Builder
	lastDash := false
	for _, r := range title {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
		if b.Len() >= maxSuggestedNameLen {
			break
		}
	}
	return strings.Trim(b.String(), "-")
}

// readReplyError reports whether a reply to `fs.handle.{uuid}.read` is the
// broker refusing rather than the file's bytes.
//
// The op answers with the raw body on success and a CBOR DialogReply on
// failure, so the two share one channel and something has to tell them apart.
// A successful decode is the discriminator, and it is safe for the documents
// this app edits rather than merely unlikely to collide: the encoded reply
// begins 0x81, a UTF-8 CONTINUATION byte, which no valid UTF-8 text can start
// with. A test pins that leading byte so the argument cannot rot quietly.
//
// An empty file decodes to nothing and reads as content, which is right — an
// empty document is a document.
func readReplyError(reply []byte) (reason string, isErr bool) {
	r, err := fsbroker.UnmarshalDialogReply(reply)
	if err != nil || r.Granted {
		return "", false
	}
	reason = r.Reason
	if reason == "" {
		reason = "the file could not be read"
	}
	return reason, true
}

// ---------------------------------------------------------------------------
// Gestures
// ---------------------------------------------------------------------------

// fileInFlight reports whether a dialog or a transfer is already running. The
// buttons render regardless and the click is dropped instead — a control that
// vanishes mid-gesture is harder to understand than one that ignores a second
// press.
func (inst *App) fileInFlight() (busy bool) {
	inst.mu.Lock()
	busy = inst.fileBusy
	inst.mu.Unlock()
	return
}

// beginFile claims the single in-flight slot, reporting false when something
// else already holds it.
func (inst *App) beginFile(status string) (ok bool) {
	inst.mu.Lock()
	if inst.fileBusy {
		inst.mu.Unlock()
		return false
	}
	inst.fileBusy = true
	inst.mu.Unlock()
	inst.status = status
	return true
}

// finishFile publishes a result to the render thread.
func (inst *App) finishFile(res fileResult) {
	inst.mu.Lock()
	inst.fileBusy = false
	inst.fileDone = true
	inst.fileRes = res
	inst.mu.Unlock()
}

// openFile raises the read dialog and loads whatever the user picks.
//
// An open REPLACES the buffer through a whole-buffer rebind, outside the
// widget's edit path, so egui's undo does not describe it and the autosave
// carries the new document over the persisted one within a few seconds. With
// unsaved changes that is a way to lose work by clicking the wrong button, so
// the first click on a modified document refuses and says so; a second click
// means it. Two clicks rather than a confirmation dialog because there is no
// dialog facility here but the picker, and rather than a flat refusal because
// a scratch buffer the reader wants rid of should not have to be saved
// somewhere first to be discarded.
func (inst *App) openFile() {
	if inst.bus == nil {
		inst.status = "no bus wired — cannot open a file"
		return
	}
	if !inst.confirmReplace("Open again to discard them") {
		return
	}
	if !inst.beginFile("choose a file to open…") {
		return
	}
	// Snapshot and clear the current follow on the render thread; its bus
	// teardown runs FIRST in the goroutine below, ahead of the dialog —
	// handle uuids are stable per (app, path, op), so re-opening the same
	// file mints the same uuid, and a close landing after the new grant
	// would destroy it.
	inst.followActive = false
	inst.diskChanged = false
	inst.diskGone = false
	inst.mu.Lock()
	prevFollow := inst.follow
	inst.follow = followState{}
	inst.mu.Unlock()

	bus := inst.bus
	go func() {
		res := fileResult{op: fileOpOpen}
		defer func() { inst.finishFile(res) }()

		teardownFollow(bus, prevFollow.handle, prevFollow.unsub)

		reply, err := bus.RequestWithTimeout(fsbroker.SubjectDialogRead, nil, dialogTimeout)
		if err != nil {
			res.err = err
			return
		}
		dr, derr := fsbroker.UnmarshalDialogReply(reply)
		if derr != nil {
			res.err = derr
			return
		}
		if !dr.Granted {
			res.cancelled = true
			return
		}
		res.displayName = dr.DisplayName

		body, rerr := bus.RequestWithTimeout(dr.HandleSubjectPrefix+opRead, nil, transferTimeout)
		if rerr != nil {
			res.err = rerr
			// The read failed but the grant stands; close it rather than
			// leaving standing authority behind a failed gesture.
			_, _ = bus.RequestWithTimeout(dr.HandleSubjectPrefix+opClose, nil, transferTimeout)
			return
		}
		if reason, isErr := readReplyError(body); isErr {
			res.err = errFile(reason)
			_, _ = bus.RequestWithTimeout(dr.HandleSubjectPrefix+opClose, nil, transferTimeout)
			return
		}
		res.text = string(body)

		// The handle is KEPT — the departure from M4's close-after-read —
		// because it is what the file is followed through: the broker
		// watches the file for this handle and re-reads ride it. If the
		// follow cannot start, the old economy returns: nothing will read
		// twice, so the handle goes back.
		if ferr := inst.startFollow(dr.HandleSubjectPrefix); ferr != nil {
			res.followErr = ferr
			_, _ = bus.RequestWithTimeout(dr.HandleSubjectPrefix+opClose, nil, transferTimeout)
			return
		}
		res.followOk = true
	}()
}

// saveFile writes the buffer to the bound file, raising the save dialog when
// there is none — or when the caller asked for one ("Save as").
func (inst *App) saveFile(chooseFile bool) {
	if inst.bus == nil {
		inst.status = "no bus wired — cannot save to a file"
		return
	}
	handle := inst.writeHandle
	if chooseFile {
		handle = ""
	}
	status := "saving…"
	if handle == "" {
		status = "choose where to save…"
	}
	if !inst.beginFile(status) {
		return
	}
	bus := inst.bus
	snapshot := inst.src
	suggested := suggestedFileName(inst.headings())
	go func() {
		res := fileResult{op: fileOpSave, handle: handle}
		defer func() { inst.finishFile(res) }()

		if res.handle == "" {
			reqBytes, merr := fsbroker.MarshalDialogRequest(fsbroker.DialogRequest{SuggestedName: suggested})
			if merr != nil {
				res.err = merr
				return
			}
			reply, err := bus.RequestWithTimeout(fsbroker.SubjectDialogWrite, reqBytes, dialogTimeout)
			if err != nil {
				res.err = err
				return
			}
			dr, derr := fsbroker.UnmarshalDialogReply(reply)
			if derr != nil {
				res.err = derr
				return
			}
			if !dr.Granted {
				res.cancelled = true
				return
			}
			res.handle = dr.HandleSubjectPrefix
			res.displayName = dr.DisplayName
		}

		ack, werr := bus.RequestWithTimeout(res.handle+opWrite, []byte(snapshot), transferTimeout)
		if werr != nil {
			res.err = werr
			return
		}
		wr, perr := fsbroker.UnmarshalDialogReply(ack)
		if perr != nil {
			res.err = perr
			return
		}
		if !wr.Granted {
			// A refused write invalidates the handle as a place to save: it
			// may have been closed or revoked under us. Dropping it sends the
			// next Save back through the dialog rather than retrying against
			// something the broker has already declined.
			res.dropHandle = true
			res.err = errFile(wr.Reason)
			return
		}
		res.saved = snapshot
	}()
}

// headings is the preview's heading list, or nil before the first parse.
func (inst *App) headings() (hs []markdown.HeadingInfo) {
	if inst.doc == nil {
		return nil
	}
	return inst.doc.Headings()
}

// errFile is a broker-reported refusal as an error. A plain string carrier —
// the reason came off the wire and there is nothing to wrap.
type errFile string

func (e errFile) Error() string { return string(e) }

// ---------------------------------------------------------------------------
// Drain
// ---------------------------------------------------------------------------

// applyFileResult moves a finished gesture onto the render thread. Called from
// drainAsync, so every c.* touch below is on the render goroutine.
func (inst *App) applyFileResult(res fileResult) {
	switch {
	case res.cancelled:
		// Not an error: the user closed the picker. Say so plainly and log
		// nothing.
		inst.status = "cancelled"
		return
	case res.err != nil:
		if res.op == fileOpOpen {
			inst.status = "open failed: " + res.err.Error()
		} else {
			inst.status = "save failed: " + res.err.Error()
			if res.dropHandle {
				// The broker refused the write, so the binding is no longer
				// somewhere to save — and its name goes with it. Every other
				// failure leaves both alone.
				inst.writeHandle = ""
				inst.boundName = ""
			}
		}
		inst.logger.Warn().Err(res.err).Msg("mdedit: file operation failed")
		return
	}

	switch res.op {
	case fileOpOpen:
		// The buffer the editor holds was computed here rather than typed, so
		// it needs the same override the find-and-replace rewrite uses.
		inst.rebindBuffer(res.text)
		inst.saved = res.text
		// The opened document is not the persisted one, so let the autosave
		// carry it into the store rather than leaving the two out of step
		// until the next keystroke.
		inst.persistedSrc = ""
		// The caret has nowhere meaningful to be in a document the reader has
		// not seen; the top is where they will start reading.
		inst.requestCaret(0, 0, false)
		inst.readName = res.displayName
		inst.readFromSnapshot = false
		inst.followActive = res.followOk
		if res.followErr != nil {
			// The open stands; only the following half is missing. Worth a
			// log line, not a red status.
			inst.logger.Warn().Err(res.followErr).Msg("mdedit: file follow unavailable")
		}
		inst.status = "opened"
		if res.displayName != "" {
			inst.status = "opened " + res.displayName
		}
	case fileOpSave:
		inst.writeHandle = res.handle
		// Only a dialog names the file; a silent save through the existing
		// handle carries no name and must not clear the one the dialog gave.
		if res.displayName != "" {
			inst.boundName = res.displayName
		}
		inst.saved = res.saved
		inst.status = "saved"
		if inst.boundName != "" {
			inst.status = "saved " + inst.boundName
		}
	}
}

// fileBound reports whether the document has somewhere to save to without
// asking again.
func (inst *App) fileBound() (yes bool) {
	return inst.writeHandle != ""
}

// fileBadge resolves the bar's file badge. The strongest claim wins: the save
// target when the document is bound, else the source it was loaded from —
// a Powerbox-named file or a snapshot entry, each under the tooltip that
// states its contract. Names only, never paths, either way.
func (inst *App) fileBadge() (label, tip string, show bool) {
	switch {
	case inst.fileBound():
		label = inst.boundName
		if label == "" {
			// Bound before the broker named files (or the name was lost with
			// a restore); the fact still deserves its badge.
			label = "file bound"
		}
		return label, tipFileBound, true
	case inst.readName != "" && inst.readFromSnapshot:
		return inst.readName, tipFileSnapshot, true
	case inst.readName != "":
		return inst.readName, tipFileRead, true
	}
	return "", "", false
}

// confirmReplace is the shared two-click guard for every gesture that
// REPLACES the buffer — Open, and a snapshot load: on a modified document the
// first click refuses and says so (hint completes the sentence "unsaved
// changes — …"), a second click straight after means it. Two clicks rather
// than a dialog for the reason openFile's comment records.
func (inst *App) confirmReplace(hint string) (ok bool) {
	if inst.dirty() && !inst.confirmDiscard {
		inst.confirmDiscard = true
		inst.confirmDiscardSrc = inst.src
		inst.status = "unsaved changes — " + hint
		return false
	}
	inst.confirmDiscard = false
	return true
}

// clearDiscardConfirm disarms the two-click confirmation. Called once a
// frame from the render body, so the arming only survives a click made
// straight after the refusal: an armed confirmation that outlived a keystroke
// or another gesture would turn a later, unrelated replace into a silent
// discard, which is the failure the confirmation exists to prevent.
//
// The buffer's own text is the discriminator rather than a timer — the reader
// having typed since is what makes the warning stale, not how long they took
// to read it.
func (inst *App) clearDiscardConfirm() {
	if !inst.confirmDiscard {
		return
	}
	if inst.src != inst.confirmDiscardSrc {
		inst.confirmDiscard = false
	}
}
