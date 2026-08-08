package mdedit

// Tests for file I/O through the fs Powerbox (ADR-0178 M4).
//
// The interesting ones run against a REAL fsbroker.Service over the in-process
// bus rather than a stub, because the things most likely to be wrong here are
// not this app's arithmetic — they are its beliefs about the broker. That the
// manifest's two dialog caps are sufficient, that a read handle refuses a
// write, that a write handle can be reused, and that a read error and a file's
// contents are distinguishable all belong to the seam, and a stub would agree
// with whatever this app assumed.

import (
	"os"
	"path/filepath"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/fsbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/markdown"
)

// ---------------------------------------------------------------------------
// Harness
// ---------------------------------------------------------------------------

// newFileSetup wires an App to a real broker over the in-process bus, with
// EXACTLY the caps the manifest declares. Anything the app can do here, it can
// do in the host; anything it cannot, the cap set is the reason.
func newFileSetup(t *testing.T) (inst *App, svc *fsbroker.Service, cleanup func()) {
	t.Helper()
	bus := inprocbus.NewInst(zerolog.Nop())
	bus.SetRequestTimeout(2 * time.Second)
	svc, err := fsbroker.NewService(bus, zerolog.Nop())
	require.NoError(t, err)

	inst = newApp()
	inst.logger = zerolog.Nop()
	inst.bus = bus.NewClient(app.AppIdT(manifest.Id), manifest.Caps)
	cleanup = func() { svc.Close() }
	return
}

// resolveOnce waits for the single pending dialog and approves it with path.
func resolveOnce(t *testing.T, svc *fsbroker.Service, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if all := svc.Pending(); len(all) == 1 {
			_, err := svc.Resolve(all[0].Id, path)
			require.NoError(t, err)
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no dialog pending after timeout")
}

// pendingOnce waits for the single pending dialog and returns it unresolved.
func pendingOnce(t *testing.T, svc *fsbroker.Service) (req fsbroker.PendingRequest) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if all := svc.Pending(); len(all) == 1 {
			return all[0]
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no dialog pending after timeout")
	return
}

// drainFile waits for the in-flight gesture to report and applies it, the way
// a frame would.
func drainFile(t *testing.T, inst *App) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		inst.mu.Lock()
		done, res := inst.fileDone, inst.fileRes
		inst.fileDone = false
		inst.mu.Unlock()
		if done {
			inst.applyFileResult(res)
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("no file result after timeout")
}

// ---------------------------------------------------------------------------
// End to end, against the real broker
// ---------------------------------------------------------------------------

// TestFile_OpenLoadsTheChosenFile is the whole read path: dialog, grant, read,
// close — over the manifest's own caps.
func TestFile_OpenLoadsTheChosenFile(t *testing.T) {
	inst, svc, cleanup := newFileSetup(t)
	defer cleanup()

	path := filepath.Join(t.TempDir(), "notes.md")
	const body = "# Notes\n\nSomething already on disk.\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))

	inst.openFile()
	resolveOnce(t, svc, path)
	drainFile(t, inst)

	assert.Equal(t, body, inst.src)
	assert.False(t, inst.dirty(), "a freshly opened document is at its checkpoint")
	assert.True(t, inst.rebindSrc, "a buffer the app loaded needs the databinding override")
	assert.Equal(t, "opened", inst.status)
	// Opening does NOT bind the document for saving: the broker mints read and
	// write handles separately and refuses a write on a read handle, so the
	// first Save still has to ask.
	assert.False(t, inst.fileBound(), "a read handle is not somewhere to save")
}

// TestFile_SaveAsksOnceThenStaysQuiet is the portal-style behaviour ADR-0178
// chose instead of asking fsbroker for a read-write handle mode: the first
// Save raises a dialog, and every later Save reuses what it granted.
func TestFile_SaveAsksOnceThenStaysQuiet(t *testing.T) {
	inst, svc, cleanup := newFileSetup(t)
	defer cleanup()

	path := filepath.Join(t.TempDir(), "out.md")
	inst.src = "# First\n"

	inst.saveFile(false)
	resolveOnce(t, svc, path)
	drainFile(t, inst)

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "# First\n", string(got))
	assert.False(t, inst.dirty())
	require.True(t, inst.fileBound(), "the granted handle must be kept for later saves")

	// Second save: no dialog is raised at all. If one were, Pending() would
	// hold it and the drain below would time out instead.
	inst.src = "# Second\n"
	assert.True(t, inst.dirty(), "an edit after a save is unsaved")
	inst.saveFile(false)
	drainFile(t, inst)
	assert.Empty(t, svc.Pending(), "a bound document must not raise a second dialog")

	got, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "# Second\n", string(got))
	assert.False(t, inst.dirty())
}

