package agentconsole

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/apps/opsdemo"
	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// keepRows bounds the call log.
const keepRows = 50

// statusWait is how long one status poll waits for a final phase.
const statusWait = 2 * time.Second

// row is one call in the log.
type row struct {
	key    string
	op     string
	phase  string
	reason string
	result string
}

// App is one console window. Calls run on goroutines of the console's own;
// what they learn lands under mu and is drawn by the next Frame.
type App struct {
	ids    *c.WidgetIdStack
	log    zerolog.Logger
	bus    app.BusI
	cli    *agent.Client
	appCtx context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Bound to widgets; render goroutine only.
	openApp      string
	destinations string
	windowKey    string
	op           string
	args         string
	n            int

	mu        sync.Mutex
	grant     agent.Grant
	note      string
	rows      []*row
	inflight  int
	openedKey uint64
}

var _ app.AppI = (*App)(nil)

func newApp() (inst *App) {
	inst = &App{ids: c.NewWidgetIdStack(), op: "get_state", args: "{}", openApp: "opsdemo",
		destinations: DestinationsSeed.Get()}
	return
}

func (inst *App) Manifest() (m app.Manifest) { m = manifest; return }

func (inst *App) Mount(ctx app.MountContextI) (err error) {
	inst.ids = ctx.Ids()
	inst.log = ctx.Log()
	inst.bus = ctx.Bus()
	inst.cli = agent.NewClient(inst.bus)
	inst.appCtx, inst.cancel = context.WithCancel(context.Background())
	return
}

func (inst *App) Unmount(ctx app.MountContextI) (err error) {
	if inst.cancel != nil {
		inst.cancel()
	}
	inst.mu.Lock()
	handle := inst.grant.Handle
	inst.mu.Unlock()
	if handle != "" {
		// A grant ends with its coordinator (ADR-0269 §SD6).
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		_ = inst.cli.Stop(stopCtx, handle)
		cancel()
	}
	inst.wg.Wait()
	return
}

// spawn runs fn off the render goroutine: the host refuses runtime.agent
// requests made on it.
func (inst *App) spawn(fn func(ctx context.Context)) {
	inst.mu.Lock()
	inst.inflight++
	inst.mu.Unlock()
	inst.wg.Add(1)
	go func() {
		defer inst.wg.Done()
		defer func() {
			inst.mu.Lock()
			inst.inflight--
			inst.mu.Unlock()
		}()
		fn(inst.appCtx)
	}()
}

func (inst *App) setNote(s string) {
	inst.mu.Lock()
	inst.note = s
	inst.mu.Unlock()
}

func (inst *App) instance() (key uint64, ok bool) {
	key, err := strconv.ParseUint(strings.TrimSpace(inst.windowKey), 10, 64)
	ok = err == nil && key != 0
	return
}

func (inst *App) Frame(ctx app.FrameContextI) (err error) {
	inst.mu.Lock()
	if inst.openedKey != 0 {
		// Before the key's widget draws, so the field shows it this frame.
		inst.windowKey = strconv.FormatUint(inst.openedKey, 10)
		inst.openedKey = 0
	}
	grant, note, inflight := inst.grant, inst.note, inst.inflight
	rows := slices.Clone(inst.rows)
	inst.mu.Unlock()

	for range c.HorizontalTop().KeepIter() {
		if c.Button(inst.ids.PrepareStr("open-demo"), c.Atoms().Text("Open operations demo").Keep()).SendResp().HasPrimaryClicked() {
			inst.openDemo(string(opsdemo.AppId))
		}
		c.TextEdit(inst.ids.PrepareStr("open-app"), inst.openApp, false).DesiredWidth(90).HintText("app").SendRespVal(&inst.openApp)
		if c.Button(inst.ids.PrepareStr("open-app-button"), c.Atoms().Text("Open").Keep()).SendResp().HasPrimaryClicked() {
			inst.openDemo(strings.TrimSpace(inst.openApp))
		}
		c.Label("window").Send()
		c.TextEdit(inst.ids.PrepareStr("window-key"), inst.windowKey, false).DesiredWidth(80).HintText("window key").SendRespVal(&inst.windowKey)
		if c.Button(inst.ids.PrepareStr("grant"), c.Atoms().Text("Request grant").Keep()).SendResp().HasPrimaryClicked() {
			inst.requestGrant()
		}
		if c.Button(inst.ids.PrepareStr("stop"), c.Atoms().Text("Stop").Keep()).SendResp().HasPrimaryClicked() {
			inst.stop()
		}
	}
	for range c.HorizontalTop().KeepIter() {
		c.Label("destinations").Send()
		c.TextEdit(inst.ids.PrepareStr("destinations"), inst.destinations, false).DesiredWidth(320).
			HintText("e.g. keelson:apps, clickhouse:localhost:8123").SendRespVal(&inst.destinations)
	}
	grantLine := "no grant"
	if grant.Handle != "" {
		grantLine = "grant " + grant.Task
	}
	c.Label(grantLine).Send()
	for range c.HorizontalTop().KeepIter() {
		c.Label("operation").Send()
		c.TextEdit(inst.ids.PrepareStr("op"), inst.op, false).DesiredWidth(140).HintText("operation").SendRespVal(&inst.op)
		if c.Button(inst.ids.PrepareStr("describe"), c.Atoms().Text("Describe").Keep()).SendResp().HasPrimaryClicked() {
			inst.describe()
		}
		if c.Button(inst.ids.PrepareStr("capture"), c.Atoms().Text("Capture").Keep()).SendResp().HasPrimaryClicked() {
			inst.capture(agent.CaptureFormatSvg)
		}
		if c.Button(inst.ids.PrepareStr("capture-png"), c.Atoms().Text("Capture PNG").Keep()).SendResp().HasPrimaryClicked() {
			inst.capture(agent.CaptureFormatPng)
		}
		if c.Button(inst.ids.PrepareStr("turn"), c.Atoms().Text("Turn").Keep()).SendResp().HasPrimaryClicked() {
			inst.turn()
		}
	}
	c.TextEdit(inst.ids.PrepareStr("args"), inst.args, true).DesiredRows(3).HintText("arguments").SendRespVal(&inst.args)
	if c.Button(inst.ids.PrepareStr("call"), c.Atoms().Text("Call").Keep()).SendResp().HasPrimaryClicked() {
		inst.call()
	}
	if note != "" {
		c.Label(note).Wrap().Send()
	}
	if inflight > 0 {
		c.Label("waiting on " + strconv.Itoa(inflight) + " request(s)").Send()
	}
	for _, r := range slices.Backward(rows) {
		line := r.key + " · " + r.op + " · " + r.phase
		if r.reason != "" {
			line += " · " + r.reason
		}
		c.Label(line).Wrap().Send()
		if r.result != "" {
			c.Label("    " + r.result).Wrap().Send()
		}
	}
	return
}

