package chat

import (
	"context"
	"encoding/json/v2"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun/projection"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/selector"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/sqleditor"
)

// The window's side of stops (ADR-0370 §SD6, §SD8, proposed): the preset's
// stops, fixed for the window; the person's own, kept for the window across
// its conversations as its Settings are; the Stops tab; and what a stop
// that ends the conversation does to the window.

// StopsSeed gives a new window stops of the person's own (ADR-0009 seed
// variable), as the JSON list the journal keeps them in.
var StopsSeed = env.NewString(env.Spec{
	Name:        "BOXER_CHAT_STOPS",
	Description: `stops a new chat window starts with, as a JSON list such as [{"name":"box","then":"ask","after":"15m"}] (ADR-0370); they are the person's own, shown and removable in the Stops tab; for scenes and demos`,
	Category:    env.CategoryE("boxer-chat"),
})

// seedStops takes the seed's stops into a new window. A seed that does not
// construct is said in the window and gives no stop: the person reads why,
// where a silent drop would read as a conversation that has none.
func (inst *App) seedStops(raw string) {
	if strings.TrimSpace(raw) == "" {
		return
	}
	var specs []Stop
	if err := json.Unmarshal([]byte(raw), &specs); err != nil {
		inst.setNote(StopsSeed.Spec().Name+" is not a JSON list of stops: "+err.Error(), true)
		return
	}
	for _, spec := range specs {
		if err := inst.takeStop(spec); err != nil {
			inst.setNote(StopsSeed.Spec().Name+": "+failureReason(err), true)
			return
		}
	}
}

// takeStop adds spec to the person's stops, refusing one that does not
// construct or whose name is taken.
func (inst *App) takeStop(spec Stop) (err error) {
	s, err := spec.compile(stopOriginPerson)
	if err != nil {
		return
	}
	if err = append(inst.stopsInForce(), s).check(); err != nil {
		return
	}
	inst.stops.own = append(inst.stops.own, s)
	return
}

// stopKindE is the kind the tab's form declares.
type stopKindE uint8

const (
	stopKindAfter stopKindE = iota
	stopKindArtefactBytes
	stopKindArtefactMatch
	stopKindJournalMatch
	stopKindToolMatch
	stopKindSQL
)

// stopKindForm is how the form asks for a kind's value.
type stopKindForm struct {
	kind  stopKindE
	label string
	ask   string
	hint  string
}

var stopKindForms = []stopKindForm{
	{stopKindAfter, "Time", "after", "a duration of the turn's working time, such as 15m; the time you were asked something does not count"},
	{stopKindArtefactBytes, "Artefact size", "bytes", "fires each time the artefact has grown by this many bytes"},
	{stopKindArtefactMatch, "Artefact text", "pattern", "a regular expression (RE2); each match in the artefact fires once"},
	{stopKindJournalMatch, "Reply or result", "pattern", "a regular expression (RE2) over this turn's replies and tool results"},
	{stopKindToolMatch, "Tool call", "tool pattern", "a regular expression (RE2) over the names of the tools the model calls"},
	{stopKindSQL, "SQL", "statement", "one SELECT over journal, toolcalls, artefact and clock; every row it returns is evidence"},
}

// stopsView is the window's stops and the tab's form.
type stopsView struct {
	// preset are the preset's stops, constructed once; own the person's.
	preset stopSet
	own    stopSet
	// The form's fields, bound to its widgets.
	name      string
	value     string
	arguments string
	kind      stopKindE
	then      stopActionE
	// refused is why the last Add was not taken.
	refused string
	// editors are the statement editors of the person's own stops, by name
	// (chat_stops_app.go renderStopStatement); formEd is the form's, for a
	// stop declared as SQL, and formCheck what its draft was last checked as.
	editors   map[string]*stopEditor
	formEd    *sqleditor.Editor
	formCheck stopCheck
	// closeAsked is the reason the window is to ask its host to close it
	// with, set when a stop closed the conversation; empty otherwise.
	closeAsked string
}

// stopsInForce is what the next turn runs under: the preset's stops, then
// the person's.
func (inst *App) stopsInForce() (set stopSet) {
	return append(append(set, inst.stops.preset...), inst.stops.own...)
}