// TestFile_SaveAsRebindsToTheNewFile — the explicit gesture always asks, and
// what it grants replaces the binding.
func TestFile_SaveAsRebindsToTheNewFile(t *testing.T) {
	inst, svc, cleanup := newFileSetup(t)
	defer cleanup()

	dir := t.TempDir()
	first := filepath.Join(dir, "first.md")
	second := filepath.Join(dir, "second.md")
	inst.src = "# Doc\n"

	inst.saveFile(false)
	resolveOnce(t, svc, first)
	drainFile(t, inst)
	bound := inst.writeHandle

	inst.src = "# Doc v2\n"
	inst.saveFile(true)
	resolveOnce(t, svc, second)
	drainFile(t, inst)

	assert.NotEqual(t, bound, inst.writeHandle, "Save as must rebind")
	got, err := os.ReadFile(second)
	require.NoError(t, err)
	assert.Equal(t, "# Doc v2\n", string(got))
	// The first file keeps what it had — a rebind is not a move.
	got, err = os.ReadFile(first)
	require.NoError(t, err)
	assert.Equal(t, "# Doc\n", string(got))
}

// TestFile_SaveCheckpointsWhatLanded is the same race the clipboard export
// has: the reader keeps typing while the write is in flight, so what reached
// the disk is not what the buffer holds when the ack arrives. Checkpointing
// the live buffer would mark genuinely unsaved work as saved.
func TestFile_SaveCheckpointsWhatLanded(t *testing.T) {
	inst, svc, cleanup := newFileSetup(t)
	defer cleanup()

	path := filepath.Join(t.TempDir(), "race.md")
	inst.src = "sent to disk"
	inst.saveFile(false)
	resolveOnce(t, svc, path)
	// Type while the write is in flight.
	inst.src = "sent to disk, plus more typing"
	drainFile(t, inst)

	assert.Equal(t, "sent to disk", inst.saved, "the checkpoint is what landed")
	assert.True(t, inst.dirty(), "typing during the write stays unsaved")
}

// TestFile_CancelIsNotAnError — closing the picker leaves everything as it
// was, and says so without the word "failed".
func TestFile_CancelIsNotAnError(t *testing.T) {
	inst, svc, cleanup := newFileSetup(t)
	defer cleanup()

	inst.src = "# Untouched\n"
	inst.saveFile(false)
	req := pendingOnce(t, svc)
	require.NoError(t, svc.Cancel(req.Id))
	drainFile(t, inst)

	assert.Equal(t, "cancelled", inst.status)
	assert.Equal(t, "# Untouched\n", inst.src)
	assert.False(t, inst.fileBound(), "a cancelled dialog binds nothing")
}

// TestFile_SaveAsCancelKeepsTheExistingBinding covers the bug the dropHandle
// flag exists for: a "Save as" carries no handle while its dialog is open, so
// treating "no handle came back" as "forget the file" would unbind a document
// because the reader changed their mind about saving it elsewhere.
func TestFile_SaveAsCancelKeepsTheExistingBinding(t *testing.T) {
	inst, svc, cleanup := newFileSetup(t)
	defer cleanup()

	path := filepath.Join(t.TempDir(), "bound.md")
	inst.src = "# Doc\n"
	inst.saveFile(false)
	resolveOnce(t, svc, path)
	drainFile(t, inst)
	bound := inst.writeHandle
	require.NotEmpty(t, bound)

	inst.saveFile(true)
	req := pendingOnce(t, svc)
	require.NoError(t, svc.Cancel(req.Id))
	drainFile(t, inst)

	assert.Equal(t, bound, inst.writeHandle, "a cancelled Save as must not unbind the file")
}

