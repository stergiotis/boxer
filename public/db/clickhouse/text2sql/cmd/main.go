// Command text2sql translates natural language to ClickHouse SQL using a local Ollama LLM.
//
// Usage:
//
//	go run ./cmd/text2sql -model qwen2.5-coder:7b -schema schema.json "count orders by country"
//	go run ./cmd/text2sql -model qwen2.5-coder:7b -schema schema.json -interactive
//
// Schema file format (JSON):
//
//	[{
//	  "database": "default",
//	  "table": "orders",
//	  "comment": "Customer orders",
//	  "columns": [
//	    {"name": "id", "type": "UInt64", "comment": "primary key"},
//	    {"name": "customer_id", "type": "UInt64"},
//	    {"name": "amount", "type": "Float64"},
//	    {"name": "country", "type": "String"},
//	    {"name": "created_date", "type": "Date"}
//	  ]
//	}]
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/text2sql"
)

func main() {
	endpoint := flag.String("endpoint", "http://localhost:11434", "Ollama endpoint")
	model := flag.String("model", "qwen3-coder-next", "Ollama model")
	schemaFile := flag.String("schema", "", "path to schema JSON file")
	maxAttempts := flag.Int("attempts", 3, "max LLM retry attempts")
	interactive := flag.Bool("interactive", false, "interactive mode (read from stdin)")
	showRaw := flag.Bool("raw", false, "show raw LLM SQL before normalization")
	showAST := flag.Bool("ast", false, "show AST structure")
	flag.Parse()

	if *schemaFile == "" {
		log.Fatal("--schema is required")
	}

	schema, err := loadSchema(*schemaFile)
	if err != nil {
		log.Fatalf("load schema: %v", err)
	}

	gen := text2sql.New(*endpoint, *model, schema, text2sql.WithMaxAttempts(*maxAttempts))
	ctx := context.Background()

	if *interactive {
		runInteractive(ctx, gen, *showRaw, *showAST)
		return
	}

	if flag.NArg() == 0 {
		log.Fatal("provide a question as argument, or use --interactive")
	}
	question := strings.Join(flag.Args(), " ")
	runOnce(ctx, gen, question, *showRaw, *showAST)
}

func runOnce(ctx context.Context, gen *text2sql.Generator, question string, showRaw, showAST bool) {
	result, err := gen.Generate(ctx, question)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	printResult(result, showRaw, showAST)
}

func runInteractive(ctx context.Context, gen *text2sql.Generator, showRaw, showAST bool) {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Println("text2sql interactive mode. Type your question, or 'quit' to exit.")
	for {
		fmt.Print("\n> ")
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
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			continue
		}
		printResult(result, showRaw, showAST)
	}
}

func printResult(result text2sql.Result, showRaw, showAST bool) {
	if showRaw && result.RawSQL != result.SQL {
		fmt.Println("--- Raw SQL (before normalization) ---")
		fmt.Println(result.RawSQL)
		fmt.Println()
	}
	fmt.Println("--- SQL ---")
	fmt.Println(result.SQL)
	fmt.Printf("\n(model: %s, attempts: %d, tokens: %d)\n", result.Model, result.Attempts, result.TotalTokens)
	if showAST {
		fmt.Println("\n--- AST ---")
		astJSON, err := json.MarshalIndent(result.AST, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "AST marshal error: %v\n", err)
		} else {
			fmt.Println(string(astJSON))
		}
	}
}

func loadSchema(path string) ([]text2sql.SchemaTable, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var schema []text2sql.SchemaTable
	err = json.Unmarshal(data, &schema)
	return schema, err
}
