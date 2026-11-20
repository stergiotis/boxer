package tabhost

import (
	"net/url"
	"os"
	"slices"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
)

// A tab's module environment is written by the worker from the page link's
// `env=NAME=value` parameters, plus the two it adds itself (BOXER_TAB_BASE,
// and CLICKHOUSE_URL when the link names none). Anyone can craft a link, so
// the module admits only what a link may decide (ADR-0391): what to show and
// how, never what may be written, with which credentials, or which server is
// reached.

// linkEnvNames are the variables a link may set, reviewed in one place:
// display, theme, layout, focus and seed variables, and the trial knobs the
// committed measurement scripts pass. Each is a decision about presentation
// or a read; none enables a write or carries a credential.
var linkEnvNames = []string{
	"IMZERO2_THEME",
	"BOXER_LOG_LEVEL",
	// play's seed and layout (BOXER_PLAY_AUTORUN is admitted conditionally:
	// linkAutorunName)
	"BOXER_PLAY_SQL",
	"BOXER_PLAY_DOCK_SPLITS",
	"BOXER_PLAY_TAB_ZONES",
	"BOXER_PLAY_MAP_CENTER",
	"BOXER_PLAY_MAP_ZOOM",
	"BOXER_PLAY_MAP_TABLE",
	"BOXER_PLAY_WINDOW_SIZE",
	// an applet document beside the page; ADR-0299 refuses one that leaves
	// the page's origin or whose buffer is not read-class
	"BOXER_SQLAPPLET_TAB_DOC",
	// a user name, not a secret: the endpoint decides what it may do
	"CLICKHOUSE_USER",
	// set by the worker itself, which overrides any link value
	"BOXER_TAB_BASE",
	// trial knobs (doc/trials/audio-lane-playout)
	"BOXER_WAVEFORM_DEMO_AUTOPLAY",
	"BOXER_AUDIO_LANE_TARGET",
	"BOXER_AUDIO_LANE_TRACE",
}

// linkEnvPrefixes admit a family by prefix.
var linkEnvPrefixes = []string{
	"BOXER_PLAY_FOCUS_",
}

const (
	linkSQLName      = "BOXER_PLAY_SQL"
	linkAutorunName  = "BOXER_PLAY_AUTORUN"
	linkEndpointName = "CLICKHOUSE_URL"
	linkBaseName     = "BOXER_TAB_BASE"
)

// LinkEnvRefusal is one variable the module does not take from a link, and
// why.
type LinkEnvRefusal struct {
	Name   string
	Reason string
}

// admitLinkEnv splits a tab module's environment (NAME=value entries) into
// the names it keeps and the ones it refuses. extra are further names the
// binary admits (Options.LinkEnv).
//
//   - A name on the reviewed list, or under one of its prefixes, is kept.
//   - CLICKHOUSE_URL is kept when it cannot reach a server other than the
//     page's own: its host is under the reserved .invalid domain (the
//     in-process endpoint, or nothing), or it has the page's origin
//     (BOXER_TAB_BASE), where the deployment's /ch/ proxy sits.
//   - BOXER_PLAY_AUTORUN is kept when no SQL is seeded, or the seeded SQL
//     classifies read (analysis.ClassifyQuerySecurity): a link may show a
//     query's result, never run a write or an egress on the visitor's
//     authority.
//   - Everything else is refused: write switches, credentials, endpoints.
func admitLinkEnv(environ []string, extra []string) (keep []string, refused []LinkEnvRefusal) {
	vals := make(map[string]string, len(environ))
	order := make([]string, 0, len(environ))
	for _, kv := range environ {
		name, value, _ := strings.Cut(kv, "=")
		if _, seen := vals[name]; !seen {
			order = append(order, name)
		}
		vals[name] = value
	}
	for _, name := range order {
		value := vals[name]
		switch {
		case name == linkEndpointName:
			if reason := endpointRefusal(value, vals[linkBaseName]); reason != "" {
				refused = append(refused, LinkEnvRefusal{Name: name, Reason: reason})
				continue
			}
		case name == linkAutorunName:
			if reason := autorunRefusal(vals[linkSQLName]); reason != "" {
				refused = append(refused, LinkEnvRefusal{Name: name, Reason: reason})
				continue
			}
		case !linkNameAdmitted(name, extra):
			refused = append(refused, LinkEnvRefusal{Name: name, Reason: "not a variable a link may set"})
			continue
		}
		keep = append(keep, name)
	}
	return
}

func linkNameAdmitted(name string, extra []string) (ok bool) {
	if slices.Contains(linkEnvNames, name) || slices.Contains(extra, name) {
		return true
	}
	for _, p := range linkEnvPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// endpointRefusal says why a link's CLICKHOUSE_URL is refused, or "" when
// it is kept.
func endpointRefusal(value string, pageBase string) (reason string) {
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "not an absolute http(s) URL"
	}
	if u.User != nil {
		return "carries credentials"
	}
	host := strings.ToLower(u.Hostname())
	if host == "invalid" || strings.HasSuffix(host, ".invalid") {
		return ""
	}
	base, err := url.Parse(pageBase)
	if err == nil && base.Host != "" && strings.EqualFold(base.Scheme, u.Scheme) && strings.EqualFold(base.Host, u.Host) {
		return ""
	}
	return "reaches a server other than the page's origin"
}

// autorunRefusal says why a link's BOXER_PLAY_AUTORUN is refused, or "" when
// it is kept.
func autorunRefusal(sql string) (reason string) {
	if strings.TrimSpace(sql) == "" {
		return ""
	}
	pr, err := nanopass.Parse(sql)
	if err != nil {
		return "the seeded SQL does not parse, so it cannot be shown to be a read"
	}
	class, _, err := analysis.ClassifyQuerySecurity(pr)
	if err != nil || class != analysis.QuerySecurityRead {
		return "the seeded SQL is not a read"
	}
	return ""
}

// admitLinkEnvironment removes from this process's environment every
// variable a link may not set, before the program reads any of it, and logs
// each refusal where the worker shows the module's output. It runs only
// under the reactor, whose environment is the worker's; a native run's is
// the operator's.
func (inst *Program) admitLinkEnvironment() {
	environ := os.Environ() //boxer:lint disable=CS011 reason="the tab module's environment is the page link's; it is filtered as a whole before the registry reads any of it"
	_, refused := admitLinkEnv(environ, inst.opts.LinkEnv)
	for _, r := range refused {
		_ = os.Unsetenv(r.Name)
		log.Warn().Str("name", r.Name).Str("reason", r.Reason).Msg("tabhost: a page link may not set this variable; it is ignored (ADR-0391)")
	}
}
