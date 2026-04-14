//go:build llm_generated_opus46

package commitdigest

import (
	"errors"
	"os"
	"strings"

	"encoding/json/v2"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/urfave/cli/v2"
)

type ChunkResult struct {
	Index      int32         `json:"index"`
	TokenCount int64         `json:"tokenCount"`
	Summary    string        `json:"summary"`
	Metrics    DigestMetrics `json:"metrics"`
	Commits    []CommitEntry `json:"commits"`
}

func NewCliCommand() *cli.Command {
	return &cli.Command{
		Name:      "commitdigest",
		Usage:     "Prepare recent commits from multiple repos for LLM summarization",
		ArgsUsage: "REPO [REPO...]",
		Flags: []cli.Flag{
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
			&cli.Int64Flag{
				Name:  "token-budget",
				Usage: "Max tokens per chunk. When >0, enables chunked summarization mode",
			},
			&cli.Int64Flag{
				Name:  "reserved-tokens",
				Value: 512,
				Usage: "Tokens reserved for system prompt and prior summaries within each chunk",
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
			&cli.IntFlag{
				Name:  "window-size",
				Value: 3,
				Usage: "Max number of prior chunk summaries to carry forward as context",
			},
			&cli.StringFlag{
				Name:  "summaries-dir",
				Usage: "Directory to read/write summary .md files for sliding window persistence",
			},
			&cli.StringFlag{
				Name:  "llm-endpoint",
				Value: "http://localhost:11434/v1",
				Usage: "OpenAI-compatible API base URL (ollama, vllm)",
			},
			&cli.StringFlag{
				Name:  "llm-model",
				Usage: "Model name for the LLM (required in chunked mode)",
			},
			&cli.StringFlag{
				Name:  "llm-apikey",
				Usage: "API key (or blank) for LLM provider",
			},
			&cli.IntFlag{
				Name:  "llm-timeout",
				Usage: "number of seconds to wait for LLM provider",
				Value: 0,
			},
			&cli.StringFlag{
				Name:  "known-authors",
				Usage: "Comma-separated author emails; commits from others are flagged as foreign",
			},
			&cli.IntFlag{
				Name:  "hotspot-top",
				Value: 10,
				Usage: "Number of iteration hotspots to report per chunk",
			},
			&cli.StringFlag{
				Name:  "system-prompt",
				Usage: "Override the default summarization system prompt",
			},
			&cli.IntFlag{
				Name:  "num-ctx",
				Value: 8192,
				Usage: "Context window size for the LLM (ollama num_ctx option)",
			},
			&cli.BoolFlag{
				Name:  "dry-run",
				Usage: "Show chunk boundaries, token counts, and metrics without calling the LLM",
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
					return eh.Errorf("failed to collect digest for %q: %w", repo, err)
				}
				digests = append(digests, d)
			}

			tokenBudget := c.Int64("token-budget")
			if tokenBudget <= 0 {
				err := WriteDigest(os.Stdout, digests)
				if err != nil {
					return eh.Errorf("failed to write digest: %w", err)
				}
				return nil
			}

			// chunked summarization mode
			llmModel := c.String("llm-model")
			if llmModel == "" && !c.Bool("dry-run") {
				return eh.Errorf("--llm-model is required in chunked mode (--token-budget > 0) unless --dry-run is set")
			}

			counter := &TiktokenCounter{
				Encoding:             c.String("encoding"),
				CorrectionMultiplier: c.Float64("correction"),
			}
			err := counter.Init()
			if err != nil {
				return eh.Errorf("failed to initialize tokenizer: %w", err)
			}

			window := &SlidingWindow{
				MaxSummaries: int32(c.Int("window-size")),
				Dir:          c.String("summaries-dir"),
			}
			err = window.LoadFromDir()
			if err != nil {
				return eh.Errorf("failed to load summaries: %w", err)
			}

			chunker := &DigestChunker{
				TokenBudget:    tokenBudget,
				ReservedTokens: c.Int64("reserved-tokens"),
				Counter:        counter,
			}

			metricsConfig := MetricsConfig{
				HotspotTopN: int32(c.Int("hotspot-top")),
			}
			if knownAuthors := c.String("known-authors"); knownAuthors != "" {
				metricsConfig.KnownAuthors = strings.Split(knownAuthors, ",")
			}

			systemPrompt := c.String("system-prompt")
			if systemPrompt == "" {
				systemPrompt = DefaultSystemPrompt
			}

			llm := &LlmClient{
				Endpoint:   c.String("llm-endpoint"),
				Model:      llmModel,
				NumCtx:     int32(c.Int("num-ctx")),
				ApiKey:     c.String("llm-apikey"),
				TimeoutSec: int32(c.Int("llm-timeout")),
			}
			llm.Init()

			dryRun := c.Bool("dry-run")

			allResults := make([]ChunkResult, 0, 16)
			for _, d := range digests {
				remaining := d.Commits
				var idx int32
				headerTokens := counter.CountTokens(RenderRepoHeader(d.RepoName))
				systemPromptTokens := counter.CountTokens(systemPrompt)

				for len(remaining) > 0 {
					windowTokens := window.ContextTokenCount(counter)
					overheadTokens := headerTokens + systemPromptTokens + windowTokens

					var chunk Chunk
					var consumed int
					chunk, consumed = chunker.ChunkNext(remaining, idx, overheadTokens, d.RepoName)
					remaining = remaining[consumed:]

					chunk.Metrics = ComputeMetrics(chunk.Commits, metricsConfig)

					if dryRun {
						log.Info().
							Int32("chunk", chunk.Index).
							Int64("tokens", chunk.TokenCount).
							Int64("overhead", overheadTokens).
							Int("commits", len(chunk.Commits)).
							Int("remaining", len(remaining)).
							Str("repo", d.RepoName).
							Msg("chunk (dry-run)")

						allResults = append(allResults, ChunkResult{
							Index:      chunk.Index,
							TokenCount: chunk.TokenCount,
							Metrics:    chunk.Metrics,
							Commits:    chunk.Commits,
						})
						idx++
						continue
					}

					windowContext := window.RenderContext()
					system, user := RenderChunkPrompt(d.RepoName, chunk.Commits, chunk.Metrics, windowContext, systemPrompt)

					var summary string
					summary, err = llm.Summarize(c.Context, system, user)
					if err != nil {
						return eh.Errorf("LLM summarization failed for chunk %d of %q: %w", chunk.Index, d.RepoName, err)
					}

					window.Push(summary)
					err = window.Persist(chunk.Index, chunk.Commits)
					if err != nil {
						return eh.Errorf("failed to persist summary for chunk %d: %w", chunk.Index, err)
					}

					log.Info().
						Int32("chunk", chunk.Index).
						Int64("tokens", chunk.TokenCount).
						Int64("overhead", overheadTokens).
						Int("commits", len(chunk.Commits)).
						Int("remaining", len(remaining)).
						Str("repo", d.RepoName).
						Msg("chunk summarized")

					allResults = append(allResults, ChunkResult{
						Index:      chunk.Index,
						TokenCount: chunk.TokenCount,
						Summary:    summary,
						Metrics:    chunk.Metrics,
						Commits:    chunk.Commits,
					})
					idx++
				}
			}

			err = json.MarshalWrite(os.Stdout, allResults)
			if err != nil {
				return eh.Errorf("failed to write JSON output: %w", err)
			}
			return nil
		},
	}
}
