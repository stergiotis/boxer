package wasmsurvey

import (
	"strings"

	"github.com/stergiotis/boxer/public/code/analysis/golang/godep"
)

// This file is the curated TinyGo-support knowledge the static triage seeds
// from. It is an approximation of a moving target (TinyGo 0.39's wasm
// surface) sourced from tinygo.org's stdlib-support matrix plus the
// structural facts that wasm has no process model, no raw sockets, and (for
// wasm-unknown) no host at all. It is deliberately conservative: the
// empirical probe is ground truth and overturns both false reds and false
// greens, so an imperfect seed costs at most a probe, never a wrong final
// verdict (in `both` mode).

// redStdlib are standard-library packages TinyGo does not provide on wasm, or
// that require a host facility wasm lacks. A package reaching one of these is
// seeded Red (not expected to compile/link). net/url, net/netip and similar
// pure-parsing packages are intentionally absent (they are fine) — only the
// socket/process/dynamic-loading surface is listed.
var redStdlib = map[string]bool{
	"os/exec":           true, // no process model under wasm
	"os/signal":         true, // no OS signals
	"plugin":            true, // dynamic loading unsupported
	"runtime/cgo":       true, // cgo unavailable for wasm
	"net":               true, // no raw sockets
	"net/http":          true, // pulls net; TinyGo's wasm http surface is not the stdlib's
	"net/http/httptest": true,
	"net/http/httputil": true,
	"net/rpc":           true,
	"net/smtp":          true,
	"net/textproto":     true, // pulls net
	"crypto/tls":        true, // pulls net
	"database/sql":      true, // reflect + drivers + net
}

// partialStdlib are packages TinyGo provides only in part (chiefly the
// reflect-dependent ones — TinyGo implements a reflect subset). They are
// seeded Yellow: a real candidate for the empirical probe rather than a
// condemnation. The mapped ReasonKind is the "why".
var partialStdlib = map[string]ReasonKind{
	"reflect":                ReasonReflect,
	"encoding/json":          ReasonReflect, // TinyGo's encoding/json runs on the reflect subset
	"encoding/xml":           ReasonReflect,
	"encoding/gob":           ReasonReflect, // reflect-heavy; frequently fails
	"encoding/asn1":          ReasonReflect,
	"text/template":          ReasonReflect,
	"html/template":          ReasonReflect,
	"unsafe":                 ReasonUnsafe,
	"encoding/json/v2":       ReasonGoexperimentJSONv2,
	"encoding/json/jsontext": ReasonGoexperimentJSONv2,
}

// unsupportedExternalPrefix are external module/import prefixes known not to
// build (or run) under TinyGo/wasm — high-confidence denials only. Matched as
// import-path prefixes. Everything external and unmatched defaults to Yellow
// (unknown — probe it), so this list need not be exhaustive.
var unsupportedExternalPrefix = []string{
	"golang.org/x/tools",                  // go/packages shells out to `go` via os/exec
	"github.com/ClickHouse/clickhouse-go", // network database driver
	"google.golang.org/grpc",              // sockets
	"google.golang.org/protobuf/reflect",  // protoreflect: heavy reflect
	"github.com/apache/arrow",             // cgo/unsafe-heavy columnar runtime
}

// supportedExternalPrefix are external prefixes confidently pure-Go and
// wasm-clean, seeded Green so the static pass has positive signal rather than
// painting every logging/CLI package Yellow. Kept short and high-confidence;
// the probe corrects any over-optimism.
var supportedExternalPrefix = []string{
	"github.com/rs/zerolog",
	"github.com/urfave/cli",
}

// leafSeed returns the base verdict a single package contributes on its own
// account — before propagation folds in what it imports. class is the godep
// provenance (stdlib | external | internal). The returned reason is ReasonNone
// for a Green seed.
//
// Internal packages always seed Green: an internal package is "blocked" only
// through what it (transitively) imports, which propagation handles. stdlib
// and external identities are where the real seeds live.
func leafSeed(importPath string, class string, target TargetID) (tier Tier, kind ReasonKind) {
	switch class {
	case godep.ClassStdlib:
		if redStdlib[importPath] {
			return TierRed, ReasonUnsupportedStdlib
		}
		if k, ok := partialStdlib[importPath]; ok {
			return TierYellow, k
		}
		// wasm-unknown has no host: os/syscall compile but are inert. Flag
		// them Yellow on that target only, so the freestanding column differs
		// from wasi/js without condemning ubiquitous packages outright.
		if target == TargetWasmUnknown && (importPath == "os" || importPath == "syscall") {
			return TierYellow, ReasonUnsupportedStdlib
		}
		return TierGreen, ReasonNone
	case godep.ClassExternal:
		for _, p := range unsupportedExternalPrefix {
			if strings.HasPrefix(importPath, p) {
				return TierRed, ReasonUnsupportedExternal
			}
		}
		for _, p := range supportedExternalPrefix {
			if strings.HasPrefix(importPath, p) {
				return TierGreen, ReasonNone
			}
		}
		return TierYellow, ReasonUnknownExternal
	default: // ClassInternal and anything else
		return TierGreen, ReasonNone
	}
}
