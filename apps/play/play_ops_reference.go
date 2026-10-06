package play

// The reference half of play's catalog: the snippet libraries — play's own
// and every contributed one — and the vocabulary of functions a buffer may
// call, as an agent reads them. Both are the build's, not the window's, and
// are the app's own text, so they are not marked untrusted.

import (
	"regexp"
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlvocab"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/help/search"
)

const (
	opListSnippets  = "list_snippets"
	opReadSnippet   = "read_snippet"
	opListFunctions = "list_functions"
)

// Bounds on what the reference queries return.
const (
	refMaxSnippets  = 200
	refMaxHits      = 25
	refMaxText      = 6 << 10
	refMaxFunctions = 150
	refMaxDoc       = 300
)

// SnippetSearchArgs is list_snippets' argument.
type SnippetSearchArgs struct {
	Search string `json:",omitzero" desc:"words to find in the snippets; every snippet when left out"`
}

// SnippetRef names one snippet.
type SnippetRef struct {
	Library string `desc:"the library: snippets for play's own, else a contributed library's tab id"`
	Section string `desc:"the section slug read_snippet takes"`
	Heading string `desc:"the section's heading"`
}

// SnippetList is list_snippets' result.
type SnippetList struct {
	Snippets  []SnippetRef `desc:"the snippets"`
	Truncated bool         `desc:"true when the bound cut the list; search to narrow it"`
}

// SnippetArgs is read_snippet's argument.
type SnippetArgs struct {
	Library string `desc:"the library, as list_snippets names it"`
	Section string `desc:"the section slug, as list_snippets names it"`
}

// Snippet is read_snippet's result.
type Snippet struct {
	Library   string   `desc:"the library"`
	Section   string   `desc:"the section slug"`
	Heading   string   `desc:"the section's heading"`
	Text      string   `desc:"the section as markdown: what the query does and when to use it"`
	Sql       []string `desc:"the section's SQL blocks, each ready for set_sql"`
	Truncated bool     `desc:"true when the text was cut"`
}

// FunctionArgs is list_functions' argument.
type FunctionArgs struct {
	Search string `json:",omitzero" desc:"words or regular expressions, all of which must match a function's name, doc and family taken together, as in the Vocabulary pane's filter; every function when left out"`
	Where  string `json:",omitzero" desc:"server, client or host; every population when left out"`
}

// FunctionInfo is one function a buffer may call.
type FunctionInfo struct {
	Name   string `desc:"the function's name"`
	Call   string `desc:"a call template with the parameter names"`
	Doc    string `desc:"what it does, cut at 300 bytes"`
	Where  string `desc:"server: a SQL UDF that must be installed on the endpoint; client: expanded by play before the statement ships, so it works anywhere; host: computed in play over a sub-query's rows"`
	Family string `desc:"the family it belongs to"`
	// Available is false for a reserved name that refuses.
	Available bool   `desc:"false for a name the vocabulary reserves but does not implement"`
	Installed string `desc:"for a server function: yes or no once play's Vocabulary pane has probed the endpoint, else unknown"`
	// Dependencies are server functions a client macro expands into.
	Dependencies []string `desc:"server functions a client macro's expansion calls; the macro works only where they are installed"`
	// MissingDependencies is filled only once the probe answered: empty
	// before then means not known, not all present.
	MissingDependencies []string `desc:"those of Dependencies the endpoint lacks, once play's Vocabulary pane has probed it; empty before then"`
}

// FunctionList is list_functions' result.
type FunctionList struct {
	Functions []FunctionInfo `desc:"the functions"`
	Probed    bool           `desc:"true when installed reflects the endpoint"`
	Truncated bool           `desc:"true when the bound cut the list; search or where narrow it"`
	// AlsoMatching is the thesaurus' expansions of the search, as the pane
	// shows them under its filter.
	AlsoMatching string `json:",omitzero" desc:"what the search also matched for, from the Vocabulary pane's thesaurus: typed word → alternates"`
	// UnlistedExtras counts endpoint functions left out because their names
	// are not plain identifiers, so their text never reaches the model.
	UnlistedExtras int `json:",omitzero" desc:"functions on the endpoint that no roster declares and whose names are not plain identifiers; counted, not listed"`
}

func addReferenceOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	appops.Query(s, app.OperationSpec{Name: opListSnippets, Version: 1,
		Summary: "list the snippet libraries' worked queries, or find them by words", Agents: true,
		Follows: []string{"read_snippet reads one with its SQL; set_sql puts it in the buffer"}},
		func(sn opsSnap, in SnippetSearchArgs) (SnippetList, error) {
			return listSnippets(referenceLibraries(), in.Search), nil
		})
	appops.Query(s, app.OperationSpec{Name: opReadSnippet, Version: 1,
		Summary: "read one snippet: what it does and its SQL", Agents: true},
		func(sn opsSnap, in SnippetArgs) (Snippet, error) {
			return readSnippet(referenceLibraries(), in.Library, in.Section)
		})
	appops.Query(s, app.OperationSpec{Name: opListFunctions, Version: 2,
		Summary: "list the functions boxer adds to a query in play — its server UDFs, client macros and host functions, not ClickHouse's built-ins: where each runs, its parameters and whether the endpoint has it", Agents: true,
		Follows: []string{"once the Vocabulary pane has probed the endpoint, functions it carries that no roster declares are listed too, under their own family"}},
		func(sn opsSnap, in FunctionArgs) (FunctionList, error) {
			return listFunctions(sqlvocab.Default, sn.installed, sn.probed, in.Search, in.Where)
		})
}

// snippetLibrary is one library as the reference queries read it.
type snippetLibrary struct {
	key string
	src *snippetSource
}

