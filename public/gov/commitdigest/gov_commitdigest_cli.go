//go:build llm_generated_opus46

package commitdigest

import (
	"errors"
	"io"
	"os"

	"encoding/json/v2"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/urfave/cli/v2"
)

// RepoChunks is the intermediate format emitted by extract and consumed by summarize.
type RepoChunks struct {
	RepoName string        `json:"repoName"`
	RepoPath string        `json:"repoPath"`
	Chunks   []ChunkResult `json:"chunks"`
}

type ChunkResult struct {
	Index      int32         `json:"index"`
	TokenCount int64         `json:"tokenCount"`
	Summary    string        `json:"summary,omitempty"`
	Metrics    DigestMetrics `json:"metrics"`
	Commits    []CommitEntry `json:"commits"`
}

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:  "commitdigest",
		Usage: "Prepare recent commits from multiple repos for LLM summarization",
		Subcommands: []*cli.Command{
			newExtractCommand(),
			newSummarizeCommand(),
		},
	}
}

func newExtractCommand() *cli.Command {
	return &cli.Command{
		Name:      "extract",
		Usage:     "Collect commits, chunk by token budget, compute metrics, and emit JSON",
		ArgsUsage: "REPO [REPO...]",
		Flags: []cli.Flag{
			// digest collection
			&cli.StringFlag{
				Name:    "since",
				Aliases: []string{"s"},
				Value:   "1 day ago",
				Usage:   "Show commits since this time (git date string, e.g. '4h ago', '1 day ago', '2025-01-01')",
			},
			&cli.StringFlag{
				Name:  "author",
				Usage: "Filter commits by author (substring match)",
			},
			&cli.BoolFlag{
				Name:  "no-stat",
				Usage: "Omit changed-file statistics from output",
			},
			// chunking
			&cli.Int64Flag{
				Name:     "token-budget",
				Usage:    "Max tokens per chunk (required)",
				Required: true,
			},
			&cli.Int64Flag{
				Name:  "reserved-tokens",
				Value: 512,
				Usage: "Tokens reserved for system prompt and sliding window context within each chunk",
			},
			&cli.StringFlag{
				Name:  "encoding",
				Value: "o200k_base",
				Usage: "Tiktoken encoding name (o200k_base, cl100k_base, p50k_base, r50k_base)",
			},
			&cli.Float64Flag{
				Name:  "correction",
				Value: 1.18,
				Usage: "Token count correction multiplier",
			},
			// metrics
			&cli.IntFlag{
				Name:  "hotspot-top",
				Value: 10,
				Usage: "Number of iteration hotspots to report per chunk",
			},
			// ownership
			&cli.BoolFlag{
				Name:  "detect-crossings",
				Usage: "Detect ownership boundary crossings via git blame",
			},
		},
		Action: func(c *cli.Context) error {
			repos := c.Args().Slice()
			if len(repos) == 0 {
				repos = []string{"."}
			}
			since := c.String("since")
			author := c.String("author")
			noStat := c.Bool("no-stat")
			detectCrossings := c.Bool("detect-crossings")

			digests := make([]RepoDigest, 0, len(repos))
			for _, repo := range repos {
				d, err := CollectDigest(c.Context, repo, since, author, noStat)
				if err != nil {
					if errors.Is(err, ErrNotAGitRepo) {
						log.Warn().Str("path", repo).Msg("skipping directory: not a git repository")
						continue
					}
					if errors.Is(err, ErrNoCommits) {
						log.Warn().Str("path", repo).Msg("skipping repository: no commits")
						continue
					}
					return eb.Build().Str("repo", repo).Errorf("unable to collect digest: %w", err)
				}
				digests = append(digests, d)
			}

			counter := &TiktokenCounter{
				Encoding:             c.String("encoding"),
				CorrectionMultiplier: c.Float64("correction"),
			}
			err := counter.Init()
			if err != nil {
				return eh.Errorf("unable to initialize tokenizer: %w", err)
			}

			chunker := &DigestChunker{
				TokenBudget:    c.Int64("token-budget"),
				ReservedTokens: c.Int64("reserved-tokens"),
				Counter:        counter,
			}

			metricsConfig := MetricsConfig{
				HotspotTopN: int32(c.Int("hotspot-top")),
			}

			allRepos := make([]RepoChunks, 0, len(digests))
			for _, d := range digests {
				remaining := d.Commits
				var idx int32
				chunks := make([]ChunkResult, 0, 4)

				for len(remaining) > 0 {
					var chunk Chunk
					var consumed int
					chunk, consumed = chunker.ChunkNext(remaining, idx, 0, d.RepoName)
					remaining = remaining[consumed:]

					chunk.Metrics = ComputeMetrics(chunk.Commits, metricsConfig)

					if detectCrossings {
						var crossings []BoundaryCrossing
						crossings, err = DetectBoundaryCrossings(c.Context, d.RepoPath, chunk.Commits)
						if err != nil {
							return eb.Build().Str("repo", d.RepoName).Int32("chunk", idx).Errorf("unable to detect boundary crossings: %w", err)
						}
						chunk.Metrics.BoundaryCrossings = crossings
					}

					log.Info().
						Int32("chunk", chunk.Index).
						Int64("tokens", chunk.TokenCount).
						Int("commits", len(chunk.Commits)).
						Int("remaining", len(remaining)).
						Str("repo", d.RepoName).
						Msg("chunk extracted")

					chunks = append(chunks, ChunkResult{
						Index:      chunk.Index,
						TokenCount: chunk.TokenCount,
						Metrics:    chunk.Metrics,
						Commits:    chunk.Commits,
					})
					idx++
				}

				allRepos = append(allRepos, RepoChunks{
					RepoName: d.RepoName,
					RepoPath: d.RepoPath,
					Chunks:   chunks,
				})
			}

			err = json.MarshalWrite(os.Stdout, allRepos)
			if err != nil {
				return eh.Errorf("unable to write JSON output: %w", err)
			}
			return nil
		},
	}
}