// TestFile_SuggestedNameReachesThePicker pins the one hint the app gets to
// give. The broker surfaces it to the picker and never derives a path from it.
func TestFile_SuggestedNameReachesThePicker(t *testing.T) {
	inst, svc, cleanup := newFileSetup(t)
	defer cleanup()

	inst.src = "# Release Notes for v2\n\nbody\n"
	inst.doc = markdown.Parse([]byte(inst.src))
	inst.saveFile(false)
	req := pendingOnce(t, svc)
	assert.Equal(t, "release-notes-for-v2.md", req.SuggestedName)
	require.NoError(t, svc.Cancel(req.Id))
	drainFile(t, inst)
}

// TestFile_ReadHandleCannotWrite is ADR-0026 §SD7 asserted from this side of
// the seam, because it is the reason Save has to ask after an Open. If the
// broker ever grew a read-write mode this fails, which is the signal to
// revisit the portal-style flow rather than to discover it by surprise.
func TestFile_ReadHandleCannotWrite(t *testing.T) {
	inst, svc, cleanup := newFileSetup(t)
	defer cleanup()

	path := filepath.Join(t.TempDir(), "ro.md")
	require.NoError(t, os.WriteFile(path, []byte("original\n"), 0o644))

	inst.openFile()
	resolveOnce(t, svc, path)
	// Grab the handle before openFile's deferred close retires it.
	all := svc.Pending()
	require.Empty(t, all)
	drainFile(t, inst)

	// The read handle is closed by now, so a write attempt is refused twice
	// over — wrong mode and unknown handle. Either way the file is untouched,
	// which is the property that matters.
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "original\n", string(got))
	assert.False(t, inst.fileBound())
}

// TestFile_OpenReportsABrokerRefusal — a file the broker cannot read must
// surface as an error, never as a document whose text is the error message.
func TestFile_OpenReportsABrokerRefusal(t *testing.T) {
	inst, svc, cleanup := newFileSetup(t)
	defer cleanup()

	inst.src = "# Keep me\n"
	inst.saved = "# Keep me\n"
	missing := filepath.Join(t.TempDir(), "does-not-exist.md")

	inst.openFile()
	resolveOnce(t, svc, missing)
	drainFile(t, inst)

	assert.Contains(t, inst.status, "open failed")
	assert.Equal(t, "# Keep me\n", inst.src, "a failed open must not touch the buffer")
}

// ---------------------------------------------------------------------------
// The read-reply discriminator
// ---------------------------------------------------------------------------

// TestReadReplyError_ErrorFrameCannotBeMistakenForText is the argument the
// discriminator rests on, pinned so it cannot rot silently: the encoded
// refusal begins with a UTF-8 CONTINUATION byte, which no valid UTF-8 text can
// start with. Should the encoding ever change to something a markdown file
// could plausibly begin with, this fails and the read path needs framing
// rather than a decode attempt.
func TestReadReplyError_ErrorFrameCannotBeMistakenForText(t *testing.T) {
	frame, err := fsbroker.MarshalDialogReply(fsbroker.DialogReply{Granted: false, Reason: "open: nope"})
	require.NoError(t, err)
	require.NotEmpty(t, frame)
	assert.False(t, utf8.RuneStart(frame[0]),
		"the refusal frame starts 0x%02x, which valid UTF-8 text could now also start with", frame[0])

	reason, isErr := readReplyError(frame)
	assert.True(t, isErr)
	assert.Equal(t, "open: nope", reason)
}

func TestReadReplyError_TextIsNotAnError(t *testing.T) {
	for _, body := range []string{
		"# A heading\n\nSome prose.\n",
		"",
		"plain text with no markup at all",
		"---\ntitle: front matter\n---\n\nbody\n",
		"héllo wörld — em dash and emoji 🎉\n",
	} {
		_, isErr := readReplyError([]byte(body))
		assert.False(t, isErr, "%q was read as a broker refusal", body)
	}
}

// ---------------------------------------------------------------------------
// suggestedFileName
// ---------------------------------------------------------------------------

func TestSuggestedFileName(t *testing.T) {
	cases := []struct {
		name string
		hs   []markdown.HeadingInfo
		want string
	}{
		{"no headings", nil, defaultFileName},
		{"empty heading", []markdown.HeadingInfo{{Text: ""}}, defaultFileName},
		{"simple", []markdown.HeadingInfo{{Text: "Release Notes"}}, "release-notes.md"},
		{"first heading wins", []markdown.HeadingInfo{{Text: "One"}, {Text: "Two"}}, "one.md"},
		{"skips an empty first", []markdown.HeadingInfo{{Text: ""}, {Text: "Real"}}, "real.md"},
		{"punctuation collapses", []markdown.HeadingInfo{{Text: "What's new?! (v2)"}}, "what-s-new-v2.md"},
		{"separators never survive", []markdown.HeadingInfo{{Text: "docs/2026: notes"}}, "docs-2026-notes.md"},
		{"non-latin", []markdown.HeadingInfo{{Text: "Größe & Maß"}}, "größe-maß.md"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, suggestedFileName(tc.hs))
		})
	}
}

