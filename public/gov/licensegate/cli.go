package licensegate

import (
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	cli "github.com/urfave/cli/v2"
)

// NewCliCommand returns the `license-gate` subcommand. Mounted under
// boxer's `gov` parent by public/gov/gov.go.
//
// The CycloneDX 1.6 SBOM is produced upstream by cyclonedx-gomod and the
// Rust crate trees arrive as `cargo metadata` documents (see
// scripts/ci/license_gate.sh). This command applies the inbound-license
// policy declared in policy.go to both; ADR-0004 captures the rationale
// and ADR-0246 the Rust half.
func NewCliCommand() (cmd *cli.Command) {
	cmd = &cli.Command{
		Name:  "license-gate",
		Usage: "apply the boxer inbound-license policy to a CycloneDX JSON SBOM and Rust crate trees (ADR-0004, ADR-0246)",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "sbom",
				Usage: "path to CycloneDX JSON SBOM (the Go module graph)",
			},
			&cli.StringSliceFlag{
				Name:  "cargo-metadata",
				Usage: "path to `cargo metadata --locked --format-version 1` output; repeat once per crate tree",
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
	cargoMetadataPaths := ctx.StringSlice("cargo-metadata")
	csvPath := ctx.String("csv")
	violations, err := Run(sbomPath, cargoMetadataPaths, csvPath)
	if err != nil {
		return
	}
	if violations > 0 {
		err = eb.Build().Int("violations", violations).Errorf("license-gate: policy violations found")
	}
	return
}