// referenceLibraries are play's own library and every contributed one.
func referenceLibraries() (libs []snippetLibrary) {
	libs = append(libs, snippetLibrary{key: builtinSnippetsKey, src: builtinSnippetSource()})
	for _, r := range registeredSnippetLibraries() {
		libs = append(libs, snippetLibrary{key: r.lib.TabID, src: r.src()})
	}
	return
}

// plainIdentifier is a function name list_functions repeats from the endpoint.
var plainIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// snippetSqlFence matches a fenced block and its language.
var snippetSqlFence = regexp.MustCompile("(?ms)^```([A-Za-z]*)[^\\n]*\\n(.*?)^```")

// sqlBlocks are the snippet blocks of a section's text: those the Snippets
// pane offers Insert and Replace for.
func sqlBlocks(text string) (blocks []string) {
	for _, m := range snippetSqlFence.FindAllStringSubmatch(text, -1) {
		if sqlBlockActionable("", m[1]) {
			blocks = append(blocks, strings.TrimSpace(m[2]))
		}
	}
	return
}

// sectionTexts are a library's sections by slug: heading and own text.
func sectionTexts(src *snippetSource) (order []string, heading map[string]string, text map[string]string) {
	heading, text = make(map[string]string), make(map[string]string)
	if src == nil || src.text == "" {
		return
	}
	for _, sp := range search.SliceSections(src.text, src.sections) {
		if sp.Slug == "" {
			continue
		}
		if _, dup := text[sp.Slug]; !dup {
			order = append(order, sp.Slug)
		}
		heading[sp.Slug], text[sp.Slug] = sp.Heading, src.text[sp.Start:sp.End]
	}
	return
}

func listSnippets(libs []snippetLibrary, query string) (out SnippetList) {
	query = strings.TrimSpace(query)
	for _, lib := range libs {
		order, heading, text := sectionTexts(lib.src)
		add := func(slug string) bool {
			if len(sqlBlocks(text[slug])) == 0 {
				return true
			}
			if len(out.Snippets) == refMaxSnippets {
				out.Truncated = true
				return false
			}
			out.Snippets = append(out.Snippets, SnippetRef{Library: lib.key, Section: slug, Heading: heading[slug]})
			return true
		}
		if query == "" {
			for _, slug := range order {
				if !add(slug) {
					return
				}
			}
			continue
		}
		if lib.src == nil || lib.src.index == nil {
			continue
		}
		seen := map[string]bool{}
		for _, h := range lib.src.index.Search(search.ParseQueryWith(query, snippetThesaurus()), refMaxHits) {
			if h.Ref.Doc != lib.src.docName || seen[h.Ref.Section] {
				continue
			}
			seen[h.Ref.Section] = true
			if !add(h.Ref.Section) {
				return
			}
		}
	}
	return
}

func readSnippet(libs []snippetLibrary, library string, section string) (out Snippet, err error) {
	for _, lib := range libs {
		if lib.key != library {
			continue
		}
		_, heading, text := sectionTexts(lib.src)
		t, ok := text[section]
		if !ok {
			err = app.RefuseOperation("no section " + section + " in " + library + "; list_snippets names them")
			return
		}
		out = Snippet{Library: library, Section: section, Heading: heading[section], Sql: sqlBlocks(t), Text: t}
		if len(out.Text) > refMaxText {
			out.Text, out.Truncated = truncateBytes(out.Text, refMaxText), true
		}
		return
	}
	err = app.RefuseOperation("no snippet library " + library + "; list_snippets names them")
	return
}

func listFunctions(r *sqlvocab.Registry, installed map[string]string, probed bool, query string, where string) (out FunctionList, err error) {
	var want sqlvocab.WhereE
	switch strings.TrimSpace(where) {
	case "":
	case "server":
		want = sqlvocab.WhereServer
	case "client":
		want = sqlvocab.WhereClient
	case "host":
		want = sqlvocab.WhereHost
	default:
		err = app.RefuseOperation("where is server, client or host")
		return
	}
	entries := vocabDeclared(r)
	if probed {
		vocabMarkInstalled(entries, installed)
		// The pane lists what the endpoint carries and no roster declares;
		// so does the operation. A name that is not a plain identifier is
		// counted, not listed: it is the endpoint's text, and this
		// operation's output is the build's.
		for _, e := range vocabExtras(installed, entries) {
			if !plainIdentifier.MatchString(e.Name) {
				out.UnlistedExtras++
				continue
			}
			entries = append(entries, e)
		}
	}
	sortVocabByName(entries)
	var battery search.Battery
	if q := strings.TrimSpace(query); q != "" {
		battery = search.ParseQueryWith(q, snippetThesaurus())
		out.AlsoMatching = battery.AlternatesHint()
	}
	out.Probed = probed
	for _, e := range entries {
		if want != 0 && e.Where != want {
			continue
		}
		if !battery.IsZero() && !vocabMatches(&battery, e) {
			continue
		}
		if len(out.Functions) == refMaxFunctions {
			out.Truncated = true
			break
		}
		f := FunctionInfo{Name: e.Name, Call: e.call(), Doc: e.Doc, Where: e.Where.String(), Family: e.Family,
			Available: e.Available, Dependencies: slices.Clone(e.Dependencies), MissingDependencies: slices.Clone(e.MissingDeps)}
		if len(f.Doc) > refMaxDoc {
			f.Doc = truncateBytes(f.Doc, refMaxDoc)
		}
		if e.Where == sqlvocab.WhereServer {
			switch {
			case !probed:
				f.Installed = "unknown"
			case e.Installed || !e.Declared:
				// An undeclared entry is one the probe found there.
				f.Installed = "yes"
			default:
				f.Installed = "no"
			}
		}
		out.Functions = append(out.Functions, f)
	}
	return
}
