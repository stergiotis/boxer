package mdedit

// File I/O through the fs Powerbox (ADR-0178 M4).
//
// Three gestures — Open, Save, Save as — over `fsbroker`'s two dialog
// subjects. The broker's grant model shapes all of them, and it is worth
// stating what it gives and what it withholds, because both are visible to
// whoever uses this.
//
// **The app never learns the path.** `DialogReply` carries a handle subject
// prefix and nothing else; `handle.path` stays inside the broker
// ([ADR-0026](../../doc/adr/0026-app-runtime-and-capability-subjects.md) §SD7).
// So the editor cannot title itself after the file, cannot show a directory,
// and cannot tell the reader which of two documents they are looking at. It
// knows only whether it HAS a file. The name it puts in the save dialog is a
// suggestion it made up; the user may have typed something else entirely, and
// echoing it back afterwards would be a guess presented as a fact.
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
	"time"
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

	// dialogTimeout is how long a dialog request waits, and it is not a
	// service latency — it is how long a person may take to find a folder,
	// type a name and press Save. The transport default is five seconds,
	// which fails while the picker is still on screen and reports "timeout"
	// for a dialog the user is looking at; that is what driving this app
	// found, and it applies to every fsbroker dialog consumer.
	//
	// Finite rather than unbounded so a picker nobody ever answers releases
	// the goroutine instead of leaking it for the life of the window. Ten
	// minutes is past the point where a forgotten dialog is a live gesture.
	dialogTimeout = 10 * time.Minute

	// transferTimeout covers the read and write ops, which are answered by
	// the broker with no human in the way — a file system call and a bus
	// round-trip. Well above what those cost and well below the dialog's
	// wait, because a hung filesystem should not look like a slow reader.
	transferTimeout = 30 * time.Second
)

// Hover help for the file row.
const (
	tipOpen = "Open a document from a file. The system file picker chooses it — this app never sees your filesystem, only the one file you pick. The current document is replaced, so save it first if it matters."

	tipSave = "Save to the file this document is bound to, asking where to put it the first time. Opening a file does NOT bind it for saving: the Powerbox grants read and write separately and refuses a write on a read handle, so the first save after an open asks once and then stays quiet."

	tipSaveAs = "Save to a different file, and bind the document to that one from now on."

	tipFileBound = "Whether this document has a file to save to. The editor is not told which file — the Powerbox hands it a handle, never a path — so it can say that there is one, and not which."
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
	err        error
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
	title := ""
	for _, h := range headings {
		if h.Text != "" {
			title = h.Text
			break
		}
	}
	base := sanitiseFileBase(title)
	if base == "" {
		return defaultFileName
	}
	return base + ".md"
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
	if inst.dirty() && !inst.confirmDiscard {
		inst.confirmDiscard = true
		inst.confirmDiscardSrc = inst.src
		inst.status = "unsaved changes — Open again to discard them"
		return
	}
	inst.confirmDiscard = false
	if !inst.beginFile("choose a file to open…") {
		return
	}
	bus := inst.bus
	go func() {
		res := fileResult{op: fileOpOpen}
		defer func() { inst.finishFile(res) }()

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
		// The handle is closed as soon as the bytes are in hand. The broker
		// revokes its cap on close, and nothing here reads twice — leaving it
		// open would grow the app's authority for the rest of the session in
		// exchange for a round-trip it never makes.
		defer func() {
			_, _ = bus.RequestWithTimeout(dr.HandleSubjectPrefix+opClose, nil, transferTimeout)
		}()

		body, rerr := bus.RequestWithTimeout(dr.HandleSubjectPrefix+opRead, nil, transferTimeout)
		if rerr != nil {
			res.err = rerr
			return
		}
		if reason, isErr := readReplyError(body); isErr {
			res.err = errFile(reason)
			return
		}
		res.text = string(body)
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
				// somewhere to save. Every other failure leaves it alone.
				inst.writeHandle = ""
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
		inst.status = "opened"
	case fileOpSave:
		inst.writeHandle = res.handle
		inst.saved = res.saved
		inst.status = "saved"
	}
}

// fileBound reports whether the document has somewhere to save to without
// asking again.
func (inst *App) fileBound() (yes bool) {
	return inst.writeHandle != ""
}

// clearDiscardConfirm disarms Open's two-click confirmation. Called once a
// frame from the render body, so the arming only survives a click made
// straight after the refusal: an armed confirmation that outlived a keystroke
// or another gesture would turn a later, unrelated Open into a silent discard,
// which is the failure the confirmation exists to prevent.
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
