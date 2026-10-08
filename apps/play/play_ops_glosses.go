package play

// play_ops_glosses.go: list_glosses, the Glosses pane's catalog for an
// agent, and each result column's gloss in describe_result (ADR-0270,
// update of 2026-10-05). The catalog is the build's own text; the rule
// repository caches its affinity rules on first use, so both are read on
// the render goroutine and copied: the catalog once per window, the column
// resolution whenever the window resolves another schema or directive set.

import (
	"strings"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stergiotis/boxer/public/hmi/gloss/glosssql"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
)

const opListGlosses = "list_glosses"

// glossOpsMaxDoc cuts a gloss's doc, as list_functions cuts a function's.
const glossOpsMaxDoc = 300

// GlossParamInfo is one parameter a gloss takes.
type GlossParamInfo struct {
	Name   string   `desc:"the parameter, as name=value in the token"`
	Values []string `json:",omitzero" desc:"the values it takes, when it takes a closed set; the first is what the spellings use"`
	Doc    string   `json:",omitzero" desc:"what it does"`
}

// GlossInfo is one gloss of the catalog.
type GlossInfo struct {
	MediaType  string           `desc:"the gloss's media type"`
	Doc        string           `desc:"what it renders, cut at 300 bytes"`
	Accepts    string           `json:",omitzero" desc:"the value kinds it renders: any, or a list"`
	Params     []GlossParamInfo `json:",omitzero" desc:"its parameters"`
	Affinities []string         `json:",omitzero" desc:"the column spec patterns it binds to by itself when nothing else declares a gloss"`
	Sample     string           `json:",omitzero" desc:"how it renders a sample value"`
	SampleTone string           `json:",omitzero" desc:"the sample's tone when it is not neutral"`
	Alias      string           `desc:"the alias spelling for a projection item: expr AS label@token"`
	Directive  string           `desc:"the directive line binding it by rule over each column's spec line; replace the pattern"`
	Call       string           `desc:"the gloss() call spelling; replace expr"`
}

// GlossRuleInfo is one standing rule the host registered in code.
type GlossRuleInfo struct {
	Set     string `desc:"the rule set"`
	Name    string `desc:"the rule"`
	Token   string `desc:"the gloss it binds, with its parameters"`
	Pattern string `desc:"the condition over a column's spec line"`
}

// GlossCatalog is list_glosses' result.
type GlossCatalog struct {
	Glosses    []GlossInfo     `desc:"the glosses, in the order rules try them"`
	Rules      []GlossRuleInfo `json:",omitzero" desc:"the rule sets the host registered, in precedence order after the buffer's directives"`
	Precedence string          `desc:"how a column's gloss is chosen"`
	Truncated  bool            `json:",omitzero" desc:"true when a doc was cut"`
}

// GlossArgs is list_glosses' argument.
type GlossArgs struct {
	Search string `json:",omitzero" desc:"words to find in a gloss's media type or doc; every word must match"`
}

const glossPrecedence = "an alias (label@token) first, then the buffer's -- play: gloss lines top to bottom, " +
	"then the host's rule sets, then each gloss's affinities; describe_result says which bound each column, and why a gloss is not applied"

// glossCatalogOps builds the catalog reading. It reads the repository's
// rules, which caches affinities on first use: render goroutine only.
func glossCatalogOps(repo *gloss.Repository) (out *GlossCatalog) {
	out = &GlossCatalog{Precedence: glossPrecedence}
	for g := range repo.Catalog().All() {
		token, params := glossSampleToken(g)
		info := GlossInfo{MediaType: g.MediaType(), Doc: g.Doc(),
			Affinities: g.Affinities(),
			Alias:      "expr AS label" + gloss.Sep + token,
			Directive:  "-- play: gloss " + token + " name:<pattern>",
			Call:       glosssql.Call("expr", g.MediaType(), params)}
		if len(info.Doc) > glossOpsMaxDoc {
			info.Doc, out.Truncated = truncateBytes(info.Doc, glossOpsMaxDoc), true
		}
		for _, p := range g.Params() {
			info.Params = append(info.Params, GlossParamInfo{Name: p.Name, Values: p.Values, Doc: p.Doc})
		}
		if sample, bound := bindGlossSample(g, token); bound {
			info.Accepts = glossKindsLine(sample.inst)
			if sample.hasFace {
				info.Sample = sample.face.Text
				if sample.face.Tone != gloss.ToneNeutral {
					info.SampleTone = sample.face.Tone.String()
				}
			}
		}
		out.Glosses = append(out.Glosses, info)
	}
	for _, r := range repo.Rules() {
		if r.Set == "" {
			continue // an affinity, listed with its gloss
		}
		out.Rules = append(out.Rules, GlossRuleInfo{Set: r.Set, Name: r.Name, Token: r.Token(), Pattern: r.Pattern})
	}
	return
}

