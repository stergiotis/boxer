package play

// get_state's reading (ADR-0270 §SD1, widened by ADR-0274 §SD3): the
// parameters as the parameter block's widgets know them, the signals as the
// Signals section lists them, and the main result with what the status line
// says in place of its summary. Built on the render goroutine in
// snapshotPlay.

import (
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// paramWidgetKind names a widget the way get_state reports it.
func paramWidgetKind(w paramWidgetI) (kind string) {
	switch w.(type) {
	case *dateTimeRangeWidget:
		return "range"
	case *dateTimePairWidget:
		return "datetime pair"
	case *enumWidget:
		return "enum"
	case *exprWidget:
		return "expr"
	}
	return "text"
}

// paramControls is what the parameter block would draw for each slot now:
// the claim per slot name, the enum lists, and the block's note.
type paramControls struct {
	widget map[string]string
	with   map[string]string
	enums  map[string][]enumOption
	note   string
}

// paramControlsNow dispatches the slots as the block does, without drawing.
func (inst *PlayApp) paramControlsNow() (out paramControls) {
	slots := inst.paramSlots
	out.widget, out.with = make(map[string]string, len(slots)), make(map[string]string, 2)
	if len(slots) == 0 {
		return
	}
	enums, exprs := inst.setParamWidgetHints()
	out.enums = enums
	ungroup := scanUngroupHint(inst.sql)
	halfPinned, mixed := inst.mixedTierRangeHalves(slots)
	claims, grouped := inst.dispatchParamClaims(slots, ungroup, halfPinned)
	for _, cl := range claims {
		kind := paramWidgetKind(cl.widget)
		for _, s := range cl.slots {
			out.widget[s.Name] = kind
		}
		if len(cl.slots) == 2 {
			out.with[cl.slots[0].Name], out.with[cl.slots[1].Name] = cl.slots[1].Name, cl.slots[0].Name
		}
	}
	out.note = paramNearMissNote(slots, grouped, ungroup, mixed, orphanEnumHints(enums, slots), orphanExprHints(exprs, slots))
	return
}

// snapshotState is get_state's reading.
func snapshotState(p *PlayApp) (st PlayState) {
	st = PlayState{Sql: p.sql, Live: p.liveMain}
	if p.client != nil {
		st.Destination = DestinationClickHouse(endpointHost(p.client.URL()))
	}
	if slug, ok := p.tabs.slugForDockID(p.raisedTab); ok {
		st.RaisedPane = slug
	}
	ctl := p.paramControlsNow()
	st.ParamNote = ctl.note
	unfilled := p.unfilledSet()
	for _, slot := range p.paramSlots {
		ps := ParamState{Name: slot.Name, Type: slot.Type, Tier: "live", Widget: ctl.widget[slot.Name], With: ctl.with[slot.Name],
			Unfilled: unfilled[slot.Name]}
		if ps.Widget == "" {
			ps.Widget = "text"
		}
		if p.paramPinned(slot.Name) {
			ps.Tier = "pinned"
		}
		if d := p.paramDrafts[slot.Name]; d != nil {
			ps.Value = *d
		}
		if ps.Widget == "enum" {
			for _, o := range ctl.enums[slot.Name] {
				opt := ParamOption{Value: o.Value}
				if o.Label != o.Value {
					opt.Label = o.Label
				}
				ps.Options = append(ps.Options, opt)
			}
		}
		if def, ok := p.paramDefaults[slot.Name]; ok {
			ps.Default = &def
			ps.Moved = ps.Value != def
		}
		st.Params = append(st.Params, ps)
	}
	st.Signals = signalStates(p)
	rec, _, numRows, loading, _, _, executed, runErr, id := p.graph.MainSnapshot()
	if rec != nil {
		rec.Release()
	}
	phase := p.observeQueryState(loading, numRows, executed, runErr)
	st.Result = ResultState{Id: uint64(id), Phase: phase.String(), Rows: numRows,
		Notice: p.queryNotice(), Truncated: p.graph.MainTruncation()}
	if runErr != nil {
		st.Result.Error = runErr.Error()
	}
	if phase == queryStateRunning && p.frameProgress.fresh {
		st.Result.Progress = formatProgressLine(p.frameProgress)
	}
	if _, number, total := p.runBuffer(); total > 1 {
		st.Statements, st.RunStatement = int32(total), int32(number)
	}
	if obo, running := p.graph.mainLane.RunningAgent(); running {
		st.Result.RunBy = "person"
		if obo != nil {
			st.Result.RunBy = "task:" + obo.Task
		}
	}
	st.Conditions = p.conditionsState()
	return
}

// signalStates are the Signals section's rows as get_state reports them.
func signalStates(p *PlayApp) (out []SignalState) {
	referenced := make(map[string]bool, len(p.paramSlots))
	for _, s := range p.paramSlots {
		referenced[s.Name] = true
	}
	for _, r := range p.collectSignalChrome() {
		ss := SignalState{Name: r.Name, Value: r.Raw, Writer: r.Writer, Held: r.Held, Read: referenced[r.Name],
			Types: slices.Clone(r.Types), Conflict: r.Conflict, Pinned: r.Pinned, Unfilled: r.Unfilled,
			Lags: r.Lags, Revision: r.Rev}
		if decl, ok := reservedSignalIndex[SignalID(r.Name)]; ok {
			ss.Pane = decl.Owner
		}
		ss.Refused = paneOutputRefusal(SignalID(r.Name))
		out = append(out, ss)
	}
	return
}

// refuseParamValue is set_param's check against the slot's control
// (ADR-0274 §SD3): an enum takes one of its options. The person's field is
// not checked; a dropdown cannot produce anything else.
func (inst *PlayApp) refuseParamValue(name string, value string) (err error) {
	ctl := inst.paramControlsNow()
	if ctl.widget[name] != "enum" {
		return
	}
	opts := ctl.enums[name]
	values := make([]string, 0, len(opts))
	for _, o := range opts {
		if o.Value == value {
			return
		}
		v := "'" + o.Value + "'"
		if o.Label != o.Value {
			v += " (" + o.Label + ")"
		}
		values = append(values, v)
	}
	return app.RefuseOperation("parameter " + name + " is an enum; its values are " + strings.Join(values, ", "))
}
