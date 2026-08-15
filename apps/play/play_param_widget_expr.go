package play

import (
	"math"
	"sort"
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/sqleditor"
)

// The SQL-expression knob: a placeholder whose value is SQL rather than a
// value — ADR-0187 §SD1 and §SD3, milestone M1.
//
// # Two mechanisms behind one control
//
// §SD2 is the thing to know before reading anything else here. `Identifier` is
// a ClickHouse parameter: it rides ADR-0124 §SD4 unchanged — a `SET param_*`
// prelude, the URL channel, server-side substitution — and works end to end
// today. `Expr` and `ExprList` cannot: ClickHouse substitutes values, and
// nothing in its param channel substitutes an expression. They are declared in
// the buffer with `-- play: expr <slot> = <text>` and substituted client-side.
//
// This file gives all three the same editing surface and keeps their value
// paths apart, because a reader meeting three type names in one table will
// otherwise assume one rule governs them.
//
// # What M1 deliberately does not do
//
// The splice is not built (M2), so an `Expr` / `ExprList` slot is DECLARED but
// not substituted: the buffer cannot run, and the run gate holds it exactly as
// it holds any unfilled placeholder — which is correct, since a query carrying
// a type ClickHouse does not know could only fail at the server. The pane says
// so in a line. Their drafts are kept out of the prelude and out of the signal
// store rather than left to fall through to a path that would ship them as a
// string, and the tier control is withheld until M3 gives pin/unpin a
// directive arm to write.

// paramExprCategoryE classifies a placeholder's declared type into the three
// categories ADR-0187 §SD1 names, or none.
type paramExprCategoryE uint8

const (
	// exprCatNone is every ordinary value-typed placeholder — UInt64, String,
	// DateTime — which this file does not touch.
	exprCatNone paramExprCategoryE = iota
	// exprCatExpr is `{c:Expr}`: one expression, a predicate or a scalar.
	exprCatExpr
	// exprCatList is `{c:ExprList}`: an aliased column-expression list.
	exprCatList
	// exprCatIdentifier is `{t:Identifier}`: a database, table or column name,
	// and ClickHouse's own syntactic parameter type.
	exprCatIdentifier
)

// spliced reports whether the category is substituted client-side. It is the
// discriminator §SD2 turns on, and every value-path fork in this package reads
// it rather than re-testing the category names.
func (inst paramExprCategoryE) spliced() bool {
	return inst == exprCatExpr || inst == exprCatList
}

// exprCategoryFor classifies a slot's raw type text.
//
// Case-insensitive, and only on an exact word: `Expr`, `ExprList`,
// `Identifier`. Case-insensitive because a reader who wrote `{c:expr}` meant
// the category and a silent text field is the worst possible answer; exact
// because a parameterised type (`Nullable(Expr)`) is not one of these and
// guessing at one would invent a category with no substitution rule behind it.
func exprCategoryFor(typeExpr string) paramExprCategoryE {
	switch strings.ToLower(strings.TrimSpace(typeExpr)) {
	case "expr":
		return exprCatExpr
	case "exprlist":
		return exprCatList
	case "identifier":
		return exprCatIdentifier
	}
	return exprCatNone
}

// exprMarkerPrefix is the comment body an expression declaration starts with,
// after the leading `--` is stripped and the rest lowercased.
const exprMarkerPrefix = "play: expr "

// scanExprHints reads every `-- play: expr <slot> = <text>` line in sql.
//
// The value is everything past the FIRST `=`, which is a rule and not an
// implementation detail: expressions contain `=` constantly, and any other
// split would make `cond = a = 1` mean something the author did not write. It
// also means the spacing around the separator is free — `cond=a=1` and
// `cond = a = 1` declare the same thing.
//
// The value is trimmed at both ends. Trailing whitespace in a one-line SQL
// fragment carries nothing, and preserving it would make the drift comparison
// M3 rests on depend on characters nobody can see.
//
// Malformed lines are dropped rather than reported, for [scanEnumHints]'s
// reason: this runs every frame over a buffer somebody may be halfway through
// typing, and a marker that is not yet a marker must not make the pane shout.
// An empty value is not a declaration — a slot's position is mandatory and
// `WHERE ()` is not a query — so it leaves the slot unfilled, which the run
// gate already knows how to say. The first hint for a name wins, so the pane's
// answer does not depend on where in the buffer a duplicate was added.
func scanExprHints(sql string) (hints map[string]string) {
	for line := range strings.SplitSeq(sql, "\n") {
		ln := strings.TrimSpace(line)
		if !strings.HasPrefix(ln, "--") {
			continue
		}
		ln = strings.TrimSpace(strings.TrimPrefix(ln, "--"))
		if !strings.HasPrefix(strings.ToLower(ln), exprMarkerPrefix) {
			continue
		}
		rest := strings.TrimSpace(ln[len(exprMarkerPrefix):])
		name, value, hasValue := strings.Cut(rest, "=")
		if !hasValue {
			continue
		}
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if name == "" || value == "" {
			continue
		}
		if _, taken := hints[name]; taken {
			continue
		}
		if hints == nil {
			hints = make(map[string]string, 2)
		}
		hints[name] = value
	}
	return hints
}

