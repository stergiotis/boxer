package chat

import (
	"context"
	"crypto/sha256"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/projection"
	"github.com/stergiotis/boxer/public/keelson/runtime/anchorstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// The preservation harness (ADR-0310 §SD11): a corpus of scripted
// conversations driven through runTurn, recording per round the request as
// the wire carries it and every operation call the host received, into a
// golden under testdata/preserve. "Behaviour-preserving" for the refactors
// of the agentic tier means this golden does not change (ADR-0310 §SD2's
// verification); a step that must change it names the change and reruns
// with -update-preserved.
//
// What the golden holds is what a provider and the apps would have
// received, not what the person sees; nothing here reads the window.

var updatePreserved = flag.Bool("update-preserved", false, "rewrite testdata/preserve/*.golden from the current behaviour")

const preservedDir = "testdata/preserve"

// preserveHarness is one scenario's rig: the note host and the coordinator
// of coordRig, the conversation the turns land in, and the recording.
type preserveHarness struct {
	t     *testing.T
	host  *noteHost
	coord *coordinator
	conv  *conversation
	model *scriptedModel
	ctx   context.Context
	cli   *llm.Client
	// anchors is the run's anchor store: in memory, so the heads it
	// receipts are local, and law L32 is checked over them in process.
	anchors *anchorstore.MemStore

	mu  sync.Mutex
	out strings.Builder
	// known are the tool sets and messages the recording printed in full,
	// by their text, with the reference later rounds cite them by.
	known map[string]string
	// seenRounds and seenOps are how far the model's and the host's records
	// were copied into out.
	seenRounds int
	seenOps    int
}

// preserveTurn is one message of the person and the model's scripted
// replies to it; replies past the script fail with fail when it is set.
type preserveTurn struct {
	text    string
	replies []openaichat.CompletionResponse
	fail    error
	// before runs on the harness before the turn starts: the person's
	// change, a setting moved.
	before func(h *preserveHarness)
	// during hears each round as runTurn starts it, like the window's
	// progress line; a setting moved mid-turn goes here.
	during func(h *preserveHarness, round int)
	// person answers what the turn asks of the person, in a goroutine of
	// its own: ask_user's form.
	person func(h *preserveHarness)
	// script, when set, composes the replies as the turn starts, for a
	// reply that names what the journal holds by then: a stub's ordinal.
	script func(h *preserveHarness) []openaichat.CompletionResponse
}

type preserveScenario struct {
	name string
	// apps, questions and artefact are the conversation's toggles at its
	// first send.
	apps, questions, artefact bool
	opTools                   bool
	setup                     func(h *preserveHarness)
	turns                     []preserveTurn
}

// errProvider stands in for a provider failure that is not a cancel: a
// model error after tool activity stops the turn rather than failing it.
var errProvider = errors.New("provider: HTTP 503 upstream unavailable")

func preserveCorpus() []preserveScenario {
	tool := toolCall
	answer := func(s string) openaichat.CompletionResponse {
		return openaichat.CompletionResponse{Content: s, FinishReason: "stop"}
	}
	listing := func(n int) (out []openaichat.CompletionResponse) {
		for i := range n {
			out = append(out, tool("l"+strconv.Itoa(i), "list_windows", `{}`))
		}
		return
	}
	return []preserveScenario{
		{name: "apps-tool-loop", apps: true, turns: []preserveTurn{
			// Access, a window, a read, two refused writes (one repeated), a
			// write; the ceiling note on the first turn.
			{text: "please tidy my note", replies: []openaichat.CompletionResponse{
				tool("c1", "request_access", `{"plan":"tidy the note","open":[{"app":"notes"}],"title":"Asking for the notes"}`),
				tool("c2", "open_window", `{"app":"notes"}`),
				tool("c3", "call_operation", `{"window":100,"operation":"get_note","args":{},"title":"Reading the note"}`),
				tool("s1", "call_operation", `{"window":100,"operation":"set_note","text":"tidied"}`),
				tool("s2", "call_operation", `{"window":100,"operation":"set_note","args":{}}`),
				tool("s3", "call_operation", `{"window":100,"operation":"set_note","args":{},"title":"Writing it, again"}`),
				tool("c4", "call_operation", `{"window":100,"operation":"set_note","args":{"text":"tidied"},"reason":"tidy it"}`),
				tool("d1", "describe_app", `{"app":"notes","operation":"set_note"}`),
				tool("h1", "read_help", `{"search":"tidy"}`),
				answer("done"),
			}},
			// The person changed the note and lowered the settings: the
			// changes note and a second ceiling note.
			{text: "and now?", before: func(h *preserveHarness) {
				h.host.person(100, "the person's")
				time.Sleep(30 * time.Millisecond)
				h.coord.setOptions(permissions{allow: allowRead}.ceiling(true), false)
			}, replies: []openaichat.CompletionResponse{
				tool("c5", "call_operation", `{"window":100,"operation":"get_note","args":{}}`),
				tool("s4", "call_operation", `{"window":100,"operation":"set_note","args":{"text":"mine"}}`),
				tool("x1", "stop_task", `{}`),
				answer("I may only read now."),
			}},
		}},
		{name: "operation-tools", apps: true, opTools: true, turns: []preserveTurn{
			{text: "please tidy my note", replies: []openaichat.CompletionResponse{
				tool("r1", "request_access", `{"plan":"tidy the note","open":[{"app":"notes"}]}`),
				tool("o1", "open_window", `{"app":"notes"}`),
				tool("g1", "w100_get_note", `{}`),
				tool("t1", "w100_set_note", `{"text":"typed","reason":"tidy it","title":"Writing"}`),
				answer("done"),
			}},
		}},
		{name: "desktop-and-marks", apps: true, turns: []preserveTurn{
			{text: "tidy the desktop", replies: []openaichat.CompletionResponse{
				tool("r1", "request_access", `{"plan":"tidy the desktop","open":["notes"]}`),
				tool("a1", "arrange_windows", `{"command":"tile"}`),
				tool("r2", "request_access", `{"plan":"tidy the desktop","desktop":true}`),
				tool("a2", "arrange_windows", `{"command":"columns","windows":[100]}`),
				tool("x1", "raise_window", `{"window":7}`),
				tool("p1", "place_window", `{"window":100,"x":1,"y":2,"w":300,"h":200}`),
				tool("m1", "point_out", `{"id":"here","op":"highlight","targets":[{"window":100}]}`),
				tool("m2", "point_out", `{"id":"there","op":"callout","targets":[{"tree":"t9","node":"#1"}],"text":"this"}`),
				tool("m3", "clear_marks", `{}`),
				tool("q1", "query_windows", `{"table":"windows","sql":"SELECT key FROM keelson('windows')"}`),
				tool("w1", "read_window_tree", `{"windows":[100]}`),
				answer("done"),
			}},
		}},
		{name: "last-round-answer", apps: true, turns: []preserveTurn{
			{text: "list forever", replies: append(listing(defaultRounds-1), answer("Here is what I found."))},
			{text: "again", replies: []openaichat.CompletionResponse{answer("still here")}},
		}},
		{name: "last-round-stop", apps: true, turns: []preserveTurn{
			{text: "list forever", replies: listing(defaultRounds)},
			{text: "again", replies: []openaichat.CompletionResponse{answer("still here")}},
		}},
		{name: "failed-turns", apps: true, turns: []preserveTurn{
			// A failure before any activity fails the turn; the ceiling note
			// was composed for it.
			{text: "hello", fail: errProvider},
			// A failure after activity stops the turn.
			{text: "try again", replies: []openaichat.CompletionResponse{
				tool("c1", "request_access", `{"plan":"tidy the note","open":[{"app":"notes"}]}`),
			}, fail: errProvider},
			// A cancel after activity fails the turn.
			{text: "once more", replies: []openaichat.CompletionResponse{
				tool("l1", "list_windows", `{}`),
			}, fail: context.Canceled},
			{text: "and now?", replies: []openaichat.CompletionResponse{answer("fine")}},
		}},
		{name: "questions", questions: true, turns: []preserveTurn{
			{text: "write me the report", replies: []openaichat.CompletionResponse{
				tool("q1", "ask_user", `{"questions":[{"question":"Which scope?","header":"Scope","options":[{"label":"Whole","description":"everything"},{"label":"Part"}]},{"question":"Which tone?","multi_select":true,"options":[{"label":"Formal"},{"label":"Brief"},{"label":"Warm"}]}]}`),
				tool("q2", "ask_user", `{"questions":[{"question":"Really?","options":[{"label":"Yes"},{"label":"No"}]}]}`),
				tool("q3", "ask_user", `{"questions":[]}`),
				answer("Here is the report."),
			}, person: func(h *preserveHarness) {
				o := h.waitAsk()
				o.form.single[0], o.form.chosen[0][1], o.form.notes[0][1] = 1, true, "the second half"
				o.form.chosen[1][0], o.form.chosen[1][2], o.form.other[1] = true, true, "and short"
				o.submit()
				h.waitAsk().skip()
			}},
		}},
		{name: "artefact", artefact: true, setup: func(h *preserveHarness) {
			h.conv.art.setPolicy(artPolicy{write: true})
		}, turns: []preserveTurn{
			{text: "draft a plan", replies: []openaichat.CompletionResponse{
				tool("a1", "artefact_read", `{}`),
				tool("a2", "artefact_write", `{"base_revision":0,"text":"# Plan\n\nFirst.\n\n## Steps\n\n- one\n"}`),
				tool("a3", "artefact_outline", `{}`),
				tool("a4", "artefact_edit", `{"base_revision":1,"old_text":"First.","new_text":"First, then second."}`),
				tool("a5", "artefact_edit", `{"base_revision":1,"old_text":"nowhere","new_text":"x"}`),
				tool("a6", "artefact_insert", `{"base_revision":2,"text":"- two","heading":["Plan","Steps"],"at":"end"}`),
				tool("a7", "artefact_find", `{"pattern":"two"}`),
				tool("a8", "artefact_lint", `{}`),
				tool("a9", "artefact_images", `{}`),
				answer("Drafted."),
			}},
			// The person reverted the artefact in the panel: the artefact
			// note says where it stands.
			{text: "and now?", before: func(h *preserveHarness) {
				_, _ = h.conv.art.revert(1)
			}, replies: []openaichat.CompletionResponse{
				tool("b1", "artefact_read", `{"from_line":1,"to_line":2}`),
				answer("Back at the first draft."),
			}},
		}},
		{name: "pixels-on", apps: true, artefact: true, setup: func(h *preserveHarness) {
			h.conv.art.setPolicy(artPolicy{write: true})
			h.conv.art.store = testStore(h.t, roomy())
			shot := testPNG(h.t, 16, 12, 4)
			h.coord.captureHook = func(context.Context, []uint64, *[4]float32) ([]byte, string) { return shot, "" }
			h.coord.discloseHook = func(agent.Disclosure) error { return nil }
			h.coord.setPixels(pixelPolicy{level: agent.PixelsCaptures, localOnly: true, modelLocal: true, endpoint: "m · 127.0.0.1 · on this machine"})
		}, turns: []preserveTurn{
			{text: "look at the note", replies: []openaichat.CompletionResponse{
				tool("r1", "request_access", `{"plan":"look","open":[{"app":"notes"}]}`),
				tool("o1", "open_window", `{"app":"notes"}`),
				tool("c1", "artefact_capture", `{"base_revision":0,"windows":[100]}`),
				tool("v1", "artefact_view_image", `{"name":"screenshot-1.png"}`),
				tool("v2", "artefact_view_image", `{"name":"screenshot-1.png"}`),
				tool("l1", "list_windows", `{}`),
				answer("It shows a gradient."),
			}},
			// The next turn resends a placeholder; the setting lowered
			// mid-turn strips the pixels from the turn's own history.
			{text: "and now?", replies: []openaichat.CompletionResponse{
				tool("v3", "artefact_view_image", `{"name":"screenshot-1.png"}`),
				tool("l2", "list_windows", `{}`),
				tool("l3", "list_windows", `{}`),
				answer("Gone."),
			}, during: func(h *preserveHarness, round int) {
				if round == 2 {
					h.coord.setPixels(pixelPolicy{level: agent.PixelsNone})
				}
			}},
			// Told that it no longer sees pixels; the view tool is gone.
			{text: "anything else?", replies: []openaichat.CompletionResponse{answer("No.")}},
		}},
		{name: "pixels-off", artefact: true, setup: func(h *preserveHarness) {
			h.conv.art.setPolicy(artPolicy{write: false})
			h.coord.setPixels(pixelPolicy{level: agent.PixelsNone})
		}, turns: []preserveTurn{
			{text: "look", replies: []openaichat.CompletionResponse{
				tool("a1", "artefact_write", `{"base_revision":0,"text":"x"}`),
				tool("v1", "artefact_view_image", `{"name":"screenshot-1.png"}`),
				tool("a2", "artefact_images", `{}`),
				answer("Nothing to see."),
			}},
		}},
		// A consequential command under a task's grant (ADR-0311 §SD3, law
		// L15): the grant is journaled as the person's answer, the intent
		// names the task it runs under, and the call waits on the person —
		// under a test grant nobody confirms, so the wait ends with the
		// turn and the call is withdrawn; nothing completed, so L15 has
		// nothing to check here, and a completion under the person's
		// grant or dialog is covered by the law's own test.
		{name: "consent-held", apps: true, setup: func(h *preserveHarness) {
			// The settings let the model act outside the app.
			h.coord.setOptions(permissions{allow: allowOutside, changes: changesApply, launch: true, desktop: true, reach: agent.ReachData}.ceiling(true), false)
		}, turns: []preserveTurn{
			{text: "publish my note", replies: []openaichat.CompletionResponse{
				tool("c1", "request_access", `{"plan":"publish the note","open":[{"app":"notes"}],"destinations":["publish:report"]}`),
				tool("c2", "open_window", `{"app":"notes"}`),
				tool("p1", "call_operation", `{"window":100,"operation":"publish_note","args":{"name":"report-1"}}`),
				answer("Published."),
			}},
		}},
		// Compaction (ADR-0316) on a route at the profile's floor: a note
		// larger than the trigger is read in the second turn; the third
		// turn has no cut outside the kept turns and goes out over the
		// trigger, recorded; the fourth elides the result to a stub, and
		// the model reads it back with recall.
		{name: "compaction-recall", apps: true, setup: func(h *preserveHarness) {
			h.coord.setRoute("", true, compactContext)
		}, turns: []preserveTurn{
			{text: "open my note", replies: []openaichat.CompletionResponse{
				tool("c1", "request_access", `{"plan":"read the note","open":[{"app":"notes"}]}`),
				tool("c2", "open_window", `{"app":"notes"}`),
				answer("Opened."),
			}},
			{text: "read it", before: func(h *preserveHarness) {
				h.host.person(100, strings.Repeat("<row/>", 8000))
				time.Sleep(30 * time.Millisecond)
			}, replies: []openaichat.CompletionResponse{
				tool("c3", "call_operation", `{"window":100,"operation":"get_note","args":{}}`),
				answer("A long table."),
			}},
			{text: "and now?", replies: []openaichat.CompletionResponse{answer("Still here.")}},
			{text: "what was in it?", script: func(h *preserveHarness) []openaichat.CompletionResponse {
				var ordinal uint32
				for _, e := range h.coord.journalNow().Entries() {
					if e.Kind == agentrun.KindOutcome && len(e.Body) > 1000 {
						ordinal = e.Ordinal
					}
				}
				return []openaichat.CompletionResponse{
					tool("r1", "recall", `{"ordinal":`+strconv.FormatUint(uint64(ordinal), 10)+`}`),
					answer("Rows."),
				}
			}},
		}},
	}
}

// TestThePreservationGolden runs the corpus and compares the recording
// with the golden byte for byte.
func TestThePreservationGolden(t *testing.T) {
	for _, sc := range preserveCorpus() {
		t.Run(sc.name, func(t *testing.T) {
			got := runPreserveScenario(t, sc)
			path := filepath.Join(preservedDir, sc.name+".golden")
			if *updatePreserved {
				require.NoError(t, os.MkdirAll(preservedDir, 0o755))
				require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
				return
			}
			want, err := os.ReadFile(path)
			require.NoError(t, err, "no golden yet: run with -update-preserved")
			if string(want) != got {
				newPath := path + ".new"
				_ = os.WriteFile(newPath, []byte(got), 0o644)
				t.Fatalf("the recording differs from %s; wrote %s (diff with `diff -u`); rerun with -update-preserved once the change is named", path, newPath)
			}
		})
	}
}

// TestThePreservationRecordingIsDeterministic runs each scenario twice and
// asks for the same bytes: what the golden pins has to be a function of
// the corpus alone.
func TestThePreservationRecordingIsDeterministic(t *testing.T) {
	for _, sc := range preserveCorpus() {
		t.Run(sc.name, func(t *testing.T) {
			first := runPreserveScenario(t, sc)
			second := runPreserveScenario(t, sc)
			require.Equal(t, first, second)
		})
	}
}

func runPreserveScenario(t *testing.T, sc preserveScenario) (recording string) {
	t.Helper()
	bus := inprocbus.NewInst(zerolog.Nop())
	model := &scriptedModel{}
	host, coord, cli, _, ctx := coordRig(t, bus, model, sc.opTools)
	conv := newConversation()
	coord.conversation = conv.id
	h := &preserveHarness{t: t, host: host, coord: coord, conv: conv, model: model, ctx: ctx, cli: cli, known: map[string]string{}, anchors: anchorstore.NewMemStore()}
	coord.anchors = h.anchors
	conv.apps, conv.questions, conv.artefact = sc.apps, sc.questions, sc.artefact
	if sc.setup != nil {
		sc.setup(h)
	}
	for i, turn := range sc.turns {
		h.runTurn(i+1, turn)
	}
	h.checkL4()
	return numberTasks(h.out.String())
}

// checkL4 is the laws over the scenario's journal: the chain verifies
// (L31) and its heads against the anchor store (L32); the compactions
// cover, progress, bound and keep the person verbatim (L22-L25); every
// consequential act completed under consent (L15); and, where the engine
// is installed, the request the coordinator's fold projects equals the one
// the local worker projects, by digest (L4, ADR-0309 §SD3).
func (h *preserveHarness) checkL4() {
	w := h.coord.journalNow()
	if w == nil {
		return
	}
	entries := w.Entries()
	broken, err := agentrun.Verify(w.Run().Digest, entries)
	require.NoError(h.t, err)
	require.Zero(h.t, broken, "L31: the chain verifies")
	anchored, err := h.anchors.Read(context.Background(), w.Run().Cell, w.Run().Id)
	require.NoError(h.t, err)
	require.Empty(h.t, anchorstore.VerifyHeads(w.Run().Digest, entries, anchored), "L32: the heads verify against the store")
	require.Empty(h.t, projection.CheckCompaction(entries), "L22, L23, L25")
	if c := h.coord.compactConstants(headTokens(projection.Fold(entries, sectionOrder()))); c.TriggerTokens > 0 {
		require.Empty(h.t, projection.CheckBound(entries, sectionOrder(), c, lastReported), "L24")
	}
	require.Empty(h.t, checkEndorsed(entries), "L15")
	runner := l4Runner(h.t)
	if runner == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	fromWorker, err := projection.Project(ctx, runner, entries, sectionOrder())
	require.NoError(h.t, err)
	fromFold := projection.Fold(entries, sectionOrder())
	require.True(h.t, fromFold.Equal(fromWorker), "L4: the worker's projection differs from the fold's")
	d1, _ := fromFold.Digest("fp")
	d2, _ := fromWorker.Digest("fp")
	require.Equal(h.t, d1, d2)
}

// l4Runner is a local worker for L4, nil without the engine.
func l4Runner(t *testing.T) projection.RunnerI {
	t.Helper()
	if _, err := chlocalpool.LookupBinary(); err != nil {
		return nil
	}
	logger := zerolog.Nop()
	bus := inprocbus.NewInst(logger)
	bus.SetRequestTimeout(30 * time.Second)
	svc, err := chlocalbroker.NewService(bus, chlocalpool.Config{BaseTmpDir: t.TempDir(), MinIdle: 1, MaxConcurrent: 2, SpawnConcurrency: 1,
		MaxMemoryPerWorker: 256 << 20, SpawnTimeout: 10 * time.Second, WatchdogMaxLifetime: 120 * time.Second, KillGrace: 250 * time.Millisecond, StderrCapBytes: 4096}, logger)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = svc.Stop(ctx)
	})
	caller := bus.NewClient("test.caller", []app.SubjectFilter{{Pattern: chlocalbroker.SubjectExecAll, Direction: app.CapDirectionBoth, Reason: "test"}})
	return projection.BrokerRunner{Bus: caller, Pool: "l4"}
}

