package play

// complete_sql and validate_sql's literal check: the Completion pane's
// engine (ADR-0190) at an offset of a statement the caller names, rather
// than at the person's caret. The engine here is built per call over the
// in-process providers and over copies of what the pane's probes have
// already landed; it never starts a probe, so an endpoint domain nothing
// has asked about yet answers "not ready" (the list_functions stance of
// ADR-0270, update of 2026-10-02).

import (
	"maps"
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/containers"
	"github.com/stergiotis/boxer/public/db/clickhouse/chtype"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/highlight"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlcomplete"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlvocab"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
)

const opCompleteSql = "complete_sql"

// Bounds on what complete_sql returns, and on validate_sql's literals.
const (
	completeMaxCandidates = 100
	completeMaxDoc        = 200
	validateMaxLiterals   = 50
)

// CompleteArgs is complete_sql's argument.
type CompleteArgs struct {
	Sql    string `json:",omitzero" desc:"the statement to complete in; the buffer as get_state shows it when left out"`
	Offset *int32 `json:",omitzero" desc:"the byte offset in the statement to complete at; its end when left out"`
}

// CompleteCandidate is one thing that may stand at the offset.
type CompleteCandidate struct {
	Text   string   `desc:"the candidate itself, unquoted"`
	Insert string   `json:",omitzero" desc:"the spelling that names it at the offset, quoted where the position takes a literal and none is open; left out when it equals text"`
	Kind   string   `desc:"what it is: kind, field, table, column, function, channel, aspect, gloss, gloss key, …"`
	Type   string   `json:",omitzero" desc:"its ClickHouse type, for a field or a column"`
	Doc    string   `json:",omitzero" desc:"one line about it, cut at 200 bytes"`
	Source string   `json:",omitzero" desc:"where it came from: an in-process registry, or a system table of the endpoint"`
	Marks  []string `json:",omitzero" desc:"the Vocabulary pane's marks for a function, such as MISSING on this endpoint"`
}

// CompleteRange is a byte range of the statement.
type CompleteRange struct {
	Start int32 `desc:"the first byte"`
	Stop  int32 `desc:"one past the last byte"`
}

// CompleteResult is complete_sql's result.
type CompleteResult struct {
	Offset   int32  `desc:"the byte offset completed at"`
	Domain   string `json:",omitzero" desc:"what belongs at the offset, such as a component kind or a gloss key of argument 1; empty when the engine resolved nothing"`
	Callee   string `json:",omitzero" desc:"the call whose argument the offset is in"`
	Argument int32  `json:",omitzero" desc:"the argument's position in that call, 1 for the first; 0 when the offset is not in an argument"`
	// Typed and Replace are what an edit at the offset would replace.
	Typed      string              `json:",omitzero" desc:"what is already typed of the token before the offset; candidates extend it"`
	Replace    CompleteRange       `desc:"the token a candidate replaces, as a byte range of the statement"`
	Match      string              `desc:"the token against the candidates: exact, prefix or none"`
	Exact      string              `json:",omitzero" desc:"the candidate equal to the whole token, when there is one"`
	Candidates []CompleteCandidate `json:",omitzero" desc:"the candidates extending what is typed, all when nothing is"`
	Total      int32               `desc:"how many candidates extend what is typed before the bound"`
	Truncated  bool                `desc:"true when the bound of 100 cut the candidates; type more of the token to narrow them"`
	Silent     string              `json:",omitzero" desc:"why nothing is offered"`
	// NotReady is the endpoint half: a domain only a probe answers, which
	// this read does not start.
	NotReady []string `json:",omitzero" desc:"endpoint listings this answer needed that no pane has probed yet, so their candidates are missing; list_tables and describe_table read the catalog under the grant"`
}

// completionOpsCopy is what the pane's probes have landed, copied on the
// render goroutine: the catalog memo and column types, and the endpoint's
// function listing. The memo's values are replaced, never mutated, so a
// shallow copy is enough.
type completionOpsCopy struct {
	memo        map[string][]sqlcomplete.Item
	types       map[string]chtype.Type
	allFuncs    *containers.BinarySearchGrowingKV[string, string]
	userDefined map[string]string
	probed      bool
}

// completionOpsView is what complete_sql and validate_sql read.
type completionOpsView struct {
	landed *completionOpsCopy
	client *Client
}

