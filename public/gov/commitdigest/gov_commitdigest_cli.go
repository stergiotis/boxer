package commitdigest

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	"encoding/json/v2"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/llm/openaichat"
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
			newMineTrendsCommand(),
			newSynthesizeThreadsCommand(),
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
			// resume
			&cli.StringFlag{
				Name:  "resume-dir",
				Usage: "Directory holding cursors.json from a prior summarize run (usually same as --summaries-dir). For repos with a cursor, extracts commits since that hash instead of applying --since.",
			},
			&cli.BoolFlag{
				Name:  "reset-cursor",
				Usage: "Ignore existing cursor entries and fall back to --since. Cursors.json is not deleted; subsequent summarize runs will overwrite.",
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
			resumeDir := c.String("resume-dir")
			resetCursor := c.Bool("reset-cursor")

			var cursors CursorMap
			if resumeDir != "" && !resetCursor {
				var loadErr error
				cursors, loadErr = LoadCursors(resumeDir)
				if loadErr != nil {
					return loadErr
				}
			}

			digests := make([]RepoDigest, 0, len(repos))
			for _, repo := range repos {
				absPath, absErr := filepath.Abs(repo)
				if absErr != nil {
					return eb.Build().Str("repo", repo).Errorf("unable to resolve repo path: %w", absErr)
				}
				repoName := filepath.Base(absPath)

				var fromHash string
				if cursor, ok := cursors[repoName]; ok {
					if validateErr := ValidateCursorHash(c.Context, absPath, cursor.LastCommitHash); validateErr != nil {
						return eb.Build().Str("repo", repoName).Str("hash", cursor.LastCommitHash).Str("cursorsFile", filepath.Join(resumeDir, cursorsFileName)).Errorf("cursor references unknown hash (history rewritten?); delete the cursors file or pass --reset-cursor: %w", validateErr)
					}
					fromHash = cursor.LastCommitHash
					log.Info().Str("repo", repoName).Str("fromHash", shortHash(fromHash)).Msg("resuming from cursor")
				}

				d, err := CollectDigest(c.Context, repo, since, author, noStat, fromHash)
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
				Name:    "llm-apikey",
				EnvVars: []string{"LLM_API_KEY"},
				Usage:   "API key for non-Gemini LLM providers (LM Studio, generic OpenAI-compat). Prefer LLM_API_KEY env to avoid exposing the secret in process argv.",
			},
			&cli.StringFlag{
				Name:  "gemini-api-key",
				Usage: "Gemini API key. When set, overrides --llm-apikey; when set empty, walks GEMINI_API_KEY → GOOGLE_API_KEY → ~/.config/gemini/api_key. Auto-engaged when --llm-endpoint points at generativelanguage.googleapis.com.",
			},
			&cli.IntFlag{
				Name:  "llm-timeout",
				Usage: "Per-call timeout in seconds; 0 keeps the legacy 120s default",
				Value: 0,
			},
			&cli.IntFlag{
				Name:  "num-ctx",
				Value: 8192,
				Usage: "Ollama options.num_ctx pass-through; set to 0 for OpenAI / Gemini endpoints (which reject the unknown field)",
			},
			// prompt
			&cli.StringFlag{
				Name:  "system-prompt",
				Usage: "Override the default summarization system prompt",
			},
			&cli.StringFlag{
				Name:  "thread-registry",
				Usage: "Path to a thread registry JSON (output of `synthesize-threads`). When set, chunk summaries anchor their themes to registered thread IDs.",
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

			var renderedRegistry string
			registryPath := c.String("thread-registry")
			if registryPath != "" {
				registryData, readErr := os.ReadFile(registryPath)
				if readErr != nil {
					return eb.Build().Str("path", registryPath).Errorf("unable to read thread registry: %w", readErr)
				}
				var threads []Thread
				readErr = json.Unmarshal(registryData, &threads)
				if readErr != nil {
					return eb.Build().Str("path", registryPath).Errorf("unable to parse thread registry JSON: %w", readErr)
				}
				renderedRegistry = RenderThreadRegistry(threads)
			}

			summariesDir := c.String("summaries-dir")
			window := &SlidingWindow{
				MaxSummaries: int32(c.Int("window-size")),
				Dir:          summariesDir,
			}
			err = window.LoadFromDir()
			if err != nil {
				return eh.Errorf("unable to load summaries: %w", err)
			}

			cursors, err := LoadCursors(summariesDir)
			if err != nil {
				return err
			}

			apiKey, err := resolveLlmApiKey(c)
			if err != nil {
				return eh.Errorf("resolve api key: %w", err)
			}
			llm, err := openaichat.NewClient(c.String("llm-endpoint"), apiKey)
			if err != nil {
				return eh.Errorf("new llm client: %w", err)
			}
			defer func() { _ = llm.Close() }()
			numCtx := int32(c.Int("num-ctx"))
			timeoutSec := c.Int("llm-timeout")
			if timeoutSec <= 0 {
				timeoutSec = 120
			}

			for ri := range repos {
				for ci := range repos[ri].Chunks {
					chunk := &repos[ri].Chunks[ci]

					if dryRun {
						windowContext := window.RenderContext()
						system, user := RenderChunkPrompt(repos[ri].RepoName, chunk.Commits, chunk.Metrics, windowContext, systemPrompt, renderedRegistry)
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
					system, user := RenderChunkPrompt(repos[ri].RepoName, chunk.Commits, chunk.Metrics, windowContext, systemPrompt, renderedRegistry)

					var summary string
					summary, err = summarizeOnce(c.Context, llm, llmModel, numCtx, timeoutSec, system, user)
					if err != nil {
						return eb.Build().Str("repo", repos[ri].RepoName).Int32("chunk", chunk.Index).Errorf("LLM summarization failed: %w", err)
					}

					chunk.Summary = summary
					window.Push(summary)

					err = window.Persist(chunk.Index, chunk.Commits)
					if err != nil {
						return eb.Build().Int32("chunk", chunk.Index).Errorf("unable to persist summary: %w", err)
					}

					if summariesDir != "" {
						if cursor, ok := NewCursorForChunk(chunk.Index, chunk.Commits); ok {
							cursors[repos[ri].RepoName] = cursor
							err = SaveCursors(summariesDir, cursors)
							if err != nil {
								return eb.Build().Str("repo", repos[ri].RepoName).Int32("chunk", chunk.Index).Errorf("unable to persist cursor: %w", err)
							}
						}
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