// taskId is a task's id as the host mints it.
var taskId = regexp.MustCompile(`task-[0-9a-f]{8,}`)

// numberTasks replaces each task id with its order of first appearance,
// the one thing in a recording the host mints afresh per run.
func numberTasks(s string) string {
	seen := map[string]string{}
	s = taskId.ReplaceAllStringFunc(s, func(id string) string {
		n, ok := seen[id]
		if !ok {
			n = "task-#" + strconv.Itoa(len(seen)+1)
			seen[id] = n
		}
		return n
	})
	// The run id a recalled band names (ADR-0311 §SD4) is minted per
	// conversation, and the call key an elision's stub names (ADR-0316
	// §SD2) per model call; numbered the same way.
	runs := map[string]string{}
	s = runId.ReplaceAllStringFunc(s, func(id string) string {
		n, ok := runs[id]
		if !ok {
			n = "run-#" + strconv.Itoa(len(runs)+1)
			runs[id] = n
		}
		return n
	})
	calls := map[string]string{}
	return callId.ReplaceAllStringFunc(s, func(id string) string {
		n, ok := calls[id]
		if !ok {
			n = "call-#" + strconv.Itoa(len(calls)+1)
			calls[id] = n
		}
		return n
	})
}

// runId is a conversation's id as newConversation mints it; callId a
// model call's as the llm service mints it.
var (
	runId  = regexp.MustCompile(`chat-[0-9a-z]+-[0-9a-z]+`)
	callId = regexp.MustCompile(`llm-[0-9a-z]+-[0-9]+`)
)

