// Package promptbook is the transformation book (ADR-0216 §SD2, lifted
// for ADR-0254 §SD6): a corpus of markdown documents whose body IS the
// system prompt and whose frontmatter is the descriptor, parsed to zero
// errors by a gate test, extensible without a rebuild through Register,
// and run one at a time through the host's model (llm.Client).
//
// The mechanism is the applet book's: an embedded fs of documents, the
// filename base as public slug, frontmatter as the descriptor, an open
// Register seam for contributed corpora. mdedit's transformations and
// play's model affordance are two books over the one mechanism.
// Package transform is mdedit's pluggable LLM text transformations: a prompt
// corpus (embedded markdown documents, one per transformation) and the one
// call that runs a prompt over a piece of the buffer.
//
// It is a SIBLING package rather than part of mdedit for the reason the withdrawn ADR-0120
// §SD2 records for play's Ask panel: the app package itself never imports the
// LLM client, so the network dependency is one seam wide, and the capability
// gate (capslock) attributes the egress to this package rather than to
// everything mdedit touches.
//
// A transformation is a markdown document: YAML frontmatter naming it, and a
// body that IS the system prompt — a prompt is prose, and unlike an applet's
// SQL it has no surrounding commentary to fence it off from. Discovery is the
// applet-book shape (sqlapplet.RegisterBook): packages contribute an fs.FS of
// prompt docs at init, the filesystem is the index, and the filename base is
// the slug. The in-tree corpus is gated by a test that parses every embedded
// document; a third-party book that fails to parse logs and skips rather than
// breaking the bar, because the registry is open by design.
package promptbook

import (
	"context"
	"errors"
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"

	"github.com/stergiotis/boxer/public/keelson/runtime/llm"
	"github.com/stergiotis/boxer/public/llm/openaichat"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian"
)

// ScopeE is what a transformation wants as its input.
type ScopeE uint8

const (
	// ScopeSelection runs over the selection when there is one and falls back
	// to the whole document — the default, and what an "improve this" prompt
	// wants.
	ScopeSelection ScopeE = iota
	// ScopeDocument always runs over the whole document, selection or not —
	// what a summary wants.
	ScopeDocument
	// ScopeBuffer runs over a SQL buffer as it stands — what an explain
	// wants (play, ADR-0254 §SD6).
	ScopeBuffer
	// ScopeBufferAndError runs over the buffer and the last run's error —
	// what a fix wants.
	ScopeBufferAndError
	// ScopeQuestion runs over a question the reader types — what an ask
	// wants; its body is a preamble the consumer grounds, not a system
	// prompt it runs alone.
	ScopeQuestion
)

// Name is the frontmatter spelling of a scope.
func (inst ScopeE) Name() (s string) {
	switch inst {
	case ScopeSelection:
		return "selection"
	case ScopeDocument:
		return "document"
	case ScopeBuffer:
		return "buffer"
	case ScopeBufferAndError:
		return "buffer+error"
	case ScopeQuestion:
		return "question"
	}
	return "unknown"
}

// PromptDef is one parsed transformation.
type PromptDef struct {
	// BookId is the corpus the definition came from and Slug the filename
	// base — together the definition's identity, and durably public the way
	// an applet slug is.
	BookId string
	Slug   string

	// Title and Summary are the picker's line and its tooltip; both required.
	// Icon is optional and conventionally an emoji.
	Title   string
	Summary string
	Icon    string

	Scope ScopeE

	// Temperature overrides the provider default when non-nil; MaxTokens
	// overrides Config.MaxTokens when non-zero.
	Temperature *float32
	MaxTokens   int32

	// System is the whole document body after the frontmatter — the system
	// prompt, verbatim.
	System string
}

// Purpose is the definition's identity as a call names it — `book/slug`,
// the spelling keelson('llm_calls').purpose carries so it joins
// keelson('llm_prompts') exactly (ADR-0254 §SD4).
func (inst PromptDef) Purpose() (s string) { return inst.BookId + "/" + inst.Slug }

