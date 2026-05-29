//go:build llm_generated_opus47

// keelsoncodec is the standalone CLI for the keelson Go ↔ leeway codec
// generator (ADR-0042). Invoked from ./generate.sh and ad-hoc by
// contributors.
//
// Usage:
//
//	keelsoncodec [--target=facts|anchor] <dto1.go> [<dto2.go> ...]
//
// For each input DTO source, writes a sibling `<name>.out.go`.
//
// --target selects the wrapper around boxerstaging/leeway/marshallgen's
// schema-agnostic core:
//
//   - facts (default): factswrapper.FactsWrapper — vdd-resolved kind
//     ids, dml_cbor pool, ActiveSections / ActiveFields hints,
//     Marshal / Unmarshal methods, buscodec.CodecI bridge.
//   - anchor: marshallgen.NoOpWrapper — schema-agnostic surface only
//     (Columns / Append / Row / derived interfaces / BuildEntities /
//     FillFromArrow). The caller wires the generated helpers into
//     whatever schema-specific Marshal / Unmarshal they want.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/stergiotis/boxer/public/semistructured/leeway/marshallgen"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/factswrapper"

	"flag"
)

func main() {
	targetStr := flag.String("target", "facts", "schema family target (facts|anchor)")
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: keelsoncodec [--target=facts|anchor] <dto1.go> [<dto2.go> ...]")
		os.Exit(2)
	}

	var failed int
	for _, inputPath := range args {
		if !strings.HasSuffix(inputPath, ".go") || strings.HasSuffix(inputPath, ".out.go") {
			fmt.Fprintf(os.Stderr, "keelsoncodec: %s: input must be a .go file (not .out.go)\n", inputPath)
			failed++
			continue
		}
		outputPath := strings.TrimSuffix(inputPath, ".go") + ".out.go"

		var err error
		switch *targetStr {
		case "", "facts":
			_, err = factswrapper.FactsWrapper{}.Generate(inputPath, outputPath)
		case "anchor":
			_, err = marshallgen.Generate(inputPath, outputPath, marshallgen.NoOpWrapper{})
		default:
			err = fmt.Errorf("unknown target %q (want facts|anchor)", *targetStr)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "keelsoncodec: %s: %v\n", inputPath, err)
			failed++
			continue
		}
		fmt.Fprintf(os.Stderr, "keelsoncodec: wrote %s (target=%s)\n", outputPath, *targetStr)
	}
	if failed > 0 {
		os.Exit(1)
	}
}
