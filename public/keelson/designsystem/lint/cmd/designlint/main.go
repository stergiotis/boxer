// Command designlint is the `go vet -vettool=` binary that enforces the
// ImZero2 Design System Tier 1 mechanical rules (ADR-0029 §SD8). It is a
// multichecker over the shipped L-rule analyzers; build it to a tempfile
// and drive it through go vet:
//
//	tmpbin=$(mktemp -t designlint.XXXXXX)
//	go build -tags "$(cat tags)" -o "$tmpbin" ./public/keelson/designsystem/lint/cmd/designlint
//	go vet -vettool="$tmpbin" -tags "$(cat tags)" ./public/thestack/imzero2/...
//
// scripts/ci/lint.sh wires exactly this as the warn-only "designlint" step.
//
// Why a standalone package main and not a boxer.sh subcommand: the
// `go vet -vettool=` protocol requires a binary that speaks the unitchecker
// wire format, which multichecker.Main provides and a urfave/cli command
// cannot. This is the one sanctioned exception to the CODINGSTANDARDS
// "Entry Points" rule, grandfathered in scripts/ci/entry-points-baseline.txt.
//
// Build tags must be passed through `go vet -tags=`; multichecker.Main's own
// -tags flag is a deprecated no-op (feedback_multichecker_tags_deprecated).
package main

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/lint/l10stroke"
	"github.com/stergiotis/boxer/public/keelson/designsystem/lint/l11motion"
	"github.com/stergiotis/boxer/public/keelson/designsystem/lint/l12manifestid"
	"github.com/stergiotis/boxer/public/keelson/designsystem/lint/l1labelcasing"
	"github.com/stergiotis/boxer/public/keelson/designsystem/lint/l2color"
	"github.com/stergiotis/boxer/public/keelson/designsystem/lint/l3spacing"
	"github.com/stergiotis/boxer/public/keelson/designsystem/lint/l4rounding"
	"github.com/stergiotis/boxer/public/keelson/designsystem/lint/l5allocrect"
	"github.com/stergiotis/boxer/public/keelson/designsystem/lint/l9radiochanged"
	"golang.org/x/tools/go/analysis/multichecker"
)

// main runs the L-rule analyzers under the unitchecker protocol. The order
// mirrors the L-numbering for readable `go vet` output; multichecker sorts
// internally so it is presentational only.
func main() {
	multichecker.Main(
		l1labelcasing.Analyzer,
		l2color.Analyzer,
		l3spacing.Analyzer,
		l4rounding.Analyzer,
		l5allocrect.Analyzer,
		l9radiochanged.Analyzer,
		l10stroke.Analyzer,
		l11motion.Analyzer,
		l12manifestid.Analyzer,
	)
}