// slugPattern is the applet-book rule: the filename base is public identity.
var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// ---------------------------------------------------------------------------
// The registry
// ---------------------------------------------------------------------------

type promptBook struct {
	id   string
	fsys fs.FS
}

var (
	booksMu sync.Mutex
	books   []promptBook
)

// Register contributes a prompt corpus: an fs.FS whose *.md files
// each define one transformation. Packages call it from init (the
// sqlapplet.RegisterBook shape); mdedit enumerates everything registered the
// first time its transform surface renders. The id names the book in
// diagnostics and must be unique.
func Register(id string, fsys fs.FS) (err error) {
	if id == "" || fsys == nil {
		err = eh.Errorf("promptbook: Register: empty id or nil fs")
		return
	}
	booksMu.Lock()
	defer booksMu.Unlock()
	for _, b := range books {
		if b.id == id {
			err = eb.Build().Str("id", id).Errorf("promptbook: Register: duplicate book id")
			return
		}
	}
	books = append(books, promptBook{id: id, fsys: fsys})
	return
}

// All parses every registered book and returns the definitions sorted by
// (book, slug). Errors are per-document and returned beside the definitions
// that did parse: the in-tree corpus is test-gated to zero errors, and a
// contributed book that fails should cost its own entries, not the bar.
func All() (defs []PromptDef, errs []error) {
	return Book("")
}

// Book is All narrowed to one registered book; the empty id is every book.
func Book(id string) (defs []PromptDef, errs []error) {
	booksMu.Lock()
	snapshot := make([]promptBook, len(books))
	copy(snapshot, books)
	booksMu.Unlock()
	for _, b := range snapshot {
		if id != "" && b.id != id {
			continue
		}
		d, e := ParseBook(b.id, b.fsys)
		defs = append(defs, d...)
		errs = append(errs, e...)
	}
	sort.Slice(defs, func(i, j int) bool {
		if defs[i].BookId != defs[j].BookId {
			return defs[i].BookId < defs[j].BookId
		}
		return defs[i].Slug < defs[j].Slug
	})
	return
}

// ---------------------------------------------------------------------------
// Parsing
// ---------------------------------------------------------------------------

// Document is one *.md of a book as the walk found it: the parsed
// definition, or the error that kept it out of the picker. A failed
// document is a row, not an absence, so a contributed book that drifts is
// visible in keelson('llm_prompts') rather than silently short.
type Document struct {
	BookId string
	Path   string
	Def    PromptDef
	Err    error
}

// Documents walks every *.md in the book, parsing each. Pure.
func Documents(bookId string, fsys fs.FS) (docs []Document) {
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			docs = append(docs, Document{BookId: bookId, Path: path, Err: eb.Build().Str("bookId", bookId).Str("path", path).Errorf("promptbook: walk: %w", werr)})
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		src, rerr := fs.ReadFile(fsys, path)
		if rerr != nil {
			docs = append(docs, Document{BookId: bookId, Path: path, Err: eb.Build().Str("bookId", bookId).Str("path", path).Errorf("promptbook: read: %w", rerr)})
			return nil
		}
		def, perr := ParseDocSource(bookId, path, src)
		docs = append(docs, Document{BookId: bookId, Path: path, Def: def, Err: perr})
		return nil
	})
	if err != nil {
		docs = append(docs, Document{BookId: bookId, Err: eb.Build().Str("bookId", bookId).Errorf("promptbook: book walk: %w", err)})
	}
	return
}

// ParseBook parses every *.md in the book. Pure — the corpus gate test's
// entry point.
func ParseBook(bookId string, fsys fs.FS) (defs []PromptDef, errs []error) {
	for _, doc := range Documents(bookId, fsys) {
		if doc.Err != nil {
			errs = append(errs, doc.Err)
			continue
		}
		defs = append(defs, doc.Def)
	}
	return
}

