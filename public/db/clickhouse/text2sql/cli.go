package text2sql

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	cli "github.com/urfave/cli/v2"
)

// NewCliCommand returns the `text2sql` subcommand. Mounted directly
// under the boxer top-level CLI by public/app/main.go.
//
// In non-interactive mode the question is supplied as positional
// arguments (joined with a single space). Interactive mode reads from
// stdin, prompts for a question per line, and prints the result.
func NewCliCommand() (cmd *cli.Command) {
	cmd = &cli.Command{
		Name:      "text2sql",
		Usage:     "translate natural-language questions into ClickHouse SQL via a local Ollama LLM",
		ArgsUsage: "[question ...]",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "endpoint",
				Value: "http://localhost:11434",
				Usage: "Ollama endpoint",
			},
			&cli.StringFlag{
				Name:  "model",
				Value: "qwen3-coder-next",
				Usage: "Ollama model",
			},
			&cli.StringFlag{
				Name:     "schema",
				Usage:    "path to schema JSON file",
				Required: true,
			},
			&cli.IntFlag{
				Name:  "attempts",
				Value: 3,
				Usage: "max LLM retry attempts",
			},
			&cli.BoolFlag{
				Name:  "interactive",
				Usage: "interactive mode (read questions from stdin)",
			},
			&cli.BoolFlag{
				Name:  "raw",
				Usage: "show raw LLM SQL before normalization",
			},
			&cli.BoolFlag{
				Name:  "ast",
				Usage: "show AST structure",
			},
		},
		Action: runCli,
	}
	return
}

func runCli(ctx *cli.Context) (err error) {
	schemaPath := ctx.String("schema")
	schema, err := loadSchema(schemaPath)
	if err != nil {
		err = eb.Build().Str("schemaPath", schemaPath).Errorf("load schema: %w", err)
		return
	}

	gen := New(ctx.String("endpoint"), ctx.String("model"), schema, WithMaxAttempts(ctx.Int("attempts")))
	cctx := ctx.Context
	showRaw := ctx.Bool("raw")
	showAST := ctx.Bool("ast")

	if ctx.Bool("interactive") {
		runInteractive(cctx, gen, showRaw, showAST)
		return
	}
	args := ctx.Args().Slice()
	if len(args) == 0 {
		err = eb.Build().Errorf("text2sql: provide a question as positional arguments, or use --interactive")
		return
	}
	question := strings.Join(args, " ")
	result, err := gen.Generate(cctx, question)
	if err != nil {
		return
	}
	printResult(result, showRaw, showAST)
	return
}

func runInteractive(ctx context.Context, gen *Generator, showRaw, showAST bool) {
	scanner := bufio.NewScanner(os.Stdin)
	_, _ = fmt.Fprintln(os.Stdout, "text2sql interactive mode. Type your question, or 'quit' to exit.")
	for {
		_, _ = fmt.Fprint(os.Stdout, "\n> ")
		if !scanner.Scan() {
			break
		}
		question := strings.TrimSpace(scanner.Text())
		if question == "" {
			continue
		}
		if question == "quit" || question == "exit" {
			break
		}
		result, err := gen.Generate(ctx, question)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			continue
		}
		printResult(result, showRaw, showAST)
	}
}

func printResult(result Result, showRaw, showAST bool) {
	if showRaw && result.RawSQL != result.SQL {
		_, _ = fmt.Fprintln(os.Stdout, "--- Raw SQL (before normalization) ---")
		_, _ = fmt.Fprintln(os.Stdout, result.RawSQL)
		_, _ = fmt.Fprintln(os.Stdout)
	}
	_, _ = fmt.Fprintln(os.Stdout, "--- SQL ---")
	_, _ = fmt.Fprintln(os.Stdout, result.SQL)
	_, _ = fmt.Fprintf(os.Stdout, "\n(model: %s, attempts: %d, tokens: %d)\n", result.Model, result.Attempts, result.TotalTokens)
	if showAST {
		_, _ = fmt.Fprintln(os.Stdout, "\n--- AST ---")
		astJSON, err := json.MarshalIndent(result.AST, "", "  ")
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "AST marshal error: %v\n", err)
			return
		}
		_, _ = fmt.Fprintln(os.Stdout, string(astJSON))
	}
}

func loadSchema(path string) (schema []SchemaTable, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	err = json.Unmarshal(data, &schema)
	return
}
