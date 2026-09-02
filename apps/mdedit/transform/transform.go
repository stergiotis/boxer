// Package transform is mdedit's pluggable LLM text transformations: a prompt
// corpus (embedded markdown documents, one per transformation) and the one
// call that runs a prompt over a piece of the buffer.
//
// It is a SIBLING package rather than part of mdedit for the reason ADR-0120
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
//
// Nothing here touches imzero2 — every function is callable from a worker
// goroutine, which is where mdedit runs Run (behind a bgjob.Runner).
package transform

import (
	"context"
	"errors"
	"io/fs"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"

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
)

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

// RegisterPromptBook contributes a prompt corpus: an fs.FS whose *.md files
// each define one transformation. Packages call it from init (the
// sqlapplet.RegisterBook shape); mdedit enumerates everything registered the
// first time its transform surface renders. The id names the book in
// diagnostics and must be unique.
func RegisterPromptBook(id string, fsys fs.FS) (err error) {
	if id == "" || fsys == nil {
		err = eh.Errorf("transform: RegisterPromptBook: empty id or nil fs")
		return
	}
	booksMu.Lock()
	defer booksMu.Unlock()
	for _, b := range books {
		if b.id == id {
			err = eb.Build().Str("id", id).Errorf("transform: RegisterPromptBook: duplicate book id")
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
	booksMu.Lock()
	snapshot := make([]promptBook, len(books))
	copy(snapshot, books)
	booksMu.Unlock()
	for _, b := range snapshot {
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

// ParseBook parses every *.md in the book. Pure — the corpus gate test's
// entry point.
func ParseBook(bookId string, fsys fs.FS) (defs []PromptDef, errs []error) {
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			errs = append(errs, eb.Build().Str("bookId", bookId).Str("path", path).Errorf("transform: walk: %w", werr))
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		src, rerr := fs.ReadFile(fsys, path)
		if rerr != nil {
			errs = append(errs, eb.Build().Str("bookId", bookId).Str("path", path).Errorf("transform: read: %w", rerr))
			return nil
		}
		def, perr := ParseDocSource(bookId, path, src)
		if perr != nil {
			errs = append(errs, perr)
			return nil
		}
		defs = append(defs, def)
		return nil
	})
	if err != nil {
		errs = append(errs, eb.Build().Str("bookId", bookId).Errorf("transform: book walk: %w", err))
	}
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
			Errorf("transform: slug must match the required pattern")
		return
	}

	gm := obsidian.New(obsidian.Options{Features: obsidian.FeatureFrontmatter})
	pc := obsidian.NewParserContext()
	gm.Parser().Parse(text.NewReader(src), parser.WithContext(pc))
	meta, ferr := obsidian.TryGetFrontmatter(pc)
	if ferr != nil {
		err = eb.Build().Str("bookId", bookId).Str("path", path).Errorf("transform: frontmatter: %w", ferr)
		return
	}

	def = PromptDef{BookId: bookId, Slug: slug}
	def.Title, _ = meta["title"].(string)
	def.Summary, _ = meta["summary"].(string)
	def.Icon, _ = meta["icon"].(string)
	if def.Title == "" || def.Summary == "" {
		err = eb.Build().Str("bookId", bookId).Str("path", path).Errorf("transform: title and summary are required")
		return
	}
	switch scope, _ := meta["scope"].(string); scope {
	case "", "selection":
		def.Scope = ScopeSelection
	case "document":
		def.Scope = ScopeDocument
	default:
		// A parse error rather than a runtime surprise — the ADR-0132 §SD6
		// posture: the corpus gate is where an unknown value fails.
		err = eb.Build().Str("bookId", bookId).Str("path", path).Str("scope", scope).
			Errorf("transform: unknown scope (known: selection, document)")
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
		err = eb.Build().Str("bookId", bookId).Str("path", path).Errorf("transform: empty prompt body")
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

// Config is the endpoint the transformations run against, resolved from the
// ADR-0009 env registry (transform_env.go).
type Config struct {
	Endpoint string // OpenAI-compatible base URL, e.g. http://localhost:1234/v1
	Model    string
	ApiKey   string // empty is valid — local endpoints take no key
	// MaxTokens is the per-run default; a PromptDef's own value wins.
	MaxTokens int32
	// Timeout bounds one Complete call. The client has no timeout of its own
	// — the context is the only clock.
	Timeout time.Duration
}

// ConfigFromEnv resolves the config. enabled is the registration gate:
// endpoint AND model must both be set — a wrong default model is worse than
// a refusal, so neither has one.
func ConfigFromEnv() (cfg Config, enabled bool) {
	cfg = Config{
		Endpoint:  Endpoint.Get(),
		Model:     Model.Get(),
		ApiKey:    ApiKey.Get(),
		MaxTokens: int32(MaxTokens.Get()),
		Timeout:   Timeout.Get(),
	}
	enabled = cfg.Endpoint != "" && cfg.Model != ""
	return
}

// EndpointHost is the endpoint reduced to its host, for the egress-visibility
// label the UI shows beside the picker (the ADR-0120 §SD3 reasoning: where
// text goes should be readable where it is sent from).
func EndpointHost(endpoint string) (host string) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return endpoint
	}
	return u.Host
}

// NewClient builds the one client mdedit uses. No retry policy on purpose:
// this backs an interactive gesture, and a failed attempt should surface as
// a line the reader can act on rather than as half a minute of silent
// backoff — the re-click IS the retry.
func NewClient(cfg Config) (client openaichat.ClientI, err error) {
	client, err = openaichat.NewClient(cfg.Endpoint, cfg.ApiKey)
	return
}

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
// commitdigest summarizeOnce shape). Blocks until the provider answers, the
// timeout ends it, or ctx is cancelled — never call it on the render
// goroutine.
func Run(ctx context.Context, client openaichat.ClientI, cfg Config, def PromptDef, input string) (res Result, err error) {
	maxTokens := def.MaxTokens
	if maxTokens == 0 {
		maxTokens = cfg.MaxTokens
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	started := time.Now()
	resp, cerr := client.Complete(ctx, openaichat.CompletionRequest{
		ModelId: cfg.Model,
		Messages: []openaichat.Message{
			{Role: openaichat.ChatRoleSystem, Content: def.System},
			{Role: openaichat.ChatRoleUser, Content: input},
		},
		Temperature: def.Temperature,
		MaxTokens:   maxTokens,
	})
	res = Result{
		Content:      resp.Content,
		Elapsed:      time.Since(started),
		InputTokens:  resp.InputTokens,
		OutputTokens: resp.OutputTokens,
	}
	if cerr != nil {
		// A truncated completion still carries its content (the client's
		// documented contract); hand it over marked rather than losing work
		// the provider already did.
		if errors.Is(cerr, openaichat.ErrIncompleteCompletion) && resp.Content != "" {
			res.Truncated = true
			return res, nil
		}
		err = eb.Build().Str("model", cfg.Model).Str("prompt", def.Slug).Errorf("transform: complete: %w", cerr)
		return
	}
	if res.Content == "" {
		err = eb.Build().Str("model", cfg.Model).Str("prompt", def.Slug).Errorf("transform: empty completion")
		return
	}
	return
}

// FailureLine maps a Run error onto the one-line explanation the result pane
// leads with; the wrapped detail stays underneath for whoever wants it.
func FailureLine(err error) (s string) {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.DeadlineExceeded):
		return "the endpoint did not answer within the timeout"
	case errors.Is(err, context.Canceled):
		return "cancelled"
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
