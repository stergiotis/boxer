package chat

import (
	"context"
	"encoding/json/v2"
	"strconv"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/projection"
	"github.com/stergiotis/boxer/public/keelson/runtime/anchorstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/cell"
	"github.com/stergiotis/boxer/public/keelson/runtime/clipboardbroker"
	"github.com/stergiotis/boxer/public/llm/openaichat"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// The journal is the conversation's only input (ADR-0308 §SD3): the
// coordinator holds no message list. Every act is written here before it
// leaves (§SD2) — the prompt sections, the profile, the tool set, the
// host's notes, the person's message, each request with its digest, each
// reply, each tool call's intent and outcome, each question and answer,
// the summariser's and the title's calls, each copy, the turn's end — and
// every request the model receives is the projection of the entries so
// far (ADR-0309), encoded once for the route. The projection is the Go
// fold; the SQL text is its twin for the worker and the server, and law
// L4 compares the two in the test lanes.
//
// Labels (ADR-0311): the person's and the host's words are level 0; a
// reply is this run's model output; an app's untrusted result is external
// content; confined content carries the tier the cell declaration maps
// the compile-date `confined` bit to. A remote route's host is the
// exposure mark on every request sent to it and the origin mark on every
// reply from it.

// confinedTierNow is the tier the compile-date `confined` bit maps to
// (ADR-0311 §SD1, ADR-0312): the cell declaration's, read from the same
// environment the host builds its declaration from, since an app has no
// handle on the host's copy.
var confinedTierNow = sync.OnceValue(func() agentrun.TierE {
	return cell.FromEnv(nil).ConfinedTier
})

// journalCtx is the context a journal write runs under: the act's, kept
// past a cancel, so a Stop while a call waits never leaves the record
// short of what happened; the client bounds each write by its own
// timeout.
func journalCtx(ctx context.Context) context.Context { return context.WithoutCancel(ctx) }

// journalRoute is what the coordinator knows of its model route: the
// endpoint host and whether it is inside the cell, for the marks; the
// context size, for the compaction trigger.
type journalRoute struct {
	host    string
	local   bool
	context int32
}

// setRoute tells the coordinator its route.
func (inst *coordinator) setRoute(host string, local bool, contextTokens int32) {
	inst.mu.Lock()
	inst.route = journalRoute{host: host, local: local, context: contextTokens}
	inst.mu.Unlock()
}

// journalSettings are the conversation's settings fixed at the first send,
// on the run row (ADR-0308 §SD6).
type journalSettings struct {
	Keep      bool   `json:"keep"`
	Apps      bool   `json:"apps"`
	Questions bool   `json:"questions"`
	Artefact  bool   `json:"artefact"`
	Ceiling   string `json:"ceiling,omitempty"`
	// CeilingSpec is the ceiling itself, which resume restores; Ceiling
	// is its level, for a reader of the row.
	CeilingSpec *agent.Ceiling `json:"ceiling_spec,omitempty"`
}

// openJournal opens the run on the first act of a conversation: the run
// row pinned to the profile, then the profile entry. A writer a transient
// failure left refusing every write is reopened from the store on the
// next turn, the chain verified, and the record says so.
func (inst *coordinator) openJournal(ctx context.Context, keep bool) (w *agentrun.Writer, err error) {
	inst.mu.Lock()
	w, store, retired, failed := inst.journal, inst.store, inst.retired, inst.journalFailed
	apps, questions, art := inst.apps, inst.questions, inst.art != nil
	ceiling := inst.ceiling
	inst.mu.Unlock()
	if w != nil && failed {
		return inst.reopenJournal(ctx, w)
	}
	if w != nil {
		return
	}
	if retired {
		// The conversation moved on before its first act: no run is
		// opened for a turn that will not be shown.
		err = eb.Build().Str("conversation", inst.conversation).Errorf("the conversation was replaced")
		return
	}
	if store == nil {
		err = eb.Build().Str("conversation", inst.conversation).Errorf("the conversation has no journal store")
		return
	}
	p := profileNow()
	settings, _ := json.Marshal(journalSettings{Keep: keep, Apps: apps, Questions: questions, Artefact: art, Ceiling: ceiling.Level().String(), CeilingSpec: &ceiling})
	w, err = agentrun.Open(ctx, store, agentrun.Run{Id: inst.conversation, Conversation: inst.conversation, OwnerApp: string(ManifestId),
		Coordinator: inst.window, Profile: p.Id, Fingerprint: p.Fingerprint(), Settings: string(settings)})
	if err != nil {
		return
	}
	if _, err = inst.append(ctx, w, agentrun.Entry{Kind: agentrun.KindProfile, Name: p.Id, Body: p.Fingerprint(), Label: harnessLabel()}); err != nil {
		return
	}
	inst.mu.Lock()
	inst.journal, inst.profile, inst.fingerprint = w, p, p.Fingerprint()
	inst.mu.Unlock()
	return
}

// reopenJournal replaces a writer that refuses every write since a failed
// one with a fresh writer over the same run: the store's record is read
// back and verified, and an ignorable entry records the reconnection.
func (inst *coordinator) reopenJournal(ctx context.Context, old *agentrun.Writer) (w *agentrun.Writer, err error) {
	inst.mu.Lock()
	store := inst.store
	inst.mu.Unlock()
	id := old.Run().Id
	if w, err = agentrun.Reopen(ctx, store, id); err != nil {
		return nil, eb.Build().Str("run", id).Errorf("the journal refused an earlier write and does not reopen: %w", err)
	}
	if w.Run().State != agentrun.StateOpen {
		return nil, eb.Build().Str("run", id).Str("state", w.Run().State.String()).Errorf("the journal refused an earlier write and the run is not open")
	}
	if _, err = inst.append(ctx, w, agentrun.Entry{Kind: agentrun.KindIgnorable, Name: "journal", Form: "reopened", Body: "the writer refused a write earlier; the run was reopened from the store and its chain verified", Label: harnessLabel()}); err != nil {
		return nil, err
	}
	log.Warn().Str("run", id).Msg("chat: the journal was reopened after a failed write")
	inst.mu.Lock()
	inst.journal, inst.journalFailed = w, false
	inst.mu.Unlock()
	return
}

// noteJournalFailed marks the writer as refusing writes: the next turn
// reopens the run before its first act.
func (inst *coordinator) noteJournalFailed() {
	inst.mu.Lock()
	inst.journalFailed = true
	inst.mu.Unlock()
}

// append is every journal write the coordinator makes: a write the
// writer refuses marks it for reopening at the next turn, since a writer
// that refused one write after it may have reached the store refuses
// every later one.
func (inst *coordinator) append(ctx context.Context, w *agentrun.Writer, entries ...agentrun.Entry) (written []agentrun.Entry, err error) {
	if written, err = w.Append(ctx, entries...); err != nil {
		inst.noteJournalFailed()
	}
	return
}

// journalNow is the open writer, nil before the first act.
func (inst *coordinator) journalNow() (w *agentrun.Writer) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.journal
}