// AllDocuments walks every registered book, sorted by (book, path).
func AllDocuments() (docs []Document) {
	booksMu.Lock()
	snapshot := make([]promptBook, len(books))
	copy(snapshot, books)
	booksMu.Unlock()
	for _, b := range snapshot {
		docs = append(docs, Documents(b.id, b.fsys)...)
	}
	sort.Slice(docs, func(i, j int) bool {
		if docs[i].BookId != docs[j].BookId {
			return docs[i].BookId < docs[j].BookId
		}
		return docs[i].Path < docs[j].Path
	})
	return
}

// ParseDocSource parses one prompt document. The slug is the filename base,
// the frontmatter names the transformation, and the whole body after the
// frontmatter is the system prompt.
func ParseDocSource(bookId string, path string, src []byte) (def PromptDef, err error) {
	base := path
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	slug := strings.TrimSuffix(base, ".md")
	if !slugPattern.MatchString(slug) {
		err = eb.Build().Str("bookId", bookId).Str("path", path).Str("pattern", slugPattern.String()).
			Errorf("promptbook: slug must match the required pattern")
		return
	}

	gm := obsidian.New(obsidian.Options{Features: obsidian.FeatureFrontmatter})
	pc := obsidian.NewParserContext()
	gm.Parser().Parse(text.NewReader(src), parser.WithContext(pc))
	meta, ferr := obsidian.TryGetFrontmatter(pc)
	if ferr != nil {
		err = eb.Build().Str("bookId", bookId).Str("path", path).Errorf("promptbook: frontmatter: %w", ferr)
		return
	}

	def = PromptDef{BookId: bookId, Slug: slug}
	def.Title, _ = meta["title"].(string)
	def.Summary, _ = meta["summary"].(string)
	def.Icon, _ = meta["icon"].(string)
	if def.Title == "" || def.Summary == "" {
		err = eb.Build().Str("bookId", bookId).Str("path", path).Errorf("promptbook: title and summary are required")
		return
	}
	switch scope, _ := meta["scope"].(string); scope {
	case "", ScopeSelection.Name():
		def.Scope = ScopeSelection
	case ScopeDocument.Name():
		def.Scope = ScopeDocument
	case ScopeBuffer.Name():
		def.Scope = ScopeBuffer
	case ScopeBufferAndError.Name():
		def.Scope = ScopeBufferAndError
	case ScopeQuestion.Name():
		def.Scope = ScopeQuestion
	default:
		err = eb.Build().Str("bookId", bookId).Str("path", path).Str("scope", scope).
			Errorf("promptbook: unknown scope (known: selection, document, buffer, buffer+error, question)")
		return
	}
	if t, ok := frontmatterFloat(meta["temperature"]); ok {
		def.Temperature = &t
	}
	if n, ok := frontmatterInt(meta["max-tokens"]); ok {
		def.MaxTokens = n
	}

	def.System = strings.TrimSpace(string(src[frontmatterEnd(string(src)):]))
	if def.System == "" {
		err = eb.Build().Str("bookId", bookId).Str("path", path).Errorf("promptbook: empty prompt body")
		return
	}
	return
}

// frontmatterFloat reads a YAML number, which arrives as int or float64
// depending on how it was written.
func frontmatterFloat(v any) (f float32, ok bool) {
	switch n := v.(type) {
	case float64:
		return float32(n), true
	case int:
		return float32(n), true
	}
	return 0, false
}

func frontmatterInt(v any) (n int32, ok bool) {
	switch m := v.(type) {
	case int:
		return int32(m), true
	case float64:
		return int32(m), true
	}
	return 0, false
}

