package widgets

import (
	"math"
	"time"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/registry"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timescrubber"
)

// The time strip of ADR-0251 on its own, with no map under it: a forecast
// that is hourly and then three-hourly, whose steps load as the playhead
// nears them. Two demos, because three transport rows on one page are three
// sets of the same button — noise for a reader and an ambiguous anchor for a
// scripted driver. The first is the strip to play with; the second is the
// same widget in its other forms, driven from nothing.
//
// The loading of the first is simulated from the transport alone — what is
// held is the bracket and what Transport.Ahead lists, the step past that is
// loading, one step is missing — so the picture is a function of the position
// and a capture does not depend on when a goroutine ran. The wall clock is
// fixed, inside the series, so the now line and the tint before it show.
const (
	timeScrubberDemoW       = 1080
	timeScrubberDemoMissing = 36
	timeScrubberDemoSteps   = 40
	// timeScrubberDemoHourly is how many steps are an hour apart before the
	// series goes to three-hourly.
	timeScrubberDemoHourly = 24
)

// timeScrubberDemoStart is the first step's instant, and timeScrubberDemoNow
// a wall clock inside the series.
var (
	timeScrubberDemoStart = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	timeScrubberDemoNow   = timeScrubberDemoStart.Add(9*time.Hour + 30*time.Minute)
)

// timeScrubberForecast is the fixture: a diurnal signal with a front on the
// second day, whose gusts run well above the means — which is what the strip
// draws clipped when a peak passes the top of the scale.
func timeScrubberForecast() (steps []timescrubber.Step) {
	at := timeScrubberDemoStart
	for i := 0; i < timeScrubberDemoSteps; i++ {
		h := at.Sub(timeScrubberDemoStart).Hours()
		mean := 6 + 3*math.Sin(h/24*2*math.Pi-1) + 7*math.Exp(-math.Pow((h-40)/6, 2))
		peak := mean * (1.35 + 1.6*math.Exp(-math.Pow((h-40)/5, 2)))
		steps = append(steps, timescrubber.Step{At: at, Value: float32(mean), Peak: float32(peak)})
		if i < timeScrubberDemoHourly {
			at = at.Add(time.Hour)
		} else {
			at = at.Add(3 * time.Hour)
		}
	}
	return
}

// timeScrubberDemoColor is a ramp from calm to strong, 0xRRGGBBAA.
func timeScrubberDemoColor(v float32) uint32 {
	t := float64(min(max(v/20, 0), 1))
	return uint32(60+190*t)<<24 | uint32(170-60*t)<<16 | uint32(200-150*t)<<8 | 0xff
}

func timeScrubberDemoOptions(key string) timescrubber.Options {
	return timescrubber.Options{
		ScopeKey: key, ValueName: "mean speed", ValueUnit: "m/s",
		Now: func() time.Time { return timeScrubberDemoNow },
	}
}

func newTimeScrubberDemo(ids *c.WidgetIdStack, opts timescrubber.Options) (sc *timescrubber.Scrubber) {
	sc = timescrubber.New(ids, opts)
	sc.ValueColor = timeScrubberDemoColor
	return
}

// --- the strip to play with ---

type timeScrubberDemoState struct {
	sc    *timescrubber.Scrubber
	steps []timescrubber.Step
	ahead []int
}

func newTimeScrubberDemoState(ids *c.WidgetIdStack) (st *timeScrubberDemoState) {
	st = &timeScrubberDemoState{steps: timeScrubberForecast()}
	st.sc = newTimeScrubberDemo(ids, timeScrubberDemoOptions("ts-demo"))
	st.sc.Marks = []timescrubber.Mark{{At: timeScrubberDemoStart.Add(6 * time.Hour), Label: "run 06:00"}}
	st.sc.Transport.Pos = 12
	st.sc.Transport.SetRange(8, 30)
	return
}

func demoTimeScrubber(_ *c.WidgetIdStack, st *timeScrubberDemoState) {
	// What a loader would hold for this position: the bracket, the steps the
	// transport says playback reaches next, and the one past those on its way.
	tr := &st.sc.Transport
	n := len(st.steps)
	st.ahead = tr.Ahead(n, 4, st.ahead)
	for i := range st.steps {
		st.steps[i].State = timescrubber.StepStateIdle
	}
	hold := func(i int, s timescrubber.StepStateE) {
		if i >= 0 && i < n {
			st.steps[i].State = s
		}
	}
	hold(int(math.Floor(tr.Pos)), timescrubber.StepStateHeld)
	hold(int(math.Ceil(tr.Pos)), timescrubber.StepStateHeld)
	for k, step := range st.ahead {
		if k == len(st.ahead)-1 {
			hold(step, timescrubber.StepStateLoading)
		} else {
			hold(step, timescrubber.StepStateHeld)
		}
	}
	hold(timeScrubberDemoMissing, timescrubber.StepStateMissing)

	c.Label("A forecast, hourly for a day and three-hourly after it. The bars are the mean speed in some view and the caps its gusts; a notch says what the layer has of each step, filled for held, hollow for loading, a cross for missing. Drag the band along the top for a loop range, its edges and body to change it; In and Out set it from the playhead, and a double click on the band clears it. Click the strip and the keys are yours: arrows a step, Shift or PageUp and PageDown a stride, Ctrl a day, Alt a mark, Shift+Home and Shift+End the range, Delete clears it.").Wrap().Send()
	st.sc.Render(timeScrubberDemoW, st.steps)
}

