package grammar2

import "github.com/stergiotis/boxer/public/parsing/antlr4utils"

// SharedDFA is the one bounded DFA cache every seam that parses grammar2 uses
// (ADR-0084 for the bound, ADR-0196 §SD3 for why it is shared and lives here).
// grammar2 has only one seam today — nanopass.ParseCanonical — but it carries
// the same WITH-clause ambiguity as grammar1 and the same reason to keep the
// holder next to the ATN it caches.
//
// Hand-written, co-located with generated code — the package_props.go
// precedent (ADR-0080). generate.sh only sweeps clickhouse*.go and *.out.*, so
// this file survives regeneration.
var SharedDFA antlr4utils.DFACache
