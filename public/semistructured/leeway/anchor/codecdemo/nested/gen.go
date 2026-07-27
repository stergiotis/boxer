package nested

// Regeneration of record for this package's `*.out.go` codecs — the nested
// attribute-model spellings (ADR-0113). Same `--target=anchor` rationale as the
// parent package; see its gen.go.

//go:generate sh -c "go run -tags=\"$(cat ../../../../../../tags)\" github.com/stergiotis/boxer/public/app keelsoncodec --target=anchor labeledtextnested.go lineagenested.go manytagsdoc.go namedtextnested.go optnotedoc.go textdocnested.go"
