package widgets

import (
	"encoding/json"
	"fmt"

	"github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/dql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshallgen"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/mappingplanview"
)

// mappingPlanViewDemoState holds the playground's editable model plus the
// anchor example schema's IR, which the dql read-back generator joins each
// edited plan against to produce the SQL artefact panes.
type mappingPlanViewDemoState struct {
	model *mappingplanview.Model
	ir    *dql.InformationRetrieval // anchor example schema; nil if it failed to load
	irErr error
}

// newMappingPlanViewState seeds the playground with the DroneMission DTO
// (anchor/codecdemo/dronemission.go) and loads the anchor example schema so the
// SQL read-back panes have a physical schema to generate against.
func newMappingPlanViewState() *mappingPlanViewDemoState {
	m := mappingplanview.NewModel("droneMission", "codecdemo", "DroneMission")

	id := m.AddRow()
	id.GoField, id.Section = "ID", "id"
	id.SetGoType("uint64")

	track := m.AddRow()
	track.GoField, track.Section = "Tracking", "naturalKey"
	track.SetGoType("[]byte")

	status := m.AddRow()
	status.GoField = "Status"
	status.SetGoType("string")
	status.Membership, status.Section = "droneStatus", "symbol"

	battery := m.AddRow()
	battery.GoField = "Battery"
	battery.SetGoType("uint64")
	battery.Membership, battery.Section = "battery", "u64Array"
	battery.Unit = true

	st := &mappingPlanViewDemoState{model: m}
	st.ir, st.irErr = buildAnchorIR()
	// Populate the output panes once up front so the dock's initial split (on
	// the widget's first frame) has the output tab ids to place on the right.
	st.recompute(m)
	return st
}

// buildAnchorIR loads the anchor example schema into a dql InformationRetrieval
// — the physical schema the read-back generator joins plans against. All public
// API; mirrors dql's round-trip test. The SQL panes are bound to this schema,
// so plan fields targeting sections it doesn't define surface a generation
// error in the SQL tab rather than SQL.
func buildAnchorIR() (*dql.InformationRetrieval, error) {
	manip, err := anchor.GetSchemaInManipulator()
	if err != nil {
		return nil, err
	}
	tblDesc, err := manip.BuildTableDesc()
	if err != nil {
		return nil, err
	}
	conv, err := ddl.NewHumanReadableNamingConvention(":")
	if err != nil {
		return nil, err
	}
	ir := common.NewIntermediateTableRepresentation()
	if err = ir.LoadFromTable(&tblDesc, clickhouse.NewTechnologySpecificCodeGenerator()); err != nil {
		return nil, err
	}
	info := dql.NewInformationRetrieval(conv)
	if err = info.LoadTable(ir, anchor.TableRowConfig); err != nil {
		return nil, err
	}
	return info, nil
}

// idLookup is a dql.IdLookup backed by a name→id map built from the plan, so
// every ref membership resolves to a stable (illustrative) id embedded in the
// generated SQL.
type idLookup map[string]uint64

func (l idLookup) LookupMembership(name string) (uint64, error) {
	if id, ok := l[name]; ok {
		return id, nil
	}
	return 0, fmt.Errorf("membership %q not in lookup", name)
}

