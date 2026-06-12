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

// redStdlib are standard-library packages that genuinely fail to COMPILE/LINK
// under TinyGo on wasm. The survey's verdict is compile+link (SD4), so this list
// must not pre-judge packages that merely fail at *runtime*. And because a
// seeded-Red package is pruned before the empirical probe ever runs, a wrong
// entry here is never overturned — the "both mode never gives a wrong verdict"
// property holds only for the Yellow seed (which is probed), not this one.
//
// Validated empirically against TinyGo 0.41.1 (2026-06-12, targets wasi +
// wasm-unknown). The earlier seed marked the whole socket/process/dynamic-load
// surface Red on intuition; the compiler disagrees. TinyGo overlays or stubs
// most of it, so it compiles and links — Green by this survey's definition —
// even though it fails at runtime on wasm. These all COMPILE and were removed:
//
//	net, os/exec, os/signal, runtime/cgo, net/http, net/http/httptest,
//	net/rpc, net/textproto, crypto/tls, database/sql.
//
// plugin compiles on wasi but fails to link on wasm-unknown (dlopen); it is
// target-dependent, so it is left to the per-target probe rather than a blanket
// seed. net/url, net/netip and similar pure-parsing packages are fine and
// absent. Only these still fail to compile on wasi (the most permissive target):
var redStdlib = map[string]bool{
	"net/smtp":          true, // references tls.Conn, which TinyGo's crypto/tls omits
	"net/http/httputil": true, // TinyGo's net/http overlay lacks a symbol dump.go needs
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
