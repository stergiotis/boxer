package moderator

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
)

var (
	t0   = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	chat = Window{App: "chat", Instance: 7}
)

func done(key string, at time.Time, digest string) ActionObs {
	return ActionObs{At: at, Key: key, Task: "task-1", Window: chat, Instance: 9, Operation: "set_text",
		ArgsDigest: digest, Result: opwire.ResultDone}
}

func kinds(signals []Signal) (out []SignalE) {
	for _, s := range signals {
		out = append(out, s.Kind)
	}
	return
}

// The same completed call three times raises repeat; different arguments,
// a duplicate record of one call, or calls spread past the window do not.
func TestRepeat(t *testing.T) {
	e := NewEngine(DefaultThresholds())
	s, _ := e.Action(done("k1", t0, "d1"))
	assert.Empty(t, s)
	s, _ = e.Action(done("k1", t0.Add(time.Second), "d1"))
	assert.Empty(t, s, "a call's final record is not a second call")
	s, _ = e.Action(done("k2", t0.Add(2*time.Second), "d2"))
	assert.Empty(t, s, "other arguments")
	s, _ = e.Action(done("k3", t0.Add(3*time.Second), "d1"))
	assert.Empty(t, s)
	s, _ = e.Action(done("k4", t0.Add(4*time.Second), "d1"))
	require.Equal(t, []SignalE{SignalRepeat}, kinds(s))
	assert.Equal(t, "task-1", s[0].Task)
	assert.Equal(t, chat, s[0].Window)
	assert.Contains(t, s[0].Detail, "set_text on window 9")

	e = NewEngine(DefaultThresholds())
	for i := range 3 {
		s, _ = e.Action(done("s"+strconv.Itoa(i), t0.Add(time.Duration(i)*6*time.Minute), "d1"))
		assert.Empty(t, s, "spread past the window")
	}
}

// A turn reaching the round raises rounds once.
func TestRounds(t *testing.T) {
	e := NewEngine(DefaultThresholds())
	var all []Signal
	for r := range uint32(20) {
		s, _ := e.Call(CallObs{At: t0, CallId: "c", Window: chat, Conversation: "c1", Turn: "t1", Round: r})
		all = append(all, s...)
	}
	require.Equal(t, []SignalE{SignalRounds}, kinds(all))
	assert.Contains(t, all[0].Detail, "round 16")
}

// Input growing each round up to the share of the context raises context;
// without the context size, it cannot.
func TestContext(t *testing.T) {
	cfg := DefaultThresholds()
	cfg.ContextTokens = 1000
	e := NewEngine(cfg)
	var all []Signal
	for i, in := range []int64{500, 700, 850, 900} {
		s, _ := e.Call(CallObs{At: t0, Window: chat, Conversation: "c1", Turn: "t1", Round: uint32(i), InputTokens: in})
		all = append(all, s...)
	}
	assert.Equal(t, []SignalE{SignalContext}, kinds(all), "raised at 850, once")

	e = NewEngine(DefaultThresholds())
	for i, in := range []int64{500, 700, 850, 900} {
		s, _ := e.Call(CallObs{At: t0, Window: chat, Conversation: "c1", Turn: "t1", Round: uint32(i), InputTokens: in})
		assert.Empty(t, s)
	}
}

// Refusals and failures in a row raise errors, across model calls and
// actions; a success breaks the streak, and the moderator's own refusals
// do not count.
func TestErrors(t *testing.T) {
	e := NewEngine(DefaultThresholds())
	var all []Signal
	feed := func(s []Signal, _ []Action) { all = append(all, s...) }
	feed(e.Call(CallObs{At: t0, Window: chat, Failed: true}))
	feed(e.Call(CallObs{At: t0, Window: chat, Refused: true, Rule: "budget"}))
	feed(e.Action(ActionObs{At: t0, Key: "a", Window: chat, Result: opwire.ResultNotDone}))
	feed(e.Call(CallObs{At: t0, Window: chat}))
	assert.Empty(t, all, "the success broke the streak")
	for i := range 4 {
		feed(e.Call(CallObs{At: t0, Window: chat, Refused: true, Rule: RulePrefix + "slow/7"}))
		feed(e.Action(ActionObs{At: t0, Key: "b" + strconv.Itoa(i), Window: chat, Result: opwire.ResultNotDone}))
	}
	assert.Empty(t, all, "four failures; the moderator's own refusals do not count")
	feed(e.Call(CallObs{At: t0, Window: chat, Failed: true}))
	assert.Equal(t, []SignalE{SignalErrors}, kinds(all))
}

// Long waits in a row raise queue, which never escalates.
func TestQueueDoesNotEscalate(t *testing.T) {
	e := NewEngine(DefaultThresholds())
	var all []Signal
	var acts []Action
	for range 9 {
		s, a := e.Call(CallObs{At: t0, Window: chat, Queued: time.Minute})
		all, acts = append(all, s...), append(acts, a...)
	}
	assert.Equal(t, []SignalE{SignalQueue, SignalQueue, SignalQueue}, kinds(all))
	assert.Empty(t, acts)
	assert.Equal(t, LevelNone, e.States()[0].Level)
}

// Each escalating signal moves the window one step: note, slow, stop, and
// stop again; quiet windows step down one level per Quiet.
func TestLadder(t *testing.T) {
	e := NewEngine(DefaultThresholds())
	at := t0
	signal := func() (Signal, []Action) {
		var s []Signal
		var a []Action
		for i := range 3 {
			at = at.Add(time.Second)
			s, a = e.Action(done("k"+at.String()+strconv.Itoa(i), at, "d"))
		}
		require.Len(t, s, 1)
		return s[0], a
	}
	s, a := signal()
	assert.Equal(t, LevelNoted, s.Level)
	assert.Empty(t, a)
	s, a = signal()
	assert.Equal(t, LevelSlowed, s.Level)
	require.Len(t, a, 1)
	assert.Equal(t, ActionKindSlow, a[0].Kind)
	s, a = signal()
	assert.Equal(t, LevelStopped, s.Level)
	assert.Equal(t, ActionKindStop, a[0].Kind)
	_, a = signal()
	assert.Equal(t, ActionKindStop, a[0].Kind, "stopped stays stopped and stops again")

	assert.Empty(t, e.Tick(at.Add(5*time.Minute)), "not quiet long enough")
	steps := e.Tick(at.Add(10 * time.Minute))
	require.Equal(t, []Step{{Window: chat, Level: LevelSlowed}}, steps)
	assert.Empty(t, e.Tick(at.Add(15*time.Minute)), "one level per quiet period")
	assert.Equal(t, []Step{{Window: chat, Level: LevelNoted}}, e.Tick(at.Add(20*time.Minute)))
	assert.Equal(t, []Step{{Window: chat, Level: LevelNone}}, e.Tick(at.Add(30*time.Minute)))
	e.Tick(at.Add(3 * time.Hour))
	assert.Empty(t, e.States(), "a quiet window with nothing left to count is forgotten")
}

// A call no window made is not watched.
func TestNoWindowIsIgnored(t *testing.T) {
	e := NewEngine(DefaultThresholds())
	for range 10 {
		s, _ := e.Call(CallObs{At: t0, Failed: true})
		assert.Empty(t, s)
	}
	assert.Empty(t, e.States())
}
