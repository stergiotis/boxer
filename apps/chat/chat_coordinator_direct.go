package chat

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// Direct tools: an operation an app declares direct (app.OperationSpec.Direct)
// is a tool of its own in every request of a conversation with Apps on,
// named <app alias>_<operation>, whether or not a window of the app is
// open. A call to it gets what the operation needs on its way — access to
// the app, as request_access would ask for it, and a ready window — and then
// is the call call_operation makes.

// DirectToolsEnv offers the apps' direct operations as tools of their own.
var DirectToolsEnv = env.NewBool(env.Spec{
	Name:        "BOXER_CHAT_DIRECT_TOOLS",
	Default:     "true",
	Description: "offer each operation an app marks direct as a tool of its own, <app alias>_<operation>, in every request of a chat with Apps on, whether or not a window of the app is open; a call asks for access to the app and opens a window of it when the task has neither",
	Category:    env.CategoryE("boxer-chat"),
})

// maxDirectTools bounds the direct tools a conversation offers, in the
// order describe_app lists the apps.
const maxDirectTools = 8

// directOpenWait bounds how long a direct call waits, beyond the host's own
// wait, for a window of its app to become ready.
const directOpenWait = 15 * time.Second

// directPoll is how often a direct call looks at a window still opening.
const directPoll = 200 * time.Millisecond

// directOp is the operation a direct tool stands for.
type directOp struct {
	name    string
	app     string
	display string
	op      string
	effect  string
	// titled, reasoned and windowed say the tool's schema added title,
	// reason and window beside the operation's arguments: false when the
	// operation has an argument of that name, which then stays its own.
	titled   bool
	reasoned bool
	windowed bool
	tool     openaichat.Tool
}

// directPlace is where a direct call ran: the window, whether the call
// opened it, and the access the call was granted on its way.
type directPlace struct {
	window uint64
	opened bool
	access string
}

// typedToolName matches the names of the operation tools of a window.
var typedToolName = regexp.MustCompile(`^w[0-9]+_`)

// directToolName names the direct tool of an app's operation; empty when
// the name has the shape of a window's operation tool. A name one of the
// coordinator's own tools holds is left out where the tools are listed.
func directToolName(appId string, op string) (name string) {
	name = toolNameUnsafe.ReplaceAllString(app.AliasOf(app.AppIdT(appId))+"_"+op, "_")
	if len(name) > 64 {
		name = name[:64]
	}
	if typedToolName.MatchString(name) {
		return ""
	}
	return
}

// directOps are the conversation's direct tools: read from describe_app once
// and kept, so the tool list stays the same from round to round. Empty when
// direct tools are off.
func (inst *coordinator) directOps(ctx context.Context) (ops []directOp) {
	inst.mu.Lock()
	on, built := inst.direct, inst.directsBuilt
	ops = inst.directs
	inst.mu.Unlock()
	if !on || built {
		return
	}
	ops, complete := inst.buildDirectOps(ctx)
	if !complete {
		// An answer that did not come is asked for again next round, rather
		// than leave the tool out for the conversation's life.
		return
	}
	inst.mu.Lock()
	inst.directs, inst.directsBuilt = ops, true
	inst.mu.Unlock()
	return
}

// buildDirectOps reads the direct operations from the apps' catalogs, with
// their schemas; complete is false when an answer did not come.
func (inst *coordinator) buildDirectOps(ctx context.Context) (ops []directOp, complete bool) {
	apps, err := inst.cli.Describe(ctx, agent.DescribeRequest{})
	if err != nil {
		return nil, false
	}
	complete = true
	taken := make(map[string]bool, maxDirectTools)
	for _, a := range apps {
		for _, o := range a.Operations {
			if !o.Direct {
				continue
			}
			name := directToolName(a.App, o.Name)
			if name == "" || taken[name] {
				log.Warn().Str("app", a.App).Str("operation", o.Name).Msg("chat: a direct operation's tool name is taken; it is not offered")
				continue
			}
			if len(ops) == maxDirectTools {
				log.Warn().Str("app", a.App).Str("operation", o.Name).Int("max", maxDirectTools).Msg("chat: more direct operations than the chat offers; the rest are reached through call_operation")
				return
			}
			full, ferr := inst.cli.Describe(ctx, agent.DescribeRequest{App: a.App, Operation: o.Name})
			if ferr != nil || len(full) == 0 || len(full[0].Operations) == 0 {
				complete = false
				continue
			}
			fo := full[0].Operations[0]
			ops = append(ops, directOpOf(name, a, fo))
			taken[name] = true
		}
	}
	inst.noteEffects(apps)
	return
}

