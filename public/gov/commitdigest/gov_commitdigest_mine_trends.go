package commitdigest

import (
	_ "embed"
	"io"
	"os"
	"os/exec"

	"encoding/json/v2"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/urfave/cli/v2"
)

//go:embed sql/trend_mining.sql
var trendMiningSQL string

func newMineTrendsCommand() *cli.Command {
	return &cli.Command{
		Name:  "mine-trends",
		Usage: "Read extract JSON from stdin, emit a window-level trend digest via clickhouse-local",
		Description: "Runs the bundled trend-mining SQL via clickhouse-local over the flattened " +
			"file-change rows derived from extract output. Output is a single JSON object covering " +
			"mode distribution, path-prefix churn, follow-up density, revert events, temporal " +
			"coupling edges, net-LOC series, and hot files. Intended to feed `synthesize-threads`.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "clickhouse-binary",
				Value: "clickhouse-local",
				Usage: "Path to the clickhouse-local executable (looked up in PATH if not absolute)",
			},
		},
		Action: func(c *cli.Context) error {
			bin := c.String("clickhouse-binary")
			binPath, lookupErr := exec.LookPath(bin)
			if lookupErr != nil {
				return eb.Build().Str("binary", bin).Errorf(
					"clickhouse-local not found; install from https://clickhouse.com/docs/en/install "+
						"or set --clickhouse-binary to an absolute path: %w", lookupErr)
			}

			inputData, err := io.ReadAll(os.Stdin)
			if err != nil {
				return eh.Errorf("unable to read stdin: %w", err)
			}
			var repos []RepoChunks
			err = json.Unmarshal(inputData, &repos)
			if err != nil {
				return eh.Errorf("unable to parse extract JSON from stdin: %w", err)
			}

			// clickhouse-local reads piped stdin via ParallelParsingBlockInputFormat
			// which has ragged behaviour when Go wraps a non-*os.File Reader —
			// manifests as silent 0-byte output. Route flattened rows through a
			// named temp file and invoke with --file / --table, which is the
			// documented form anyway.
			flatFile, err := os.CreateTemp("", "mine-trends-flat-*.jsonl")
			if err != nil {
				return eh.Errorf("unable to create flat-rows tempfile: %w", err)
			}
			defer func() {
				removeErr := os.Remove(flatFile.Name())
				if removeErr != nil && !os.IsNotExist(removeErr) {
					log.Warn().Str("path", flatFile.Name()).Err(removeErr).Msg("unable to remove flat-rows tempfile")
				}
			}()

			err = WriteFlattenedJSONEachRow(flatFile, repos)
			closeErr := flatFile.Close()
			if err != nil {
				return eh.Errorf("unable to write flattened rows: %w", err)
			}
			if closeErr != nil {
				return eh.Errorf("unable to close flat-rows tempfile: %w", closeErr)
			}

			cmd := exec.CommandContext(c.Context, binPath,
				"--input-format=JSONEachRow",
				"--structure="+FlatCommitChangeStructure,
				"--file="+flatFile.Name(),
				"--output_format_json_escape_forward_slashes=0",
				"--query="+trendMiningSQL,
			)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			err = cmd.Run()
			if err != nil {
				return eb.Build().Str("binPath", binPath).Errorf("clickhouse-local failed: %w", err)
			}
			return nil
		},
	}
}