// runTurn mirrors App.startTurn and the landing of its result: the
// request from the conversation, the system prompt on the first send, the
// coordinator's per-turn resets, then runTurn and landTurn.
func (h *preserveHarness) runTurn(n int, turn preserveTurn) {
	conv, coord := h.conv, h.coord
	if turn.before != nil {
		turn.before(h)
	}
	replies := turn.replies
	if turn.script != nil {
		replies = turn.script(h)
	}
	h.model.mu.Lock()
	h.model.replies, h.model.fail = replies, turn.fail
	h.model.mu.Unlock()
	req := conv.request(turn.text)
	coord.steps.reset()
	coord.forgetRefusals()
	coord.offer(conv.apps, conv.questions)
	if conv.artefact {
		coord.offerArtefact(conv.art)
	}
	conv.coord = coord
	limit := clampRounds(0)
	coord.setRounds(limit)
	conv.begin(turn.text, int64(n)*1000, false)
	fmt.Fprintf(&h.out, "# turn %d: %q\n", n, turn.text)
	if turn.person != nil {
		go turn.person(h)
	}
	res, err := runTurn(h.ctx, h.cli, coord, req, func(round int, doing string) {
		if doing == "" {
			h.flush()
			if turn.during != nil {
				turn.during(h, round)
			}
		}
	})
	h.flush()
	conv.landTurn(req, res, err, int64(n)*1000+500)
	fmt.Fprintf(&h.out, "## turn %d ended\n", n)
	switch {
	case err != nil:
		fmt.Fprintf(&h.out, "error: %s\n", failureReason(err))
	case res.stopped != "":
		fmt.Fprintf(&h.out, "stopped: %s\n", res.stopped)
	default:
		fmt.Fprintf(&h.out, "answer: %q\n", res.final.Content)
	}
	if res != nil {
		for _, a := range res.activity {
			fmt.Fprintf(&h.out, "activity: %s\n", a)
		}
		fmt.Fprintf(&h.out, "calls: %d, omitTo: %d\n", len(res.calls), res.omitTo)
	}
	history, _ := projection.Encode(projection.Fold(coord.journalNow().Entries(), sectionOrder()), coord.resolveAttachment)
	fmt.Fprintf(&h.out, "history: %d messages, omitTo %d, parentSet %t\n%s", len(history), conv.omitTo, conv.parent != "", previewHistory(history))
}