// --- the other forms ---

type timeScrubberFormsState struct {
	compact, byIndex, crowd *timescrubber.Scrubber
	forecast, dense         []timescrubber.Step
}

func newTimeScrubberFormsState(ids *c.WidgetIdStack) (st *timeScrubberFormsState) {
	st = &timeScrubberFormsState{forecast: timeScrubberForecast()}
	for i := range st.forecast {
		st.forecast[i].State = timescrubber.StepStateHeld
	}
	st.forecast[timeScrubberDemoMissing].State = timescrubber.StepStateMissing
	st.forecast[timeScrubberDemoMissing+2].State = timescrubber.StepStateIdle

	for i := 0; i < 2000; i++ {
		h := float64(i) / 6
		v := 5 + 4*math.Sin(h/24*2*math.Pi) + 2*math.Sin(h/3.1)
		if i == 1234 {
			v = 19 // the one step a subsampled strip would lose
		}
		state := timescrubber.StepStateHeld
		if i > 1500 {
			state = timescrubber.StepStateIdle
		}
		st.dense = append(st.dense, timescrubber.Step{
			At:    timeScrubberDemoStart.Add(time.Duration(i) * 10 * time.Minute),
			Value: float32(v), Peak: float32(math.NaN()), State: state,
		})
	}
	st.dense[1700].State = timescrubber.StepStateMissing

	o := timeScrubberDemoOptions("ts-forms-compact")
	o.Compact = true
	st.compact = newTimeScrubberDemo(ids, o)
	st.compact.Transport.Pos = 12

	o = timeScrubberDemoOptions("ts-forms-index")
	o.ByIndex = true
	st.byIndex = newTimeScrubberDemo(ids, o)
	st.byIndex.Transport.Pos = 27

	o = timeScrubberDemoOptions("ts-forms-crowd")
	o.Now = func() time.Time { return timeScrubberDemoStart.Add(200 * time.Hour) }
	st.crowd = newTimeScrubberDemo(ids, o)
	st.crowd.Transport.Pos = 900
	return
}

func demoTimeScrubberForms(_ *c.WidgetIdStack, st *timeScrubberFormsState) {
	c.Label("The same forecast on one line (Options.Compact): the state lane, the playhead and the range, and the time.").Wrap().Send()
	st.compact.Render(timeScrubberDemoW, st.forecast)
	c.Separator().Send()
	c.Label("The same forecast by index (Options.ByIndex): every step as wide as the next, so the dense block is not squeezed by the sparse one — and the change of cadence no longer shows.").Wrap().Send()
	st.byIndex.Render(timeScrubberDemoW, st.forecast)
	c.Separator().Send()
	c.Label("Two thousand steps, ten minutes apart: a pixel column draws the largest value among its steps and the worst of their states, so the one strong step and the one missing step are both still there. Nothing is subsampled.").Wrap().Send()
	st.crowd.Render(timeScrubberDemoW, st.dense)
}

func init() {
	registry.Register(registry.Demo{
		Name:        "timescrubber",
		Category:    "Layout & widgets",
		Title:       "time scrubber (a strip for a stepped series)",
		Stage:       [2]float32{1120, 600},
		Flags:       registry.DemoFlagNeedsLargeArea,
		Kind:        registry.DemoKindUX,
		Description: "The time strip of ADR-0251: steps at their own instants on a calendar axis, a bar to each step's value and a cap to its peak, each step's load state told by shape rather than by hue, a playhead, a loop range edited on its own band, a mark for the model run, and a transport whose playback waits for a step that is loading and dwells at the ends. What is loaded is simulated from the position — the bracket and what Transport.Ahead says playback reaches next — and the wall clock is fixed inside the series, so the capture is a function of the state.",
		Init: func(ids *c.WidgetIdStack) (state any) {
			return newTimeScrubberDemoState(ids)
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoTimeScrubber(ids, state.(*timeScrubberDemoState))
		},
	})
	registry.Register(registry.Demo{
		Name:        "timescrubberforms",
		Category:    "Layout & widgets",
		Title:       "time scrubber forms (compact, by index, crowded)",
		Stage:       [2]float32{1120, 840},
		Flags:       registry.DemoFlagNeedsLargeArea,
		Kind:        registry.DemoKindUX,
		Description: "The same widget in the forms a host picks when the strip is not the page's subject: one line, an index axis for a series whose dense block would otherwise be squeezed, and two thousand steps drawn as a min–max envelope per pixel column with the worst state of each column on the state lane.",
		Init: func(ids *c.WidgetIdStack) (state any) {
			return newTimeScrubberFormsState(ids)
		},
		RenderStateful: func(ids *c.WidgetIdStack, state any) {
			demoTimeScrubberForms(ids, state.(*timeScrubberFormsState))
		},
	})
}