// directOpOf is the direct tool of one operation, fo with its schema.
func directOpOf(name string, a agent.AppOperations, fo agent.Operation) (d directOp) {
	d = directOp{name: name, app: a.App, display: a.Display, op: fo.Name, effect: fo.Effect}
	params, added := withPropertiesAdded(fo.ArgsSchema, map[string]any{
		"reason": map[string]any{"type": "string", "description": "one line, shown to the person; when the call has to ask for access to the app, the plan the person decides on"},
		"title":  titleProperty,
		"window": map[string]any{"type": "integer", "description": "a window of this app in your task to run in; leave it out to use your task's window of the app, which the call opens when there is none"}})
	d.titled, d.reasoned, d.windowed = added["title"], added["reason"], added["window"]
	desc := a.Display + ": " + strings.TrimSuffix(fo.Summary, ".") + "."
	if a.WhenToUse != "" {
		desc += " " + a.WhenToUse
	}
	desc += " Call it without request_access or open_window first: when your task cannot use " + a.Display +
		" yet, the call asks for access and opens a window of it; later calls reuse that window. The answer names the window, where call_operation reaches the app's other operations."
	for _, line := range followsWithin(fo.Follows, maxDirectFollowsRunes) {
		desc += "\n- " + line
	}
	d.tool = openaichat.Tool{Name: name, Description: desc, Parameters: params}
	return
}

// maxDirectFollowsRunes bounds the operation's Follows lines a direct
// tool's description carries: the description is in every request.
const maxDirectFollowsRunes = 1600

// followsWithin is the leading lines of follows that fit in limit runes
// together; a line that does not fit ends the list.
func followsWithin(follows []string, limit int) (out []string) {
	n := 0
	for _, line := range follows {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		n += utf8.RuneCountInString(line)
		if n > limit {
			return
		}
		out = append(out, line)
	}
	return
}

// directOf is the direct tool named name, when direct tools are on.
func (inst *coordinator) directOf(name string) (d directOp, ok bool) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return inst.directOfLocked(name)
}

// directOfLocked is directOf for a caller holding mu.
func (inst *coordinator) directOfLocked(name string) (d directOp, ok bool) {
	if !inst.direct {
		return
	}
	for _, x := range inst.directs {
		if x.name == name {
			return x, true
		}
	}
	return
}

// directToolsOf are the direct tools of an app, by name, for the apps note
// and describe_app.
func directToolsOf(ops []directOp, appId string) (names []string) {
	for _, d := range ops {
		if d.app == appId {
			names = append(names, d.name)
		}
	}
	return
}

// directCall runs a direct tool: it finds or opens a ready window of the
// app, asking for access on its way when the task has none, and then calls
// the operation there as call_operation does.
func (inst *coordinator) directCall(ctx context.Context, o toolOrigin, toolCall string, d directOp, args map[string]any) (content string, activity string) {
	var reason string
	var want uint64
	var wantSet bool
	opArgs := make(map[string]any, len(args))
	for k, v := range args {
		switch {
		case d.reasoned && k == "reason":
			reason, _ = v.(string)
		case d.windowed && k == "window":
			if f, isNum := v.(float64); isNum && f >= 0 {
				want, wantSet = uint64(f), true
			}
		default:
			opArgs[k] = v
		}
	}
	place, content, activity := inst.directWindow(ctx, o, toolCall, d, reason, want, wantSet)
	if content != "" {
		return
	}
	inst.mu.Lock()
	inst.directWindows[o.key()] = place.window
	inst.directLast[d.app] = place.window
	inst.mu.Unlock()
	content, activity = inst.callWith(ctx, o, toolCall, map[string]any{"window": float64(place.window), "operation": d.op, "args": opArgs, "reason": reason}, &place)
	if place.opened {
		activity = "opened " + d.display + " as window " + strconv.FormatUint(place.window, 10) + " · " + activity
	}
	return
}

