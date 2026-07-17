// Package introspecthost is the in-process start hook for the keelson
// introspection HTTP table source (ADR-0094 §SD3/§SD4). A keelson GUI host
// calls [Start] once, and the introspection tables become queryable — both
// by an external clickhouse-local/-server over `url()` and by a co-resident
// app (apps/play) that points at the loopback `/query` endpoint.
//
// The server MUST run in the host's own OS process: the providers read live
// in-process state (the running window host, live env values, the app/demo
// registries), which a separate process cannot see. That is why this is a
// host hook rather than a standalone daemon — see the ADR-0094 §SD3 update.
package introspecthost

import (
	"context"
	"io"

	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalbroker"
	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/introspecthttp"
	introspectproviders "github.com/stergiotis/boxer/public/keelson/runtime/introspect/providers"
	introspectprovidersgui "github.com/stergiotis/boxer/public/keelson/runtime/introspect/providersgui"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmetricsbus"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
)

// Enabled gates whether a host that calls [Start] actually stands up the
// table source. Default on (preserves the historical always-start carousel
// behaviour); set falsey to suppress. The bind address is the separate
// introspecthttp.ListenAddr (KEELSON_INTROSPECT_HTTP_LISTEN) knob.
var Enabled = env.NewBool(env.Spec{
	Name:        "KEELSON_INTROSPECT_ENABLE",
	Default:     "true",
	Description: "start the keelson introspection HTTP table source in-process (ADR-0094 §SD3); set falsey to disable",
	Category:    env.CategorySystem,
})

// queryPoolName is the chlocal pool the `/query` runner targets. It matches
// introspectengine.DefaultPoolName; kept a local literal so this host hook
// need not import the heavier engine package just for the constant.
const queryPoolName = "introspect"

// queryBusAppId is the bus client identity the `/query` runner publishes as.
const queryBusAppId runtimeapp.AppIdT = "runtime.introspect.query"

// topoBusAppId is the bus client identity of the metric-plane consumer
// feeding the observed-topology tables (ADR-0126 §SD5).
const topoBusAppId runtimeapp.AppIdT = "runtime.introspect.topo"

// Deps are the host-supplied collaborators [Start] needs.
type Deps struct {
	// WindowHost is the running window host. nil is allowed (e.g. screenshot
	// mode) and drops only the live keelson.windows table.
	WindowHost *windowhost.Inst
	// Bus is the host's in-process bus. Required to back POST /query with the
	// chlocal broker; nil leaves /query disabled (answers 503).
	Bus *inprocbus.Inst
	// ChlocalAvailable reports whether the chlocalbroker service is running.
	// When false, /query is left disabled even if Bus is set.
	ChlocalAvailable bool
	// Log is the host logger.
	Log zerolog.Logger
}

// noopStop is returned whenever there is nothing to shut down, so callers can
// always `defer stop(ctx)` unconditionally.
func noopStop(context.Context) error { return nil }