// addStop takes the form as a stop of the person's.
func (inst *App) addStop() {
	v := &inst.stops
	spec := Stop{Name: strings.TrimSpace(v.name), Then: v.then.String()}
	value := strings.TrimSpace(v.value)
	switch v.kind {
	case stopKindAfter:
		spec.After = value
	case stopKindArtefactBytes:
		n, err := strconv.Atoi(value)
		if err != nil || n <= 0 {
			v.refused = "the size is a positive number of bytes"
			return
		}
		spec.ArtefactBytes = n
	case stopKindArtefactMatch:
		spec.ArtefactMatch = value
	case stopKindJournalMatch:
		spec.JournalMatch = value
	case stopKindToolMatch:
		spec.ToolMatch, spec.ArgumentsMatch = value, strings.TrimSpace(v.arguments)
	case stopKindSQL:
		spec.SQL = value
	}
	if err := inst.takeStop(spec); err != nil {
		v.refused = stopRefusal(err)
		return
	}
	v.refused, v.name, v.value, v.arguments = "", "", "", ""
	sm := c.CurrentApplicationState.StateManager
	sm.OverrideDatabindingSPtr(&v.name)
	sm.OverrideDatabindingSPtr(&v.value)
	sm.OverrideDatabindingSPtr(&v.arguments)
}

// removeStop drops one of the person's stops; a preset's is not theirs to
// drop (ADR-0370 §SD6).
func (inst *App) removeStop(name string) {
	inst.stops.own = slices.DeleteFunc(inst.stops.own, func(s stop) bool { return s.spec.Name == name })
	delete(inst.stops.editors, name)
}

// editStop replaces the statement of one of the person's stops. The stop
// keeps its name and action and is from then on declared by its statement,
// whatever kind it was spelled as before; a statement that is not admitted
// leaves it as it was. Like any change, it is in force from the next turn.
func (inst *App) editStop(name string, sql string) (err error) {
	i := slices.IndexFunc(inst.stops.own, func(s stop) bool { return s.spec.Name == name })
	if i < 0 {
		return eb.Build().Str("stop", name).Errorf("stop: not a stop of your own")
	}
	s, err := Stop{Name: name, Then: inst.stops.own[i].spec.Then, SQL: sql}.compile(stopOriginPerson)
	if err != nil {
		return
	}
	inst.stops.own[i] = s
	return
}

// stopCheck remembers whether a draft statement is admitted, so a draft is
// parsed when it changes and not on every frame.
type stopCheck struct {
	sql    string
	reason string
	done   bool
}

// of is why sql is not admitted, empty when it is.
func (inst *stopCheck) of(name string, sql string) string {
	if !inst.done || inst.sql != sql {
		inst.sql, inst.reason, inst.done = sql, "", true
		if err := vetStopSQL(name, sql); err != nil {
			inst.reason = stopRefusal(err)
		}
	}
	return inst.reason
}

// stopRefusal is a construction error as the tab words it.
func stopRefusal(err error) string {
	return strings.TrimPrefix(failureReason(err), "stop: ")
}

// stopEditor is the statement editor of one of the person's stops: the
// draft it is bound to, and the statement in force the draft was taken from.
type stopEditor struct {
	ed    *sqleditor.Editor
	draft string
	base  string
	check stopCheck
	// refused is why the last Apply was not taken.
	refused string
}

// The rows a statement editor shows: its lines, within these.
const (
	stopEditorRowsMin = 3
	stopEditorRowsMax = 14
)

func stopEditorRows(sql string) uint32 {
	return uint32(min(max(strings.Count(sql, "\n")+2, stopEditorRowsMin), stopEditorRowsMax))
}

// adoptStops takes a resumed run's stops: its firings for the tab, and the
// person's own stops as the run last recorded them. A recorded stop this
// build no longer admits is left out, and said.
func (inst *App) adoptStops(coord *coordinator, entries []agentrun.Entry) {
	coord.loadStopEvents(entries)
	rec, _, ok := lastStopsRecord(entries)
	if !ok {
		return
	}
	inst.stops.own = nil
	for _, spec := range rec.Stops {
		if slices.Contains(rec.Preset, spec.Name) {
			continue
		}
		if _, taken := inst.stops.preset.find(spec.Name); taken {
			continue
		}
		s, err := spec.compile(stopOriginPerson)
		if err != nil {
			inst.setNote("resume: the stop "+strconv.Quote(spec.Name)+" is no longer admitted and was left out: "+failureReason(err), true)
			continue
		}
		inst.stops.own = append(inst.stops.own, s)
	}
}