// openDemo opens a window of an app by id or subject alias.
func (inst *App) openDemo(name string) {
	bus := inst.bus
	target := app.AppIdT(name)
	for _, m := range app.AllManifests() {
		if string(m.Id) == name || m.Id.SubjectAlias() == name {
			target = m.Id
		}
	}
	inst.spawn(func(ctx context.Context) {
		key, err := windowhost.RequestOpen(bus, target, "", nil)
		if err != nil {
			inst.setNote("open: " + err.Error())
			return
		}
		inst.mu.Lock()
		inst.openedKey = key
		inst.note = "opened window " + strconv.FormatUint(key, 10)
		inst.mu.Unlock()
	})
}

func (inst *App) requestGrant() {
	key, ok := inst.instance()
	if !ok {
		inst.setNote("give a window key first")
		return
	}
	var dests []string
	for _, d := range strings.Split(inst.destinations, ",") {
		if d = strings.TrimSpace(d); d != "" {
			dests = append(dests, d)
		}
	}
	inst.setNote("asked the person; waiting for approval")
	inst.spawn(func(ctx context.Context) {
		g, err := inst.cli.Request(ctx, agent.GrantRequest{Plan: "drive window " + strconv.FormatUint(key, 10) + " by hand",
			Entries: []agent.GrantEntry{{Instance: key, Mode: agent.ModeAct}}, Destinations: dests})
		if err != nil {
			inst.setNote("grant: " + err.Error())
			return
		}
		inst.mu.Lock()
		inst.grant = g
		inst.note = "act on window " + strconv.FormatUint(key, 10)
		inst.mu.Unlock()
	})
}

func (inst *App) stop() {
	inst.mu.Lock()
	handle := inst.grant.Handle
	inst.grant = agent.Grant{}
	inst.mu.Unlock()
	if handle == "" {
		return
	}
	inst.spawn(func(ctx context.Context) {
		if err := inst.cli.Stop(ctx, handle); err != nil {
			inst.setNote("stop: " + err.Error())
			return
		}
		inst.setNote("stopped")
	})
}

func (inst *App) describe() {
	inst.mu.Lock()
	handle := inst.grant.Handle
	inst.mu.Unlock()
	key, ok := inst.instance()
	if handle == "" || !ok {
		inst.setNote("describe needs a grant and a window key")
		return
	}
	op := strings.TrimSpace(inst.op)
	inst.spawn(func(ctx context.Context) {
		insts, err := inst.cli.List(ctx, handle)
		if err != nil {
			inst.setNote("list: " + err.Error())
			return
		}
		appId := ""
		for _, i := range insts {
			if i.Instance == key {
				appId = i.App
			}
		}
		req := agent.DescribeRequest{App: appId}
		if op != "" {
			req.Operation = op
		}
		apps, err := inst.cli.Describe(ctx, req)
		if err != nil {
			inst.setNote("describe: " + err.Error())
			return
		}
		var b strings.Builder
		for _, a := range apps {
			for _, o := range a.Operations {
				b.WriteString(o.Name + " (" + o.Effect + "): " + o.Summary + "\n")
				if o.ArgsSchema != "" {
					b.WriteString("  args " + o.ArgsSchema + "\n")
				}
			}
		}
		inst.setNote(strings.TrimSpace(b.String()))
	})
}

