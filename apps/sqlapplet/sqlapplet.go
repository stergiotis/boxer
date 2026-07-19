// Package sqlapplet makes SQL-defined applets first-class boxer apps
// (ADR-0132): each applet is one committed markdown document — frontmatter
// as the manifest, prose as the help page, the first `sql` fence as the play
// buffer — and the host mints one real app.Manifest per document, serving
// every instance as an attenuated embedded play (`NewLivePlayApp` with the
// exploration chrome removed).
//
// Contributing packages register applet books via [RegisterBook] (an
// embedded fs.FS of .md documents, mirroring the help facility); the shell
// calls [MintManifests] once at startup, before launch resolution, so
// `--launch <appletId>` and the Apps menu see the minted set.
package sqlapplet

import (
	"io/fs"
	"maps"
	"regexp"
	"sort"
	"strings"

	"github.com/stergiotis/boxer/apps/play"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/help"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// appletIdPrefix is the synthetic-id namespace minted manifests live under
// (the demo-id precedent: a path extended past the owning package). The slug
// completes it; both halves are durably public — renaming a shipped slug is
// a deprecation event (ADR-0132 §SD2).
const appletIdPrefix = "github.com/stergiotis/boxer/apps/sqlapplet/"

// EndpointE selects the server an applet speaks to (ADR-0132 §SD7).
type EndpointE uint8

const (
	// EndpointDefault — the env-configured ClickHouse, exactly as play's own
	// launcher resolves it.
	EndpointDefault EndpointE = iota
	// EndpointIntrospection — the in-process ADR-0094 `/query` endpoint.
	// Today it serves only param-less buffers (its parity gaps are the
	// recorded `chhttp` follow-up).
	EndpointIntrospection
)

// TabSel is one entry of an explicit frontmatter `tabs:` list: a result-panel
// slug, optionally bound to a split node by CTE name (`table:recent`,
// ADR-0132 §SD4 riding ADR-0097 slice 6c).
type TabSel struct {
	ID   string
	Node string
}

// AppletDef is one parsed applet document, ready to mint.
type AppletDef struct {
	Slug     string
	BookID   string
	Title    string
	Icon     string
	Tabs     []TabSel // nil = auto (all result panels; accept/reject decides at render)
	Endpoint EndpointE
	SQL      string
	BandsSQL string // optional `sql bands` aux fence (Timeline panel-local SQL)
	// Class is the ADR-0132 §SD5 security class of SQL, computed at parse
	// time. It gates AutoRun at mount: only QuerySecurityRead applets run on
	// open.
	Class analysis.QuerySecurityClassE
	// HasSlots notes whether the buffer carries `{name:Type}` placeholders;
	// a slotted applet opens with the Live toggle preset (panel-written
	// signals re-run the buffer, ADR-0132 §SD3).
	HasSlots bool
}

// slugPattern is the accepted applet-slug shape. The slug becomes the minted
// Manifest.Id basename and therefore its NATS-safe SubjectAlias — keep it to
// lowercase alphanumerics and dashes.
var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// resultTabIDs are play's result-panel registry slugs an explicit `tabs:`
// list may name (the chrome tabs are never listable — attenuation removes
// them wholesale, ADR-0132 §SD3).
var resultTabIDs = map[string]struct{}{
	"table":      {},
	"projection": {},
	"timeline":   {},
	"world":      {},
	"kanban":     {},
	"network":    {},
	"schema":     {},
	"detail":     {},
}

// fence is one fenced code block of a document: the info-string language,
// the optional role marker (the second info-string token, ADR-0132 §SD1),
// and the body text.
type fence struct {
	Lang string
	Role string
	Text string
}

// scanFences extracts the fenced code blocks of a markdown source. Fences
// open with three backticks at the start of a line (the corpus convention;
// indented fences are not recognized) and close with a bare three-backtick
// line. Nested longer fences are not supported.
func scanFences(src []byte) (fences []fence) {
	lines := strings.Split(string(src), "\n")
	var cur *fence
	var body []string
	for _, line := range lines {
		if cur == nil {
			if !strings.HasPrefix(line, "```") {
				continue
			}
			info := strings.Fields(strings.TrimPrefix(line, "```"))
			cur = &fence{}
			if len(info) > 0 {
				cur.Lang = info[0]
			}
			if len(info) > 1 {
				cur.Role = info[1]
			}
			body = body[:0]
			continue
		}
		if strings.TrimSpace(line) == "```" {
			cur.Text = strings.Join(body, "\n")
			fences = append(fences, *cur)
			cur = nil
			continue
		}
		body = append(body, line)
	}
	return
}

// ParseBook parses every markdown document of one applet book into applet
// definitions. A document without a role-less `sql` fence is a plain prose
// page and yields no definition (a book may carry an overview page); every
// violation of the ADR-0132 §SD1/§SD6 rules yields one error naming the
// document. defs come back sorted by slug.
func ParseBook(bookID string, fsys fs.FS) (defs []*AppletDef, errs []error) {
	book, err := help.NewBook(app.AppIdT(appletIdPrefix+"book/"+bookID), fsys)
	if err != nil {
		errs = append(errs, eh.Errorf("sqlapplet: book %q: %w", bookID, err))
		return
	}
	docs := book.Docs()
	sort.Slice(docs, func(i, j int) bool { return docs[i].Path < docs[j].Path })
	for _, info := range docs {
		def, derr := parseDoc(bookID, book, info)
		if derr != nil {
			errs = append(errs, derr)
			continue
		}
		if def != nil {
			defs = append(defs, def)
		}
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].Slug < defs[j].Slug })
	return
}

