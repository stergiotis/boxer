package servicehost

import (
	"context"
	"strings"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/launchlimit"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Where is the service predicate (ADR-0352 (proposed) §SD3). It is spliced
// into `SELECT id FROM apps WHERE surface = 'headless' AND launchable AND
// (<Where>)`, so it carries the trust of any other variable the process
// reads, and it can select nothing but a headless app the launch limit
// admits.
var Where = env.NewString(env.Spec{
	Name:        "KEELSON_SERVICES_WHERE",
	Description: "ClickHouse boolean expression over keelson.apps columns; the headless apps it selects at boot, among those the launch limit admits, run as services (ADR-0352). Unset means no service runs",
	Category:    env.CategorySystem,
})

// AppId is the bus identity the host and its evaluation run under.
const AppId app.AppIdT = "github.com/stergiotis/boxer/public/keelson/runtime/servicehost"

// Select evaluates Where against reg and returns the ids of the services to
// run. set is false when the variable is unset, in which case nothing is
// evaluated and chlocal is not touched. Set, any failure — no chlocal, a
// predicate that does not parse, a query error — is returned, so the host
// refuses to boot rather than run a different set of services than it was
// given. Call it after the launch limit has been applied to reg: the
// composition with ADR-0272 is the `launchable` column.
func Select(ctx context.Context, bus *inprocbus.Inst, reg *app.Registry, chlocalAvailable bool, log zerolog.Logger) (ids []app.AppIdT, set bool, err error) {
	where := strings.TrimSpace(Where.Get())
	if where == "" {
		return
	}
	set = true
	if !chlocalAvailable {
		err = eb.Build().Str("var", Where.Spec().Name).Errorf("servicehost: the predicate needs clickhouse-local, which did not start")
		return
	}
	if reg == nil {
		reg = app.DefaultRegistry
	}
	ids, err = launchlimit.EvaluateAs(ctx, bus, reg, AppId, "servicehost", Compose(where), log)
	return
}

// Compose is the predicate the host evaluates for an operator's expression:
// the host's two conjuncts and the operator's, in that order.
func Compose(where string) (composed string) {
	composed = "surface = 'headless' AND launchable AND (" + where + ")"
	return
}