// turn starts a turn the way a coordinator does before each model turn:
// it lists what other writers changed and lifts the task's pauses.
func (inst *App) turn() {
	inst.mu.Lock()
	handle := inst.grant.Handle
	inst.mu.Unlock()
	if handle == "" {
		inst.setNote("a turn needs a grant")
		return
	}
	inst.spawn(func(ctx context.Context) {
		changes, err := inst.cli.Turn(ctx, handle)
		if err != nil {
			inst.setNote("turn: " + err.Error())
			return
		}
		var b strings.Builder
		b.WriteString("turn: " + strconv.Itoa(len(changes)) + " change(s) by others")
		for _, ch := range changes {
			b.WriteString("\n  " + ch.Writer + " changed " + strings.Join(ch.Resources, ", ") + " in window " +
				strconv.FormatUint(ch.Instance, 10))
			if ch.Op != "" {
				b.WriteString(" (" + ch.Op + ")")
			}
		}
		inst.setNote(b.String())
	})
}

func (inst *App) newRow(op string) (r *row) {
	inst.n++
	r = &row{key: "k" + strconv.Itoa(inst.n), op: op, phase: "sent"}
	inst.mu.Lock()
	inst.rows = append(inst.rows, r)
	if len(inst.rows) > keepRows {
		inst.rows = inst.rows[len(inst.rows)-keepRows:]
	}
	inst.mu.Unlock()
	return
}

func (inst *App) update(r *row, out agent.Outcome) {
	inst.mu.Lock()
	r.phase, r.reason = out.Phase, out.Reason
	inst.mu.Unlock()
}

func (inst *App) call() {
	inst.mu.Lock()
	handle := inst.grant.Handle
	inst.mu.Unlock()
	key, ok := inst.instance()
	if handle == "" || !ok {
		inst.setNote("a call needs a grant and a window key")
		return
	}
	r := inst.newRow(strings.TrimSpace(inst.op))
	args := inst.args
	inst.spawn(func(ctx context.Context) {
		out, err := inst.cli.Call(ctx, agent.CallRequest{Handle: handle, Instance: key, Operation: r.op, Args: args,
			Key: r.key, Reason: "by hand from the agent console"})
		if err != nil {
			inst.update(r, agent.Outcome{Phase: "error", Reason: err.Error()})
			return
		}
		inst.update(r, out)
		inst.follow(ctx, handle, r, out)
	})
}

func (inst *App) capture(format string) {
	inst.mu.Lock()
	handle := inst.grant.Handle
	inst.mu.Unlock()
	key, ok := inst.instance()
	if handle == "" || !ok {
		inst.setNote("a capture needs a grant and a window key")
		return
	}
	r := inst.newRow("capture " + format)
	inst.spawn(func(ctx context.Context) {
		out, err := inst.cli.CaptureAs(ctx, handle, key, r.key, format)
		if err != nil {
			inst.update(r, agent.Outcome{Phase: "error", Reason: err.Error()})
			return
		}
		inst.update(r, out)
		inst.follow(ctx, handle, r, out)
	})
}

// follow polls a call until its phase is final, then reads its result.
func (inst *App) follow(ctx context.Context, handle string, r *row, out agent.Outcome) {
	for !out.Final() && ctx.Err() == nil {
		var err error
		out, err = inst.cli.Status(ctx, handle, r.key, statusWait)
		if err != nil {
			inst.update(r, agent.Outcome{Phase: "error", Reason: err.Error()})
			return
		}
		inst.update(r, out)
	}
	ref := out.ResultRef
	if ref == "" && out.Phase == "completed" {
		ref = out.Job
	}
	if ref == "" {
		return
	}
	res, err := inst.cli.Read(ctx, handle, ref)
	inst.mu.Lock()
	defer inst.mu.Unlock()
	switch {
	case err != nil:
		r.result = "read: " + err.Error()
	case res.DataHandle != "":
		r.result = "confined: data handle " + res.DataHandle + " (" + res.Source + ")"
	case res.Text != "" && res.Untrusted:
		// How a coordinator shows its model untrusted content: delimited
		// and attributed, as data (ADR-0269 §SD7).
		r.result = "untrusted, from " + res.Source + ": " + res.Text
	case res.Text != "":
		r.result = res.Text
	default:
		r.result = res.MediaType + " " + res.Path
		if len(res.Data) > 0 {
			r.result = res.MediaType + " · " + strconv.Itoa(len(res.Data)) + " bytes, sealed"
		}
	}
}
