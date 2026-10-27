package chat

import (
	"context"
	"encoding/json/v2"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/projection"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// Resume (ADR-0308 §SD4–§SD6; ADR-0303 M3–M4, superseded by ADR-0308): a
// conversation outlives its process. The Conversations panel lists the
// runs this app wrote; resuming one reopens its journal, verifies the
// chain, folds it — the transcript back from the entries, the settings
// and the label from the run row — settles every unfinished act by its
// effect class and tells the model so in a note, and asks for a grant
// again as a new task when the next turn needs one. A closed run opens
// again; a run whose fingerprint differs from this build's profile is
// stranded: readable, not continuing (§SD5). Deleting a run tombstones it
// (§SD8).

const runsListLimit = 50

// resumed is what the resume job brings back: the run, its writer, its
// entries, the fold, and what resume settled, already told to the model.
type resumed struct {
	run     agentrun.Run
	writer  *agentrun.Writer
	entries []agentrun.Entry
	fold    agentrun.Resume
	// stranded says the run does not continue: its fingerprint differs
	// from this build's, or it was stranded or deleted before.
	stranded bool
	// lines are what resume settled, as the person reads them.
	lines []string
	// art is the artefact read back from its last export, nil when there
	// is none or it could not be read.
	art *artefact
}

// listRuns asks the host for this app's runs, off the frame.
func (inst *App) listRuns() {
	store := inst.journalStore()
	if store == nil {
		return
	}
	inst.runsJob.Start(nil, bgjob.Spec{Kind: "chat-runs", Title: "conversations"}, func(ctx context.Context) (runs *[]agentrun.Run, err error) {
		got, err := store.ListRuns(ctx, string(ManifestId), runsListLimit)
		if err != nil {
			return
		}
		sort.SliceStable(got, func(i, j int) bool { return got[i].Created.After(got[j].Created) })
		return &got, nil
	})
}

// journalStore is the store the app writes through: the host's service,
// or the coordinator's in-process store in a test.
func (inst *App) journalStore() (store agentrun.StoreI) {
	if inst.journals != nil {
		return inst.journals
	}
	if inst.coord != nil {
		return inst.coord.store
	}
	return nil
}

// resumeRun reopens a run off the frame and settles it; drain adopts it.
// A run this window moved on from is reopened once its coordinator has
// closed it, so the late close never lands on the writer resume adopts.
func (inst *App) resumeRun(id string) {
	store := inst.journalStore()
	if store == nil {
		return
	}
	if !inst.canAct() {
		inst.busyNote()
		return
	}
	if inst.deleting == id {
		inst.setNote("this conversation is being deleted", true)
		return
	}
	fingerprint := runFingerprint(profileNow().Fingerprint(), inst.confinedTier)
	retiring := inst.retiring[id]
	delete(inst.retiring, id)
	inst.resumeJob.Start(nil, bgjob.Spec{Kind: "chat-resume", Title: "resume"}, func(ctx context.Context) (r *resumed, err error) {
		if retiring != nil {
			wctx, cancel := context.WithTimeout(ctx, agentrun.DefaultTimeout)
			closed := retiring.awaitClosed(wctx)
			cancel()
			if !closed {
				return nil, eb.Build().Str("conversation", id).Errorf("the run is still being closed; resume it again in a moment")
			}
		}
		of, found, err := agentrun.RunOfConversation(ctx, store, string(ManifestId), id)
		if err != nil {
			return
		}
		if !found {
			return nil, eb.Build().Str("conversation", id).Errorf("no run of the conversation")
		}
		w, err := agentrun.Reopen(ctx, store, of.Id)
		if err != nil {
			return
		}
		run := w.Run()
		r = &resumed{run: run, writer: w, stranded: run.Fingerprint != fingerprint || run.State == agentrun.StateStranded || run.State == agentrun.StateDeleted}
		switch {
		case r.stranded && run.State == agentrun.StateOpen:
			_ = w.Update(ctx, func(r *agentrun.Run) { r.State = agentrun.StateStranded })
		case !r.stranded && run.State == agentrun.StateClosed:
			if err = w.Resume(ctx); err != nil {
				return
			}
		}
		// What the record left open, read before anything settles it.
		r.fold = agentrun.Fold(w.Entries())
		r.lines = resumeLines(r)
		if !r.stranded {
			// What the record leaves open is settled before the model
			// reads it again: a call it made with no result recorded gets
			// one that says so, a screenshot it was shown goes to its
			// placeholder, the turn the host stopped in is ended, and a
			// note tells it the rest.
			if err = settleOpenCalls(ctx, w); err != nil {
				return
			}
			if _, err = stripPixelEntries(ctx, w); err != nil {
				return
			}
			if err = settleOpenTurn(ctx, w, r.fold.OpenTurn); err != nil {
				return
			}
		}
		if rec, ok := lastExport(w.Entries()); ok && !r.stranded {
			if r.art, err = readExport(ctx, rec); err != nil {
				r.lines = append(r.lines, "the artefact's export of revision "+strconv.Itoa(rec.Revision)+" could not be read back, so the artefact starts empty: "+err.Error())
				err = nil
			} else {
				r.lines = append(r.lines, "the artefact was restored from its export of revision "+strconv.Itoa(rec.Revision)+", with "+plural(len(rec.Images), "screenshot"))
			}
		}
		if len(r.lines) > 0 && !r.stranded {
			note := "This conversation resumed after the host stopped. Windows and grants did not survive: request_access starts a new task when you need one. What was left open:\n- " + strings.Join(r.lines, "\n- ")
			if _, err = w.Append(ctx, agentrun.Entry{Kind: agentrun.KindNote, Name: "resume", Form: contextFormNotice.String(), Body: note, Label: harnessLabel()}); err != nil {
				return
			}
		}
		r.entries = w.Entries()
		r.fold = agentrun.Fold(r.entries)
		return
	})
}

// settleOpenCalls writes an outcome for every call a reply made that has
// none: the process ended before the result was recorded, and a request
// that ends with an unanswered call is one no route takes. A call with an
// intent was dispatched and its outcome is unknown; one without was never
// dispatched, and its intent is written first, so no outcome stands
// without one (ADR-0308 §SD2).
func settleOpenCalls(ctx context.Context, w *agentrun.Writer) (err error) {
	entries := w.Entries()
	answered := map[string]bool{}
	intended := map[string]bool{}
	settledKeys := map[string]bool{}
	for _, e := range entries {
		switch e.Kind {
		case agentrun.KindOutcome:
			settledKeys[e.Key] = true
			if e.Role == projection.RoleTool {
				answered[e.Name] = true
			}
		case agentrun.KindIntent:
			intended[e.Key] = true
		}
	}
	var settling []agentrun.Entry
	for _, e := range entries {
		// The coordinator's own model calls — the title's, the
		// summariser's — are settled by an outcome too.
		if e.Kind == agentrun.KindIntent && (e.Name == "title" || e.Name == "summarise") && !settledKeys[e.Key] {
			settledKeys[e.Key] = true
			settling = append(settling, agentrun.Entry{Kind: agentrun.KindOutcome, Key: e.Key, Body: "error: the call did not complete; the process ended before its answer was recorded", Form: "unknown", Turn: e.Turn, Label: harnessLabel()})
		}
	}
	for _, e := range liveMessages(entries) {
		if e.Kind != agentrun.KindReply || e.Calls == "" {
			continue
		}
		var meta agentrun.ReplyMeta
		if e.Reply != nil {
			meta = *e.Reply
		}
		for i, call := range projection.ParseCalls(e.Calls) {
			if answered[call.Id] {
				continue
			}
			answered[call.Id] = true
			key := trail.ToolKey(meta.CallId, i)
			body := "outcome unknown: the process ended before the result was recorded"
			if !intended[key] {
				body = "never dispatched: the process ended before the call was made"
				settling = append(settling, agentrun.Entry{Kind: agentrun.KindIntent, Key: key, Name: call.Name, Arguments: call.Arguments,
					Effect: agentrun.EffectNone, Form: intentFormNeverDispatched, Turn: e.Turn, Label: harnessLabel()})
			}
			settling = append(settling, agentrun.Entry{Kind: agentrun.KindOutcome, Key: key, Name: call.Id, Role: projection.RoleTool,
				Body: body, Form: "unknown", Turn: e.Turn, Label: harnessLabel()})
		}
	}
	if len(settling) == 0 {
		return
	}
	_, err = w.Append(ctx, settling...)
	return
}

// intentFormNeverDispatched marks an intent resume wrote for a call the
// process never made: no act followed it.
const intentFormNeverDispatched = "never-dispatched"

// dropFormUnanswered names the drop edits of a turn the host stopped in
// before it was answered: the transcript shows its message as failed.
const dropFormUnanswered = "unanswered"

// settleOpenTurn ends the turn the host stopped in: the notes written for
// one call only go, a turn that got no answer is dropped from the
// projection as a failed turn is (ADR-0265 §SD3), and a turn entry closes
// it, so a later resume finds nothing open and repeats nothing.
func settleOpenTurn(ctx context.Context, w *agentrun.Writer, turn string) (err error) {
	if turn == "" {
		return
	}
	entries := w.Entries()
	var edits []agentrun.Entry
	answered := false
	for _, e := range liveMessages(entries) {
		if e.Turn != turn {
			continue
		}
		switch {
		case e.Kind == agentrun.KindNote && e.Form == "once":
			edits = append(edits, agentrun.Entry{Kind: agentrun.KindEdit, Target: e.Ordinal, Form: agentrun.FormDrop, Turn: turn, Label: harnessLabel()})
		case e.Kind == agentrun.KindReply && e.Calls == "" && e.Body != "":
			answered = true
		}
	}
	if !answered {
		edits = edits[:0]
		for _, e := range liveMessages(entries) {
			if e.Turn == turn {
				edits = append(edits, agentrun.Entry{Kind: agentrun.KindEdit, Target: e.Ordinal, Form: agentrun.FormDrop, Name: dropFormUnanswered, Turn: turn, Label: harnessLabel()})
			}
		}
	}
	state := "failed: the host stopped before the turn was answered"
	if answered {
		state = "stopped: the host stopped after the turn was answered"
	}
	edits = append(edits, agentrun.Entry{Kind: agentrun.KindTurn, Turn: turn, Body: state, Label: harnessLabel()})
	_, err = w.Append(ctx, edits...)
	return
}

// resumeLines are what resume settled, one line per unsettled act by its
// resolution (ADR-0308 §SD4): a consequential command whose outcome is
// unknown is listed first.
func resumeLines(r *resumed) (lines []string) {
	if r.stranded {
		return
	}
	outcomes := map[string]bool{}
	for _, e := range r.writer.Entries() {
		if e.Kind == agentrun.KindOutcome {
			outcomes[e.Key] = true
		}
	}
	for _, u := range r.fold.Unsettled {
		var line string
		if u.Resolution == agentrun.ResolutionRemake && outcomes[u.Entry.Key] {
			// The summariser's call (chat_compact.go): its request is
			// answered by an outcome, not a reply.
			continue
		}
		switch u.Resolution {
		case agentrun.ResolutionListToPerson:
			line = "a consequential command may have run before the host stopped, and its outcome is unknown: " + u.Entry.Name + " " + clip(u.Entry.Arguments, 120)
		case agentrun.ResolutionUnknown:
			line = "the command " + u.Entry.Name + " was sent and its outcome is unknown; read before you write there"
		case agentrun.ResolutionNotCompleted:
			line = "the call " + u.Entry.Name + " did not complete; read again if you need it"
		case agentrun.ResolutionPending:
			line = "the command " + u.Entry.Name + " was applied and its completion was not recorded"
		case agentrun.ResolutionAskAgain:
			line = "a question to the person was not answered; ask it again if it still matters"
		case agentrun.ResolutionLapsed:
			line = "the task's grant lapsed with the host; request_access starts a new task in this conversation"
		case agentrun.ResolutionRemake:
			line = "a model call got no answer before the host stopped"
		}
		if line != "" {
			lines = append(lines, line)
		}
	}
	if r.fold.OpenTurn != "" {
		lines = append(lines, "the turn "+r.fold.OpenTurn+" was cut short; its calls stand as listed")
	}
	return
}

// deleteRun tombstones a run off the frame: the current conversation's
// through its live writer, so one writer stands on the run (ADR-0308
// §SD9), and not while a turn runs over it; no turn starts over it while
// the delete is in flight. The deletion runs under a context nothing
// cancels, so it is never left half done.
func (inst *App) deleteRun(id string) {
	store := inst.journalStore()
	if store == nil {
		return
	}
	var live *agentrun.Writer
	if inst.conv.id == id {
		if !inst.canAct() {
			inst.busyNote()
			return
		}
		if inst.coord != nil {
			live = inst.coord.journalNow()
		}
	}
	started := inst.runAction("chat-delete-run", "delete conversation", func(ctx context.Context) (note string, err error) {
		ctx = journalCtx(ctx)
		w := live
		if w == nil {
			of, found, ferr := agentrun.RunOfConversation(ctx, store, string(ManifestId), id)
			if ferr != nil {
				return "", ferr
			}
			if !found {
				return "", eb.Build().Str("conversation", id).Errorf("no run of the conversation")
			}
			if w, err = agentrun.Reopen(ctx, store, of.Id); err != nil {
				return
			}
		}
		if err = w.Delete(ctx, "person"); err != nil {
			return
		}
		return "deleted " + id, nil
	}, func() {
		if inst.conv.id == id {
			inst.conv.stranded = true
		}
		inst.listRuns()
	})
	if started {
		inst.deleting = id
	}
}

// drainResume adopts a resumed run on the render thread; a turn started
// since the resume began keeps the conversation it was sent in.
func (inst *App) drainResume() {
	r, _, ok := inst.resumeJob.TakeResult()
	if !ok {
		if snap := inst.resumeJob.Snapshot(); snap.State == bgjob.StateFailed {
			inst.resumeJob.Invalidate()
			inst.setNote("resume failed: "+failureReason(snap.Err), true)
		}
		return
	}
	if !inst.canAct() {
		inst.setNote("resume: a turn was sent meanwhile; resume the conversation again once it is answered", true)
		return
	}
	inst.adopt(r)
}

// adopt makes the resumed run the window's conversation (ADR-0308 §SD4):
// the transcript from the journal, the settings and the label from the run
// row, the coordinator over the run's writer, and what resume settled
// shown to the person.
func (inst *App) adopt(r *resumed) {
	inst.newConversation()
	conv := inst.conv
	conv.id, conv.started, conv.startedAt = r.run.Conversation, true, r.run.Created.UnixMilli()
	var settings journalSettings
	_ = json.Unmarshal([]byte(r.run.Settings), &settings)
	conv.keep, conv.apps, conv.questions, conv.artefact = settings.Keep, settings.Apps, settings.Questions, settings.Artefact
	if settings.Rounds > 0 {
		// The Settings the run was started under, within the profile's
		// limits; syncAuthority hands them to the coordinator.
		inst.rounds, inst.opTools = float64(clampRounds(settings.Rounds)), settings.OperationTools
		inst.perms = inst.perms.withPixels(pixelsOf(settings.Pixels), settings.PixelsLocal)
	}
	conv.stranded = r.stranded || r.run.State == agentrun.StateDeleted || r.fold.Deleted
	conv.entries = transcriptOf(r.entries)
	conv.parent, conv.lastIn, conv.lastOut = lastReplyOf(r.entries)
	if conv.titleSource == titleNone {
		for _, e := range conv.entries {
			if e.speaker == speakerUser {
				conv.title, conv.titleSource = firstLineTitle(e.text), titleFirstLine
				break
			}
		}
	}
	coord := inst.newCoordinator(conv.id)
	coord.mu.Lock()
	// The coordinator's fingerprint is the profile's, which the call rows
	// name; the run row's folds in the confined tier (runFingerprint).
	coord.journal, coord.profile, coord.fingerprint = r.writer, profileNow(), profileNow().Fingerprint()
	coord.tainted = r.fold.Label.Level >= agentrun.LevelExternal
	coord.confined = r.fold.Label.Tier >= coord.confinedTier
	coord.compacted = slices.ContainsFunc(r.entries, func(e agentrun.Entry) bool { return e.Kind == agentrun.KindCompact })
	if settings.CeilingSpec != nil {
		coord.ceiling = *settings.CeilingSpec
	}
	coord.mu.Unlock()
	coord.offer(conv.apps, conv.questions)
	if conv.artefact {
		// The revisions live in the artefact store, not in entries: the
		// last export, when there is one, brings the exported revision back
		// (ADR-0339 §SD7, proposed); otherwise the artefact starts empty,
		// and the snapshot note re-told at the next turn says so.
		if r.art != nil {
			conv.art.close()
			conv.art, r.art = r.art, nil
		}
		conv.art.reserveThrough(revisionsNamed(r.entries))
		coord.offerArtefact(conv.art)
	}
	if r.art != nil {
		r.art.close()
	}
	inst.coord, conv.coord = coord, coord
	inst.resumeNotes(r)
}

// lastReplyOf is the last live reply's call id and usage: the next turn's
// parent and the transcript's last tokens.
func lastReplyOf(entries []agentrun.Entry) (parent string, lastIn int32, lastOut int32) {
	for _, e := range liveMessages(entries) {
		if e.Kind != agentrun.KindReply {
			continue
		}
		if meta := e.Reply; meta != nil && meta.CallId != "" {
			parent, lastIn, lastOut = meta.CallId, int32(meta.InputTokens), int32(meta.OutputTokens)
		}
	}
	return
}

// resumeNotes shows the person what resume settled, a line in the
// transcript; the model was told in the note the resume job wrote.
func (inst *App) resumeNotes(r *resumed) {
	if r.stranded {
		inst.conv.entries = append(inst.conv.entries, entry{speaker: speakerTool, atMs: time.Now().UnixMilli(),
			text: "resumed read-only: this conversation was started under another prompt, tool set, host limits or confined tier (fingerprint " + clip(r.run.Fingerprint, 12) + "), stranded, or deleted; continuing it is a new conversation"})
		return
	}
	if len(r.lines) == 0 {
		return
	}
	inst.conv.entries = append(inst.conv.entries, entry{speaker: speakerTool, atMs: time.Now().UnixMilli(), text: "resumed · " + strings.Join(r.lines, " · ")})
}

// transcriptOf rebuilds the transcript from the journal's live entries:
// the person's messages, the model's answers, and a line per tool call
// from its outcome's activity. A message of the person's that resume
// dropped as unanswered is shown as the failed turn it was.
func transcriptOf(entries []agentrun.Entry) (out []entry) {
	replies := map[string]bool{}
	unanswered := map[uint32]bool{}
	for _, e := range entries {
		if e.Kind == agentrun.KindEdit && e.Form == agentrun.FormDrop && e.Name == dropFormUnanswered {
			unanswered[e.Target] = true
		}
	}
	for _, e := range entries {
		if e.Kind == agentrun.KindUser && e.Form != formPixels && unanswered[e.Ordinal] {
			out = append(out, entry{speaker: speakerUser, text: e.Body, atMs: e.At.UnixMilli(), failed: true, reason: "the host stopped before the turn was answered"})
		}
	}
	for _, e := range liveMessages(entries) {
		at := e.At.UnixMilli()
		switch e.Kind {
		case agentrun.KindUser:
			if e.Form == formPixels {
				continue
			}
			out = append(out, entry{speaker: speakerUser, text: e.Body, atMs: at})
		case agentrun.KindReply:
			if e.Body == "" || replies[e.Key] {
				continue
			}
			replies[e.Key] = true
			out = append(out, entry{speaker: speakerModel, text: e.Body, atMs: at})
		case agentrun.KindOutcome:
			if e.Role != projection.RoleTool {
				continue
			}
			line := e.Form
			if line == "" {
				line = e.Key
			}
			out = append(out, entry{speaker: speakerTool, text: line, atMs: at})
		}
	}
	return
}

// renderRunsTab lists the conversations with Resume and Delete. It lists
// them again each time the tab comes to the front.
func (inst *App) renderRunsTab() {
	if inst.journalStore() == nil {
		c.Label("This host keeps no conversations: it has no journal store.").Wrap().Send()
		return
	}
	shown := inst.tabShown(tabConversations)
	if shown && !inst.runsShown {
		inst.listRuns()
		inst.listRoster()
	}
	inst.runsShown = shown
	if runs, _, ok := inst.runsJob.TakeResult(); ok {
		inst.runs = *runs
	}
	if inst.runsJob.Snapshot().State == bgjob.StateRunning {
		c.Label("listing…").Selectable(false).Send()
	}
	inst.renderRoster()
}
