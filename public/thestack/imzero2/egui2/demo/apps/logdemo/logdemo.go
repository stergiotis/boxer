package logdemo

import (
	"fmt"
	"sync/atomic"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// levelTone mirrors the logviewer's per-level palette so the emit
// chips here read as the same status family the operator sees in the
// viewer table. Keeping a small private copy avoids dragging the
// runtime/logviewer import into the demo (a circular-ish dependency
// since logviewer reads what logdemo writes).
func levelTone(lvl zerolog.Level) (tone badge.ToneE) {
	switch lvl {
	case zerolog.InfoLevel:
		tone = badge.ToneInfo
	case zerolog.WarnLevel:
		tone = badge.ToneWarning
	case zerolog.ErrorLevel, zerolog.FatalLevel, zerolog.PanicLevel:
		tone = badge.ToneError
	default:
		tone = badge.ToneNeutral
	}
	return
}

// instanceCounter feeds the instance number stamped on every emitted
// log event. Starts at 0; the post-increment value tags this App for
// its window's lifetime so the viewer can split events per-window.
var instanceCounter atomic.Uint64

// levelOptions is the dropdown vocabulary. Fatal/Panic are
// deliberately omitted — the demo emits routinely and we don't want
// the operator to accidentally kill the process by leaving the stream
// toggle on at Fatal.
var levelOptions = []zerolog.Level{
	zerolog.TraceLevel,
	zerolog.DebugLevel,
	zerolog.InfoLevel,
	zerolog.WarnLevel,
	zerolog.ErrorLevel,
}

// App is the per-window logdemo instance. The registry's factory ctor
// allocates a fresh App per Open() so two windows have independent
// emit counters, stream selections, and custom-message buffers.
type App struct {
	// ids is the per-instance WidgetIdStack. The host pre-pushes a
	// window-unique salt onto it before every Frame() call via
	// c.IdScope so widget ids cannot collide across open apps.
	// Captured from MountCtx.Ids() at Mount time; ctor seeds a
	// fallback stack for tests that skip Mount.
	ids *c.WidgetIdStack

	// density resolves IDS spacing tokens at the active preset
	// (ADR-0032 §SD2); cached once at newApp.
	density styletokens.DensityE

	instNum uint64

	// logger is the per-instance zerolog logger, tagged with the
	// app_id (set by the host via ctx.Log()) plus a logdemo_inst
	// field. Initialised in Mount; fall back to log.Logger if Frame
	// is reached without a prior Mount (test paths).
	logger     zerolog.Logger
	loggerInit bool

	emitted atomic.Uint64

	streamEnabled bool
	streamLevel   zerolog.Level
	// streamEveryN is the frame stride between emissions when the
	// stream toggle is on. 1 = every frame (≈60/sec at 60fps), 60 =
	// once a second, …
	streamEveryN uint64
	frameCounter uint64

	customMessage string
	customLevel   zerolog.Level

	// scenarioCounter rotates through fixture lists in scenarios.go so
	// consecutive clicks on a scenario chip produce visually distinct
	// rows in the viewer instead of N copies of the same event.
	// Per-instance so two open windows step through fixtures
	// independently.
	scenarioCounter atomic.Uint64
}

var _ runtimeapp.AppI = (*App)(nil)

func newApp() (inst *App) {
	n := instanceCounter.Add(1)
	inst = &App{
		ids:           c.NewWidgetIdStack(),
		density:       styletokens.DensityFromEnv(),
		instNum:       n,
		streamLevel:   zerolog.InfoLevel,
		streamEveryN:  30,
		customMessage: "hello from logdemo",
		customLevel:   zerolog.InfoLevel,
	}
	return
}

func (inst *App) Manifest() (m runtimeapp.Manifest) { m = manifest; return }

func (inst *App) Mount(ctx runtimeapp.MountContextI) (err error) {
	// ctx.Log() is the per-app zerolog.Logger the host built via
	// app.AppLogger(log.Logger, m.Id) — already tagged with app_id
	// from the manifest. We tack on logdemo_inst so the logviewer can
	// pick events apart per-window in its message column.
	inst.logger = ctx.Log().With().Uint64("logdemo_inst", inst.instNum).Logger()
	inst.loggerInit = true
	// Pick up the host-supplied per-instance ids stack so the
	// renderer's PrepareStr/PrepareSeq calls scope ids under the
	// host's window-unique salt instead of the ctor's fallback.
	inst.ids = ctx.Ids()
	return
}

func (inst *App) Unmount(ctx runtimeapp.MountContextI) (err error) { return }

// Frame draws the demo panel and, when the stream toggle is on, emits
// one log every streamEveryN frames. Stream emission happens before
// the render so the counter at the top of the panel reflects the
// just-emitted tick. The host has already pre-pushed a window-unique
// salt onto inst.ids via c.IdScope (windowhost.renderWindowBody) so
// widget ids are scoped without a local IdScope wrapper.
func (inst *App) Frame(ctx runtimeapp.FrameContextI) (err error) {
	inst.frameCounter++
	if inst.streamEnabled && inst.streamEveryN > 0 && inst.frameCounter%inst.streamEveryN == 0 {
		inst.emit(inst.streamLevel, fmt.Sprintf("stream tick #%d", inst.frameCounter/inst.streamEveryN))
	}
	inst.render()
	return
}

// emit dispatches a single zerolog event at the chosen level and
// increments the per-instance counter. Falls back to the package-
// level log.Logger if Mount has not yet run (test paths).
func (inst *App) emit(lvl zerolog.Level, msg string) {
	logger := inst.logger
	if !inst.loggerInit {
		logger = log.Logger
	}
	var ev *zerolog.Event
	switch lvl {
	case zerolog.TraceLevel:
		ev = logger.Trace()
	case zerolog.DebugLevel:
		ev = logger.Debug()
	case zerolog.WarnLevel:
		ev = logger.Warn()
	case zerolog.ErrorLevel:
		ev = logger.Error()
	default:
		ev = logger.Info()
	}
	ev.Msg(msg)
	inst.emitted.Add(1)
}

func (inst *App) render() {
	// Header row: instance tag + live emitted count, both as badges so
	// the demo's chrome matches what the logviewer renders downstream.
	for range c.Horizontal().KeepIter() {
		badge.New(inst.ids.PrepareStr("hdr-inst"), fmt.Sprintf("instance #%d", inst.instNum)).
			Tone(badge.TonePrimary).Variant(badge.VariantSoft).
			Size(badge.SizeMd).Monospace().Send()
		c.AddSpace(styletokens.GapItems(inst.density))
		badge.New(inst.ids.PrepareStr("hdr-count"), fmt.Sprintf("emitted %d", inst.emitted.Load())).
			Tone(badge.ToneSuccess).Variant(badge.VariantSoft).
			Size(badge.SizeMd).Monospace().
			Tooltip("events this instance has handed to the logbridge sink so far").Send()
	}
	c.Separator().Horizontal().Send()

	c.Label("Quick actions").Send()
	for range c.Horizontal().KeepIter() {
		// Per-level emit chips. Tone matches the viewer's level badge,
		// so a click here lights up the corresponding tone in the table
		// — useful for screenshot tours and quick visual verification.
		if badge.New(inst.ids.PrepareStr("emit-trace"), "TRACE").
			Tone(levelTone(zerolog.TraceLevel)).Variant(badge.VariantOutline).
			Size(badge.SizeSm).Pill().Monospace().
			Tooltip("emit one trace-level event").
			SendResp().HasPrimaryClicked() {
			inst.emit(zerolog.TraceLevel, "trace button")
		}
		if badge.New(inst.ids.PrepareStr("emit-debug"), "DEBUG").
			Tone(levelTone(zerolog.DebugLevel)).Variant(badge.VariantOutline).
			Size(badge.SizeSm).Pill().Monospace().
			Tooltip("emit one debug-level event").
			SendResp().HasPrimaryClicked() {
			inst.emit(zerolog.DebugLevel, "debug button")
		}
		if badge.New(inst.ids.PrepareStr("emit-info"), "INFO").
			Tone(levelTone(zerolog.InfoLevel)).Variant(badge.VariantSolid).
			Size(badge.SizeSm).Pill().Monospace().
			Tooltip("emit one info-level event").
			SendResp().HasPrimaryClicked() {
			inst.emit(zerolog.InfoLevel, "info button")
		}
		if badge.New(inst.ids.PrepareStr("emit-warn"), "WARN").
			Tone(levelTone(zerolog.WarnLevel)).Variant(badge.VariantSolid).
			Size(badge.SizeSm).Pill().Monospace().
			Tooltip("emit one warn-level event — tints the row in the viewer").
			SendResp().HasPrimaryClicked() {
			inst.emit(zerolog.WarnLevel, "warn button")
		}
		if badge.New(inst.ids.PrepareStr("emit-error"), "ERROR").
			Tone(levelTone(zerolog.ErrorLevel)).Variant(badge.VariantSolid).
			Size(badge.SizeSm).Pill().Monospace().Strong().
			Tooltip("emit one error-level event — tints the row red in the viewer").
			SendResp().HasPrimaryClicked() {
			inst.emit(zerolog.ErrorLevel, "error button")
		}
		c.Separator().Vertical().Send()
		if c.Button(inst.ids.PrepareStr("emit-burst"), c.Atoms().Text("Burst (5×info)").Keep()).
			SendResp().HasPrimaryClicked() {
			for i := 0; i < 5; i++ {
				inst.emit(zerolog.InfoLevel, fmt.Sprintf("burst %d/5", i+1))
			}
		}
		if c.Button(inst.ids.PrepareStr("emit-spread"), c.Atoms().Text("Spread (one each)").Keep()).
			SendResp().HasPrimaryClicked() {
			for _, lvl := range levelOptions {
				inst.emit(lvl, "spread: "+lvl.String())
			}
		}
	}

	c.Separator().Horizontal().Send()
	c.Label("Structured scenarios — fire realistic events with typed fields").Send()
	for range c.Horizontal().KeepIter() {
		// Each scenario chip rotates through a fixture set in
		// scenarios.go so consecutive clicks produce visually distinct
		// rows. Levels are derived from payload semantics (a 5xx HTTP
		// status emits error; a denied auth emits warn; etc.) so the
		// viewer's row-tint follows the data, not the click.
		if badge.New(inst.ids.PrepareStr("scn-http"), "HTTP request").
			Tone(badge.ToneInfo).Variant(badge.VariantOutline).
			Size(badge.SizeSm).Pill().
			Tooltip("emit a structured HTTP-request event (method/path/status/latency_ms/resp_bytes/remote_addr/served_at; status drives level)").
			SendResp().HasPrimaryClicked() {
			inst.emitScenarioHTTP()
		}
		if badge.New(inst.ids.PrepareStr("scn-db"), "DB query").
			Tone(badge.TonePrimary).Variant(badge.VariantOutline).
			Size(badge.SizeSm).Pill().
			Tooltip("emit a structured DB-query event (query/rows/duration_ms/conn_id/replica/ts; slow→warn, every 4th call → error with err)").
			SendResp().HasPrimaryClicked() {
			inst.emitScenarioDB()
		}
		if badge.New(inst.ids.PrepareStr("scn-auth"), "Auth event").
			Tone(badge.ToneSuccess).Variant(badge.VariantOutline).
			Size(badge.SizeSm).Pill().
			Tooltip("emit a structured auth-decision event (user/role/subject/granted/attempt/session bytes/at; denied → warn)").
			SendResp().HasPrimaryClicked() {
			inst.emitScenarioAuth()
		}
		if badge.New(inst.ids.PrepareStr("scn-boxerr"), "Boxer error").
			Tone(badge.ToneError).Variant(badge.VariantOutline).
			Size(badge.SizeSm).Pill().Strong().
			Tooltip("emit an error event whose Err is a wrapped boxer-machinery error chain — eh.ErrorMarshalFuncHuman renders cause arrows, stacks, and structured-data fields into the error envelope").
			SendResp().HasPrimaryClicked() {
			inst.emitScenarioBoxerErr()
		}
		c.Separator().Vertical().Send()
		if c.Button(inst.ids.PrepareStr("scn-burst"), c.Atoms().Text("Burst (one of each)").Keep()).
			SendResp().HasPrimaryClicked() {
			inst.emitScenarioHTTP()
			inst.emitScenarioDB()
			inst.emitScenarioAuth()
			inst.emitScenarioBoxerErr()
		}
	}

	c.Separator().Horizontal().Send()
	c.Label("Stream — emit one event every N frames while enabled").Send()
	for range c.Horizontal().KeepIter() {
		c.Checkbox(inst.ids.PrepareStr("stream-en"), inst.streamEnabled, "Enabled").
			SendRespVal(&inst.streamEnabled)
		c.AddSpace(styletokens.PaddingOuter(inst.density))
		c.Label("Level:").Send()
		streamLevelLabel := inst.streamLevel.String()
		for range c.ComboBox(inst.ids.PrepareStr("stream-lvl"),
			c.WidgetText().Text("level").Keep(),
			c.WidgetText().Text(streamLevelLabel).Keep()).KeepIter() {
			for _, lvl := range levelOptions {
				selected := lvl == inst.streamLevel
				if c.Button(inst.ids.PrepareStr("sl-"+lvl.String()),
					c.Atoms().Text(lvl.String()).Keep()).
					Selected(selected).
					FrameWhenInactive(!selected).
					Frame(true).
					SendResp().HasPrimaryClicked() {
					inst.streamLevel = lvl
				}
			}
		}
		c.AddSpace(styletokens.PaddingOuter(inst.density))
		c.Label("Every N frames:").Send()
		c.DragValueU64(inst.ids.PrepareStr("stream-every"), inst.streamEveryN).
			Speed(1.0).
			SendRespVal(&inst.streamEveryN)
	}

	c.Separator().Horizontal().Send()
	c.Label("Custom message").Send()
	for range c.Horizontal().KeepIter() {
		c.Label("Message:").Send()
		c.TextEdit(inst.ids.PrepareStr("custom-msg"), inst.customMessage, false).
			DesiredWidth(280).
			SendRespVal(&inst.customMessage)
		c.AddSpace(styletokens.PaddingOuter(inst.density))
		c.Label("Level:").Send()
		customLevelLabel := inst.customLevel.String()
		for range c.ComboBox(inst.ids.PrepareStr("custom-lvl"),
			c.WidgetText().Text("level").Keep(),
			c.WidgetText().Text(customLevelLabel).Keep()).KeepIter() {
			for _, lvl := range levelOptions {
				selected := lvl == inst.customLevel
				if c.Button(inst.ids.PrepareStr("cl-"+lvl.String()),
					c.Atoms().Text(lvl.String()).Keep()).
					Selected(selected).
					FrameWhenInactive(!selected).
					Frame(true).
					SendResp().HasPrimaryClicked() {
					inst.customLevel = lvl
				}
			}
		}
		c.AddSpace(styletokens.PaddingOuter(inst.density))
		if c.Button(inst.ids.PrepareStr("custom-emit"), c.Atoms().Text("Emit").Keep()).
			SendResp().HasPrimaryClicked() {
			inst.emit(inst.customLevel, inst.customMessage)
		}
	}
}
