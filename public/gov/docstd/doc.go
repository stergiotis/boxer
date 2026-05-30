//go:build llm_generated_opus48

// Package docstd is the canonical vocabulary for the boxer documentation
// standard's front-matter contract (DOCUMENTATION_STANDARD §4): the
// allowed Diátaxis `type:` values, the per-type `status:` lifecycle
// enums, and a pure validator over a `(type, status)` pair.
//
// It is the single source of truth shared by the two enforcers of that
// contract:
//
//   - github.com/stergiotis/boxer/public/gov/doclint — the repo-wide CI
//     linter (rule DL001) walking every Markdown file under doc-standard
//     scope.
//   - github.com/stergiotis/boxer/public/keelson/runtime/help — the
//     runtime help library validating each app's embedded help corpus.
//
// The two differ in exactly one axis, captured by the allowADR parameter
// of [ValidateFrontmatter]: repo-wide linting accepts `type: adr`, while
// operator-facing inline help does not (an ADR is design history, not
// help). Everything else — the type spellings, the descriptive vs ADR
// status sets, the missing-field precedence — is identical, so it lives
// here once.
//
// The package is intentionally a pure leaf: it imports only the standard
// library, takes already-extracted strings rather than raw bytes, and
// knows nothing about file paths, line numbers, or severity policy. Each
// consumer maps the returned [Violation] values onto its own finding type
// with its own positioning and severity. Front-matter extraction and YAML
// parsing stay with the consumer.
package docstd
