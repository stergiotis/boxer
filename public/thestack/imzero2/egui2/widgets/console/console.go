// Package console is a terminal screen: a semi-retained widget (ADR-0267)
// over the client's console opcode, decided in ADR-0359. The VT core runs in
// the client; this package owns the stream of the program's output until the
// client has read it, and returns, as Events, what the program should be
// sent and what happened on the screen. Events are one frame behind the
// frame that produced them, as every client read-back is.
//
// The widget never talks to a process. A host writes the program's output
// with Write, from any goroutine, and forwards Events.ToProgram to the
// program's input:
//
//	con := console.New(ids, "shell", console.Options{})
//	go io.Copy(con, ptyMaster)            // output in
//	// each frame:
//	ev := con.RenderFill(640, 400)
//	ptyMaster.Write(ev.ToProgram)          // input out
//	if ev.Resized { setWinsize(ev.Cols, ev.Rows) }
//
// Output is addressed by its offset in the stream and kept until the client
// acknowledges it, so bytes fed to a console the host did not draw (a hidden
// dock tab) are sent again rather than lost (ADR-0359 SD2). Write blocks
// while more than Options.MaxPending bytes wait, which is the backpressure a
// pseudo-terminal reader wants.
package console

import (
	"errors"
	"sync"

	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// Options configure a Console. Zero values take the defaults.
type Options struct {
	// HistoryBytes is how much scrollback the client's core keeps; read
	// once, when the client makes the core. Zero takes the client's default.
	HistoryBytes uint32
	// FontSize is the monospace font size in points; zero takes the style's.
	FontSize float32
	// MaxFeedPerFrame bounds the bytes sent to the client in one frame;
	// zero takes 1 MiB.
	MaxFeedPerFrame int
	// MaxPending bounds the bytes Write holds before it blocks; zero takes
	// 16 MiB.
	MaxPending int
	// Cols and Rows fix the grid, whatever room the console is given: a
	// replay at a recorded size. The console then takes the grid's size in
	// points; zero on an axis follows the room.
	Cols, Rows uint16
}

// ErrClosed is what Write returns once the console is closed.
var ErrClosed = errors.New("console: closed")

const (
	defaultMaxFeedPerFrame = 1 << 20
	defaultMaxPending      = 16 << 20
)

// Events is what the client reported for the console last frame.
type Events struct {
	// Records are every row the client produced, in order: what a recorder
	// keeps. Valid until the next Render.
	Records []c.ConsoleRow
	// ToProgram is the bytes for the program, in order: keys, pastes, focus
	// reports and the terminal's own replies. Valid until the next Render.
	ToProgram []byte
	// Resized reports that Cols and Rows changed (the first frame too).
	Resized bool
	// Cols and Rows are the grid's size, once the client has reported it.
	Cols, Rows uint16
	// Title is the window title the program set last, and TitleChanged
	// whether it set one last frame.
	Title        string
	TitleChanged bool
	// Bell reports the program rang the bell.
	Bell bool
	// Gap reports the client had lost bytes it was fed (its core was
	// dropped); the screen shows only what came after.
	Gap bool
	// Acked is how many bytes of the stream the client has read.
	Acked uint64
	// ScreenChanged reports a new screen; Console.Screen returns it.
	ScreenChanged bool
}

// Console is one terminal screen. Write may be called from any goroutine;
// every other method belongs to the render goroutine.
type Console struct {
	Opts Options

	ids      *c.WidgetIdStack
	scopeKey string

	mu      sync.Mutex
	cond    *sync.Cond
	pending []byte // the stream from base on, not yet acknowledged
	base    uint64 // the stream offset of pending[0]
	closed  bool

	cols, rows uint16
	title      string
	toProgram  []byte
	screen     c.ConsoleScreen
	hasScreen  bool
}

// New makes a console under scopeKey in ids.
func New(ids *c.WidgetIdStack, scopeKey string, opts Options) *Console {
	inst := &Console{
		Opts:     opts,
		ids:      ids,
		scopeKey: scopeKey,
	}
	inst.cond = sync.NewCond(&inst.mu)
	return inst
}

// Write appends the program's output to the stream. It blocks while more
// than Options.MaxPending bytes wait for the client, and fails once the
// console is closed.
func (inst *Console) Write(p []byte) (n int, err error) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	limit := inst.Opts.MaxPending
	if limit <= 0 {
		limit = defaultMaxPending
	}
	for !inst.closed && len(inst.pending) > 0 && len(inst.pending)+len(p) > limit {
		inst.cond.Wait()
	}
	if inst.closed {
		return 0, ErrClosed
	}
	inst.pending = append(inst.pending, p...)
	return len(p), nil
}