// completionView copies the probes' answers when one has landed since the
// last copy; completionProbeGen only grows, so it is the generation.
func (inst *PlayApp) completionView() (out completionOpsView) {
	out.client = inst.client
	gen := inst.completionProbeGen()
	cache := &inst.paneViews
	if cache.completion != nil && cache.completionGen == gen {
		out.landed = cache.completion
		return
	}
	cp := &completionOpsCopy{}
	if cat := inst.completion.catalog; cat != nil {
		cp.memo = maps.Clone(cat.memo)
		cp.types = maps.Clone(cat.types)
	}
	if v := inst.vocab; v != nil && v.installed != nil {
		cp.allFuncs, cp.userDefined, cp.probed = v.installed, v.userDefined, true
	}
	cache.completion, cache.completionGen = cp, gen
	out.landed = cp
	return
}

// notReadyLog collects the endpoint listings a call needed and lacked.
type notReadyLog struct{ names []string }

func (inst *notReadyLog) add(name string) {
	if !slices.Contains(inst.names, name) {
		inst.names = append(inst.names, name)
	}
}

// opsCompletionEngine is an engine over the in-process providers and, when
// landed is given, the copied endpoint answers. It is built per call: the
// engine's memo is unsynchronised.
func opsCompletionEngine(view completionOpsView, missing *notReadyLog) (e *sqlcomplete.Engine) {
	var aliases func() []string
	if view.client != nil {
		aliases = view.client.DatasetAliases
	}
	p := inProcessCompletionProviders(aliases)
	if cp := view.landed; cp != nil {
		listing := func(key string, name string) sqlcomplete.ItemsFn {
			return func() ([]sqlcomplete.Item, bool) {
				if got, ok := cp.memo[key]; ok {
					return got, true
				}
				missing.add(name)
				return nil, false
			}
		}
		columns := func(table string) ([]sqlcomplete.Item, bool) {
			if got, ok := cp.memo["columns\x00"+table]; ok {
				return got, true
			}
			missing.add("columns of " + table)
			return nil, false
		}
		p.Catalog = sqlcomplete.Catalog{
			Databases: listing("databases", "databases"),
			Tables: func(db string) ([]sqlcomplete.Item, bool) {
				if got, ok := cp.memo["tables\x00"+db]; ok {
					return got, true
				}
				if db == "" {
					missing.add("tables")
				} else {
					missing.add("tables of " + db)
				}
				return nil, false
			},
			Columns: columns,
			ColumnType: func(table string, column string) (t chtype.Type, ok bool) {
				t, ok = cp.types[table+"\x00"+column]
				return
			},
			Settings:     listing("settings", "settings"),
			TypeNames:    listing("typefamilies", "type names"),
			TimeZones:    listing("timezones", "time zones"),
			Formats:      listing("formats", "formats"),
			Dictionaries: listing("dictionaries", "dictionaries"),
		}
		p.Expressions = func(table string) (items []sqlcomplete.Item, ready bool) {
			if table != "" {
				cols, ok := columns(table)
				if !ok {
					return nil, false
				}
				items = append(items, cols...)
			}
			items = append(items, vocabularyCompletionItems(cp.userDefined, cp.probed)...)
			if cp.allFuncs != nil {
				for name := range cp.allFuncs.IterateKeys() {
					items = append(items, sqlcomplete.Item{Text: name, Kind: sqlcomplete.ItemFunction, Source: "on this endpoint"})
				}
			} else {
				missing.add("the endpoint's functions")
			}
			sortCompletionItems(items)
			return items, true
		}
	}
	return &sqlcomplete.Engine{Vocab: sqlvocab.Default, Providers: p, NamedTupleAccess: true}
}

func addCompletionOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	// Untrusted: catalog candidates (table, column and setting names and
	// docs) come from the endpoint's system tables.
	appops.Query(s, app.OperationSpec{Name: opCompleteSql, Version: 1,
		Summary: "list what may stand at a byte offset of a statement — component kinds and fields, channels, aspects, gloss keys, functions, tables, columns — as the Completion pane would at a caret there",
		Reads:   []string{opsResSql}, Agents: true, Untrusted: true,
		Follows: []string{"nothing runs and no endpoint listing is started; not_ready names the listings no pane has probed yet",
			"validate_sql reports the string literals that name no member of their domain"}},
		func(sn opsSnap, in CompleteArgs) (out CompleteResult, err error) {
			if !sn.mounted {
				return out, app.RefuseOperation("the window has not mounted")
			}
			stmt := in.Sql
			if strings.TrimSpace(stmt) == "" {
				stmt = sn.state.Sql
			}
			if err = statementBounds(stmt); err != nil {
				return
			}
			return completeStatement(sn.completion, stmt, in.Offset)
		})
}

