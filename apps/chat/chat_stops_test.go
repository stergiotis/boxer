package chat

import (
	"context"
	"encoding/json/v2"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/projection"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// Every kind is a spelling of a statement the vetting admits (ADR-0370 §SD2,
// §SD7): a kind whose statement the parser or the classifier refused could
// not be declared at all.
func TestEveryKindCompilesToAnAdmittedStatement(t *testing.T) {
	for _, spec := range []Stop{
		{Name: "time-box", Then: "ask", After: "15m"},
		{Name: "size", Then: "stop", ArtefactBytes: 200000},
		{Name: "done", Then: "end", ArtefactMatch: `(?m)^## Conclusion`},
		{Name: "said", Then: "pause", JournalMatch: `it's \bDONE\b`},
		{Name: "said-user", Then: "pause", JournalMatch: `DONE`, In: []string{"user", "reply"}},
		{Name: "ddl", Then: "stop", ToolMatch: `^(set_sql|query)$`, ArgumentsMatch: `(?i)\b(drop|alter)\b`},
		{Name: "any-tool", Then: "close", ToolMatch: `^close_window$`},
		{Name: "spend", Then: "ask", SQL: "WITH t AS (SELECT turn FROM clock) SELECT turn, count() AS replies FROM journal WHERE kind = 'reply' AND turn IN (SELECT turn FROM t) GROUP BY turn HAVING replies >= 10;"},
	} {
		s, err := spec.compile(stopOriginPerson)
		require.NoError(t, err, spec.Name)
		assert.NotEmpty(t, s.sql, spec.Name)
		assert.NotEmpty(t, s.what, spec.Name)
		assert.False(t, strings.HasSuffix(s.sql, ";"), spec.Name)
	}
}

func TestADeclarationIsRefused(t *testing.T) {
	for name, c := range map[string]struct {
		spec Stop
		want string
	}{
		"no kind":           {Stop{Name: "a", Then: "ask"}, "exactly one of"},
		"two kinds":         {Stop{Name: "a", Then: "ask", After: "1m", ArtefactBytes: 10}, "exactly one of"},
		"unknown action":    {Stop{Name: "a", Then: "halt", After: "1m"}, "then must be"},
		"a name with space": {Stop{Name: "time box", Then: "ask", After: "1m"}, "a name is"},
		"no name":           {Stop{Then: "ask", After: "1m"}, "a name is"},
		"a short time box":  {Stop{Name: "a", Then: "ask", After: "10ms"}, "after is a duration"},
		"not a duration":    {Stop{Name: "a", Then: "ask", After: "soon"}, "after is a duration"},
		"a bad pattern":     {Stop{Name: "a", Then: "ask", ArtefactMatch: "("}, "does not compile"},
		"in without match":  {Stop{Name: "a", Then: "ask", After: "1m", In: []string{"reply"}}, "in belongs to"},
		"an unknown kind":   {Stop{Name: "a", Then: "ask", JournalMatch: "x", In: []string{"section"}}, "the kinds are"},
		"arguments alone":   {Stop{Name: "a", Then: "ask", After: "1m", ArgumentsMatch: "x"}, "arguments_match belongs to"},
		"a file":            {Stop{Name: "a", Then: "ask", SQL: "SELECT * FROM file('/etc/passwd', 'LineAsString')"}, "must only read"},
		"a url":             {Stop{Name: "a", Then: "ask", SQL: "SELECT * FROM url('http://example.com/x', 'CSV')"}, "must only read"},
		"a fifth table":     {Stop{Name: "a", Then: "ask", SQL: "SELECT name FROM secrets"}, "reads secrets"},
		"a system table":    {Stop{Name: "a", Then: "ask", SQL: "SELECT name FROM system.tables"}, "reads system.tables"},
		"two statements":    {Stop{Name: "a", Then: "ask", SQL: "SELECT 1 FROM clock; SELECT 2 FROM clock"}, "one statement"},
		"an insert":         {Stop{Name: "a", Then: "ask", SQL: "INSERT INTO journal SELECT * FROM journal"}, "must only read"},
		"not sql":           {Stop{Name: "a", Then: "ask", SQL: "DROP TABLE journal"}, "does not parse"},
		"the wall clock":    {Stop{Name: "a", Then: "ask", SQL: "SELECT now() AS t FROM clock"}, "differs at every check"},
		"a settings change": {Stop{Name: "a", Then: "ask", SQL: "SET max_threads = 1; SELECT 1 FROM clock"}, "one statement"},
		"a settings clause": {Stop{Name: "a", Then: "ask", SQL: "SELECT turn FROM clock SETTINGS max_threads = 1"}, "no SETTINGS clause"},
		"an output format":  {Stop{Name: "a", Then: "ask", SQL: "SELECT turn FROM clock FORMAT CSV"}, "no FORMAT clause"},
		"an outfile":        {Stop{Name: "a", Then: "ask", SQL: "SELECT turn FROM clock INTO OUTFILE '/tmp/x'"}, ""},
	} {
		_, err := c.spec.compile(stopOriginPerson)
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), c.want, name)
	}
	_, err := compileStops([]Stop{{Name: "a", Then: "ask", After: "1m"}, {Name: "a", Then: "stop", ArtefactBytes: 9}}, stopOriginPerson)
	require.ErrorContains(t, err, "share a name")
	many := make([]Stop, maxStops+1)
	for i := range many {
		many[i] = Stop{Name: "s" + string(rune('a'+i)), Then: "ask", After: "1m"}
	}
	_, err = compileStops(many, stopOriginPerson)
	require.ErrorContains(t, err, "too many stops")
}