// recompute rebuilds the plan from the model and produces the dock's output
// panes: the Go codec (marshallgen), the parsed Plan IR (JSON), and the dql SQL
// read-back artefacts. This host-injected Recompute runs on every edit; keeping
// it here confines the marshallgen/dql back-ends to the demo, not the widget.
func (st *mappingPlanViewDemoState) recompute(m *mappingplanview.Model) {
	b := mappingplan.NewPlanBuilder("playground", m.PackageName, m.KindType)
	// The kind `_` field never errors at add-time (an empty/odd kind is caught
	// at Finish, surfacing as a plan-level FinishErr below), so the per-field
	// pass can always run and report real per-field verdicts.
	_ = b.AddUnderscoreField(m.Kind, "", "")

	// Sequential per-field pass — the input the widget's per-field state
	// machines derive from. PlanBuilder is fail-fast and stateful: record the
	// first rejection (index + error) and stop; every later field is unreached
	// (the widget marks them Blocked).
	br := mappingplanview.BuildResult{FirstFailIdx: -1}
	for i, r := range m.Fields {
		var err error
		if r.IsConst {
			err = b.AddUnderscoreField("", "", r.LWTag())
		} else {
			err = b.AddField(r.GoField, r.LWTag(), r.Shape())
		}
		if err != nil {
			br.FirstFailIdx, br.FirstFailErr = i, err
			break
		}
	}

	// Finish (cross-field checks) only when every AddField was accepted.
	var plan *mappingplan.Plan
	if br.FirstFailIdx < 0 {
		var finishErr error
		plan, finishErr = b.Finish()
		br.FinishErr = finishErr
	}
	m.SetBuildResult(br)

	// Global verdict + output panes. A per-field rejection, a plan-level Finish
	// error, or an emit error all clear the panes and headline the verdict.
	switch {
	case br.FirstFailIdx >= 0:
		m.SetInvalid(br.FirstFailErr)
		return
	case br.FinishErr != nil:
		m.SetInvalid(br.FinishErr)
		return
	}
	goSrc, err := marshallgen.EmitPlan(plan, marshallgen.NoOpWrapper{})
	if err != nil {
		m.SetInvalid(err)
		return
	}
	planJSON, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		m.SetInvalid(err)
		return
	}

	outs := []mappingplanview.Output{
		{TabID: 1, Title: "Go codec", Lang: mappingplanview.LangGo, Source: string(goSrc)},
		{TabID: 2, Title: "Plan IR", Lang: mappingplanview.LangJSON, Source: string(planJSON)},
	}
	outs = append(outs, st.sqlOutputs(plan)...)
	m.SetOutputs(outs...)
}

// sqlOutputs runs the dql read-back generator against the anchor IR and returns
// the SQL artefact panes (presence / projection / validator). The membership
// lookup is built from the plan so every ref membership resolves to a stable
// id. A generation error — e.g. a section the anchor schema doesn't define, or
// a missing IR — becomes a single explanatory SQL pane rather than failing the
// whole plan (the Go + JSON panes still render).
func (st *mappingPlanViewDemoState) sqlOutputs(plan *mappingplan.Plan) []mappingplanview.Output {
	if st.ir == nil {
		msg := "-- SQL read-back preview unavailable: anchor example schema failed to load."
		if st.irErr != nil {
			msg += "\n--   " + st.irErr.Error()
		}
		return []mappingplanview.Output{{TabID: 3, Title: "SQL", Lang: mappingplanview.LangSQL, Source: msg}}
	}

	lookup := idLookup{}
	var next uint64 = 1
	for _, f := range plan.Fields {
		if f.LWMembership == "" {
			continue
		}
		if _, ok := lookup[f.LWMembership]; !ok {
			lookup[f.LWMembership] = next
			next++
		}
	}

	a, err := dql.NewGenerator(st.ir, dql.NewLookupResolver(lookup)).Generate(plan)
	if err != nil {
		return []mappingplanview.Output{{
			TabID: 3, Title: "SQL", Lang: mappingplanview.LangSQL,
			Source: "-- SQL read-back preview is bound to the anchor example schema\n" +
				"-- and is unavailable for this plan:\n--   " + err.Error(),
		}}
	}
	// Present each artefact as a runnable example query rather than a bare
	// fragment: it doubles as usage docs (where the fragment embeds) AND lets
	// the ClickHouse highlighter's semantic pass refine function / column
	// tokens — a bare expression only lexes (every name an undifferentiated
	// identifier), which reads as "unhighlighted".
	const src = "file('rows.arrow', 'Arrow')"
	return []mappingplanview.Output{
		{TabID: 3, Title: "SQL · presence", Lang: mappingplanview.LangSQL,
			Source: "-- Presence prefilter (necessary, not sufficient).\nSELECT *\nFROM " + src + "\nWHERE " + a.Presence},
		{TabID: 4, Title: "SQL · projection", Lang: mappingplanview.LangSQL,
			Source: "-- Projection: a named Tuple; address slots as t.<field>.\nSELECT\n  " + a.Projection + " AS t\nFROM " + src},
		{TabID: 5, Title: "SQL · validator", Lang: mappingplanview.LangSQL,
			Source: "-- Validator: exact conformance check.\nSELECT *\nFROM " + src + "\nWHERE " + a.Validator},
	}
}

// demoMappingPlanView renders the playground for the given per-window state.
func demoMappingPlanView(ids *c.WidgetIdStack, st *mappingPlanViewDemoState) {
	mappingplanview.Render(mappingplanview.Input{
		Ids:       ids,
		ScopeKey:  "mpv",
		Model:     st.model,
		Recompute: st.recompute,
	})
}