// harnessLabel is the label of the host's own words.
func harnessLabel() agentrun.Label { return agentrun.Label{Level: agentrun.LevelPerson} }

// sectionOrder is the prompt book's declared order (chat_prompts.go).
func sectionOrder() (order projection.Order) {
	for _, s := range promptSections {
		order = append(order, s.slug)
	}
	return
}

// syncSections writes a section entry for every section whose live text
// differs from what the conversation carries now: all of them at the first
// send, a change or a clearing later. A section change rewrites the head
// (ADR-0309 §SD2) and is what the fingerprint folds over.
func (inst *coordinator) syncSections(ctx context.Context, w *agentrun.Writer) (err error) {
	apps, questions := inst.offers()
	art := inst.artefactOf() != nil
	live := map[string]string{}
	for _, e := range w.Entries() {
		if e.Kind == agentrun.KindSection {
			live[e.Name] = e.Body
		}
	}
	var entries []agentrun.Entry
	for _, sec := range promptSections {
		want := ""
		if sec.when(apps, questions, art) {
			want = promptText(sec.slug)
		}
		if live[sec.slug] == want {
			continue
		}
		entries = append(entries, agentrun.Entry{Kind: agentrun.KindSection, Name: sec.slug, Book: promptBookId, Body: want, Label: harnessLabel()})
	}
	if len(entries) == 0 {
		return
	}
	_, err = inst.append(ctx, w, entries...)
	return
}