func firedEntry(t *testing.T, ord uint32, key string, form string, f stopFiring) agentrun.Entry {
	t.Helper()
	body, err := json.Marshal(f)
	require.NoError(t, err)
	return agentrun.Entry{Ordinal: ord, Kind: agentrun.KindIgnorable, Name: stopEntryName, Form: form, Key: key, Body: string(body)}
}

// What the person acknowledged is a fold of the journal (ADR-0370 §SD3):
// Continue acknowledges the firings of its check, a message of theirs every
// firing before it, and a declined hold nothing until they speak.
func TestAcknowledgementIsAFoldOfTheJournal(t *testing.T) {
	entries := []agentrun.Entry{
		{Ordinal: 1, Kind: agentrun.KindUser},
		firedEntry(t, 2, "k1", stopFormFired, stopFiring{Stop: "box", Rows: []string{"r1"}}),
		firedEntry(t, 3, "k1", stopFormFired, stopFiring{Stop: "size", Rows: []string{"s1"}}),
		{Ordinal: 4, Kind: agentrun.KindIgnorable, Name: stopEntryName, Form: stopFormContinue, Key: "k1"},
		firedEntry(t, 5, "k2", stopFormFired, stopFiring{Stop: "box", Rows: []string{"r1", "r2"}}),
		{Ordinal: 6, Kind: agentrun.KindIgnorable, Name: stopEntryName, Form: stopFormDeclined, Key: "k2"},
	}
	acked := stopAcks(entries)
	assert.Contains(t, acked["box"], "r1")
	assert.NotContains(t, acked["box"], "r2", "a declined hold acknowledges nothing")
	assert.Contains(t, acked["size"], "s1")

	entries = append(entries, agentrun.Entry{Ordinal: 7, Kind: agentrun.KindUser},
		firedEntry(t, 8, "k3", stopFormNoted, stopFiring{Stop: "size", Rows: []string{"s2"}}))
	acked = stopAcks(entries)
	assert.Contains(t, acked["box"], "r2", "the person's next message acknowledges what stopped the turn")
	assert.NotContains(t, acked["size"], "s2")

	events := stopEventsOf(entries)
	require.Len(t, events, 4)
	assert.Equal(t, stopFormContinue, events[0].answer)
	assert.Equal(t, stopFormDeclined, events[2].answer)
	assert.True(t, events[3].noted)
}