// flush copies what the model and the host saw since the last flush: the
// rounds' wire requests and the operation calls, in order.
func (h *preserveHarness) flush() {
	h.model.mu.Lock()
	seen := h.model.seen[h.seenRounds:]
	h.seenRounds = len(h.model.seen)
	h.model.mu.Unlock()
	h.host.mu.Lock()
	ops := h.host.submitted[h.seenOps:]
	h.seenOps = len(h.host.submitted)
	h.host.mu.Unlock()
	for _, op := range ops {
		fmt.Fprintf(&h.out, "## op %s window=%d writer=%s args=%s\n", op.op, op.window, op.writer, op.args)
	}
	for _, r := range seen {
		body, err := openaichat.EncodeRequest(r)
		require.NoError(h.t, err)
		// The run id inside a recalled band is minted per conversation;
		// the digest is over the body with it numbered.
		body = []byte(numberTasks(string(body)))
		sum := sha256.Sum256(body)
		fmt.Fprintf(&h.out, "## request sha256=%x bytes=%d model=%s max_tokens=%d tool_choice=%q\n", sum, len(body), r.ModelId, r.MaxTokens, r.ToolChoice)
		tools, _ := json.Marshal(r.Tools, json.Deterministic(true))
		if ref, known := h.known["tools "+string(tools)]; known {
			fmt.Fprintf(&h.out, "tools: =%s\n", ref)
		} else {
			ref = "t" + strconv.Itoa(len(h.known)+1)
			h.known["tools "+string(tools)] = ref
			fmt.Fprintf(&h.out, "tools %s: %s\n", ref, tools)
		}
		for i, m := range r.Messages {
			line := messageLine(m)
			if ref, known := h.known[line]; known {
				fmt.Fprintf(&h.out, "%d =%s\n", i, ref)
				continue
			}
			ref := "m" + strconv.Itoa(len(h.known)+1)
			h.known[line] = ref
			fmt.Fprintf(&h.out, "%d %s: %s\n", i, ref, line)
		}
	}
}

