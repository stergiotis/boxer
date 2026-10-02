// Package launchlimit limits which apps may be opened to those a SQL
// predicate over keelson.apps selects (ADR-0272). The predicate comes from
// KEELSON_LAUNCHABLE_APPS_WHERE and is evaluated once, at boot, on
// clickhouse-local; unset, nothing here runs and chlocal is not touched.
package launchlimit

import (
	"bufio"
	"bytes"
	"context"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/introspectengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/providers"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Where is the predicate. It is spliced into
// `SELECT id FROM apps WHERE (<Where>)`, so it carries the trust of any other
// variable the process reads.
var Where = env.NewString(env.Spec{
	Name:        "KEELSON_LAUNCHABLE_APPS_WHERE",
	Description: "ClickHouse boolean expression over keelson.apps columns; only apps it selects at boot can be opened, by a person or an agent (ADR-0272). Apps registered later stay launchable. Unset means no limit",
	Category:    env.CategorySystem,
})

// AppId is the bus identity the evaluation runs under.
const AppId app.AppIdT = "github.com/stergiotis/boxer/public/keelson/runtime/launchlimit"

// evalTimeout bounds the boot-time query.
const evalTimeout = 10 * time.Second

// Apply evaluates Where against reg and limits reg's launches to the apps it
// selects. Unset, it returns at once. Set, any failure — no chlocal, a
// predicate that does not parse, a query error — is returned, so the host
// refuses to boot rather than run without the limit it was given.
func Apply(ctx context.Context, bus *inprocbus.Inst, reg *app.Registry, chlocalAvailable bool, log zerolog.Logger) (err error) {
	where := strings.TrimSpace(Where.Get())
	if where == "" {
		return
	}
	if !chlocalAvailable {
		err = eb.Build().Str("var", Where.Spec().Name).Errorf("launchlimit: the predicate needs clickhouse-local, which did not start")
		return
	}
	var allowed []app.AppIdT
	allowed, err = Evaluate(ctx, bus, reg, where, log)
	if err != nil {
		return
	}
	reg.LimitLaunches(allowed)
	log.Info().Str("where", where).Int("launchable", len(allowed)).Int("registered", reg.Len()).
		Msg("launchlimit: launches limited")
	return
}

// Evaluate returns the ids of the apps in reg that where selects.
func Evaluate(ctx context.Context, bus *inprocbus.Inst, reg *app.Registry, where string, log zerolog.Logger) (ids []app.AppIdT, err error) {
	tables := introspect.NewRegistry()
	if err = tables.Register(providers.NewAppsProvider(reg)); err != nil {
		return
	}
	pool := introspectengine.DefaultPoolName
	client := bus.NewClient(AppId, []app.SubjectFilter{
		{Pattern: chlocalbroker.SubjectExecPrefix + pool, Direction: app.CapDirectionPub, Reason: "launchlimit: evaluate the launch predicate on clickhouse-local"},
	})
	defer func() { _ = client.Close() }()
	eng, err := introspectengine.New(introspectengine.Config{Registry: tables, Bus: client, PoolName: pool}, log)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, evalTimeout)
	defer cancel()
	body, _, err := eng.Query(ctx, "SELECT id FROM apps WHERE ("+where+") ORDER BY id", "TSVRaw")
	if err != nil {
		err = eb.Build().Str("where", where).Errorf("launchlimit: evaluate the predicate: %w", err)
		return
	}
	sc := bufio.NewScanner(bytes.NewReader(body))
	for sc.Scan() {
		if id := strings.TrimSpace(sc.Text()); id != "" {
			ids = append(ids, app.AppIdT(id))
		}
	}
	return
}