func newSummarizeCommand() *cli.Command {
	return &cli.Command{
		Name:  "summarize",
		Usage: "Read extract JSON from stdin, call LLM for each chunk, emit JSON with summaries",
		Flags: []cli.Flag{
			// LLM
			&cli.StringFlag{
				Name:  "llm-endpoint",
				Value: "http://localhost:11434/v1",
				Usage: "OpenAI-compatible API base URL (ollama, vllm)",
			},
			&cli.StringFlag{
				Name:     "llm-model",
				Usage:    "Model name for the LLM (required unless --dry-run)",
			},
			&cli.StringFlag{
				Name:  "llm-apikey",
				Usage: "API key (or blank) for LLM provider",
			},
			&cli.IntFlag{
				Name:  "llm-timeout",
				Usage: "Number of seconds to wait for LLM provider",
				Value: 0,
			},
			&cli.IntFlag{
				Name:  "num-ctx",
				Value: 8192,
				Usage: "Context window size for the LLM (ollama num_ctx option)",
			},
			// prompt
			&cli.StringFlag{
				Name:  "system-prompt",
				Usage: "Override the default summarization system prompt",
			},
			// sliding window
			&cli.IntFlag{
				Name:  "window-size",
				Value: 3,
				Usage: "Max number of prior chunk summaries to carry forward as context",
			},
			&cli.StringFlag{
				Name:  "summaries-dir",
				Usage: "Directory to read/write summary .md files for sliding window persistence",
			},
			// debug
			&cli.BoolFlag{
				Name:  "dry-run",
				Usage: "Show what would be sent to the LLM without calling it",
			},
		},
		Action: func(c *cli.Context) error {
			inputData, err := io.ReadAll(os.Stdin)
			if err != nil {
				return eh.Errorf("unable to read stdin: %w", err)
			}

			var repos []RepoChunks
			err = json.Unmarshal(inputData, &repos)
			if err != nil {
				return eh.Errorf("unable to parse extract JSON from stdin: %w", err)
			}

			dryRun := c.Bool("dry-run")
			llmModel := c.String("llm-model")
			if llmModel == "" && !dryRun {
				return eh.Errorf("--llm-model is required unless --dry-run is set: %w", errors.New("missing required flag"))
			}

			systemPrompt := c.String("system-prompt")
			if systemPrompt == "" {
				systemPrompt = DefaultSystemPrompt
			}

			window := &SlidingWindow{
				MaxSummaries: int32(c.Int("window-size")),
				Dir:          c.String("summaries-dir"),
			}
			err = window.LoadFromDir()
			if err != nil {
				return eh.Errorf("unable to load summaries: %w", err)
			}

			llm := &LlmClient{
				Endpoint:   c.String("llm-endpoint"),
				Model:      llmModel,
				NumCtx:     int32(c.Int("num-ctx")),
				ApiKey:     c.String("llm-apikey"),
				TimeoutSec: int32(c.Int("llm-timeout")),
			}
			llm.Init()

			for ri := range repos {
				for ci := range repos[ri].Chunks {
					chunk := &repos[ri].Chunks[ci]

					if dryRun {
						windowContext := window.RenderContext()
						system, user := RenderChunkPrompt(repos[ri].RepoName, chunk.Commits, chunk.Metrics, windowContext, systemPrompt)
						log.Info().
							Int32("chunk", chunk.Index).
							Int64("tokens", chunk.TokenCount).
							Int("commits", len(chunk.Commits)).
							Int("systemLen", len(system)).
							Int("userLen", len(user)).
							Str("repo", repos[ri].RepoName).
							Msg("chunk (dry-run)")
						continue
					}

					windowContext := window.RenderContext()
					system, user := RenderChunkPrompt(repos[ri].RepoName, chunk.Commits, chunk.Metrics, windowContext, systemPrompt)

					var summary string
					summary, err = llm.Summarize(c.Context, system, user)
					if err != nil {
						return eb.Build().Str("repo", repos[ri].RepoName).Int32("chunk", chunk.Index).Errorf("LLM summarization failed: %w", err)
					}

					chunk.Summary = summary
					window.Push(summary)

					err = window.Persist(chunk.Index, chunk.Commits)
					if err != nil {
						return eb.Build().Int32("chunk", chunk.Index).Errorf("unable to persist summary: %w", err)
					}

					log.Info().
						Int32("chunk", chunk.Index).
						Int64("tokens", chunk.TokenCount).
						Int("commits", len(chunk.Commits)).
						Str("repo", repos[ri].RepoName).
						Msg("chunk summarized")
				}
			}

			err = json.MarshalWrite(os.Stdout, repos)
			if err != nil {
				return eh.Errorf("unable to write JSON output: %w", err)
			}
			return nil
		},
	}
}