// messageLine is one message in full: the role, the content, the tool
// calls, and each image by its digest. A message the recording printed
// already is cited by the reference it got.
func messageLine(m openaichat.Message) string {
	var b strings.Builder
	b.WriteString(m.Role.String())
	if m.ToolCallId != "" {
		b.WriteString(" answers=" + m.ToolCallId)
	}
	for _, img := range m.Images {
		sum := sha256.Sum256(img.Data)
		fmt.Fprintf(&b, " image=%s:%x", img.MediaType, sum[:8])
	}
	if len(m.ToolCalls) > 0 {
		tc, _ := json.Marshal(m.ToolCalls, json.Deterministic(true))
		b.WriteString(" tool_calls=" + string(tc))
	}
	b.WriteString(" " + strconv.Quote(m.Content))
	return b.String()
}

// waitAsk waits for the next question to the person.
func (h *preserveHarness) waitAsk() (o *openAsk) {
	require.Eventually(h.t, func() bool { o = h.coord.ask.current(); return o != nil && !o.form.sent }, 5*time.Second, time.Millisecond)
	return
}

// previewHistory is the history as a short outline, one message per line:
// the role, the images, and the content's head.
func previewHistory(ms []openaichat.Message) string {
	var b strings.Builder
	for i, m := range ms {
		c := strings.Join(strings.Fields(m.Content), " ")
		if r := []rune(c); len(r) > 80 {
			c = string(r[:80]) + "…"
		}
		fmt.Fprintf(&b, "  %d %s", i, m.Role.String())
		if len(m.Images) > 0 {
			fmt.Fprintf(&b, " images=%d", len(m.Images))
		}
		if len(m.ToolCalls) > 0 {
			fmt.Fprintf(&b, " tool_calls=%d", len(m.ToolCalls))
		}
		if m.ToolCallId != "" {
			fmt.Fprintf(&b, " answers=%s", m.ToolCallId)
		}
		fmt.Fprintf(&b, " %q\n", c)
	}
	return b.String()
}