// syncTools writes a tools entry when the tool set the model is offered
// changed since the last declaration.
func (inst *coordinator) syncTools(ctx context.Context, w *agentrun.Writer, tools []openaichat.Tool) (err error) {
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name)
	}
	body := projection.ToolsBody(names)
	last := ""
	for _, e := range w.Entries() {
		if e.Kind == agentrun.KindTools {
			last = e.Body
		}
	}
	if last == body || (last == "" && len(names) == 0) {
		return
	}
	_, err = inst.append(ctx, w, agentrun.Entry{Kind: agentrun.KindTools, Name: agentrun.ContentDigest(body), Body: body, Label: harnessLabel()})
	return
}

// project is the request the journal says to send now: the fold over the
// entries, validated, its messages encoded for the route, its digest.
func (inst *coordinator) project(ctx context.Context, w *agentrun.Writer) (batch projection.Batch, msgs []openaichat.Message, digest string, err error) {
	batch = projection.Fold(w.Entries(), sectionOrder())
	if err = projection.Validate(batch); err != nil {
		return
	}
	if msgs, err = projection.Encode(batch, inst.resolveAttachment); err != nil {
		return
	}
	inst.mu.Lock()
	fp := inst.fingerprint
	inst.mu.Unlock()
	digest, err = batch.Digest(fp)
	return
}

// resolveAttachment reads a screenshot's bytes by the digest the artefact's
// store keeps them under.
func (inst *coordinator) resolveAttachment(a projection.Attachment) (data []byte, ok bool) {
	art := inst.artefactOf()
	if art == nil {
		return nil, false
	}
	data, err := art.store.read(a.Digest)
	return data, err == nil
}

// replyLabel is a reply's label: this run's model output, no lower than
// the request it answers (ADR-0311 §SD6, law L6), marked with its origin
// when the route is remote.
func (inst *coordinator) replyLabel(request ...agentrun.Label) (l agentrun.Label) {
	inst.mu.Lock()
	r := inst.route
	inst.mu.Unlock()
	l = agentrun.Label{Level: agentrun.LevelOwnModel}
	for _, req := range request {
		l = l.Join(req)
	}
	if r.host != "" && !r.local {
		l = l.WithOrigin(r.host)
	}
	return
}

// requestLabel is a request's label: the batch's, marked exposed to the
// route when it is remote.
func (inst *coordinator) requestLabel(batch projection.Batch) (l agentrun.Label) {
	inst.mu.Lock()
	r := inst.route
	inst.mu.Unlock()
	l = batch.Label()
	if r.host != "" && !r.local {
		l = l.WithExposure(r.host)
	}
	return
}

// outcomeLabel is a tool result's label: external content when the app's
// text is untrusted, the host's own data otherwise; the confined tier
// when the result derives from sealed data.
func outcomeLabel(untrusted bool, confined bool) (l agentrun.Label) {
	l.Level = agentrun.LevelSystem
	if untrusted {
		l.Level = agentrun.LevelExternal
	}
	if confined {
		l.Tier = confinedTierNow()
	}
	return
}

// requestBody is what a request entry carries beside its key.
type requestBody struct {
	Digest   string `json:"digest"`
	Messages int    `json:"messages"`
	Tools    int    `json:"tools"`
	Round    int    `json:"round"`
	Label    string `json:"label"`
}

// replyBody is what a reply entry carries beside the message: the call id
// and the usage.
type replyBody struct {
	CallId       string `json:"call_id"`
	FinishReason string `json:"finish_reason,omitempty"`
	InputTokens  int32  `json:"input_tokens,omitempty"`
	OutputTokens int32  `json:"output_tokens,omitempty"`
}

// requestKey keys a round's request and its reply in the journal.
func requestKey(turn string, round int) string { return turn + "#" + strconv.Itoa(round) }