// stopRunnerFor is the worker a window's checks run in: the host's local
// SQL worker over the window's bus, nil without one.
func stopRunnerFor(bus app.BusI) stopRunnerI {
	if bus == nil {
		return nil
	}
	return projection.BrokerRunner{Bus: bus, Pool: stopsPool}
}

// landStops ends the conversation when the turn that just landed was ended
// by a stop that says so (ADR-0370 §SD4): its task is stopped, its run
// closes once the turn's tail is written, and the window asks to be closed
// when the stop closes it.
func (inst *App) landStops(res *turnResult) {
	if res == nil || res.ended < stopActionEnd {
		return
	}
	why := res.endedWhy
	inst.conv.ended = why
	if coord := inst.coord; coord != nil {
		if h := coord.handle(); h != "" && inst.agentCli != nil {
			cli := inst.agentCli
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), agent.DefaultTimeout)
				defer cancel()
				_ = cli.StopWith(ctx, h, agent.StopRequest{Reason: "the conversation was ended by a stop"})
			}()
		}
		coord.retire()
	}
	inst.setNote("this conversation was ended: "+why+". New conversation starts over", true)
	if res.ended == stopActionClose {
		inst.stops.closeAsked = why
	}
}

// syncStopClose asks the host to close the window a stop closed. A pane in
// another app's window, and a host with no window to close, keep it: the
// conversation is ended all the same.
func (inst *App) syncStopClose(ctx app.FrameContextI) {
	why := inst.stops.closeAsked
	if why == "" {
		return
	}
	inst.stops.closeAsked = ""
	if inst.embed != nil {
		return
	}
	// A watched conversation's notice says why its window went.
	inst.turnEnded("closed by a stop: " + clip(why, 60))
	if cl, ok := ctx.(app.WindowCloseI); ok && cl.CloseWindow(clip(why, 200)) {
		return
	}
	inst.setNote("this conversation was ended: "+why+". This host closes no window for the app, so it stays", true)
}

const tipStops = "A stop is a query over this conversation's own record. When it returns rows you have not acknowledged, it holds the turn, ends it, " +
	"ends the conversation or closes the window. The model is not shown the stops, and a stop is no safeguard against a model that means to avoid it: " +
	"what the model may do is in Settings."