// glossCatalogView returns the window's catalog reading, built once.
func (inst *PlayApp) glossCatalogView() *GlossCatalog {
	if inst.paneViews.glossCatalog == nil {
		inst.paneViews.glossCatalog = glossCatalogOps(inst.glossRules())
	}
	return inst.paneViews.glossCatalog
}

// glossColumnsOps is a resolution's per-column gloss state as
// describe_result reports it, for the schema it resolved.
type glossColumnsOps struct {
	schema     *arrow.Schema
	directives string
	cols       []ColumnGloss
}

// ColumnGloss is one result column's gloss.
type ColumnGloss struct {
	Token  string `desc:"the gloss and its parameters, as an alias or a directive spells it"`
	Source string `desc:"what bound it: alias, a directive line and its pattern, a rule set's rule, or an affinity"`
	Status string `desc:"applied; refused (the declaration is not valid); or not applied (the gloss does not render this column's values)"`
	Reason string `json:",omitzero" desc:"why it is refused or not applied; the cells then read as plain text"`
}

// glossColumnsView copies the window's current gloss resolution, once per
// schema and directive set it resolved. nil when it has resolved nothing.
func (inst *PlayApp) glossColumnsView() *glossColumnsOps {
	res := &inst.glossRes
	if res.forSchema == nil || len(res.cols) != res.forSchema.NumFields() {
		return nil
	}
	cache := inst.paneViews.glossColumns
	if cache != nil && cache.schema == res.forSchema && cache.directives == res.directives {
		return cache
	}
	v := &glossColumnsOps{schema: res.forSchema, directives: res.directives, cols: make([]ColumnGloss, len(res.cols))}
	for i := range res.cols {
		gc := &res.cols[i]
		if gc.mediaType == "" {
			continue
		}
		cg := ColumnGloss{Token: gloss.CompactMediaType(gc.mediaType, gc.params), Source: gc.source, Status: "applied"}
		switch {
		case gc.reason != "":
			cg.Status, cg.Reason = "refused", gc.reason
		case !gc.rowOK:
			cg.Status, cg.Reason = "not applied", gc.rowReason
		}
		v.cols[i] = cg
	}
	inst.paneViews.glossColumns = v
	return v
}

// glossOf is a column's gloss when the copied resolution is of schema.
func (inst *glossColumnsOps) glossOf(schema *arrow.Schema, i int) (cg *ColumnGloss) {
	if inst == nil || inst.schema != schema || i >= len(inst.cols) || inst.cols[i].Token == "" {
		return nil
	}
	c := inst.cols[i]
	return &c
}

func addGlossOps(s *appops.Set[*PlayLauncher, opsSnap]) {
	appops.Query(s, app.OperationSpec{Name: opListGlosses, Version: 1,
		Summary: "list the glosses a column can be rendered through, with their parameters and the spellings that declare them",
		Agents:  true,
		Follows: []string{"declare one with set_sql: an alias on a projection item, a directive line or a gloss() call; describe_result says whether it took"}},
		func(sn opsSnap, in GlossArgs) (GlossCatalog, error) {
			if !sn.mounted || sn.glossCatalog == nil {
				return GlossCatalog{}, app.RefuseOperation("the window has not mounted")
			}
			return filterGlosses(sn.glossCatalog, in.Search), nil
		})
}

// filterGlosses keeps the glosses whose media type or doc carries every
// word of search.
func filterGlosses(all *GlossCatalog, search string) (out GlossCatalog) {
	out = *all
	words := strings.Fields(strings.ToLower(search))
	if len(words) == 0 {
		return
	}
	out.Glosses = nil
	for _, g := range all.Glosses {
		hay := strings.ToLower(g.MediaType + " " + g.Doc)
		match := true
		for _, w := range words {
			if !strings.Contains(hay, w) {
				match = false
				break
			}
		}
		if match {
			out.Glosses = append(out.Glosses, g)
		}
	}
	return
}