// dropTurn writes an edit that drops every message entry of the turn from
// the projection: a turn that got no answer is not resent (ADR-0265
// §SD3); the record keeps it.
func (inst *coordinator) dropTurn(ctx context.Context, w *agentrun.Writer, turn string) (dropped []uint32, err error) {
	var edits []agentrun.Entry
	for _, e := range liveMessages(w.Entries()) {
		if e.Turn == turn {
			edits = append(edits, agentrun.Entry{Kind: agentrun.KindEdit, Target: e.Ordinal, Form: agentrun.FormDrop, Turn: turn, Label: harnessLabel()})
			dropped = append(dropped, e.Ordinal)
		}
	}
	if len(edits) == 0 {
		return
	}
	_, err = inst.append(ctx, w, edits...)
	return
}

// restoreEntries writes edits that put dropped entries back as they last
// stood: an entry edited before the drop — a screenshot replaced by its
// placeholder at the turn's end (ADR-0287 §SD5) — comes back as the edit
// left it, never with what the edit took away.
func (inst *coordinator) restoreEntries(ctx context.Context, w *agentrun.Writer, ordinals []uint32) (err error) {
	byOrd := map[uint32]agentrun.Entry{}
	effective := map[uint32]agentrun.Entry{}
	for _, e := range w.Entries() {
		byOrd[e.Ordinal] = e
		if e.Kind == agentrun.KindEdit && e.Form != agentrun.FormDrop {
			effective[e.Target] = e
		}
	}
	var edits []agentrun.Entry
	for _, o := range ordinals {
		e, ok := byOrd[o]
		if !ok {
			continue
		}
		if last, edited := effective[o]; edited {
			e.Body, e.Attachments = last.Body, last.Attachments
		}
		edits = append(edits, agentrun.Entry{Kind: agentrun.KindEdit, Target: o, Form: "restore", Body: e.Body, Attachments: e.Attachments, Turn: e.Turn, Label: harnessLabel()})
	}
	if len(edits) == 0 {
		return
	}
	_, err = inst.append(ctx, w, edits...)
	return
}

// liveMessages are the message entries the projection shows: not dropped
// by their latest edit.
func liveMessages(entries []agentrun.Entry) (live []agentrun.Entry) {
	dropped := map[uint32]bool{}
	for _, e := range entries {
		if e.Kind == agentrun.KindEdit {
			dropped[e.Target] = e.Form == agentrun.FormDrop
		}
	}
	for _, e := range entries {
		switch e.Kind {
		case agentrun.KindUser, agentrun.KindReply, agentrun.KindOutcome, agentrun.KindNote:
			if !dropped[e.Ordinal] {
				live = append(live, e)
			}
		}
	}
	return
}

// livePixels are the shown-screenshot entries still carrying their pixels.
func livePixels(entries []agentrun.Entry) (live []agentrun.Entry) {
	edited := map[uint32]agentrun.Entry{}
	for _, e := range entries {
		if e.Kind == agentrun.KindEdit {
			edited[e.Target] = e
		}
	}
	for _, e := range entries {
		if e.Kind != agentrun.KindUser || e.Form != formPixels {
			continue
		}
		if ed, ok := edited[e.Ordinal]; ok && ed.Attachments == "" {
			continue
		}
		live = append(live, e)
	}
	return
}

// formPixels marks a user entry that carries shown screenshots.
const formPixels = "pixels"

// stripPixelsJournal replaces every live pixels entry by its placeholder:
// the names it showed, without the pixels (ADR-0287 §SD5).
func (inst *coordinator) stripPixelsJournal(ctx context.Context, w *agentrun.Writer) (stripped bool, err error) {
	return stripPixelEntries(ctx, w)
}

// stripPixelEntries is stripPixelsJournal over a writer alone: what resume
// does before a coordinator stands over the run.
func stripPixelEntries(ctx context.Context, w *agentrun.Writer) (stripped bool, err error) {
	var edits []agentrun.Entry
	for _, e := range livePixels(w.Entries()) {
		edits = append(edits, agentrun.Entry{Kind: agentrun.KindEdit, Target: e.Ordinal, Form: "placeholder", Turn: e.Turn,
			Body: pixelsPlaceholder(openaichat.Message{Content: e.Body}).Content, Label: harnessLabel()})
	}
	if len(edits) == 0 {
		return
	}
	_, err = w.Append(ctx, edits...)
	return err == nil, err
}

// jsonMarshal is the journal's JSON for a record body.
func jsonMarshal(v any) (string, error) {
	b, err := json.Marshal(v, json.Deterministic(true))
	return string(b), err
}