// renderStopsTab draws the Stops tab: the stops in force, the form for one
// of the person's own, and what fired.
func (inst *App) renderStopsTab() {
	heading("Stops")
	weak(tipStops)
	v := &inst.stops
	set := inst.stopsInForce()
	if inst.bus == nil {
		weak("This host gives the window no SQL worker: a conversation with a stop is stopped at its first check.")
	}
	section("In force · " + strconv.Itoa(len(set)))
	if len(set) == 0 {
		weak("None. A turn ends with the model's answer, with its rounds, or when you stop it.")
	}
	remove := ""
	for _, s := range set {
		for range c.IdScope(inst.ids.PrepareStr("in-force-" + s.spec.Name)) {
			for range c.HorizontalWrapped().KeepIter() {
				for rt := range c.RichTextLabel(s.spec.Name) {
					rt.Strong()
				}
				for range c.HoverText(s.then.describe()).KeepIter() {
					badge.New(inst.ids.PrepareStr("then"), s.then.String()).Tone(stopTone(s.then)).Variant(badge.VariantSoft).Size(badge.SizeSm).Send()
				}
				if s.origin == stopOriginPreset {
					weak("fixed by " + inst.presetTitle())
				} else if c.Button(inst.ids.PrepareStr("remove"), c.Atoms().Text("Remove").Keep()).Kind(c.ButtonKindGhost).Small().SendResp().HasPrimaryClicked() {
					remove = s.spec.Name
				}
			}
			weak("fires on " + s.what)
			inst.renderStopStatement(s)
		}
	}
	if remove != "" {
		inst.removeStop(remove)
	}

	section("Add a stop of your own")
	weak("In force from the next turn, in this window.")
	// The options wrap: six kinds do not fit the column's width in a row.
	kind := selector.Segmented(inst.ids, "kind", &v.kind).Style(selector.StyleRadio).Inline()
	for _, f := range stopKindForms {
		kind = kind.Option(f.kind, f.label)
	}
	for range c.HorizontalWrapped().KeepIter() {
		kind.Send()
	}
	form := stopKindForms[int(v.kind)%len(stopKindForms)]
	weak(form.hint)
	// A statement is written in the SQL editor, at the column's width; the
	// simple view keeps the plain field.
	inEditor := v.kind == stopKindSQL && inst.stopEditorsShown()
	for range c.Grid(inst.ids.PrepareStr("form")).NumColumns(2).KeepIter() {
		c.Label("name").Selectable(false).Send()
		c.TextEdit(inst.ids.PrepareStr("name"), v.name, false).SendRespVal(&v.name)
		c.EndRow()
		if !inEditor {
			c.Label(form.ask).Selectable(false).Send()
			c.TextEdit(inst.ids.PrepareStr("value"), v.value, v.kind == stopKindSQL).SendRespVal(&v.value)
			c.EndRow()
		}
		if v.kind == stopKindToolMatch {
			c.Label("arguments pattern").Selectable(false).Send()
			c.TextEdit(inst.ids.PrepareStr("arguments"), v.arguments, false).SendRespVal(&v.arguments)
			c.EndRow()
		}
	}
	if inEditor {
		if v.formEd == nil {
			v.formEd = sqleditor.New(inst.ids, "form-statement")
		}
		v.formEd.Bind(sqleditor.Frame{Value: &v.value, Rows: stopEditorRows(v.value) + 2, Hint: "SELECT … FROM journal WHERE …"})
		v.formEd.Render(sqleditor.Decoration{})
		if draft := cleanStopSQL(v.value); draft != "" {
			if reason := v.formCheck.of("new", draft); reason != "" {
				weak("not admitted as it stands: " + reason)
			}
		}
	}
	if v.then == stopActionNone {
		v.then = stopActionAsk
	}
	then := selector.Segmented(inst.ids, "then", &v.then).Style(selector.StyleRadio).Inline()
	for _, a := range allStopActions {
		if a == stopActionClose && inst.embed != nil {
			// The window is the host's.
			continue
		}
		then = then.Option(a, a.String())
	}
	for range c.HorizontalWrapped().KeepIter() {
		then.Send()
	}
	weak(v.then.String() + ": " + v.then.describe())
	if c.Button(inst.ids.PrepareStr("add"), c.Atoms().Text("Add stop").Keep()).SendResp().HasPrimaryClicked() {
		inst.addStop()
	}
	if v.refused != "" {
		for rt := range c.RichTextLabel("not added: " + v.refused) {
			rt.Small()
		}
	}

	var events []stopEvent
	if inst.coord != nil {
		events = inst.coord.stopEvents()
	}
	section("Fired in this conversation · " + strconv.Itoa(len(events)))
	if len(events) == 0 {
		weak("Nothing yet.")
	}
	for i := len(events) - 1; i >= 0; i-- {
		ev := events[i]
		for range c.IdScope(inst.ids.PrepareStr("fired-" + ev.key + "-" + ev.firing.Stop)) {
			for range c.HorizontalWrapped().KeepIter() {
				weak(ev.at.Format(time.TimeOnly))
				for rt := range c.RichTextLabel(ev.firing.Stop) {
					rt.Strong()
				}
				then, _ := parseStopAction(ev.firing.Then)
				badge.New(inst.ids.PrepareStr("then"), ev.firing.Then).Tone(stopTone(then)).Variant(badge.VariantSoft).Size(badge.SizeSm).Send()
				weak(stopOutcome(ev))
			}
			switch {
			case ev.firing.Error != "":
				c.Label("could not be checked: " + ev.firing.Error).Wrap().Send()
			default:
				for _, row := range ev.firing.Shown {
					for rt := range c.RichTextLabel(row) {
						rt.Monospace().Small()
					}
				}
				if ev.firing.More > 0 {
					weak("and " + plural(ev.firing.More, "more row"))
				}
			}
		}
	}
}

// stopEditorsShown says the statements are drawn in SQL editors: outside the
// Simple view, and while the tab is in front. A tab behind another is built
// every frame and drawn on none, and an editor colours its buffer when it is
// built, so a hidden tab gets the plain text.
func (inst *App) stopEditorsShown() bool {
	return !inst.simple && inst.tabShown(tabStops)
}