// frontmatterEnd returns the offset just past the closing `---` line of a
// leading YAML frontmatter block, or 0 when there is none. A copy of
// writingstylescope's helper of the same name — goldmark-meta consumes the
// block but does not report where it ended.
func frontmatterEnd(src string) (end int) {
	rest, found := strings.CutPrefix(src, "---\n")
	if !found {
		if rest, found = strings.CutPrefix(src, "---\r\n"); !found {
			return
		}
	}
	for _, closer := range []string{"\n---\n", "\n---\r\n"} {
		if idx := strings.Index(rest, closer); idx >= 0 {
			candidate := len(src) - len(rest) + idx + len(closer)
			if end == 0 || candidate < end {
				end = candidate
			}
		}
	}
	return
}

// ---------------------------------------------------------------------------
// Running
// ---------------------------------------------------------------------------

// CompleterI is what a run completes through: the host's model over the
// bus (*llm.Client, ADR-0254), a fake in tests.
type CompleterI interface {
	Complete(ctx context.Context, req llm.Request) (res llm.Response, err error)
}

var _ CompleterI = (*llm.Client)(nil)

// Result is one completed run.
type Result struct {
	Content string
	Elapsed time.Duration

	InputTokens  int32
	OutputTokens int32

	// Truncated marks a completion that hit the token ceiling with content
	// already produced. Shown, not hidden: the reader sees exactly what they
	// would apply and decides.
	Truncated bool
}

// Run is one prompt over one input: a system+user round-trip (the
// commitdigest summarizeOnce shape) through the host's model. Blocks until
// the host answers, the client's wait ends it, or ctx is cancelled — never
// call it on the render goroutine. The model and the endpoint are the
// host's (ADR-0254 §SD2); the prompt's own max-tokens wins over the host's
// ceiling, and zero leaves the ceiling to the host.
func Run(ctx context.Context, cli CompleterI, def PromptDef, input string) (res Result, err error) {
	started := time.Now()
	resp, cerr := cli.Complete(ctx, llm.Request{
		Purpose: def.Purpose(),
		Messages: []openaichat.Message{
			{Role: openaichat.ChatRoleSystem, Content: def.System},
			{Role: openaichat.ChatRoleUser, Content: input},
		},
		Temperature: def.Temperature,
		MaxTokens:   def.MaxTokens,
	})
	res = Result{
		Content:      resp.Content,
		Elapsed:      time.Since(started),
		InputTokens:  resp.InputTokens,
		OutputTokens: resp.OutputTokens,
	}
	if cerr != nil {
		err = eb.Build().Str("prompt", def.Slug).Errorf("promptbook: complete: %w", cerr)
		return
	}
	if resp.Incomplete {
		// A truncated completion still carries its content (the client's
		// documented contract); hand it over marked rather than losing work
		// the provider already did. With nothing produced it is a failure.
		if res.Content == "" {
			err = eb.Build().Str("prompt", def.Slug).Errorf("promptbook: complete: %w", openaichat.ErrIncompleteCompletion)
			return
		}
		res.Truncated = true
		return
	}
	if res.Content == "" {
		err = eb.Build().Str("prompt", def.Slug).Errorf("promptbook: empty completion")
		return
	}
	return
}

// FailureLine maps a Run error onto the one-line explanation the result pane
// leads with; the wrapped detail stays underneath for whoever wants it.
func FailureLine(err error) (s string) {
	var refused *llm.RefusedError
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.DeadlineExceeded):
		return "the endpoint did not answer within the timeout"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.As(err, &refused):
		return "the host declined the request: " + refused.Reason
	case errors.Is(err, openaichat.ErrAuth):
		return "the endpoint refused the API key"
	case errors.Is(err, openaichat.ErrModelNotFound):
		return "the endpoint does not serve this model"
	case errors.Is(err, openaichat.ErrRateLimited):
		return "the endpoint is rate-limiting"
	case errors.Is(err, openaichat.ErrServer):
		return "the endpoint failed server-side"
	case errors.Is(err, openaichat.ErrBadRequest):
		return "the endpoint rejected the request"
	}
	return "the request failed"
}
