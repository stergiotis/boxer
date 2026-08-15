package play

import (
	"strconv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stergiotis/boxer/public/hmi/gloss/glosssql"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// play_gloss_panel.go is the Glosses tab (ADR-0186 §SD6): the result-side
// sibling of the Vocabulary tab. Three sections — the catalog (each gloss
// with its kinds, parameters, a sample rendering and its affinities), the
// buffer's effective rules (directive lines, compiled or refused, then the
// affinities), and the current result's columns with their spec line,
// resolution and the rules it shadowed. Insert writes a `-- play: gloss`
// line or a `gloss(…)` call at the caret.

// glossSampleNumber and glossSampleText are the values each catalog row
// renders as its sample face — something every gloss can show: a number that
// reads as a temperature, a length, a size; digits a check digit accepts;
// text a mask or a link takes.
const (
	glossSampleNumber = "4111111111111111"
	glossSampleText   = "https://example.com/path"
)

func (inst *PlayApp) renderGlossesTab(schema *arrow.Schema) {
	for rt := range c.RichTextLabel("Catalog") {
		rt.Strong()
	}
	for rt := range c.RichTextLabel("Every gloss this build knows. Declare one with AS `label@<media type>`, or bind it by rule with -- play: gloss <media type> <regex> over a column's spec line.") {
		rt.Small().Weak()
	}
	for range c.IdScope(inst.ids.PrepareStr("glosses-catalog")) {
		for g := range inst.glossCatalog().All() {
			inst.renderGlossCatalogRow(g)
		}
	}
	c.Separator().Horizontal().Send()

	for rt := range c.RichTextLabel("Rules") {
		rt.Strong()
	}
	for rt := range c.RichTextLabel("In precedence order: an explicit alias first, then the buffer's directive lines top to bottom, then the rule sets the host registered in code, then the affinities each gloss brings along.") {
		rt.Small().Weak()
	}
	inst.renderGlossRules(schema)
	c.Separator().Horizontal().Send()

	for rt := range c.RichTextLabel("Columns") {
		rt.Strong()
	}
	for rt := range c.RichTextLabel("The current result, column by column: the spec line the rules see, what it resolved to, and the rules that matched but lost.") {
		rt.Small().Weak()
	}
	inst.renderGlossColumns(schema)
}

// renderGlossCatalogRow draws one gloss: its media type, Insert buttons for
// a directive line and a gloss(…) call, a sample face rendered live, its
// doc, the value kinds it accepts, its parameters and affinities.
func (inst *PlayApp) renderGlossCatalogRow(g gloss.GlossI) {
	token, params := glossSampleToken(g)
	sample, bound := bindGlossSample(g, token)
	for range c.Horizontal().KeepIter() {
		for rt := range c.RichTextLabel(g.MediaType()) {
			rt.Monospace()
		}
		if c.Button(inst.ids.PrepareStr("glossIns-"+g.MediaType()), c.Atoms().Text("Insert rule").Keep()).
			SendResp().HasPrimaryClicked() {
			// A directive is a line of its own; the caret may be mid-line.
			inst.InsertSqlAtCaret("\n-- play: gloss " + token + " name:\n")
		}
		if c.Button(inst.ids.PrepareStr("glossCall-"+g.MediaType()), c.Atoms().Text("Insert call").Keep()).
			SendResp().HasPrimaryClicked() {
			// A projection item: spliced in place, `expr` left for the author.
			inst.InsertSqlAtCaret(glosssql.Call("expr", g.MediaType(), params))
		}
		if bound && sample.hasFace {
			for rt := range c.RichTextLabel("e.g.") {
				rt.Small().Weak()
			}
			if col, toned := toneColor(sample.face.Tone); toned {
				for rt := range c.RichTextLabelColored(col, color.Transparent, sample.face.Text) {
					rt.Monospace().Small()
				}
			} else {
				for rt := range c.RichTextLabel(sample.face.Text) {
					rt.Monospace().Small()
				}
			}
		}
	}
	for rt := range c.RichTextLabel(g.Doc()) {
		rt.Small().Weak()
	}
	if bound {
		for rt := range c.RichTextLabel("accepts: " + glossKindsLine(sample.inst)) {
			rt.Small().Weak()
		}
	}
	if params := g.Params(); len(params) > 0 {
		var b strings.Builder
		b.WriteString("parameters: ")
		for i, p := range params {
			if i > 0 {
				b.WriteString(" · ")
			}
			b.WriteString(p.Name)
			if len(p.Values) > 0 {
				b.WriteString("=" + strings.Join(p.Values, "|"))
			}
			if p.Doc != "" {
				b.WriteString(" (" + p.Doc + ")")
			}
		}
		for rt := range c.RichTextLabel(b.String()) {
			rt.Small().Weak()
		}
	}
	if aff := g.Affinities(); len(aff) > 0 {
		for rt := range c.RichTextLabel("affinity: " + strings.Join(aff, " · ")) {
			rt.Small().Weak()
		}
	}
}

// glossSampleToken is the token the Insert buttons write: the media type,
// with the first allowed value of every closed-set parameter — so a required
// `unit=` lands spelled rather than missing. params is that choice on its
// own, for the call spelling.
func glossSampleToken(g gloss.GlossI) (token string, params map[string]string) {
	for _, p := range g.Params() {
		if len(p.Values) > 0 {
			if params == nil {
				params = make(map[string]string, 2)
			}
			params[p.Name] = p.Values[0]
		}
	}
	return gloss.CompactMediaType(g.MediaType(), params), params
}

// glossSample is a gloss bound for the catalog listing: the instance (for
// the kinds it accepts) and, when a sample value of an accepted kind exists,
// its inline face.
type glossSample struct {
	inst    gloss.InstanceI
	face    gloss.Inline
	hasFace bool
}

// bindGlossSample binds the gloss with its sample token and renders its inline
// face over a sample value of a kind it accepts. bound is false when the
// token does not bind (a programming error in the gloss's own parameters);
// hasFace is false when no sample fits.
func bindGlossSample(g gloss.GlossI, token string) (sample glossSample, bound bool) {
	d, declared := (&sampleCatalog{g: g}).parse(token)
	if !declared || d.Instance == nil {
		return sample, false
	}
	sample.inst = d.Instance
	for _, cell := range []gloss.TextCell{
		{S: glossSampleNumber, K: gloss.ValueKindNumeric},
		{S: glossSampleText, K: gloss.ValueKindText},
	} {
		if accepted, _ := d.Instance.Accepts(cell.K); accepted {
			sample.face, sample.hasFace = d.Instance.Inline(cell), true
			break
		}
	}
	return sample, true
}

// glossKindsLine spells the value kinds an instance accepts: "any" when it
// refuses none, else the kinds in enum order.
func glossKindsLine(inst gloss.InstanceI) string {
	kinds, all := gloss.AcceptedKinds(inst)
	if all {
		return "any"
	}
	names := make([]string, len(kinds))
	for i, k := range kinds {
		names[i] = k.String()
	}
	return strings.Join(names, " · ")
}

// sampleCatalog binds one gloss for a sample without touching the live
// catalog's registration order.
type sampleCatalog struct{ g gloss.GlossI }

func (inst *sampleCatalog) parse(token string) (gloss.Declaration, bool) {
	cat := gloss.NewCatalog()
	cat.MustRegister(inst.g)
	return cat.ParseColumn("sample" + gloss.Sep + token)
}

// renderGlossRules lists the buffer's directive lines — compiled, or refused
// with the reason — then the catalog's affinities.
func (inst *PlayApp) renderGlossRules(schema *arrow.Schema) {
	// Resolving (or re-reading the cache) is what compiles the directives.
	inst.glossColumns(schema)
	res := &inst.glossRes
	if len(res.rules) == 0 && len(res.notes) == 0 {
		for rt := range c.RichTextLabel("(no -- play: gloss lines in the buffer)") {
			rt.Small().Weak()
		}
	}
	for _, r := range res.rules {
		for range c.Horizontal().KeepIter() {
			for rt := range c.RichTextLabel(r.Source) {
				rt.Small().Weak()
			}
			for rt := range c.RichTextLabel(r.Token() + "  ←  " + r.Pattern) {
				rt.Monospace()
			}
		}
	}
	for _, n := range res.notes {
		for rt := range c.RichTextLabel(n) {
			rt.Small()
		}
	}
	// The repository's standing rules: each set under its name, in
	// registration order — code the deployment checked in — then the
	// affinities the glosses bring along.
	set := ""
	for _, r := range inst.glossRules().Rules() {
		if r.Set != set {
			set = r.Set
			for rt := range c.RichTextLabel("rule set " + set) {
				rt.Small().Strong()
			}
		}
		for range c.Horizontal().KeepIter() {
			label := r.Source
			if r.Set != "" {
				label = r.Name
			}
			for rt := range c.RichTextLabel(label) {
				rt.Small().Weak()
			}
			for rt := range c.RichTextLabel(r.Token() + "  ←  " + r.Pattern) {
				rt.Monospace()
			}
		}
	}
}

// renderGlossColumns lists the current result's columns with their spec
// line and resolution.
func (inst *PlayApp) renderGlossColumns(schema *arrow.Schema) {
	if schema == nil {
		for rt := range c.RichTextLabel("Run a query to see its columns.") {
			rt.Small().Weak()
		}
		return
	}
	cols := inst.glossColumns(schema)
	for i := range cols {
		gc := &cols[i]
		field := schema.Field(i)
		for range c.Horizontal().KeepIter() {
			for rt := range c.RichTextLabel(strconv.Itoa(i+1) + ".") {
				rt.Small().Weak()
			}
			name := field.Name
			if gc.label != "" {
				name = gc.label
			}
			for rt := range c.RichTextLabel(name) {
				rt.Monospace()
			}
			switch {
			case gc.mediaType == "":
				for rt := range c.RichTextLabel("plain") {
					rt.Small().Weak()
				}
			case gc.reason != "":
				for rt := range c.RichTextLabel("refused: " + gc.reason) {
					rt.Small()
				}
			case !gc.rowOK && !gc.elemOK:
				for rt := range c.RichTextLabel(gloss.CompactMediaType(gc.mediaType, gc.params) + " (" + gc.source + ") — not applied: " + gc.rowReason) {
					rt.Small()
				}
			default:
				for rt := range c.RichTextLabel(gloss.CompactMediaType(gc.mediaType, gc.params) + " (" + gc.source + ")") {
					rt.Small()
				}
			}
		}
		for rt := range c.RichTextLabel("spec: " + gc.specLine) {
			rt.Small().Weak().Monospace()
		}
		if len(gc.shadowed) > 0 {
			for rt := range c.RichTextLabel("shadowed: " + glossShadowedLine(gc.shadowed)) {
				rt.Small().Weak()
			}
		}
	}
}

// glossShadowedLine lists the rules a column's binding shadowed, each as
// its source, token and pattern, in the order they would have applied.
func glossShadowedLine(rules []gloss.Rule) string {
	parts := make([]string, len(rules))
	for i, r := range rules {
		parts[i] = r.Source + ": " + r.Token() + " ← " + r.Pattern
	}
	return strings.Join(parts, " · ")
}