// renderStopStatement draws a stop's statement. In the Simple view it is
// folded text. Otherwise a stop of the person's own is an SQL editor over a
// draft: an edit is not in force until Apply takes it, which a statement
// that is not admitted cannot be. A preset's stop is not the person's to
// change (ADR-0370 §SD6), so its statement is coloured and read-only.
func (inst *App) renderStopStatement(s stop) {
	if !inst.stopEditorsShown() {
		for range c.CollapsingHeader(inst.ids.PrepareStr("statement"), c.WidgetText().Text("Statement").Keep()).KeepIter() {
			for rt := range c.RichTextLabel(s.sql) {
				rt.Monospace().Small()
			}
		}
		return
	}
	if s.origin == stopOriginPreset {
		c.CodeView(inst.ids.PrepareStr("statement"), codeview.PrepareSql(s.sql)).Send()
		return
	}
	v := &inst.stops
	e := v.editors[s.spec.Name]
	if e == nil {
		if v.editors == nil {
			v.editors = make(map[string]*stopEditor)
		}
		e = &stopEditor{ed: sqleditor.New(inst.ids, "statement"), draft: s.sql, base: s.sql}
		v.editors[s.spec.Name] = e
	}
	sm := c.CurrentApplicationState.StateManager
	if e.base != s.sql {
		// The stop's statement changed under the editor: a resume, a seed.
		e.draft, e.base, e.refused = s.sql, s.sql, ""
		sm.OverrideDatabindingSPtr(&e.draft)
	}
	e.ed.Bind(sqleditor.Frame{Value: &e.draft, Rows: stopEditorRows(e.draft)})
	e.ed.Render(sqleditor.Decoration{})
	draft := cleanStopSQL(e.draft)
	if draft == s.sql {
		return
	}
	reason := e.check.of(s.spec.Name, draft)
	apply, revert := false, false
	for range c.HorizontalWrapped().KeepIter() {
		if reason == "" {
			apply = c.Button(inst.ids.PrepareStr("apply"), c.Atoms().Text("Apply").Keep()).Small().SendResp().HasPrimaryClicked()
		}
		revert = c.Button(inst.ids.PrepareStr("revert"), c.Atoms().Text("Revert").Keep()).Kind(c.ButtonKindGhost).Small().SendResp().HasPrimaryClicked()
		weak("edited; the statement in force is the one before your edit")
	}
	switch {
	case reason != "":
		weak("not admitted as it stands: " + reason)
	case e.refused != "":
		weak("not applied: " + e.refused)
	}
	switch {
	case revert:
		e.draft, e.refused = s.sql, ""
		sm.OverrideDatabindingSPtr(&e.draft)
	case apply:
		if err := inst.editStop(s.spec.Name, draft); err != nil {
			e.refused = stopRefusal(err)
			return
		}
		// The draft is the statement now, as typed.
		e.base, e.refused = draft, ""
	}
}

// stopOutcome is what became of a firing, in a few words.
func stopOutcome(ev stopEvent) string {
	where := "before a request"
	if ev.firing.Point == stopPointReply {
		where = "after a reply"
	}
	switch {
	case ev.noted:
		return where + " · noted with the turn's answer"
	case ev.answer == stopFormContinue:
		return where + " · you continued"
	case ev.answer == stopFormDeclined:
		return where + " · you ended the turn"
	}
	if then, _ := parseStopAction(ev.firing.Then); then.holds() {
		return where + " · waiting for you"
	}
	return where
}

// stopTone colours an action by how much it ends.
func stopTone(a stopActionE) badge.ToneE {
	switch a {
	case stopActionPause, stopActionAsk:
		return badge.ToneWarning
	case stopActionStop:
		return badge.ToneNeutral
	case stopActionEnd, stopActionClose:
		return badge.ToneError
	}
	return badge.ToneNeutral
}

// presetTitle names the preset a fixed stop came from.
func (inst *App) presetTitle() string {
	if inst.preset == nil {
		return "the window"
	}
	return inst.preset.Manifest.Title
}

// stopsTitle is the tab's title, with the count of stops in force.
func (inst *App) stopsTitle() string {
	if n := len(inst.stops.preset) + len(inst.stops.own); n > 0 {
		return tabStops.title() + " · " + strconv.Itoa(n)
	}
	return tabStops.title()
}
