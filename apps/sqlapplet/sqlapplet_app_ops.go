package sqlapplet

// A minted applet window serves agents the operable subset of play's
// catalog: the subset ADR-0288 §SD8 declares for a bundle view, under
// play's own names, served by the attenuated play the window holds. The
// subset fits the applet's posture (ADR-0132 §SD3): the document is the
// author's, so changing the SQL, binding datasets, publishing and the
// authoring tools are not offered, and a pane the document's `tabs:`
// pruned is declared and reported unavailable, with the reason. The
// catalog is one static value shared by every minted manifest; the
// handler is bound per window.

import (
	"slices"

	"github.com/stergiotis/boxer/apps/play"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
)

// appletOpsMount is the key the inner play's snapshot is captured under.
const appletOpsMount = "applet"

// appletSnap is the set's own snapshot: nothing, since every query reads
// the inner play's snapshot captured under appletOpsMount.
type appletSnap struct{}

// appletCapture is the inner play as a snapshot captured it: nil before
// Mount.
type appletCapture struct {
	snap  app.OperationsSnapshotI
	panes []string
}

// appletPanes are the tabs the attenuated play shows; render goroutine
// only.
func appletPanes(inner *play.PlayApp) (panes []string) {
	for _, spec := range inner.Tabs().Specs() {
		panes = append(panes, spec.ID)
	}
	return
}

var appletOps = func() (s *appops.Set[*appletApp, appletSnap]) {
	s = appops.NewSet(func(*appletApp) appletSnap { return appletSnap{} })
	s.Mount(appletOpsMount, func(inst *appletApp) any {
		capture := appletCapture{}
		if inst.inner != nil {
			capture.snap = inst.inner.ServedOperations().Snapshot()
			capture.panes = appletPanes(inst.inner)
		}
		return capture
	})
	for _, r := range play.OperableResources() {
		name := r.Name
		s.Resource(name, r.Summary, func(inst *appletApp) any {
			if inst.inner == nil {
				return nil
			}
			return inst.inner.ServedOperations().ResourceValue(name)
		})
	}
	for _, o := range play.OperableOperations() {
		spec := appletSpecOf(o.Spec)
		target, pane := o.Spec.Name, o.Pane
		s.MountedAvailable(spec.Name, appletOpsMount, func(v any) (ok bool, reason string) {
			c, _ := v.(appletCapture)
			return appletAvailable(c, target, pane)
		})
		if o.Spec.Class == app.OperationClassQuery {
			appops.MountedQueryRaw(s, appletOpsMount, spec, func(v any, args []byte) (result []byte, err error) {
				c, _ := v.(appletCapture)
				if c.snap == nil {
					return nil, app.RefuseOperation("the window has not mounted")
				}
				if err = refuseAppletPane(c.panes, pane); err != nil {
					return
				}
				return c.snap.Query(target, args)
			})
			continue
		}
		appops.MountedCommandRaw(s, spec, func(inst *appletApp, call app.OperationCall, args []byte) (result []byte, err error) {
			p := inst.inner
			if p == nil {
				return nil, app.RefuseOperation("the window has not mounted")
			}
			if err = refuseAppletPane(appletPanes(p), pane); err != nil {
				return
			}
			// Availability was judged over the captured snapshot; the
			// command runs now, so it is judged again here.
			if ok, reason := p.ServedOperations().Snapshot().Available(target); !ok {
				return nil, app.RefuseOperation(reason)
			}
			return p.ServedOperations().ApplyCommand(call, target, args)
		})
		if spec.Async {
			// The inner play settles it (ADR-0358 §SD2); a window that went
			// away ends the call rather than leaving it running.
			appops.SettlesRaw(s, spec.Name, func(inst *appletApp, call app.OperationCall, args []byte) (settled bool, result []byte, err error) {
				p := inst.inner
				if p == nil {
					return true, nil, app.RefuseOperation("the window went away while the work ran")
				}
				h, ok := p.ServedOperations().(app.OperationsAsyncI)
				if !ok {
					return true, nil, app.RefuseOperation("the window cannot settle " + target)
				}
				return h.SettleAsync(call, target, args)
			})
		}
	}
	return
}()

// Operations serves the applet's catalog over this window (ADR-0269 §SD2).
func (inst *appletApp) Operations() (h app.OperationsHandlerI) { return appletOps.Bind(inst) }

var _ app.OperationsAppI = (*appletApp)(nil)

// appletSpecOf is play's spec as an applet offers it: the same operation,
// offered to agents, without play's cross-references to operations the
// subset leaves out.
func appletSpecOf(s app.OperationSpec) (out app.OperationSpec) {
	out = s
	out.Refs = nil
	out.Agents = true
	return
}

// refuseAppletPane refuses a pane operation whose pane the document's
// `tabs:` pruned.
func refuseAppletPane(panes []string, pane string) (err error) {
	if pane != "" && !slices.Contains(panes, pane) {
		return app.RefuseOperation("the applet does not show the " + pane + " pane: its document's tabs do not name it")
	}
	return
}

// appletAvailable says whether the window could serve target: mounted,
// showing pane when the operation belongs to one, and where play would
// run it.
func appletAvailable(c appletCapture, target string, pane string) (ok bool, reason string) {
	if c.snap == nil {
		return false, "the window has not mounted"
	}
	if pane != "" && !slices.Contains(c.panes, pane) {
		return false, "the applet does not show the " + pane + " pane"
	}
	return c.snap.Available(target)
}
