//go:build llm_generated_opus46

package commitdigest

import (
	"errors"
	"io"
	"os"
	"strings"

	"encoding/json/v2"

	"github.com/go-json-experiment/json/jsontext"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/urfave/cli/v2"
)

// Thread is one durable narrative arc identified over a chunk window. The
// registry is written by synthesize-threads and consumed by summarize via
// --thread-registry; chunk summaries anchor their themes to these ids.
type Thread struct {
	ID                  string     `json:"id"`
	Title               string     `json:"title"`
	Span                ThreadSpan `json:"span"`
	Summary             string     `json:"summary"`
	ComplexityDirection string     `json:"complexityDirection"`
	PathPrefixes        []string   `json:"pathPrefixes"`
	AnchorCommits       []string   `json:"anchorCommits"`
}

type ThreadSpan struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

const ThreadSynthesisSystemPrompt = `You are an analyst that identifies durable narrative threads from a statistical trend digest over a multi-week window of git history.

Input: a single JSON object with fields windowSummaryCore, windowSummaryChurn, modeDistributionByWeek, pathPrefixChurnByWeek, followUpDensityByWeek, netLocByWeek, revertEvents, temporalCouplingEdges, hotFiles.

Output: a JSON array of 3–7 thread objects. Each thread:
- id: kebab-case slug, max 40 chars, globally unique within this registry
- title: human-readable phrase, max 80 chars, no trailing period
- span: {"start": "YYYY-MM-DD", "end": "YYYY-MM-DD"} — dates must fall within the window bounds given in windowSummaryCore
- summary: 1–2 sentences describing what the thread is about
- complexityDirection: one of "build-up" / "shed" / "stable" / "mixed", chosen from netLocByWeek and hotFiles evidence
- pathPrefixes: array of 1–4 top-level path prefixes primarily affected, drawn from pathPrefixChurnByWeek
- anchorCommits: array of 1–5 commit short hashes from revertEvents or hotFiles most representative of this thread; may be empty if no specific commit stands out

Rules:
- Threads must be anchored in the data — never invent path prefixes that do not appear in the digest, never cite dates outside the window bounds.
- Prefer long-running threads (multi-week spans) over single-week variation.
- Consolidate related sub-themes into one thread rather than emitting many narrow threads.
- Do NOT wrap the output in markdown code fences. Do NOT add prose before or after. Emit only the JSON array.
- If the window is too small or featureless to support 3 threads, emit fewer — but never zero; at minimum, summarise the dominant activity as one thread.
`

func newSynthesizeThreadsCommand() *cli.Command {
	return &cli.Command{
		Name:  "synthesize-threads",
		Usage: "Read trend digest JSON from stdin, emit a thread registry via one LLM call",
		Description: "Reads the output of `mine-trends` from stdin, calls the configured LLM " +
			"once with a thread-extraction prompt, and writes a JSON array of 3–7 threads to stdout. " +
			"The registry is designed to be passed back to `summarize --thread-registry=<path>` so " +
			"per-chunk summaries anchor their themes to a shared vocabulary.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "llm-endpoint",
				Value: "http://localhost:11434/v1",
				Usage: "OpenAI-compatible API base URL (ollama, vllm)",
			},
			&cli.StringFlag{
				Name:  "llm-model",
				Usage: "Model name for the LLM (required unless --dry-run)",
			},
			&cli.StringFlag{
				Name:    "llm-apikey",
				EnvVars: []string{"LLM_API_KEY"},
				Usage:   "API key for LLM provider. Prefer the env var to avoid exposing the secret in process argv.",
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
			&cli.BoolFlag{
				Name:  "dry-run",
				Usage: "Print the prompt that would be sent to the LLM and exit without calling",
			},
		},
		Action: func(c *cli.Context) error {
			inputData, err := io.ReadAll(os.Stdin)
			if err != nil {
				return eh.Errorf("unable to read stdin: %w", err)
			}
			if len(inputData) == 0 {
				return eh.Errorf("empty trend digest on stdin: %w", errors.New("no input"))
			}

			dryRun := c.Bool("dry-run")
			llmModel := c.String("llm-model")
			if llmModel == "" && !dryRun {
				return eh.Errorf("--llm-model is required unless --dry-run is set: %w", errors.New("missing required flag"))
			}

			user := "Here is the trend digest:\n\n" + string(inputData)

			if dryRun {
				_, err = os.Stdout.Write([]byte("# system\n"))
				if err != nil {
					return eh.Errorf("unable to write dry-run header: %w", err)
				}
				_, err = os.Stdout.Write([]byte(ThreadSynthesisSystemPrompt))
				if err != nil {
					return eh.Errorf("unable to write dry-run system prompt: %w", err)
				}
				_, err = os.Stdout.Write([]byte("\n# user\n"))
				if err != nil {
					return eh.Errorf("unable to write dry-run user header: %w", err)
				}
				_, err = os.Stdout.Write([]byte(user))
				if err != nil {
					return eh.Errorf("unable to write dry-run user prompt: %w", err)
				}
				return nil
			}

			llm := &LlmClient{
				Endpoint:   c.String("llm-endpoint"),
				Model:      llmModel,
				NumCtx:     int32(c.Int("num-ctx")),
				ApiKey:     c.String("llm-apikey"),
				TimeoutSec: int32(c.Int("llm-timeout")),
			}
			llm.Init()

			raw, err := llm.Summarize(c.Context, ThreadSynthesisSystemPrompt, user)
			if err != nil {
				return eh.Errorf("LLM thread synthesis failed: %w", err)
			}

			registry, err := parseThreadRegistry(raw)
			if err != nil {
				return eb.Build().Str("raw", truncateForError(raw, 500)).Errorf("LLM response was not a valid thread registry: %w", err)
			}

			err = json.MarshalEncode(jsontext.NewEncoder(os.Stdout, jsontext.Multiline(true), jsontext.WithIndent("  ")), registry)
			if err != nil {
				return eh.Errorf("unable to write thread registry: %w", err)
			}
			return nil
		},
	}
}

// parseThreadRegistry strips any surrounding markdown code fences and parses
// the LLM response as a JSON array of Thread. Defensive because instruction-
// tuned models occasionally add ```json fences despite being told not to.
func parseThreadRegistry(raw string) (threads []Thread, err error) {
	cleaned := stripCodeFences(strings.TrimSpace(raw))
	err = json.Unmarshal([]byte(cleaned), &threads)
	if err != nil {
		err = eh.Errorf("unable to unmarshal thread registry: %w", err)
		return
	}
	if len(threads) == 0 {
		err = eh.Errorf("thread registry is empty — prompt rule demands at least one thread: %w", errors.New("empty registry"))
		return
	}
	return
}

func stripCodeFences(s string) (out string) {
	out = s
	if strings.HasPrefix(out, "```") {
		// Drop the opening fence line (``` or ```json or ```JSON).
		if idx := strings.IndexByte(out, '\n'); idx >= 0 {
			out = out[idx+1:]
		}
	}
	if strings.HasSuffix(out, "```") {
		out = strings.TrimSuffix(out, "```")
	}
	out = strings.TrimSpace(out)
	return
}

func truncateForError(s string, max int) (out string) {
	if len(s) <= max {
		out = s
		return
	}
	out = s[:max] + "…"
	return
}