// effectOf is the declared effect class of a tool call (ADR-0269 §SD5):
// the operation's for call_operation and a typed tool, read from the
// window's app's catalog; for the coordinator's own tools, document for
// the artefact's and its images' writes and captures, none for the
// questions and the views, view for request_access, open_window,
// arrange_windows, raise_window, place_window, point_out, clear_marks and
// stop_task, none for the rest. A call into a window whose app or
// operation is unknown is consequential: the conservative reading, since
// an unknown act is the one to list to the person on resume.
func (inst *coordinator) effectOf(ctx context.Context, name string, arguments string) (effect string) {
	inst.mu.Lock()
	t, typed := inst.typed[name]
	inst.mu.Unlock()
	if typed {
		return inst.operationEffect(ctx, t.window, t.op)
	}
	if name == "call_operation" {
		var args struct {
			Window    float64 `json:"window"`
			Operation string  `json:"operation"`
		}
		if json.Unmarshal([]byte(arguments), &args) == nil && args.Operation != "" {
			return inst.operationEffect(ctx, uint64(args.Window), args.Operation)
		}
		return agentrun.EffectConsequential
	}
	spec := lookupTool(name)
	if spec == nil {
		return agentrun.EffectNone
	}
	switch spec.group {
	case toolGroupArtefactWrite, toolGroupImageWrite, toolGroupImageCapture:
		return agentrun.EffectDocument
	case toolGroupQuestions, toolGroupView:
		return agentrun.EffectNone
	}
	switch name {
	case "request_access", "open_window", "arrange_windows", "raise_window", "place_window", "point_out", "clear_marks", "stop_task":
		return agentrun.EffectView
	}
	return agentrun.EffectNone
}

// operationEffect is an operation's declared effect as the window's app
// states it in its catalog: the app is the one list_windows or
// open_window named for the window, the catalog describe_app's, read on
// demand and cached per app. Consequential when the window's app or the
// operation is unknown, the conservative reading.
func (inst *coordinator) operationEffect(ctx context.Context, window uint64, op string) (effect string) {
	inst.mu.Lock()
	appId, known := inst.windowApps[window]
	effects, described := inst.opEffects[appId]
	effect, declared := effects[op]
	inst.mu.Unlock()
	if !known {
		return agentrun.EffectConsequential
	}
	if !described {
		// The catalog was not read yet: describe_app's cache fills now.
		inst.operationsOf(ctx, appId)
		inst.mu.Lock()
		effect, declared = inst.opEffects[appId][op]
		inst.mu.Unlock()
	}
	if declared {
		return effect
	}
	return agentrun.EffectConsequential
}

// noteWindowApp records which app a window runs, from a listing or an
// opening, for the effect class of a call into it.
func (inst *coordinator) noteWindowApp(window uint64, appId string) {
	if appId == "" {
		return
	}
	inst.mu.Lock()
	inst.windowApps[window] = appId
	inst.mu.Unlock()
}

// lastResultLabel is what the last tool result was: untrusted text, and
// confined content.
func (inst *coordinator) lastResultLabel() (untrusted bool, confined bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.lastUntrusted, inst.confined
}

// turnBody is what a turn entry carries: how the turn ended.
func turnBody(state string, reason string) string {
	if reason == "" {
		return state
	}
	return state + ": " + strings.Join(strings.Fields(reason), " ")
}

// headFormLocal marks a head entry whose receipt came from a store the
// process alone holds: shipped, recorded, anchoring nothing (ADR-0314,
// Migration). Law L32 reads past such heads.
const headFormLocal = "local"

