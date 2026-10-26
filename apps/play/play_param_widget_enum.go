package play

import (
	"sort"
	"strconv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// The enumerated knob: `-- play: enum <slot> <value>[=<label>][,…]` (ADR-0124
// Update 2026-08-14).
//
// A parameter whose values are a known, short set is a different control from
// one that takes any text, and until this existed every knob was a text field —
// a reader had to know that `size_by` takes `bytes` or `count` and spell one of
// them correctly, with a typo answering as an empty result rather than as an
// error.
//
// # Why a marker comment rather than inference
//
// Nothing in a `{name:Type}` placeholder says what its values are: `UInt8` is
// not "1 through 4", and a `String` knob's options live in the author's head or
// in the data. ADR-0124 §SD7 already chose the declarative marker over
// inference for the range widget's opt-out, and §O4 sketched `-- play: range`
// in the same vocabulary; this is that shape, used for the case where the
// declaration is the only possible source.
//
// # Options from the data
//
// `-- play: enum <slot> [<options>] from <cte>` adds the rows of a CTE of the
// buffer after the literal options (ADR-0124 Update 2026-10-26): every catalog
// in the corpus, every domain. The three questions the literal form left open
// have these answers. The second execution is a node lane, the one a pane bound
// to a CTE gets (demandEnumSources), so it re-runs exactly when its compiled
// SQL does and a knob it does not read cannot re-trigger it. The shape is a
// `value` column, else the first, and an optional `label`. And the literal
// options stay first and win a repeated value, which is where `=All catalogs`
// belongs.

// enumOption is one declared choice: the value that lands in the buffer, and
// the label the reader picks it by.
//
// The two differ where the value is a code and the label is what it means —
// `1=Macro`, `0=All levels` — which is most of the useful cases. A bare option
// is its own label.
type enumOption struct {
	Value string
	Label string
}

// enumMarkerPrefix is the comment body an enum hint starts with, after the
// leading `--` is stripped and the rest lowercased.
const enumMarkerPrefix = "play: enum "

// scanEnumHints reads every `-- play: enum <slot> <options>` line in sql: the
// literal options, keyed by slot. A slot whose line names only a source has an
// empty list here and an entry in [scanEnumSources].
//
// Malformed hints are dropped rather than reported as errors: this runs every
// frame over a buffer somebody may be halfway through typing, and a marker that
// is not yet a marker must not make the pane shout. What it cannot do silently
// is claim a slot — an unusable hint leaves the slot to the text field, and
// [orphanEnumHints] says so for the case that is a typo rather than a
// half-typed line.
//
// The first hint for a name wins. Two hints for one slot is an authoring
// mistake either way, and taking the first makes the pane's answer independent
// of where in the buffer the author added the second.
func scanEnumHints(sql string) (hints map[string][]enumOption) {
	hints, _ = scanEnumDirectives(sql)
	return
}

// scanEnumSources names the CTE each sourced knob takes its options from.
func scanEnumSources(sql string) (sources map[string]string) {
	_, sources = scanEnumDirectives(sql)
	return
}

// scanEnumDirectives is the one pass behind [scanEnumHints] and
// [scanEnumSources], so the first-hint-wins rule decides both together.
func scanEnumDirectives(sql string) (hints map[string][]enumOption, sources map[string]string) {
	for line := range strings.SplitSeq(sql, "\n") {
		ln := strings.TrimSpace(line)
		if !strings.HasPrefix(ln, "--") {
			continue
		}
		ln = strings.TrimSpace(strings.TrimPrefix(ln, "--"))
		if !strings.HasPrefix(strings.ToLower(ln), enumMarkerPrefix) {
			continue
		}
		rest := strings.TrimSpace(ln[len(enumMarkerPrefix):])
		name, spec, hasSpec := strings.Cut(rest, " ")
		if !hasSpec {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		static, source := splitEnumSource(spec)
		opts := parseEnumOptions(static)
		if len(opts) == 0 && source == "" {
			continue
		}
		if _, taken := hints[name]; taken {
			continue
		}
		if hints == nil {
			hints = make(map[string][]enumOption, 2)
		}
		hints[name] = opts
		if source != "" {
			if sources == nil {
				sources = make(map[string]string, 1)
			}
			sources[name] = source
		}
	}
	return
}

// splitEnumSource takes a trailing `from <cte>` off an option spec. Only the
// last two words count, and only when the last is an identifier, so a label
// may still contain the word: `=Data from disk` reads as a source named disk,
// which is the one spelling a label cannot have.
func splitEnumSource(spec string) (static string, source string) {
	spec = strings.TrimSpace(spec)
	at := strings.LastIndexAny(spec, " \t")
	if at < 0 {
		return spec, ""
	}
	ident := spec[at+1:]
	before := strings.TrimSpace(spec[:at])
	if !isEnumSourceIdent(ident) {
		return spec, ""
	}
	switch lower := strings.ToLower(before); {
	case lower == "from":
		return "", ident
	case strings.HasSuffix(lower, " from") || strings.HasSuffix(lower, "\tfrom"):
		return strings.TrimSpace(before[:len(before)-len("from")]), ident
	}
	return spec, ""
}

// isEnumSourceIdent reports whether s can name a CTE without quoting.
func isEnumSourceIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
		case i > 0 && r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}

// enumDataMaxOptions caps the options a source contributes. A dropdown past a
// few hundred entries is a search problem, not a list, and the cap is said in
// the strip's note rather than silently applied.
const enumDataMaxOptions = 500

// enumSourceState is what a sourced knob's lane last answered: the options its
// rows give, whether a run is in flight, and why it gave none.
type enumSourceState struct {
	cte     string
	rows    []enumOption
	loading bool
	cut     bool
	problem string
}

// enumOptionsFromRecord reads a source's rows as options: `value`, else the
// first column, labelled by `label` when the result has one. Empty values are
// skipped — "no filter" is the literal option's to say — and so is a value
// already listed, keeping its first label.
func enumOptionsFromRecord(rec arrow.RecordBatch, max int) (opts []enumOption, cut bool) {
	if rec == nil || rec.NumCols() == 0 {
		return
	}
	valueCol, labelCol := 0, -1
	for i, f := range rec.Schema().Fields() {
		switch pathColumnLabel(f.Name) {
		case "value":
			valueCol = i
		case "label":
			labelCol = i
		}
	}
	seen := make(map[string]struct{}, rec.NumRows())
	for row := range rec.NumRows() {
		v := formatCell(rec, valueCol, row)
		if v == "" {
			continue
		}
		if _, dup := seen[v]; dup {
			continue
		}
		if len(opts) >= max {
			return opts, true
		}
		seen[v] = struct{}{}
		label := v
		if labelCol >= 0 {
			if l := formatCell(rec, labelCol, row); l != "" {
				label = l
			}
		}
		opts = append(opts, enumOption{Value: v, Label: label})
	}
	return
}

// mergeEnumOptions is the literal options, then the source's rows whose value
// none of them carries.
func mergeEnumOptions(static []enumOption, data []enumOption) (opts []enumOption) {
	if len(data) == 0 {
		return static
	}
	opts = make([]enumOption, 0, len(static)+len(data))
	opts = append(opts, static...)
	have := make(map[string]struct{}, len(static))
	for _, o := range static {
		have[o.Value] = struct{}{}
	}
	for _, o := range data {
		if _, dup := have[o.Value]; dup {
			continue
		}
		opts = append(opts, o)
	}
	return
}

// parseEnumOptions splits the comma-separated option list. An option is
// `value` or `value=label`; the value may be empty (`=All catalogs`), which is
// how a filter says "no filter" — the common first entry of every dropdown in a
// browsing UI.
//
// A repeated value keeps its first label, so a list cannot produce two
// identically-valued choices that look different.
func parseEnumOptions(spec string) (opts []enumOption) {
	seen := make(map[string]struct{}, 4)
	for raw := range strings.SplitSeq(spec, ",") {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		value, label, hasLabel := strings.Cut(item, "=")
		value = strings.TrimSpace(value)
		label = strings.TrimSpace(label)
		if !hasLabel || label == "" {
			label = value
		}
		if label == "" {
			// An option with neither a value nor a label is nothing at all;
			// an empty VALUE with a label is the "all" entry and is kept.
			continue
		}
		if _, dup := seen[value]; dup {
			continue
		}
		seen[value] = struct{}{}
		opts = append(opts, enumOption{Value: value, Label: label})
	}
	return opts
}

// DeclaredEnumSlots names the slots a buffer declares an option list for, in a
// stable order.
//
// Exported for the applet corpus gate: a published document's knobs are part of
// what it promises, and a marker with a typo in it renders as an ordinary text
// field with nothing to say why. A book can therefore assert that the knobs it
// documents as lists really are lists. The option values stay unexported —
// what they should be is the document's business, not the gate's.
func DeclaredEnumSlots(sql string) (names []string) {
	hints := scanEnumHints(sql)
	if len(hints) == 0 {
		return nil
	}
	names = make([]string, 0, len(hints))
	for name := range hints {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// orphanEnumHints names the hints whose slot the buffer carries no placeholder
// for, in a stable order.
//
// This is the typo case, and it is worth a line because its symptom is
// otherwise indistinguishable from having written no hint at all: the knob
// renders as a text field and nothing says why.
func orphanEnumHints(hints map[string][]enumOption, slots []paramSlot) (names []string) {
	if len(hints) == 0 {
		return nil
	}
	present := make(map[string]struct{}, len(slots))
	for _, s := range slots {
		present[s.Name] = struct{}{}
	}
	for name := range hints {
		if _, has := present[name]; !has {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// orphanEnumNote is the advisory line for [orphanEnumHints]. Pure over the
// names, so it is testable without a frame.
func orphanEnumNote(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return `"-- play: enum" names ` + strings.Join(names, ", ") +
		", which this buffer has no placeholder for — the knob falls back to a text field"
}

// enumHintAwareI is the opt-in a widget implements to receive the buffer's enum
// hints. It mirrors [evaluatorAwareI]: the orchestrator holds what the widgets
// need from outside their own slots, and hands it over rather than letting a
// widget reach for the buffer.
type enumHintAwareI interface {
	SetEnumHints(hints map[string][]enumOption)
}

// enumPendingAwareI is the opt-in for the sourced knobs still waiting for
// their rows (enumWidget.pending).
type enumPendingAwareI interface {
	SetEnumPending(pending map[string]bool)
}

// enumSourceNote is the strip's advisory line for the sources that gave a knob
// less than they were asked for: a name that is not a CTE of the buffer, a run
// that failed, an empty answer, a list cut at [enumDataMaxOptions]. Pure over
// the states, in slot order, so it is testable without a frame.
func enumSourceNote(states map[string]enumSourceState) (note string) {
	if len(states) == 0 {
		return ""
	}
	slots := make([]string, 0, len(states))
	for slot := range states {
		slots = append(slots, slot)
	}
	sort.Strings(slots)
	var parts []string
	for _, slot := range slots {
		st := states[slot]
		head := `"` + slot + ` from ` + st.cte + `" `
		switch {
		case st.problem != "":
			parts = append(parts, head+st.problem)
		case st.cut:
			parts = append(parts, head+"lists the first "+strconv.Itoa(enumDataMaxOptions)+" values")
		case !st.loading && len(st.rows) == 0:
			parts = append(parts, head+"answered no values")
		}
	}
	return strings.Join(parts, "; ")
}

// enumWidget renders a declared-options knob as a dropdown.
//
// pending names the sourced knobs whose options have not all arrived: such a
// knob is claimed even with none yet, so it does not render as a text field
// for the length of its first run and then turn into a dropdown, and a value
// the list does not carry yet is not called out as missing from it.
type enumWidget struct {
	hints   map[string][]enumOption
	pending map[string]bool
}

func newEnumWidget() *enumWidget { return &enumWidget{} }

var (
	_ paramWidgetI      = (*enumWidget)(nil)
	_ enumHintAwareI    = (*enumWidget)(nil)
	_ enumPendingAwareI = (*enumWidget)(nil)
)

func (w *enumWidget) SetEnumHints(hints map[string][]enumOption) { w.hints = hints }

// SetEnumPending names the sourced knobs still waiting for their rows.
func (w *enumWidget) SetEnumPending(pending map[string]bool) { w.pending = pending }

// Matches claims one slot per call — the first with a hint — so the dispatch
// loop can hand it several in a row, the way it does the scalar tail.
func (w *enumWidget) Matches(slots []paramSlot) (consumedIdx []int, ok bool) {
	if len(w.hints) == 0 {
		return
	}
	for i, s := range slots {
		if len(w.hints[s.Name]) == 0 && !w.pending[s.Name] {
			continue
		}
		return []int{i}, true
	}
	return
}

func (w *enumWidget) Render(ctx *paramCtx) {
	for _, s := range ctx.Slots {
		draft, has := ctx.Drafts[s.Name]
		if !has {
			continue
		}
		opts := w.hints[s.Name]
		if len(opts) == 0 && !w.pending[s.Name] {
			continue
		}
		w.renderOne(ctx.Ids, s, draft, opts, w.pending[s.Name])
	}
}

// renderOne draws one knob: its name, and a dropdown showing the label of the
// value the buffer currently holds.
//
// The children are inside an [c.IdScope] keyed by the slot: a combo draws one
// button per option off the shared stack, and two knobs in one strip would
// otherwise derive the same ids for their nth options — which does not fail
// loudly, it silently routes one control's click to the other.
func (w *enumWidget) renderOne(ids *c.WidgetIdStack, s paramSlot, draft *string, opts []enumOption, pending bool) {
	for range c.Horizontal().KeepIter() {
		for rt := range c.RichTextLabel(s.Name + " : " + s.Type) {
			rt.Small().Weak()
		}
		selected, known := enumLabelFor(opts, *draft)
		for range c.IdScope(ids.PrepareStr("paramEnumScope-" + s.Name)) {
			for range c.ComboBox(ids.PrepareStr("paramEnum-"+s.Name),
				c.WidgetText().Text("").Keep(),
				c.WidgetText().Text(selected).Keep()).
				KeepIter() {
				for i, opt := range opts {
					if c.Button(ids.PrepareSeq(uint64(i)), c.Atoms().Text(opt.Label).Keep()).
						Frame(false).
						Selected(opt.Value == *draft).
						SendResp().HasPrimaryClicked() {
						*draft = opt.Value
					}
				}
			}
		}
		switch {
		case !known && pending:
			// The value may well be among the rows still on their way.
			for rt := range c.RichTextLabel("· loading options") {
				rt.Small().Weak()
			}
		case !known:
			// The buffer holds something the list does not offer — a hand-edited
			// SET, or a list that changed under a saved value. Saying so beats
			// showing a blank control, and picking any option repairs it.
			for rt := range c.RichTextLabel("· not in the list") {
				rt.Small().Weak()
			}
		}
	}
}

// enumLabelFor renders the current value: the matching option's label, or the
// raw value when the list does not carry it. An empty value with no matching
// option shows the em dash rather than nothing, so the control has a hit area
// and a reader can tell "unset" from "still loading".
func enumLabelFor(opts []enumOption, value string) (label string, known bool) {
	for _, opt := range opts {
		if opt.Value == value {
			return opt.Label, true
		}
	}
	if value == "" {
		return "—", false
	}
	return value, false
}

func (w *enumWidget) ClearStateForAbsent(map[string]struct{}) {}

func (w *enumWidget) IsGroup() bool { return false }
