package licensegate

import (
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	cli "github.com/urfave/cli/v2"
)

// NewCliCommand returns the `license-gate` subcommand. Mounted under
// boxer's `gov` parent by public/gov/gov.go.
//
// The CycloneDX 1.6 SBOM is produced upstream by cyclonedx-gomod (see
// scripts/ci/license_gate.sh). This command consumes that JSON and
// applies the inbound-license policy declared in policy.go; ADR-0004
// captures the rationale.
func NewCliCommand() (cmd *cli.Command) {
	cmd = &cli.Command{
		Name:  "license-gate",
		Usage: "apply the boxer inbound-license policy to a CycloneDX JSON SBOM (ADR-0004)",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "sbom",
				Usage:    "path to CycloneDX JSON SBOM",
				Required: true,
			},
			&cli.StringFlag{
				Name:  "csv",
				Usage: "optional output CSV inventory path (written only if non-empty)",
			},
		},
		Action: runCli,
	}
	return
}

func runCli(ctx *cli.Context) (err error) {
	sbomPath := ctx.String("sbom")
	csvPath := ctx.String("csv")
	violations, err := Run(sbomPath, csvPath)
	if err != nil {
		return
	}
	if violations > 0 {
		err = eb.Build().Int("violations", violations).Errorf("license-gate: policy violations found")
	}
	return
}
