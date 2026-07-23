package sqlapplet

import (
	"github.com/rs/zerolog"

	"github.com/stergiotis/boxer/apps/play"
	"github.com/stergiotis/boxer/public/db/clickhouse/clickhouseenv"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// EmbedConfig carries the host-supplied collaborators NewEmbedded needs.
// It is the ADR-0132 §SD8 graduation surface: an embedder app hosts an
// applet document while every §SD1 invariant (committed, gated, classified
// buffer) survives (ADR-0134 §SD7).
type EmbedConfig struct {
	// StampAppId is the identity the log_comment stamp attributes runs to.
	// A standalone applet passes its minted manifest id; an embedder passes
	// a composed stamp — its own app id carrying the applet slug — so
	// per-applet attribution survives embedding (ADR-0134 §SD7).
	StampAppId string
	// RunId is the runtime run identity for the stamp.
	RunId string
	// Bus is the capability bus for SetCapabilities. An applet's declared
	// capabilities ride the embedder's manifest (§SD8), so this is the
	// embedder's bus.
	Bus app.BusI
	// Log is the instance logger.
	Log zerolog.Logger
	// EndpointURL overrides the resolved endpoint. Empty resolves from
	// def.Endpoint (introspection → LocalQueryEndpoint; default → env).
	EndpointURL string
	// Bindings maps each declared dataset alias to the ephemeral handle the
	// embedder published, applied pre-mount so the buffer's keelson('<alias>')
	// rewrites to the handle client-side (ADR-0134 §SD4).
	Bindings map[string]string
}

// NewEmbedded constructs an attenuated PlayApp for def, ready to render:
// it resolves the endpoint, stamps identity, applies the AutoRun/Live
// gates and the minimal toolbar, attenuates tabs, binds any ad-hoc
// datasets, and grants capabilities. It is the shared core that both the
// standalone applet (appletApp.Mount) and an embedder call, so every
// applet-definition invariant holds under embedding (ADR-0132 §SD8,
// ADR-0134 §SD7). Instance-id salting is per-PlayApp automatically
// (NewPlayApp), so embedded instances do not collide.
func NewEmbedded(def *AppletDef, cfg EmbedConfig) (inner *play.PlayApp, err error) {
	clientCfg, err := resolveClientConfig(def, cfg.EndpointURL)
	if err != nil {
		return nil, err
	}
	client := play.NewClient(clientCfg, nil)
	client.SetStampIdentity(cfg.RunId, cfg.StampAppId)

	inner = play.NewLivePlayApp(client, def.SQL, appletMaxHistory)
	// AutoRun only for the read class (ADR-0132 §SD3/§SD5): a mutating or
	// egress-reaching applet always waits for an explicit Run. Ad-hoc data
	// flows into the engine and widens no egress, so a dataset applet stays
	// read-class (ADR-0134 §SD4).
	inner.AutoRun = def.Class == analysis.QuerySecurityRead
	if def.HasUnboundSlots {
		inner.SetLiveMain(true)
	}
	if def.BandsSQL != "" {
		inner.SetTimelineBandsSql(def.BandsSQL)
	}
	inner.SetToolbarMinimal(true)
	if err = attenuateTabs(inner, def, cfg.Log); err != nil {
		return nil, err
	}
	for alias, handle := range cfg.Bindings {
		if bErr := inner.BindDataset(alias, handle); bErr != nil {
			return nil, eh.Errorf("sqlapplet %s: bind dataset %q: %w", def.Slug, alias, bErr)
		}
	}
	// nil storage: an applet's buffer is its committed definition — nothing
	// to persist or restore (play's persist paths no-op on nil).
	inner.SetCapabilities(cfg.Bus, nil, cfg.Log)
	return inner, nil
}

// resolveClientConfig picks the endpoint: an explicit override, else the
// introspection loopback /query, else the env-configured ClickHouse.
func resolveClientConfig(def *AppletDef, endpointURL string) (cfg play.ClientConfig, err error) {
	if endpointURL != "" {
		cfg.URL = endpointURL
		return
	}
	switch def.Endpoint {
	case EndpointIntrospection:
		cfg.URL = introspect.LocalQueryEndpoint()
		if cfg.URL == "" {
			err = eh.Errorf("sqlapplet %s: introspection endpoint unavailable (KEELSON_INTROSPECT_ENABLE, chlocal)", def.Slug)
			return
		}
	default:
		cfg = play.ClientConfig{
			URL:      clickhouseenv.URL.Get(),
			User:     clickhouseenv.User.Get(),
			Password: clickhouseenv.Password.Get(),
		}
	}
	return
}