// directWindow is the ready window a direct call runs in: the one window
// names, else the task's window of the app, else one it opens, asking for
// access first when the task may not open the app. content is set when
// there is none, and is the call's answer.
func (inst *coordinator) directWindow(ctx context.Context, o toolOrigin, toolCall string, d directOp, reason string, want uint64, wantSet bool) (p directPlace, content string, activity string) {
	refuse := func(why string) (directPlace, string, string) {
		inst.refuse(why)
		return directPlace{}, "error: " + why, d.name + ": " + why
	}
	h := inst.handle()
	if h != "" {
		windows, err := inst.cli.List(ctx, h)
		switch {
		case err != nil && agent.TaskGone(err.Error()):
			// The task ended: the call asks for a new one below.
			inst.dropGrant()
			h = ""
		case err != nil:
			return refuse(err.Error())
		default:
			for _, w := range windows {
				inst.noteWindowApp(w.Instance, w.App)
			}
			if wantSet {
				w, found := windowIn(windows, want)
				if !found || w.App != d.app {
					return refuse("window " + strconv.FormatUint(want, 10) + " is not a window of " + d.display + " in your task; leave window out to use your task's window of it, or one the call opens")
				}
				return inst.awaitReady(ctx, h, d, w, directPlace{window: w.Instance})
			}
			inst.mu.Lock()
			last := inst.directLast[d.app]
			inst.mu.Unlock()
			if w, found := pickWindow(windows, d.app, last); found {
				return inst.awaitReady(ctx, h, d, w, directPlace{window: w.Instance})
			}
		}
	}
	if wantSet {
		return refuse("your task holds no window " + strconv.FormatUint(want, 10) + "; leave window out and the call opens one of " + d.display)
	}
	if h != "" {
		got, err := inst.directLaunch(ctx, o, toolCall, h, d, "/open")
		if err == nil {
			return inst.launchedPlace(ctx, h, d, got, "")
		}
		if !launchNeedsAccess(err.Error()) {
			return refuse(err.Error())
		}
	}
	plan := strings.TrimSpace(reason)
	if plan == "" {
		plan = "use " + d.display + " (" + d.op + ")"
	}
	asked := inst.asked(o, toolCall)
	asked.Key += "/access"
	granted, how := inst.requestAccess(ctx, asked, plan, map[string]any{"open": []any{map[string]any{"app": d.app}}})
	if !strings.HasPrefix(granted, "access granted") {
		return directPlace{}, granted, how
	}
	if err := inst.journalGrant(ctx, o.turn); err != nil {
		log.Warn().Err(err).Str("conversation", inst.conversation).Msg("chat: the grant was not journaled")
	}
	access := inst.accessLine()
	h = inst.handle()
	got, err := inst.directLaunch(ctx, o, toolCall, h, d, "/open/granted")
	if err != nil {
		inst.refuse(err.Error())
		return directPlace{}, "error: access was granted, and the window did not open: " + err.Error(), d.name + ": " + err.Error()
	}
	return inst.launchedPlace(ctx, h, d, got, access)
}

// directLaunch opens a window of the direct tool's app for the call, under
// a key beneath the call's.
func (inst *coordinator) directLaunch(ctx context.Context, o toolOrigin, toolCall string, h string, d directOp, sub string) (got agent.Launched, err error) {
	inst.mu.Lock()
	tier := inst.curTier
	inst.mu.Unlock()
	return inst.cli.LaunchFrom(ctx, agent.CallRequest{Handle: h, Key: o.key() + sub, Turn: o.turn, ModelCall: o.modelCall,
		ToolCall: toolCall, ToolIndex: uint32(max(o.index, 0)), Tier: uint8(tier)}, d.app, "", nil)
}