// Close wakes a blocked Write and makes later ones fail. The client drops
// the console's screen when it is no longer drawn.
func (inst *Console) Close() error {
	inst.mu.Lock()
	inst.closed = true
	inst.mu.Unlock()
	inst.cond.Broadcast()
	return nil
}

// Render draws the console at w by h points (zero on an axis takes what
// the parent has left) and returns last frame's events.
func (inst *Console) Render(w, h float32) (ev Events) {
	for range c.IdScope(inst.ids.PrepareStr(inst.scopeKey)) {
		ev = inst.render(w, h)
	}
	return
}

// RenderFill is Render sized to the pane, read back one frame behind, with
// the fallbacks serving until it reports (ADR-0267 W12).
func (inst *Console) RenderFill(fallbackW, fallbackH float32) (ev Events) {
	for range c.IdScope(inst.ids.PrepareStr(inst.scopeKey)) {
		w, h, ok := c.CapturePaneSize(inst.ids.ProbeSeq("pane"))
		if !ok || w < 1 || h < 1 {
			w, h = fallbackW, fallbackH
		}
		ev = inst.render(w, h)
	}
	return
}

// render runs inside the widget's id scope.
func (inst *Console) render(w, h float32) (ev Events) {
	sm := c.CurrentApplicationState.StateManager
	id := inst.ids.PrepareStr("screen").Derive()
	rows := sm.GetConsoleRows(widgethandle.Make(id))
	ev = inst.absorb(rows)

	inst.mu.Lock()
	if ev.Acked > inst.base {
		drop := ev.Acked - inst.base
		if drop >= uint64(len(inst.pending)) {
			inst.base += uint64(len(inst.pending))
			inst.pending = inst.pending[:0]
		} else {
			inst.pending = inst.pending[drop:]
			inst.base = ev.Acked
		}
		inst.cond.Broadcast()
	}
	limit := inst.Opts.MaxFeedPerFrame
	if limit <= 0 {
		limit = defaultMaxFeedPerFrame
	}
	feed := inst.pending[:min(len(inst.pending), limit)]
	offset := inst.base
	f := c.Console(inst.ids.PrepareStr("screen"), offset, feed).Size(w, h)
	inst.mu.Unlock()

	if inst.Opts.HistoryBytes > 0 {
		f = f.HistoryBytes(inst.Opts.HistoryBytes)
	}
	if inst.Opts.FontSize > 0 {
		f = f.FontSize(inst.Opts.FontSize)
	}
	if inst.Opts.Cols > 0 || inst.Opts.Rows > 0 {
		f = f.Grid(inst.Opts.Cols, inst.Opts.Rows)
	}
	f.Send()
	return
}

// absorb reads last frame's rows into Events and the console's state.
func (inst *Console) absorb(rows []c.ConsoleRow) (ev Events) {
	inst.toProgram = inst.toProgram[:0]
	for _, row := range rows {
		switch {
		case row.ForProgram():
			inst.toProgram = append(inst.toProgram, row.Payload...)
		case row.Kind == c.ConsoleRowSize:
			if cols, rs, ok := row.Size(); ok && (cols != inst.cols || rs != inst.rows) {
				inst.cols, inst.rows = cols, rs
				ev.Resized = true
			}
		case row.Kind == c.ConsoleRowAck:
			if acked, _, ok := row.Offsets(); ok {
				ev.Acked = acked
			}
		case row.Kind == c.ConsoleRowTitle:
			inst.title = string(row.Payload)
			ev.TitleChanged = true
		case row.Kind == c.ConsoleRowBell:
			ev.Bell = true
		case row.Kind == c.ConsoleRowGap:
			ev.Gap = true
		case row.Kind == c.ConsoleRowScreen:
			if sc, ok := row.Screen(); ok {
				inst.screen, inst.hasScreen = sc, true
				ev.ScreenChanged = true
			}
		}
	}
	ev.Records = rows
	ev.ToProgram = inst.toProgram
	ev.Cols, ev.Rows = inst.cols, inst.rows
	ev.Title = inst.title
	return
}

// Screen returns the screen the client reported last: what a reader that is
// not a person (an agent, a test) sees. It lags the drawn screen by up to a
// tenth of a second; ok is false before the first report.
func (inst *Console) Screen() (s c.ConsoleScreen, ok bool) {
	return inst.screen, inst.hasScreen
}

// Pending returns how many bytes of output wait for the client.
func (inst *Console) Pending() int {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return len(inst.pending)
}