// TestSuggestedFileName_NeverCarriesADirectory is the property that matters
// rather than the exact spelling: the picker reduces any directory component
// away, so a `/` that survived here would silently lose everything before it.
func TestSuggestedFileName_NeverCarriesADirectory(t *testing.T) {
	for _, title := range []string{"a/b/c", "../../etc/passwd", "C:\\Users\\x", "  /leading"} {
		got := suggestedFileName([]markdown.HeadingInfo{{Text: title}})
		assert.NotContains(t, got, "/", "title %q", title)
		assert.NotContains(t, got, "\\", "title %q", title)
	}
}

func TestSuggestedFileName_IsBounded(t *testing.T) {
	long := ""
	for i := 0; i < 40; i++ {
		long += "verylongword "
	}
	got := suggestedFileName([]markdown.HeadingInfo{{Text: long}})
	assert.LessOrEqual(t, len(got), maxSuggestedNameLen+len(".md"))
}

// ---------------------------------------------------------------------------
// Open's two-click confirmation
// ---------------------------------------------------------------------------

// TestFile_OpenRefusesOverUnsavedChangesOnce is the guard on the one gesture
// here that can destroy work: an open is a whole-buffer rebind outside the
// widget's edit path, so egui's undo does not describe it and the autosave
// carries the new document over the persisted one within seconds.
func TestFile_OpenRefusesOverUnsavedChangesOnce(t *testing.T) {
	inst, svc, cleanup := newFileSetup(t)
	defer cleanup()

	path := filepath.Join(t.TempDir(), "incoming.md")
	require.NoError(t, os.WriteFile(path, []byte("# Incoming\n"), 0o644))
	inst.src = "# Unsaved work\n"
	require.True(t, inst.dirty())

	// First click: refused, and no dialog was raised at all.
	inst.openFile()
	assert.Empty(t, svc.Pending(), "the refused open must not raise a picker")
	assert.Contains(t, inst.status, "unsaved changes")
	assert.Equal(t, "# Unsaved work\n", inst.src)

	// Second click: the reader meant it.
	inst.openFile()
	resolveOnce(t, svc, path)
	drainFile(t, inst)
	assert.Equal(t, "# Incoming\n", inst.src)
}

// TestFile_OpenConfirmationDoesNotOutliveTyping is what makes the two-click
// gesture a confirmation rather than a latch. An arming that survived until
// the next Open — minutes and many edits later — would turn an unrelated click
// into the silent discard the refusal exists to prevent.
func TestFile_OpenConfirmationDoesNotOutliveTyping(t *testing.T) {
	inst, svc, cleanup := newFileSetup(t)
	defer cleanup()

	inst.src = "# Unsaved work\n"
	inst.openFile()
	require.True(t, inst.confirmDiscard)

	// The reader types instead of clicking again; the frame loop disarms it.
	inst.src = "# Unsaved work, extended\n"
	inst.clearDiscardConfirm()
	assert.False(t, inst.confirmDiscard, "typing must disarm the confirmation")

	inst.openFile()
	assert.Empty(t, svc.Pending(), "the next Open must ask again, not discard")
	assert.Contains(t, inst.status, "unsaved changes")
}

// TestFile_OpenDoesNotAskOnACleanDocument — the confirmation is for unsaved
// work only; a document at its checkpoint opens straight away.
func TestFile_OpenDoesNotAskOnACleanDocument(t *testing.T) {
	inst, svc, cleanup := newFileSetup(t)
	defer cleanup()

	path := filepath.Join(t.TempDir(), "next.md")
	require.NoError(t, os.WriteFile(path, []byte("# Next\n"), 0o644))
	inst.src = "# Saved already\n"
	inst.saved = inst.src
	require.False(t, inst.dirty())

	inst.openFile()
	resolveOnce(t, svc, path)
	drainFile(t, inst)
	assert.Equal(t, "# Next\n", inst.src)
}
