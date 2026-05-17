package env

import "sort"

// Snapshot returns every registered Spec sorted by Name. It is the
// deterministic input the doc generator (ADR-0009 §4) consumes when
// rendering doc/env-vars.md.
func Snapshot() (out []Spec) {
	out = All()
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return
}

// FormatValue returns the display form of value, redacting per
// Spec.Sensitive. Used by both the doc generator and the runtime "env
// list" subcommand so redaction policy is shared.
func FormatValue(spec Spec, value string) (out string) {
	if spec.Sensitive {
		return "<redacted>"
	}
	return value
}

// The go:generate directive runs envgen with the boxer build tags
// loaded from ../../../tags. Side-effect imports inside envgen pull
// every owner package; without the full tag set, tag-gated declarations
// (e.g. openaichat under llm_generated_opus47) would be skipped from
// the generated registry.
//
//go:generate sh -c "go run -tags=\"$(cat ../../../tags)\" github.com/stergiotis/boxer/internal/cmd/envgen -out ../../../doc/env-vars.md"
