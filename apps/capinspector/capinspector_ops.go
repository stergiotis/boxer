package capinspector

import (
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
)

// The operations catalog (ADR-0345 M4). The declared capability subjects
// are keelson('client_caps') and the audit rows keelson('app_audit'), so
// the catalog carries only what no table does: the window's selection, the
// selected cap's static spec with its effective backend, and the
// in-process activity counters the sparkline strip draws from.

const (
	resSelection = "selection"

	opGetView     = "get_view"
	opGetActivity = "get_activity"
	opSelectCap   = "select_cap"
)

// opsSnap is what queries read: the selection and the counters as the frame
// drew them.
type opsSnap struct {
	selected CapId
	activity []CapActivity
}

func (inst *App) opsSnapshot() (sn opsSnap) {
	sn.selected = inst.selectedCap
	caps := allCapIdsOrdered()
	sn.activity = make([]CapActivity, 0, len(caps))
	for _, capId := range caps {
		spark := Tally.Snapshot(capId)
		a := CapActivity{Cap: capId, Count: Tally.Count(capId), Buckets: spark[:],
			BucketSeconds: int(sparkBucketWidth.Seconds()), Effective: ActiveBackend(capId)}
		a.Degraded = a.Effective == ""
		sn.activity = append(sn.activity, a)
	}
	return
}

// CapChoice is one entry of the picker.
type CapChoice struct {
	Cap     string `desc:"the capability id select_cap takes"`
	Display string `desc:"the label the picker shows"`
}

// Backend is one implementation of the selected capability.
type Backend struct {
	Id        string `desc:"the backend id"`
	Display   string `desc:"the label the schematic shows"`
	Effective bool   `desc:"the runtime selected this one at boot"`
}

// View is get_view's result: the picker, and the selected cap's page.
type View struct {
	Selected      string      `desc:"the selected capability id; empty shows the picker alone"`
	Display       string      `desc:"the selected cap's label"`
	SubjectFamily string      `desc:"the subject pattern the cap serves, or what stands in for one"`
	Backend       string      `desc:"the package and types that implement the cap"`
	Backends      []Backend   `desc:"the shipped implementations; one is effective unless the cap is degraded"`
	Degraded      bool        `desc:"no backend was recorded as effective: the cap's service is unbound"`
	SchemaTable   string      `desc:"database.table the backend persists into; empty when it owns none"`
	Caps          []CapChoice `desc:"every capability the picker offers, in picker order"`
}

// GetActivityArgs narrows the activity read to one cap.
type GetActivityArgs struct {
	Cap string `desc:"a capability id; empty returns every cap"`
}

// CapActivity is one cap's audit activity as the strip draws it.
type CapActivity struct {
	Cap           string   `desc:"the capability id"`
	Count         uint64   `desc:"audit records since the process started; for bus, every record"`
	Buckets       []uint64 `desc:"records per bucket over the last minute, oldest first"`
	BucketSeconds int      `desc:"seconds one bucket covers"`
	Effective     string   `desc:"the effective backend id; empty when degraded"`
	Degraded      bool     `desc:"no backend is effective"`
}

// GetActivityResult is get_activity's result.
type GetActivityResult struct {
	Caps []CapActivity `desc:"one entry per cap asked for"`
}

// SelectCapArgs selects the cap the page shows.
type SelectCapArgs struct {
	Cap string `desc:"a capability id from get_view's caps; empty shows the picker alone"`
}

func capIdList() (s string) {
	return strings.Join(allCapIdsOrdered(), ", ")
}

func refuseUnknownCap(capId string) (err error) {
	if capId == "" {
		return
	}
	if _, ok := Registry[capId]; !ok {
		err = app.RefuseOperation("cap is one of " + capIdList())
	}
	return
}

var ops = func() (s *appops.Set[*App, opsSnap]) {
	s = appops.NewSet((*App).opsSnapshot)

	s.Resource(resSelection, "the capability the page shows; empty for the picker alone",
		func(inst *App) any { return inst.selectedCap })
	s.Restorable(resSelection, func(inst *App, v any) bool {
		capId, ok := v.(CapId)
		if ok && refuseUnknownCap(capId) == nil {
			inst.selectedCap = capId
			return true
		}
		return false
	})

	appops.Query(s, app.OperationSpec{Name: opGetView, Version: 1,
		Summary: "read the picker and the selected capability's page: subject family, backends, the effective one, its table; " +
			"the declared subjects per app are keelson('client_caps')",
		Agents: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, _ appops.None) (out View, err error) {
			out.Selected = sn.selected
			for _, capId := range allCapIdsOrdered() {
				out.Caps = append(out.Caps, CapChoice{Cap: capId, Display: Registry[capId].Display})
			}
			spec, ok := Registry[sn.selected]
			if !ok {
				return
			}
			out.Display, out.SubjectFamily, out.Backend = spec.Display, spec.SubjectFamily, spec.Backend
			effective := ActiveBackend(sn.selected)
			for _, b := range spec.Backends {
				out.Backends = append(out.Backends, Backend{Id: b.Id, Display: b.Display, Effective: b.Id == effective})
			}
			out.Degraded = effective == ""
			if spec.Schema != nil {
				out.SchemaTable = spec.Schema.Qualified()
			}
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetActivity, Version: 1,
		Summary: "read the audit activity per capability: the count since start and the last minute in buckets; " +
			"the records themselves are keelson('app_audit')",
		Agents: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in GetActivityArgs) (out GetActivityResult, err error) {
			if err = refuseUnknownCap(in.Cap); err != nil {
				return
			}
			for _, a := range sn.activity {
				if in.Cap == "" || a.Cap == in.Cap {
					out.Caps = append(out.Caps, a)
				}
			}
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSelectCap, Version: 1,
		Summary: "select the capability the page shows; an empty id shows the picker alone",
		Effect:  app.OperationEffectView, Writes: []string{resSelection}, Agents: true,
		Gesture: "the picker", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in SelectCapArgs) (out appops.None, err error) {
			if err = refuseUnknownCap(in.Cap); err != nil {
				return
			}
			inst.selectedCap = in.Cap
			return
		})

	return
}()

// Operations serves the catalog for this window (app.OperationsAppI).
func (inst *App) Operations() (h app.OperationsHandlerI) { return ops.Bind(inst) }

// gestureOr routes the person's control through the catalog (ADR-0345
// §SD3), so the command log sees it as theirs, and applies it directly
// where no host serves the catalog (tour, tests).
func gestureOr[In any](inst *App, op string, in In, direct func()) {
	if inst.frameCtx != nil {
		if _, err := appops.Gesture[In, appops.None](inst.frameCtx, op, in); err == nil {
			return
		}
	}
	direct()
}

func (inst *App) selectCapGesture(capId CapId) {
	gestureOr(inst, opSelectCap, SelectCapArgs{Cap: capId}, func() { inst.selectedCap = capId })
}