// parseDoc parses one document. A nil def with a nil error is a prose page.
func parseDoc(bookID string, book help.BookI, info help.DocInfo) (def *AppletDef, err error) {
	src, ok := book.Source(info.Path)
	if !ok {
		err = eh.Errorf("sqlapplet: %s/%s: source unavailable", bookID, info.Path)
		return
	}
	fences := scanFences(src)
	var primary, bands *fence
	for i := range fences {
		f := &fences[i]
		if f.Lang != "sql" {
			continue
		}
		switch f.Role {
		case "":
			// The FIRST role-less sql fence is the buffer; later ones are
			// prose examples (they keep their Snippets-style Insert buttons
			// when the doc renders as help, and mint nothing).
			if primary == nil {
				primary = f
			}
		case "bands":
			if bands != nil {
				err = eh.Errorf("sqlapplet: %s/%s: more than one `sql bands` fence", bookID, info.Path)
				return
			}
			bands = f
		default:
			err = eh.Errorf("sqlapplet: %s/%s: unknown fence role %q (known: bands)", bookID, info.Path, f.Role)
			return
		}
	}
	if primary == nil {
		if bands != nil {
			err = eh.Errorf("sqlapplet: %s/%s: aux fence without a buffer (no role-less `sql` fence)", bookID, info.Path)
		}
		// No fences at all: a prose page, not an applet.
		return
	}
	sql := strings.TrimSpace(primary.Text)
	if sql == "" {
		err = eh.Errorf("sqlapplet: %s/%s: empty sql buffer", bookID, info.Path)
		return
	}
	slug := strings.TrimSuffix(info.Path, ".md")
	if !slugPattern.MatchString(slug) {
		err = eh.Errorf("sqlapplet: %s/%s: slug %q must match %s", bookID, info.Path, slug, slugPattern.String())
		return
	}
	doc, _, ok := book.Doc(info.Path)
	if !ok {
		err = eh.Errorf("sqlapplet: %s/%s: doc unavailable", bookID, info.Path)
		return
	}
	fm := map[string]any{}
	if kv := doc.Frontmatter(); kv != nil {
		maps.Insert(fm, kv.IteratePairs())
	}
	// The frontmatter key, deliberately not DocInfo.Title — the latter falls
	// back to the first heading, and a minted display name should be an
	// explicit authoring decision.
	title, _ := fm["title"].(string)
	if title == "" {
		err = eh.Errorf("sqlapplet: %s/%s: frontmatter `title` is required", bookID, info.Path)
		return
	}
	def = &AppletDef{
		Slug:   slug,
		BookID: bookID,
		Title:  title,
		SQL:    sql,
	}
	if bands != nil {
		def.BandsSQL = strings.TrimSpace(bands.Text)
	}
	if icon, has := fm["icon"]; has {
		s, isStr := icon.(string)
		if !isStr {
			err = eh.Errorf("sqlapplet: %s/%s: frontmatter `icon` must be a string", bookID, info.Path)
			return
		}
		def.Icon = s
	}
	if def.Endpoint, err = parseEndpoint(bookID, info.Path, fm["endpoint"]); err != nil {
		return
	}
	if def.Tabs, err = parseTabs(bookID, info.Path, fm["tabs"]); err != nil {
		return
	}
	// The §SD5 class, computed once per corpus at parse time. An
	// unclassifiable buffer is a definition error (§SD6), never a runtime
	// surprise — the conservative direction with the corpus as the gate.
	pr, perr := nanopass.Parse(sql)
	if perr != nil {
		err = eh.Errorf("sqlapplet: %s/%s: buffer does not parse (cannot classify, ADR-0132 §SD5/§SD6): %w", bookID, info.Path, perr)
		return
	}
	class, _, cerr := analysis.ClassifyQuerySecurity(pr)
	if cerr != nil {
		err = eh.Errorf("sqlapplet: %s/%s: classify: %w", bookID, info.Path, cerr)
		return
	}
	def.Class = class
	slots, serr := play.ExtractParamSlots(sql)
	if serr == nil {
		def.HasSlots = len(slots) > 0
	}
	return
}

