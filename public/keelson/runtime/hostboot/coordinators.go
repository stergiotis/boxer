package hostboot

import (
	"context"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/launchlimit"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// coordinatorsAppId is the bus identity the predicate is evaluated under.
const coordinatorsAppId app.AppIdT = "github.com/stergiotis/boxer/public/keelson/runtime/hostboot/coordinators"

// composeCoordinators is the predicate the host evaluates for an operator's
// expression: an app no window of which can open coordinates nothing, so the
// launch limit stays the ceiling, as for services (ADR-0352 §SD3).
func composeCoordinators(where string) string {
	return "launchable AND (" + where + ")"
}

// resolveCoordinators reads who may ask the person for a grant: the names
// BOXER_AGENT_COORDINATORS lists and the apps its predicate selects, once,
// after the launch limit (ADR-0364, proposed). Set and not evaluable, the
// host does not boot, as for the launch limit: running with a different set
// of coordinators than the operator asked for is the worse failure.
func (rt *Runtime) resolveCoordinators(ctx context.Context, reg *app.Registry) (err error) {
	if !rt.opts.Services.Agent {
		return
	}
	names := agent.ParseCoordinators(agent.CoordinatorsEnv.Get())
	where := strings.TrimSpace(agent.CoordinatorsWhereEnv.Get())
	if where != "" {
		if rt.ChLocal == nil {
			return eb.Build().Str("var", agent.CoordinatorsWhereEnv.Spec().Name).Errorf("the coordinators predicate needs clickhouse-local, which did not start")
		}
		var ids []app.AppIdT
		ids, err = launchlimit.EvaluateAs(ctx, rt.Bus, reg, coordinatorsAppId, "agent-coordinators", composeCoordinators(where), rt.opts.Log)
		if err != nil {
			return
		}
		for _, id := range ids {
			names = append(names, string(id))
		}
		rt.opts.Log.Info().Str("where", where).Int("selected", len(ids)).Msg("hostboot: coordinators selected")
	}
	rt.coordinators = names
	reg.SetCoordinators(names)
	return
}
