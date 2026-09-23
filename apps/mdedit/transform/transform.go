// Package transform is mdedit's prompt book (ADR-0216 §SD2): the embedded
// corpus of transformations, registered into the shared
// [promptbook] mechanism (ADR-0254 §SD6) and run through the host's model
// over the bus. The types are the shared package's; this package holds
// the book and the names mdedit reads them by.
package transform

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/llm/promptbook"
)

// BookId is the registered id of mdedit's corpus.
const BookId = "mdedit"

type (
	// ScopeE is what a transformation wants as its input.
	ScopeE = promptbook.ScopeE
	// PromptDef is one parsed transformation.
	PromptDef = promptbook.PromptDef
	// Result is one completed run.
	Result = promptbook.Result
	// CompleterI is what a run completes through.
	CompleterI = promptbook.CompleterI
)

const (
	// ScopeSelection runs over the selection when there is one and falls
	// back to the whole document.
	ScopeSelection = promptbook.ScopeSelection
	// ScopeDocument always runs over the whole document.
	ScopeDocument = promptbook.ScopeDocument
)

// All parses mdedit's book and returns its definitions sorted by slug, with
// the per-document errors beside them.
func All() (defs []PromptDef, errs []error) { return promptbook.Book(BookId) }

// Run is one prompt over one input through cli.
var Run = promptbook.Run

// FailureLine maps a Run error onto the one-line explanation the result
// pane leads with.
var FailureLine = promptbook.FailureLine

// ParseBook parses every *.md in a book — the corpus gate test's entry
// point.
var ParseBook = promptbook.ParseBook