// launchedPlace waits for a window the call opened to become ready.
func (inst *coordinator) launchedPlace(ctx context.Context, h string, d directOp, got agent.Launched, access string) (p directPlace, content string, activity string) {
	inst.noteWindowApp(got.Instance, d.app)
	p = directPlace{window: got.Instance, opened: true, access: access}
	if got.Load == "failed" {
		c, a := inst.launched(d.display, got)
		why := "window " + strconv.FormatUint(got.Instance, 10) + " of " + d.display + " failed to open"
		inst.refuse(why)
		return directPlace{}, "error: " + why + ": " + c, a
	}
	return inst.awaitReady(ctx, h, d, agent.Instance{Instance: got.Instance, App: d.app, Load: got.Load, LoadReason: got.LoadReason, Ops: true}, p)
}

// awaitReady waits, at most directOpenWait, for window w to be ready to
// take calls.
func (inst *coordinator) awaitReady(ctx context.Context, h string, d directOp, w agent.Instance, p directPlace) (out directPlace, content string, activity string) {
	where := "window " + strconv.FormatUint(w.Instance, 10) + " of " + d.display
	deadline := time.Now().Add(directOpenWait)
	for w.Load == "opening" && time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return directPlace{}, "error: " + ctx.Err().Error(), d.name + ": " + ctx.Err().Error()
		case <-time.After(directPoll):
		}
		windows, err := inst.cli.List(ctx, h)
		if err != nil {
			inst.refuse(err.Error())
			return directPlace{}, "error: " + err.Error(), d.name + ": " + err.Error()
		}
		var found bool
		if w, found = windowIn(windows, w.Instance); !found {
			why := where + " closed while it was opening"
			inst.refuse(why)
			return directPlace{}, "error: " + why, d.name + ": " + why
		}
	}
	var why string
	switch {
	case w.Load == "opening":
		why = where + " is still opening; call again in a moment and it is used"
	case w.Load == "failed":
		why = where + " failed to open; leave window out and the call opens another"
		if w.LoadReason != "" {
			inst.markTainted()
			why += ": " + wrapUntrusted("the app's mount error", w.LoadReason)
		}
	case !w.Ops:
		why = where + " takes no operations"
	}
	if why != "" {
		inst.refuse(why)
		return directPlace{}, "error: " + why, d.name + ": " + why
	}
	return p, "", ""
}

// windowIn finds window key in a listing.
func windowIn(windows []agent.Instance, key uint64) (w agent.Instance, found bool) {
	for _, x := range windows {
		if x.Instance == key {
			return x, true
		}
	}
	return
}

// pickWindow is the task's window of an app a direct call runs in: the one
// the last direct call into the app used, else the first ready one, else
// the first still opening. A window that failed is not picked.
func pickWindow(windows []agent.Instance, appId string, last uint64) (w agent.Instance, found bool) {
	var opening agent.Instance
	var anyOpening bool
	for _, x := range windows {
		if x.App != appId || x.Load == "failed" {
			continue
		}
		if x.Instance == last {
			return x, true
		}
		switch {
		case x.Load == "opening" && !anyOpening:
			opening, anyOpening = x, true
		case x.Load != "opening" && !found:
			w, found = x, true
		}
	}
	if found {
		return
	}
	return opening, anyOpening
}

// launchNeedsAccess reports whether a refused launch would go through once
// the grant names the app again: it does not name it, or its windows of it
// are spent.
func launchNeedsAccess(reason string) bool {
	return strings.Contains(reason, "does not let the task open windows of") || strings.Contains(reason, "opened every window of")
}

// accessLine is what a direct call's answer says of the access it was
// granted: the task's bounds, and on an unattended host the note on it.
func (inst *coordinator) accessLine() (line string) {
	inst.mu.Lock()
	g := inst.grant
	inst.mu.Unlock()
	line = "granted for task " + g.Task + ": " + strings.TrimSpace(grantTermsLine(g.Terms, time.Now()))
	if g.Unattended {
		line += ". " + unattendedNote
	}
	return strings.TrimSpace(line)
}

// directEffect is the declared effect of a direct tool's operation;
// consequential when the catalog declared none, the conservative reading.
func directEffect(d directOp) (effect string) {
	if d.effect == "" {
		return agentrun.EffectConsequential
	}
	return d.effect
}
