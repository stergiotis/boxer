package widgets

import (
	"encoding/json"

	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshallgen"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/mappingplanview"
)

// mappingPlanViewDemoState holds the playground's editable model across frames.
type mappingPlanViewDemoState struct {
	model *mappingplanview.Model
}

// newMappingPlanViewState seeds the playground with the DroneMission DTO
// (anchor/codecdemo/dronemission.go): the id + naturalKey plain columns, a
// scalar `droneStatus,symbol`, and a unit `battery,u64Array`.
func newMappingPlanViewState() *mappingPlanViewDemoState {
	m := mappingplanview.NewModel("droneMission", "codecdemo", "DroneMission")

	id := m.AddRow()
	id.GoField, id.GoType, id.Section = "ID", "uint64", "id"

	track := m.AddRow()
	track.GoField, track.GoType, track.Section = "Tracking", "[]byte", "naturalKey"

	status := m.AddRow()
	status.GoField, status.GoType = "Status", "string"
	status.Membership, status.Section = "droneStatus", "symbol"

	battery := m.AddRow()
	battery.GoField, battery.GoType = "Battery", "uint64"
	battery.Membership, battery.Section = "battery", "u64Array"
	battery.Unit = true

	return &mappingPlanViewDemoState{model: m}
}

// recomputeMappingPlan rebuilds the plan from the model and renders the
// schema-agnostic Go codec marshallgen emits — the host-injected Recompute the
// widget runs on every edit. Any PlanBuilder / emit error becomes the widget's
// invalid verdict. Keeping this in the demo (not the widget package) confines
// the marshallgen dependency to the host.
func recomputeMappingPlan(m *mappingplanview.Model) {
	b := mappingplan.NewPlanBuilder("playground", m.PackageName, m.KindType)
	if err := b.AddUnderscoreField(m.Kind, "", ""); err != nil {
		m.SetInvalid(err)
		return
	}
	for _, r := range m.Fields {
		if r.IsConst {
			if err := b.AddUnderscoreField("", "", r.LWTag()); err != nil {
				m.SetInvalid(err)
				return
			}
			continue
		}
		if err := b.AddField(r.GoField, r.LWTag(), r.Shape()); err != nil {
			m.SetInvalid(err)
			return
		}
	}
	plan, err := b.Finish()
	if err != nil {
		m.SetInvalid(err)
		return
	}
	out, err := marshallgen.EmitPlan(plan, marshallgen.NoOpWrapper{})
	if err != nil {
		m.SetInvalid(err)
		return
	}
	planJSON, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		m.SetInvalid(err)
		return
	}
	// Go codec now; the dql read-back artefacts (SQL presence / projection /
	// validator) will slot in as further Outputs once that generator is wired
	// (it needs an IR the Plan alone doesn't carry — see ADR-0066).
	m.SetOutputs(
		mappingplanview.Output{TabID: 1, Title: "Go codec", Lang: mappingplanview.LangGo, Source: string(out)},
		mappingplanview.Output{TabID: 2, Title: "Plan IR", Lang: mappingplanview.LangJSON, Source: string(planJSON)},
	)
}

// demoMappingPlanView renders the playground for the given per-window state.
func demoMappingPlanView(ids *c.WidgetIdStack, st *mappingPlanViewDemoState) {
	mappingplanview.Render(mappingplanview.Input{
		Ids:       ids,
		ScopeKey:  "mpv",
		Model:     st.model,
		Recompute: recomputeMappingPlan,
	})
}