// shipHead ships the chain's head to the anchor store and journals the
// head entry, at the cadence the run's profile names (ADR-0314 §SD3). A
// head the store cannot hold past this process — the host's in-memory
// store, the default — is recorded as local, anchoring nothing; a
// coordinator with no store at all, as in a test without a host, ships
// none.
func (inst *coordinator) shipHead(ctx context.Context, w *agentrun.Writer, at func(anchorstore.Cadence) bool) {
	inst.mu.Lock()
	anchors, cadence := inst.anchors, inst.profile.AnchorCadence
	inst.mu.Unlock()
	if anchors == nil || w == nil || !at(cadence) {
		return
	}
	run := w.Run()
	ordinal, digest := w.Head()
	head := anchorstore.Head{Cell: run.Cell, Run: run.Id, Ordinal: ordinal, Digest: digest}
	r, err := anchors.Append(ctx, head)
	if err != nil {
		log.Warn().Err(err).Str("run", run.Id).Msg("chat: the chain head was not anchored")
		return
	}
	e, err := anchorstore.HeadEntry(head, r, cadence)
	if err != nil {
		log.Warn().Err(err).Str("run", run.Id).Msg("chat: the chain head was not recorded")
		return
	}
	if !r.Durable {
		e.Form = headFormLocal
	}
	if _, err = inst.append(ctx, w, e); err != nil {
		log.Warn().Err(err).Str("run", run.Id).Msg("chat: the chain head was not recorded")
	}
}

// durableHeads are the entries without the heads a local store receipted:
// what law L32 is checked over once the process that held them is gone.
func durableHeads(entries []agentrun.Entry) (out []agentrun.Entry) {
	out = make([]agentrun.Entry, 0, len(entries))
	for _, e := range entries {
		if e.Kind == agentrun.KindHead && e.Form == headFormLocal {
			continue
		}
		out = append(out, e)
	}
	return
}

// retire ends the coordinator's run: now, when no turn runs over it, or
// from the turn's own exit once it has written its tail, so the close
// never cuts a turn's record short. closed is done once the run is.
func (inst *coordinator) retire() {
	inst.mu.Lock()
	inst.retired = true
	running := inst.turns > 0
	inst.mu.Unlock()
	if running {
		return
	}
	go inst.closeJournal()
}

// enterTurn counts a turn in, unless the coordinator retired already: a
// turn that starts after the conversation moved on writes nothing.
// leaveTurn counts it out and closes the run when the coordinator retired
// meanwhile.
func (inst *coordinator) enterTurn() (entered bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if inst.retired {
		return false
	}
	inst.turns++
	return true
}

func (inst *coordinator) leaveTurn() {
	inst.mu.Lock()
	inst.turns--
	closing := inst.turns == 0 && inst.retired
	inst.mu.Unlock()
	if closing {
		inst.closeJournal()
	}
}

// closeJournal ends the run: a last head, then the close (ADR-0314
// §SD3), under its own bound since the conversation that owned it is
// gone. A run that is stranded or deleted stays as it is. closed is done
// afterwards, whatever happened.
func (inst *coordinator) closeJournal() {
	defer inst.closeOnce.Do(func() { close(inst.closed) })
	w := inst.journalNow()
	if w == nil {
		return
	}
	switch w.Run().State {
	case agentrun.StateStranded, agentrun.StateDeleted:
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), agentrun.DefaultTimeout)
	defer cancel()
	inst.shipHead(ctx, w, func(c anchorstore.Cadence) bool { return c.PerRunClose })
	if err := w.Close(ctx, agentrun.StateClosed); err != nil {
		log.Warn().Err(err).Str("run", w.Run().Id).Msg("chat: the run was not closed")
	}
}

// awaitClosed waits until the coordinator's run is closed, or ctx ends;
// it reports whether the close landed. A coordinator not retired never
// closes, so the wait is only for a retired one.
func (inst *coordinator) awaitClosed(ctx context.Context) (closed bool) {
	select {
	case <-inst.closed:
		return true
	case <-ctx.Done():
		return false
	}
}

// copyEntry journals a copy of labelled text before it is made (ADR-0317
// §SD1, ADR-0308 §SD2): a copy the journal refuses is not made. Without an
// open run there is nothing to journal it in, and nothing the copy could
// carry from a conversation.
func (inst *coordinator) copyEntry(ctx context.Context, rec clipboardbroker.CopyRecord) (err error) {
	w := inst.journalNow()
	if w == nil {
		return
	}
	body, err := jsonMarshal(rec)
	if err != nil {
		return eb.Build().Errorf("copy record: %w", err)
	}
	if _, err = inst.append(ctx, w, agentrun.Entry{Kind: agentrun.KindCopy, Key: rec.Handle, Name: rec.Source, Body: body, Label: rec.Label}); err != nil {
		return eb.Build().Errorf("the copy was not journaled: %w", err)
	}
	return
}