// stopWorker is a broker over one local worker pool, skipped where the
// engine is absent.
func stopWorker(t *testing.T) stopRunnerI {
	t.Helper()
	if _, err := chlocalpool.LookupBinary(); err != nil {
		t.Skipf("clickhouse not installed: %v", err)
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
	// The chat's own capability is all the caller holds.
	caller := bus.NewClient("test.chat", StopsCaps())
	return projection.BrokerRunner{Bus: caller, Pool: stopsPool}
}

func mustStops(t *testing.T, specs ...Stop) stopSet {
	t.Helper()
	set, err := compileStops(specs, stopOriginPerson)
	require.NoError(t, err)
	return set
}

func resultOf(t *testing.T, results []stopResult, name string) stopResult {
	t.Helper()
	for _, r := range results {
		if r.name == name {
			return r
		}
	}
	t.Fatalf("no result for %s", name)
	return stopResult{}
}

func rowTexts(r stopResult) (texts []string) {
	for _, row := range r.rows {
		texts = append(texts, row.text)
	}
	return
}

// The kinds against the engine: each returns the rows its spelling
// promises, and none when its condition does not hold.
func TestTheKindsInTheWorker(t *testing.T) {
	runner := stopWorker(t)
	calls := projection.CallsBody([]openaichat.ToolCall{{Id: "c1", Name: "query", Arguments: `{"sql":"DROP TABLE t"}`}, {Id: "c2", Name: "get_state", Arguments: `{}`}})
	in := stopInputs{
		entries: []agentrun.Entry{
			{Ordinal: 1, Kind: agentrun.KindUser, Turn: "t1", Body: "say DONE when you are"},
			{Ordinal: 2, Kind: agentrun.KindReply, Turn: "t1", Body: "working", Calls: calls},
			{Ordinal: 3, Kind: agentrun.KindOutcome, Turn: "t1", Body: "ok, all DONE here"},
			{Ordinal: 4, Kind: agentrun.KindReply, Turn: "t0", Body: "DONE in an earlier turn"},
		},
		artefact: "# Report\n\n## Conclusion\nit holds\n\n## Conclusion two\n", revision: 3,
		turn: "t1", round: 2, working: 31 * time.Minute, elapsed: 40 * time.Minute, now: time.UnixMilli(1_700_000_000_000),
	}
	set := mustStops(t,
		Stop{Name: "box", Then: "ask", After: "15m"},
		Stop{Name: "long-box", Then: "ask", After: "2h"},
		Stop{Name: "size", Then: "stop", ArtefactBytes: 20},
		Stop{Name: "big", Then: "stop", ArtefactBytes: 100000},
		Stop{Name: "conclusion", Then: "end", ArtefactMatch: `(?m)^## Conclusion[^\n]*`},
		Stop{Name: "said", Then: "pause", JournalMatch: `DONE`},
		Stop{Name: "said-user", Then: "pause", JournalMatch: `DONE`, In: []string{"user"}},
		Stop{Name: "ddl", Then: "stop", ToolMatch: `^query$`, ArgumentsMatch: `(?i)\bdrop\b`},
		Stop{Name: "no-such-tool", Then: "stop", ToolMatch: `^publish$`},
		Stop{Name: "calls", Then: "ask", SQL: "SELECT turn, count() AS n FROM toolcalls GROUP BY turn HAVING n >= 2"},
	)
	results := evalStops(context.Background(), runner, set, in)
	require.Len(t, results, len(set))
	for _, r := range results {
		require.Empty(t, r.err, r.name)
	}
	assert.Equal(t, []string{`{"turn":"t1","periods":2}`}, rowTexts(resultOf(t, results, "box")))
	assert.Empty(t, resultOf(t, results, "long-box").rows)
	assert.Equal(t, []string{`{"steps":2}`}, rowTexts(resultOf(t, results, "size")))
	assert.Empty(t, resultOf(t, results, "big").rows)
	assert.ElementsMatch(t, []string{`{"n":1,"match":"## Conclusion"}`, `{"n":2,"match":"## Conclusion two"}`}, rowTexts(resultOf(t, results, "conclusion")))
	assert.Equal(t, []string{`{"ordinal":3,"kind":"outcome","match":"DONE"}`}, rowTexts(resultOf(t, results, "said")),
		"the turn's replies and results; not the person's message, not an earlier turn")
	assert.Equal(t, []string{`{"ordinal":1,"kind":"user","match":"DONE"}`}, rowTexts(resultOf(t, results, "said-user")))
	assert.Equal(t, []string{`{"ordinal":2,"position":0,"name":"query","arguments":"{\"sql\":\"DROP TABLE t\"}"}`}, rowTexts(resultOf(t, results, "ddl")))
	assert.Empty(t, resultOf(t, results, "no-such-tool").rows)
	assert.Equal(t, []string{`{"turn":"t1","n":2}`}, rowTexts(resultOf(t, results, "calls")))

	// An empty run and no artefact: every kind is quiet, none fails.
	results = evalStops(context.Background(), runner, set, stopInputs{turn: "t1", now: time.UnixMilli(1)})
	for _, r := range results {
		assert.Empty(t, r.err, r.name)
		assert.Empty(t, r.rows, r.name)
	}
}

// A statement that fails in the engine fails alone: the error lands on the
// stop that caused it, and the others keep their rows.
func TestAFailingStopFailsAlone(t *testing.T) {
	runner := stopWorker(t)
	set := mustStops(t,
		Stop{Name: "box", Then: "ask", After: "1s"},
		Stop{Name: "broken", Then: "pause", SQL: "SELECT no_such_column FROM clock"},
		Stop{Name: "flood", Then: "pause", SQL: "SELECT number FROM (SELECT arrayJoin(range(1000)) AS number FROM clock)"},
	)
	results := evalStops(context.Background(), runner, set, stopInputs{turn: "t1", working: time.Minute, now: time.UnixMilli(1)})
	assert.Len(t, resultOf(t, results, "box").rows, 1)
	assert.Contains(t, resultOf(t, results, "broken").err, "no_such_column")
	assert.Contains(t, resultOf(t, results, "flood").err, "more than")
}

// scriptedStops answers each check with the next canned result: rows by
// stop, or an error.
type scriptedStops struct {
	mu     sync.Mutex
	checks []map[string][]string
	fail   error
	seen   int
}

func (inst *scriptedStops) Run(_ context.Context, _ string, _ map[string][]byte) (out []byte, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	inst.seen++
	if inst.fail != nil {
		return nil, inst.fail
	}
	if len(inst.checks) == 0 {
		return
	}
	rows := inst.checks[0]
	inst.checks = inst.checks[1:]
	var b strings.Builder
	for name, texts := range rows {
		for _, text := range texts {
			line, _ := json.Marshal(map[string]string{"stop": name, "row": text})
			b.Write(line)
			b.WriteByte('\n')
		}
	}
	return []byte(b.String()), nil
}

func stopForms(coord *coordinator) (forms []string) {
	for _, e := range coord.journalNow().Entries() {
		if e.Kind == agentrun.KindIgnorable && (e.Name == stopEntryName || e.Name == stopsEntryName) {
			forms = append(forms, e.Name+"/"+e.Form)
		}
	}
	return
}

// A held turn goes on from where it was when the person says so, and the
// rows they acknowledged do not fire again (ADR-0370 §SD3, §SD4).
func TestAStopHoldsTheTurnAndContinueGoesOn(t *testing.T) {
	model := &scriptedModel{replies: []openaichat.CompletionResponse{
		toolCall("a1", "no_such_tool", `{}`),
		{Content: "Here is the report.", FinishReason: "stop"},
	}}
	coord, cli, req := questionsRig(t, model)
	// Checks: before request 1 (quiet), after reply 1 (fires), before request
	// 2 (the same row: acknowledged), after the answer (the same row).
	row := `{"turn":"t","periods":1}`
	runner := &scriptedStops{checks: []map[string][]string{{}, {"box": {row}}, {"box": {row}}, {"box": {row}}}}
	coord.setStopRunner(runner)
	coord.setStops(mustStops(t, Stop{Name: "box", Then: "pause", After: "15m"}))
	go func() {
		o := waitAsk(t, coord)
		assert.Contains(t, o.questions[0].Question, `The stop "box" fired`)
		assert.Contains(t, o.questions[0].Question, row)
		assert.Equal(t, stagePerson, coord.stageNow())
		o.form.single[0], o.form.chosen[0][0] = 0, true
		o.submit()
	}()
	res, err := runTurn(context.Background(), cli, coord, req, nil)
	require.NoError(t, err)
	assert.Empty(t, res.stopped)
	assert.Equal(t, "Here is the report.", res.final.Content)
	assert.Equal(t, stopActionNone, res.ended)
	assert.Equal(t, 4, runner.seen)
	assert.Equal(t, []string{"stops/set", "stop/fired", "stop/continue"}, stopForms(coord))
	events := coord.stopEvents()
	require.Len(t, events, 1)
	assert.Equal(t, stopFormContinue, events[0].answer)
	assert.Equal(t, stopPointReply, events[0].firing.Point)
}

// A hold the person declines, or skips, ends the turn as stopped; the
// reply's tool calls are not made.
func TestADeclinedHoldStopsTheTurn(t *testing.T) {
	for _, skip := range []bool{false, true} {
		model := &scriptedModel{replies: []openaichat.CompletionResponse{toolCall("a1", "ask_user", twoQuestions)}}
		coord, cli, req := questionsRig(t, model)
		coord.setStopRunner(&scriptedStops{checks: []map[string][]string{{}, {"tools": {`{"ordinal":4}`}}}})
		coord.setStops(mustStops(t, Stop{Name: "tools", Then: "ask", ToolMatch: "^ask_user$"}))
		go func() {
			o := waitAsk(t, coord)
			if skip {
				o.skip()
				return
			}
			o.form.single[0], o.form.chosen[0][1] = 1, true
			o.submit()
		}()
		res, err := runTurn(context.Background(), cli, coord, req, nil)
		require.NoError(t, err)
		assert.Contains(t, res.stopped, `the stop "tools" fired`)
		assert.Contains(t, res.stopped, "you ended the turn")
		assert.Empty(t, res.activity, "the call the stop was about did not run")
		assert.Equal(t, stopActionNone, res.ended)
		assert.Equal(t, []string{"stops/set", "stop/fired", "stop/declined"}, stopForms(coord))
		assert.Len(t, model.seen, 1)
	}
}

// stop ends the turn where it is, before a model call when the check before
// the request fires; end and close end the conversation with it.
func TestStopEndAndCloseEndTheTurn(t *testing.T) {
	for then, ended := range map[string]stopActionE{"stop": stopActionNone, "end": stopActionEnd, "close": stopActionClose} {
		model := &scriptedModel{}
		coord, cli, req := questionsRig(t, model)
		coord.setStopRunner(&scriptedStops{checks: []map[string][]string{{"size": {`{"steps":1}`}}}})
		coord.setStops(mustStops(t, Stop{Name: "size", Then: then, ArtefactBytes: 10}))
		res, err := runTurn(context.Background(), cli, coord, req, nil)
		require.NoError(t, err, then)
		assert.Contains(t, res.stopped, `the stop "size" fired (each 10 bytes the artefact reaches): {"steps":1}`, then)
		assert.Equal(t, ended, res.ended, then)
		assert.Empty(t, model.seen, "no model call was made: "+then)
		assert.True(t, coord.wakesHeld, then)
	}
}

// With the turn's answer there is nothing left to hold or stop: a weaker
// stop is noted, and one that ends the conversation ends it on an answered
// turn.
func TestAtTheAnswerOnlyAnEndingStopActs(t *testing.T) {
	model := &scriptedModel{replies: []openaichat.CompletionResponse{{Content: "All DONE.", FinishReason: "stop"}}}
	coord, cli, req := questionsRig(t, model)
	coord.setStopRunner(&scriptedStops{checks: []map[string][]string{{}, {"said": {`{"ordinal":3}`}, "fin": {`{"n":1}`}}}})
	coord.setStops(mustStops(t, Stop{Name: "said", Then: "ask", JournalMatch: "DONE"}, Stop{Name: "fin", Then: "end", ArtefactMatch: "fin"}))
	res, err := runTurn(context.Background(), cli, coord, req, nil)
	require.NoError(t, err)
	assert.Empty(t, res.stopped, "the turn was answered")
	assert.Equal(t, "All DONE.", res.final.Content)
	assert.Equal(t, stopActionEnd, res.ended)
	assert.Contains(t, res.endedWhy, `the stop "fin" fired`)
	assert.ElementsMatch(t, []string{"stops/set", "stop/fired", "stop/noted"}, stopForms(coord))
}

// A stop that cannot be checked stops the turn: with no worker, with a
// worker that fails, and whatever its own action (ADR-0370 §SD4).
func TestAnUnevaluableStopStopsTheTurn(t *testing.T) {
	for name, runner := range map[string]stopRunnerI{"no worker": nil, "a failing worker": &scriptedStops{fail: errors.New("the pool is gone")}} {
		model := &scriptedModel{}
		coord, cli, req := questionsRig(t, model)
		if runner != nil {
			coord.setStopRunner(runner)
		}
		coord.setStops(mustStops(t, Stop{Name: "box", Then: "close", After: "15m"}))
		res, err := runTurn(context.Background(), cli, coord, req, nil)
		require.NoError(t, err, name)
		assert.Contains(t, res.stopped, `the stop "box" could not be checked`, name)
		assert.Equal(t, stopActionNone, res.ended, "an error stops; it does not close: "+name)
		assert.Empty(t, model.seen, name)
	}
}

// A conversation without stops makes no worker call and writes no entry.
func TestWithoutStopsNothingIsCheckedOrWritten(t *testing.T) {
	model := &scriptedModel{replies: []openaichat.CompletionResponse{{Content: "hello", FinishReason: "stop"}}}
	coord, cli, req := questionsRig(t, model)
	runner := &scriptedStops{}
	coord.setStopRunner(runner)
	res, err := runTurn(context.Background(), cli, coord, req, nil)
	require.NoError(t, err)
	assert.Equal(t, "hello", res.final.Content)
	assert.Zero(t, runner.seen)
	assert.Empty(t, stopForms(coord))
}

// The capability a window declares for its stops reaches the one pool.
func TestStopsCapsNameThePool(t *testing.T) {
	caps := StopsCaps()
	require.Len(t, caps, 1)
	assert.Equal(t, chlocalbroker.SubjectExecPrefix+stopsPool, caps[0].Pattern)
	assert.Equal(t, app.CapDirectionPub, caps[0].Direction)
}

// The help page's statements are stops: each is admitted, and each runs in
// the engine over an empty run. An example that rots turns this red.
func TestTheHelpPagesStatementsAreAdmittedAndRun(t *testing.T) {
	src, err := os.ReadFile("help/stops.md")
	require.NoError(t, err)
	var specs []Stop
	for i, part := range strings.Split(string(src), "```sql\n")[1:] {
		sql, _, closed := strings.Cut(part, "```")
		require.True(t, closed)
		specs = append(specs, Stop{Name: "help-" + string(rune('a'+i)), Then: "ask", SQL: sql})
	}
	require.NotEmpty(t, specs)
	set := mustStops(t, specs...)
	for _, r := range evalStops(context.Background(), stopWorker(t), set, stopInputs{turn: "t1", now: time.UnixMilli(1)}) {
		assert.Empty(t, r.err, r.name)
		assert.Empty(t, r.rows, r.name)
	}
}
