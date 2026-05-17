// envgen renders the boxer env-var registry to a Diátaxis reference
// markdown file. Backs the //go:generate directive in
// public/config/env/doc_gen.go (ADR-0009 §4). The renderer lives in
// public/config/env/envdoc so pebble2impl's mirror can reuse it.
//
// The blank imports below load every boxer package that declares an
// env-var Spec; declaration registers the spec process-globally, then
// envdoc.Render returns the sorted view.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/stergiotis/boxer/public/config/env/envdoc"

	// Side-effect: load every boxer-owned env-var declaration.
	_ "github.com/stergiotis/boxer/public/dev"
	_ "github.com/stergiotis/boxer/public/docgen"
	_ "github.com/stergiotis/boxer/public/llm/openaichat"
	_ "github.com/stergiotis/boxer/public/observability/logging"
	_ "github.com/stergiotis/boxer/public/observability/tracing"
	_ "github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
)

func main() {
	outPath := flag.String("out", "", "output markdown file ('-' for stdout)")
	flag.Parse()
	if *outPath == "" {
		fmt.Fprintln(os.Stderr, "envgen: -out is required")
		os.Exit(2)
	}
	body := envdoc.Render(envdoc.Options{
		GeneratorPath:  "internal/cmd/envgen",
		RegenerateHint: "go generate ./public/config/env/...",
	})
	if *outPath == "-" {
		_, err := os.Stdout.WriteString(body)
		if err != nil {
			fmt.Fprintf(os.Stderr, "envgen: write stdout: %v\n", err)
			os.Exit(1)
		}
		return
	}
	err := os.WriteFile(*outPath, []byte(body), 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "envgen: write %s: %v\n", *outPath, err)
		os.Exit(1)
	}
}