// orphanExprHints names the declarations no slot in the buffer can claim, in a
// stable order.
//
// Two failure modes land here and they deserve one answer, because their
// symptom is identical and invisible: a declaration naming a placeholder the
// buffer does not carry (a typo in the name), and one naming a placeholder that
// is not an expression category at all (`-- play: expr lim = 5` over
// `{lim:UInt64}`). In both cases the declaration is simply ignored and nothing
// on screen says why.
//
// `Identifier` does not claim a declaration: it is a ClickHouse parameter whose
// value lives in the prelude (§SD2), so a declaration naming one is as ignored
// as a declaration naming nothing.
func orphanExprHints(hints map[string]string, slots []paramSlot) (names []string) {
	if len(hints) == 0 {
		return nil
	}
	claimable := make(map[string]struct{}, len(slots))
	for _, s := range slots {
		if exprCategoryFor(s.Type).spliced() {
			claimable[s.Name] = struct{}{}
		}
	}
	for name := range hints {
		if _, has := claimable[name]; !has {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// orphanExprNote is the advisory line for [orphanExprHints]. Pure over the
// names, so it is testable without a frame.
func orphanExprNote(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return `"-- play: expr" declares ` + strings.Join(names, ", ") +
		", which this buffer has no {Expr} or {ExprList} placeholder for — the declaration is ignored"
}

// exprHintAwareI is the opt-in a widget implements to receive what the
// orchestrator knows about expressions from outside a widget's own slots: the
// buffer's declarations, and the error marks the splice-then-parse validation
// derived (§SD6). It mirrors [enumHintAwareI] and [evaluatorAwareI].
type exprHintAwareI interface {
	SetExprHints(hints map[string]string)
	SetExprMarks(marks map[string]nanopass.SourceRange)
}

// exprWidget renders a SQL-valued knob as a [sqleditor.Field].
type exprWidget struct {
	hints map[string]string
	marks map[string]nanopass.SourceRange
	// One Field per slot: each memoises its own lex job against its own text,
	// so a shared instance would rebuild every field's job on any frame that
	// drew two. Pruned by ClearStateForAbsent, which is what that method is
	// for.
	fields map[string]*sqleditor.Field
}

func newExprWidget() *exprWidget { return &exprWidget{} }

var (
	_ paramWidgetI   = (*exprWidget)(nil)
	_ exprHintAwareI = (*exprWidget)(nil)
)

func (w *exprWidget) SetExprHints(hints map[string]string) { w.hints = hints }

func (w *exprWidget) SetExprMarks(marks map[string]nanopass.SourceRange) { w.marks = marks }

// Matches claims one slot per call — the first of any expression category — so
// the dispatch loop can hand it several in a row, the way it does the enum
// widget and the scalar tail.
//
// It matches on the TYPE alone, never on whether a declaration exists. A slot
// typed `Expr` is a SQL knob whether or not the author has filled it in yet,
// and falling back to a text field for the unfilled case would offer the wrong
// editor exactly when the author is about to write the expression.
func (w *exprWidget) Matches(slots []paramSlot) (consumedIdx []int, ok bool) {
	for i, s := range slots {
		if exprCategoryFor(s.Type) == exprCatNone {
			continue
		}
		return []int{i}, true
	}
	return
}

func (w *exprWidget) Render(ctx *paramCtx) {
	for _, s := range ctx.Slots {
		draft, has := ctx.Drafts[s.Name]
		if !has {
			continue
		}
		cat := exprCategoryFor(s.Type)
		if cat == exprCatNone {
			continue
		}
		w.renderOne(ctx.Ids, s, cat, draft)
	}
}

// renderOne draws one knob: its name and type, then the field.
//
// No IdScope: the field is a single TextEdit under a per-slot id, unlike the
// enum widget's combo, which draws one button per option off the shared stack
// and therefore needs the scope.
func (w *exprWidget) renderOne(ids *c.WidgetIdStack, s paramSlot, cat paramExprCategoryE, draft *string) {
	for range c.Horizontal().KeepIter() {
		for rt := range c.RichTextLabel(s.Name + " : " + s.Type) {
			rt.Small().Weak()
		}
		if w.fields == nil {
			w.fields = make(map[string]*sqleditor.Field, 2)
		}
		f, held := w.fields[s.Name]
		if !held {
			f = sqleditor.NewField()
			w.fields[s.Name] = f
		}
		f.Render(ids, sqleditor.FieldFrame{
			IDSlot: "paramSlotExpr-" + s.Name,
			Value:  draft,
			Hint:   exprHintTextFor(cat, s.Name),
			// The mark is in the VALUE's own coordinates, which is what the
			// field is bound to — exprMarkFor already subtracted the splice
			// origin, so nothing here has to know where the value landed.
			Mark: w.marks[s.Name],
			// The pane's own idiom for "fill the row", as the scalar tail uses.
			Width: float32(math.Inf(1)),
		})
	}
}

// exprHintTextFor is the empty-field placeholder, which is the only place a
// reader is told what shape the knob wants.
func exprHintTextFor(cat paramExprCategoryE, name string) string {
	switch cat {
	case exprCatList:
		return "expression list for {" + name + "} — e.g. a AS x, b AS y"
	case exprCatIdentifier:
		return "name for {" + name + "} — a database, table or column"
	default:
		return "SQL expression for {" + name + "} — e.g. status = 'error'"
	}
}

// ClearStateForAbsent drops the Field of a slot the buffer no longer carries,
// so a renamed placeholder does not keep a lex job alive for text nobody can
// see.
func (w *exprWidget) ClearStateForAbsent(present map[string]struct{}) {
	for name := range w.fields {
		if _, keep := present[name]; !keep {
			delete(w.fields, name)
		}
	}
}

func (w *exprWidget) IsGroup() bool { return false }