// parseEndpoint maps the frontmatter `endpoint` value; absent or "default"
// is the env-configured ClickHouse.
func parseEndpoint(bookID string, path string, v any) (ep EndpointE, err error) {
	if v == nil {
		return
	}
	s, isStr := v.(string)
	if !isStr {
		err = eh.Errorf("sqlapplet: %s/%s: frontmatter `endpoint` must be a string", bookID, path)
		return
	}
	switch s {
	case "", "default":
		ep = EndpointDefault
	case "introspection":
		ep = EndpointIntrospection
	default:
		err = eh.Errorf("sqlapplet: %s/%s: unknown endpoint %q (known: default, introspection)", bookID, path, s)
	}
	return
}

// parseTabs maps the frontmatter `tabs` value: absent or "auto" is nil (all
// result panels; per-panel accept/reject decides at render, ADR-0132 §SD4);
// a list pins the set and order, each entry `panel` or `panel:ctename`.
func parseTabs(bookID string, path string, v any) (tabs []TabSel, err error) {
	if v == nil {
		return
	}
	if s, isStr := v.(string); isStr {
		if s == "auto" || s == "" {
			return
		}
		err = eh.Errorf("sqlapplet: %s/%s: frontmatter `tabs` must be \"auto\" or a list, got %q", bookID, path, s)
		return
	}
	list, isList := v.([]any)
	if !isList {
		err = eh.Errorf("sqlapplet: %s/%s: frontmatter `tabs` must be \"auto\" or a list", bookID, path)
		return
	}
	seen := make(map[string]struct{}, len(list))
	for _, item := range list {
		entry, isStr := item.(string)
		if !isStr {
			err = eh.Errorf("sqlapplet: %s/%s: `tabs` entries must be strings", bookID, path)
			return
		}
		sel := TabSel{ID: entry}
		if id, node, hasNode := strings.Cut(entry, ":"); hasNode {
			sel.ID = id
			sel.Node = node
			if sel.Node == "" {
				err = eh.Errorf("sqlapplet: %s/%s: `tabs` entry %q has an empty node binding", bookID, path, entry)
				return
			}
		}
		if _, known := resultTabIDs[sel.ID]; !known {
			err = eh.Errorf("sqlapplet: %s/%s: `tabs` entry %q is not a result panel", bookID, path, entry)
			return
		}
		if _, dup := seen[sel.ID]; dup {
			err = eh.Errorf("sqlapplet: %s/%s: `tabs` lists %q twice", bookID, path, sel.ID)
			return
		}
		seen[sel.ID] = struct{}{}
		tabs = append(tabs, sel)
	}
	return
}