// completeStatement answers one offset of stmt.
func completeStatement(view completionOpsView, stmt string, offset *int32) (out CompleteResult, err error) {
	off := len(stmt)
	if offset != nil {
		off = int(*offset)
		if off < 0 || off > len(stmt) {
			err = app.RefuseOperation("offset is a byte offset into the statement, from 0 to " + itoa(len(stmt)))
			return
		}
	}
	site := highlight.SiteAtIn(stmt, off)
	// A statement no repair parses leaves the site alone as the model, as
	// the editor's scope tier does (ADR-0190 §SD3).
	scope, serr := sqlcomplete.ParseScope(stmt, site, off)
	if serr != nil {
		scope = nil
	}
	var missing notReadyLog
	res := opsCompletionEngine(view, &missing).Complete(sqlcomplete.Request{Site: site, Scope: scope, Statement: stmt, Caret: off})
	out = CompleteResult{Offset: int32(off), Callee: res.Callee, Typed: site.PartialText,
		Replace: CompleteRange{Start: int32(res.Partial.Start), Stop: int32(res.Partial.Stop)},
		Match:   res.Match.String(), Silent: res.Silent, NotReady: missing.names}
	if res.Domain.Kind != 0 {
		out.Domain = res.Domain.String()
	}
	if res.Ordinal >= 0 && res.Callee != "" {
		out.Argument = int32(res.Ordinal) + 1
	}
	if it, ok := res.ExactItem(); ok {
		out.Exact = it.Text
	}
	idx := res.Prefix
	if site.PartialText == "" && len(idx) == 0 {
		idx = make([]int, len(res.Items))
		for i := range idx {
			idx[i] = i
		}
	}
	out.Total = int32(len(idx))
	for _, i := range idx {
		if i < 0 || i >= len(res.Items) {
			continue
		}
		if len(out.Candidates) == completeMaxCandidates {
			out.Truncated = true
			break
		}
		it := res.Items[i]
		cand := CompleteCandidate{Text: it.Text, Kind: it.Kind.String(), Type: it.Type,
			Doc: truncateBytes(it.Doc, completeMaxDoc), Source: it.Source, Marks: slices.Clone(it.Marks)}
		if it.Insert != it.Text {
			cand.Insert = it.Insert
		}
		out.Candidates = append(out.Candidates, cand)
	}
	return
}

// literalFindings are the string literals of stmt that name no member of a
// closed in-process domain, as the editor underlines them in red: component
// kinds and fields, sections, channels, roles, aspects, canonical types,
// glosses and gloss keys, identity tags, introspection tables.
func literalFindings(view completionOpsView, stmt string) (out []string, truncated bool) {
	site := highlight.SiteAtIn(stmt, len(stmt))
	scope, serr := sqlcomplete.ParseScope(stmt, site, len(stmt))
	if serr != nil {
		scope = nil
	}
	// The endpoint half plays no part: Validate judges closed in-process
	// domains only, so a listing not landed is never an accusation.
	e := opsCompletionEngine(completionOpsView{client: view.client}, &notReadyLog{})
	for _, f := range e.Validate(stmt, scope, -1) {
		if f.Resolved {
			continue
		}
		if len(out) == validateMaxLiterals {
			truncated = true
			break
		}
		line := "'" + truncateBytes(f.Text, 120) + "': not a " + f.Domain.Kind.String()
		if f.Callee != "" {
			line += " of " + f.Callee
		}
		if cands := literalCandidates(e, stmt, scope, f); cands != "" {
			line += "; candidates: " + cands
		}
		out = append(out, line)
	}
	return
}

// literalCandidates names a few members of the finding's domain, those
// sharing its first letter first, so the message carries a fix.
func literalCandidates(e *sqlcomplete.Engine, stmt string, scope *sqlcomplete.Scope, f sqlcomplete.Finding) (s string) {
	at := f.Range.Stop
	site := highlight.SiteAtIn(stmt, at)
	res := e.Complete(sqlcomplete.Request{Site: site, Scope: scope, Statement: stmt, Caret: at})
	if len(res.Items) == 0 {
		return
	}
	var near, rest []string
	first := strings.ToLower(f.Text)
	for _, it := range res.Items {
		if first != "" && strings.HasPrefix(strings.ToLower(it.Text), first[:1]) {
			near = append(near, it.Text)
		} else {
			rest = append(rest, it.Text)
		}
	}
	names := append(near, rest...)
	if len(names) > 8 {
		names = append(names[:8], "…")
	}
	return strings.Join(names, ", ")
}
