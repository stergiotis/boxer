package moderator

import (
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
)

const wireVersion uint8 = 1

// wireSignal is the payload on moderator.event.signal.
type wireSignal struct {
	V        uint8    `json:"v"`
	Kind     string   `json:"kind"`
	App      string   `json:"app"`
	Instance uint64   `json:"instance"`
	Task     string   `json:"task,omitempty"`
	Level    string   `json:"level"`
	Detail   string   `json:"detail,omitempty"`
	Evidence []string `json:"evidence,omitempty"`
	AtUnixMs int64    `json:"at_unix_ms"`
}

func wireOfSignal(s Signal) (w wireSignal) {
	return wireSignal{V: wireVersion, Kind: s.Kind.String(), App: s.Window.App, Instance: s.Window.Instance, Task: s.Task,
		Level: s.Level.String(), Detail: s.Detail, Evidence: s.Evidence, AtUnixMs: s.At.UnixMilli()}
}

// SignalEvent is a moderator.event.signal as a reader sees it.
type SignalEvent struct {
	Kind     string
	Window   Window
	Task     string
	Level    string
	Detail   string
	Evidence []string
	At       time.Time
}

// DecodeSignalEvent reads a moderator.event.signal payload.
func DecodeSignalEvent(payload []byte) (e SignalEvent, err error) {
	w, err := buscodec.Decode[wireSignal](payload)
	if err != nil {
		return
	}
	e = SignalEvent{Kind: w.Kind, Window: Window{App: w.App, Instance: w.Instance}, Task: w.Task, Level: w.Level,
		Detail: w.Detail, Evidence: w.Evidence, At: time.UnixMilli(w.AtUnixMs).UTC()}
	return
}

// StatesI is the read side keelson('moderator_signals') needs.
type StatesI interface {
	States() []State
}

// RegisterIntrospect registers keelson('moderator_signals') over states.
func RegisterIntrospect(reg *introspect.Registry, states StatesI) (err error) {
	return reg.Register(signalsProvider{states: states})
}

type signalsProvider struct{ states StatesI }

func (signalsProvider) Name() string                         { return TableSignals }
func (signalsProvider) Freshness() introspect.FreshnessClass { return introspect.FreshnessLive }
func (signalsProvider) Schema() *arrow.Schema                { return signalsTable(nil).Schema() }

func (p signalsProvider) Snapshot(proj introspect.Projection) (rec arrow.RecordBatch, err error) {
	var rows []State
	if p.states != nil {
		rows = p.states.States()
	}
	rec = signalsTable(rows).Build(proj, len(rows))
	return
}

func signalsTable(rows []State) (t *introspect.Table) {
	count := func(k SignalE) func(i int) int64 { return func(i int) int64 { return int64(rows[i].Counts[k]) } }
	at := func(t time.Time) (s string) {
		if !t.IsZero() {
			s = t.UTC().Format(time.RFC3339Nano)
		}
		return
	}
	return introspect.NewTable().
		String("app_id", func(i int) string { return rows[i].Window.App }).
		Uint64("instance_key", func(i int) uint64 { return rows[i].Window.Instance }).
		String("level", func(i int) string { return rows[i].Level.String() }).
		String("last_signal", func(i int) string {
			if rows[i].LastSignal == SignalNone {
				return ""
			}
			return rows[i].LastSignal.String()
		}).
		String("last_at", func(i int) string { return at(rows[i].LastAt) }).
		Int64("repeat", count(SignalRepeat)).
		Int64("rounds", count(SignalRounds)).
		Int64("context", count(SignalContext)).
		Int64("errors", count(SignalErrors)).
		Int64("queue", count(SignalQueue)).
		StringList("tasks", func(i int) []string { return rows[i].Tasks }).
		String("last_action", func(i int) string {
			if rows[i].LastAction == ActionKindNone {
				return ""
			}
			return rows[i].LastAction.String()
		}).
		String("last_action_at", func(i int) string { return at(rows[i].LastActAt) })
}
