package adhocdemo

import (
	"github.com/stergiotis/boxer/apps/sqlapplet"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
)

// adhocSnap is what the catalog's own queries read; the view's operations
// read the view's snapshot, which BundleViewOps captures.
type adhocSnap struct{}

// adhocOps offers the window's bundle view to agents (ADR-0288 (proposed)
// §SD8): the view is operable, so a model reads, runs and steers the
// bundle the window shows through the bundle_ operations.
var adhocOps = func() (s *appops.Set[*App, adhocSnap]) {
	s = appops.NewSet(func(*App) adhocSnap { return adhocSnap{} })
	sqlapplet.BundleViewOps(s, func(inst *App) map[string]*sqlapplet.BundleView {
		if inst.view == nil {
			return nil
		}
		return map[string]*sqlapplet.BundleView{"items": inst.view}
	})
	return
}()

// Operations serves the window's catalog.
func (inst *App) Operations() (h app.OperationsHandlerI) { return adhocOps.Bind(inst) }

var _ app.OperationsAppI = (*App)(nil)
