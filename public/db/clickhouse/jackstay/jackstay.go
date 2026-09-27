// Package jackstay is the engine of the guided ClickHouse-to-ClickHouse sync
// of ADR-0259. Every step of the process reads both servers and
// writes its findings into one [Plan] document. The CLI and the imzero2 wizard
// are two editors of that document (§SD1).
//
// This package covers §M1 to §M4:
//
//   - discovery of what a server holds, from its system tables only ([Discover], §SD2);
//   - a structure verdict per table, with the DDL that would bring the target
//     in line ([Judge], §SD3);
//   - the plan document itself ([BuildPlan], [Plan.Save], [LoadPlan]);
//   - applying the proposed DDL after a staleness check ([Restate], [ApplyStructure]);
//   - the content diff by primary key: the chunk layout ([DeriveChunking]),
//     one leaf-digest scan per side, and a row-by-row stage for small
//     differing leaves ([DiffTable], [DiffPlanTable], §SD4);
//   - the sync: chunks relayed as Native streams, verified by digest
//     arithmetic, journaled for resume ([SyncTable], [Journal], §SD5);
//   - the monitor: rows landed from the target's system.processes, a
//     compressed relay, the target's disks and footprints ([ReadDisks]), the
//     pre-flight ([Preflight]) and the free-space floor ([FreeFloor], §SD6);
//   - the workflow both front ends call, one function per step (§SD7).
//
// Credentials never enter a plan. An [Endpoint] names a server by URL and user,
// and the password is read from the environment registry by role when a
// client is built ([SourceClientConfig], [TargetClientConfig]).
package jackstay

import (
	"context"
	"net/url"
	"strings"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/db/clickhouse/clickhouseenv"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/observability/eh"
)

var (
	// TargetEndpointEnv names the target server. The source is the ordinary
	// CLICKHOUSE_* family (see [chclient.ConfigFromEnv]); the target has a
	// family of its own, because a sync needs both servers at once.
	TargetEndpointEnv = env.NewString(env.Spec{
		Name:        "BOXER_JACKSTAY_TARGET_ENDPOINT",
		Description: "jackstay target server: HTTP URL or host:port (e.g. http://replica:8123/); empty means no target",
		Category:    env.CategoryDatabase,
		CliFlagName: "target",
	})
	// TargetUserEnv is the user the target is connected as.
	TargetUserEnv = env.NewString(env.Spec{
		Name:        "BOXER_JACKSTAY_TARGET_USER",
		Default:     "default",
		Description: "jackstay target server user",
		Category:    env.CategoryDatabase,
		CliFlagName: "target-user",
	})
	// TargetPasswordEnv is the target's password; it has no CLI flag, so it
	// never appears in a process listing.
	TargetPasswordEnv = env.NewString(env.Spec{
		Name:        "BOXER_JACKSTAY_TARGET_PASSWORD",
		Description: "jackstay target server password; read from the environment only, never stored in a plan",
		Category:    env.CategoryDatabase,
		Sensitive:   true,
	})
)

var ErrNoTarget = eh.Errorf("no target endpoint configured")

// Endpoint names a server as a plan records it: where it is and who connects.
// The password is deliberately absent.
type Endpoint struct {
	URL  string `json:"url"`
	User string `json:"user"`
}

// NormalizeEndpointURL accepts either a URL or a bare host:port and returns an
// HTTP base URL with a trailing slash. A string with no scheme is taken to be
// host:port on plain HTTP, which is what "the databases reachable on this port"
// means for ClickHouse's HTTP interface.
func NormalizeEndpointURL(s string) (u string) {
	u = strings.TrimSpace(s)
	if u == "" {
		return
	}
	if !strings.Contains(u, "://") {
		u = "http://" + u
	}
	// The slash ends the path, not the whole string: a query string such as
	// ?database=x must stay as it is.
	parsed, perr := url.Parse(u)
	if perr != nil {
		if !strings.HasSuffix(u, "/") {
			u += "/"
		}
		return
	}
	if !strings.HasSuffix(parsed.Path, "/") {
		parsed.Path += "/"
	}
	return parsed.String()
}

// SourceEndpoint is the source as the environment (or a CLI flag bound to the
// same registry entry) describes it.
func SourceEndpoint() (ep Endpoint) {
	cfg := chclient.ConfigFromEnv()
	ep = Endpoint{URL: NormalizeEndpointURL(cfg.URL), User: cfg.User}
	return
}

// TargetEndpoint is the target as the environment describes it; ok is false
// when no target endpoint is configured.
func TargetEndpoint() (ep Endpoint, ok bool) {
	u := NormalizeEndpointURL(TargetEndpointEnv.Get())
	if u == "" {
		return
	}
	ep = Endpoint{URL: u, User: TargetUserEnv.Get()}
	ok = true
	return
}

// SourceClientConfig pairs ep with the source password from CLICKHOUSE_PASSWORD.
func SourceClientConfig(ep Endpoint) (cfg chclient.Config) {
	cfg = chclient.Config{URL: ep.URL, User: ep.User, Password: clickhouseenv.Password.Get()}
	return
}

// ddlGuardSettings relax the server's guards against DDL it calls suspicious.
// A table being created on the target was already accepted by the source,
// usually under one of these settings (leeway's facts shape needs the first).
// system.tables.create_table_query does not record query-level settings, so
// they are supplied again here. They loosen checks only; none changes what a
// statement creates.
var ddlGuardSettings = []string{
	"allow_suspicious_low_cardinality_types",
	"allow_suspicious_fixed_string_types",
	"allow_suspicious_codecs",
	"allow_suspicious_indices",
	"allow_suspicious_variant_types",
	"allow_suspicious_ttl_expressions",
}

// DDLGuardSettings returns the guard settings of [DDLClientConfig] that the
// server behind q knows. A setting a release does not know fails every
// request that names it, so an older target gets only the ones it has.
func DDLGuardSettings(ctx context.Context, q QueryI) (settings []string, err error) {
	quoted := make([]string, 0, len(ddlGuardSettings))
	for _, s := range ddlGuardSettings {
		quoted = append(quoted, "'"+s+"'")
	}
	type nameRow struct {
		Name string `json:"name"`
	}
	var rows []nameRow
	rows, err = queryRows[nameRow](ctx, q, "SELECT name FROM system.settings WHERE name IN ("+strings.Join(quoted, ", ")+") ORDER BY name"+jsonSettings)
	if err != nil {
		err = eh.Errorf("unable to read the target's settings: %w", err)
		return
	}
	settings = make([]string, 0, len(rows))
	for _, r := range rows {
		settings = append(settings, r.Name)
	}
	return
}

// DDLClientConfig derives from cfg the configuration for a client that applies a
// plan's DDL. It carries settings, normally the answer of [DDLGuardSettings],
// as HTTP query parameters, which ClickHouse applies to every statement sent
// through it. Use it for DDL only.
func DDLClientConfig(cfg chclient.Config, settings []string) (out chclient.Config) {
	out = cfg
	var b strings.Builder
	b.WriteString(cfg.URL)
	sep := byte('?')
	if strings.Contains(cfg.URL, "?") {
		sep = '&'
	}
	for _, s := range settings {
		b.WriteByte(sep)
		b.WriteString(s)
		b.WriteString("=1")
		sep = '&'
	}
	out.URL = b.String()
	return
}

// TargetClientConfig pairs ep with the target password from
// BOXER_JACKSTAY_TARGET_PASSWORD.
func TargetClientConfig(ep Endpoint) (cfg chclient.Config) {
	cfg = chclient.Config{URL: ep.URL, User: ep.User, Password: TargetPasswordEnv.Get()}
	return
}
