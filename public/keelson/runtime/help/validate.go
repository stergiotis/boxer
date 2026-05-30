package help

import (
	"github.com/stergiotis/boxer/public/gov/docstd"
)

// Problem is one documentation-standard front-matter conformance issue for
// a single help document, as surfaced by [ValidateDocInfo] and
// [BookI.Validate]. It carries the doc path so a caller iterating a whole
// book can attribute the breach; field, value, and message come straight
// from the shared [docstd.Violation].
type Problem struct {
	// DocPath is the FS-relative path (minus the `.md` suffix) of the
	// offending document — the same key [BookI.Doc] takes.
	DocPath string
	// Field is the offending front-matter key: "type" or "status".
	Field string
	// Value is the offending value, or "" when the field is absent.
	Value string
	// Message is a self-contained, human-readable description of the breach.
	Message string
}

// ValidateDocInfo checks one document's parsed front-matter (its `type`
// and `status`) against the boxer documentation standard and returns one
// [Problem] per breach. An empty result means the doc conforms.
//
// Inline help is operator-facing, so `type: adr` is rejected here even
// though repo-wide linting accepts it — an ADR is design history, not help
// (see [docstd.ValidateFrontmatter]). Title is deliberately not validated:
// the library's frontmatter→H1→filename fallback (see [DocInfo]) makes a
// missing `title:` a non-issue by design.
func ValidateDocInfo(info DocInfo) (problems []Problem) {
	for _, v := range docstd.ValidateFrontmatter(info.Type, info.Status, false) {
		problems = append(problems, Problem{
			DocPath: info.Path,
			Field:   v.Field,
			Value:   v.Value,
			Message: v.Message,
		})
	}
	return
}

// Validate checks every indexed document's front-matter and returns the
// problems in path-sorted order (empty when the whole book conforms). Like
// the other index methods it triggers the one-shot walk + parse on first
// call; the same problems are emitted as Warn logs during that walk.
func (inst *book) Validate() (problems []Problem) {
	inst.ensureIndex()
	for i := range inst.index {
		problems = append(problems, ValidateDocInfo(inst.index[i])...)
	}
	return
}