// Start builds the introspection registry, optionally backs POST /query with
// the chlocal broker, starts the loopback HTTP table source, and publishes
// its `/query` URL via [introspect.SetLocalQueryEndpoint] for co-resident
// apps. It is best-effort and never blocks boot: a disabled gate or a bind
// failure returns a no-op stop (and, on failure, the error) rather than
// aborting the host. The returned stop is always non-nil.
func Start(deps Deps) (stop func(context.Context) error, err error) {
	stop = noopStop
	if !Enabled.Get() {
		deps.Log.Debug().Msg("introspecthost: disabled via KEELSON_INTROSPECT_ENABLE")
		return
	}

	reg := introspect.NewRegistry()
	if e := introspectproviders.RegisterStatic(reg); e != nil {
		deps.Log.Warn().Err(e).Msg("introspecthost: static provider registration failed")
	}
	if e := introspectprovidersgui.RegisterAll(reg, deps.WindowHost); e != nil {
		deps.Log.Warn().Err(e).Msg("introspecthost: GUI provider registration failed")
	}
	// ADR-0126 §SD5: a process-lifetime metric-plane consumer feeds the
	// observed-topology tables (keelson.procs, keelson.sockets). imztop's
	// consumer is mount-gated, so the host holds its own. Best-effort: no
	// bus, no tables — the rest of the source still stands.
	var topoHolder *sysmetricsbus.LatestHolder
	if deps.Bus != nil {
		topoBus := deps.Bus.NewClient(topoBusAppId, []runtimeapp.SubjectFilter{
			{Pattern: sysmetricsbus.SubjectWildcard, Direction: runtimeapp.CapDirectionSub, Reason: "keelson.procs/keelson.sockets serve the latest metric-plane snapshot (ADR-0126)"},
		})
		holder, herr := sysmetricsbus.StartLatestHolder(sysmetricsbus.LatestHolderOptions{Bus: topoBus, Log: deps.Log})
		if herr != nil {
			deps.Log.Warn().Err(herr).Msg("introspecthost: metric-plane consumer failed; keelson.procs/sockets unavailable")
		} else {
			topoHolder = holder
			if e := introspectproviders.RegisterTopology(reg, holder); e != nil {
				deps.Log.Warn().Err(e).Msg("introspecthost: topology provider registration failed")
			}
		}
	}
	if e := introspect.RegisterCatalog(reg); e != nil {
		deps.Log.Warn().Err(e).Msg("introspecthost: catalog registration failed")
	}

	cfg := introspecthttp.Config{Registry: reg}
	if deps.ChlocalAvailable && deps.Bus != nil {
		// Back POST /query with the chlocal broker so a co-resident client
		// (apps/play) can query `SELECT ... FROM keelson('env')` here and get
		// ArrowStream back — no external server, no url() boilerplate
		// (ADR-0094 §SD4). The broker runs the url()-rewritten SQL, which
		// fetches tables from this server's own /table endpoints.
		queryBus := deps.Bus.NewClient(queryBusAppId, []runtimeapp.SubjectFilter{
			{Pattern: chlocalbroker.SubjectExecAll, Direction: runtimeapp.CapDirectionPub, Reason: "introspect /query runs SQL via clickhouse-local"},
		})
		cfg.Runner = introspecthttp.RunnerFunc(func(ctx context.Context, sql string) (body []byte, runErr error) {
			rep, reqErr := chlocalbroker.ExecOnPool(ctx, queryBus, queryPoolName, chlocalbroker.ExecRequest{SQL: sql})
			if reqErr != nil {
				return nil, reqErr
			}
			defer func() { _ = rep.Close() }()
			if repErr := rep.Err(); repErr != nil {
				return nil, repErr
			}
			return io.ReadAll(rep)
		})
	}

	srv := introspecthttp.New(cfg, deps.Log)
	if startErr := srv.Start(); startErr != nil {
		deps.Log.Warn().Err(startErr).Msg("introspecthost: HTTP table source start failed; keelson.* url()/query endpoint unavailable")
		if topoHolder != nil {
			_ = topoHolder.Close()
		}
		return noopStop, startErr
	}
	endpoint := srv.BaseURL() + "/query"
	// Publish /query for co-resident apps (apps/play) only when it is backed
	// by a runner — an unbacked endpoint answers 503, so offering it as a
	// query target would be a foot-gun. External consumers still reach the
	// tables via url('<BaseURL>/table/<name>') regardless.
	if cfg.Runner != nil {
		introspect.SetLocalQueryEndpoint(endpoint)
	}
	deps.Log.Info().
		Str("addr", srv.Addr()).
		Strs("tables", reg.Names()).
		Str("queryEndpoint", endpoint).
		Bool("queryBacked", cfg.Runner != nil).
		Msg("introspecthost: table source listening (external join via url(); co-resident apps target queryEndpoint)")

	stop = func(ctx context.Context) error {
		introspect.SetLocalQueryEndpoint("")
		if topoHolder != nil {
			_ = topoHolder.Close()
		}
		return srv.Stop(ctx)
	}
	return stop, nil
}
