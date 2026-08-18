package grammar1

import "github.com/stergiotis/boxer/public/parsing/antlr4utils"

// SharedDFA is the one bounded DFA cache every seam that parses grammar1 uses
// (ADR-0084 for the bound, ADR-0196 §SD3 for why it is shared and lives here).
//
// It has to sit in this package rather than in nanopass, where it started:
// nanopass imports env, so env cannot import nanopass, and env.scanBody parses
// grammar1 too. This package is the one both can already see. Private holders
// per call site would multiply ADR-0084's memory bound by the number of seams
// and split the cache warmth between them.
//
// Hand-written, co-located with generated code — the package_props.go
// precedent (ADR-0080). generate.sh only sweeps clickhouse*.go and *.out.*, so
// this file survives regeneration.
var SharedDFA antlr4utils.DFACache
