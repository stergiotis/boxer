package play

import (
	"maps"
	"sort"
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// The client-side substitution of SQL-valued placeholders — ADR-0187
// (proposed) §SD4.
//
// ClickHouse substitutes values; nothing in its param channel substitutes an
// expression, so an `{c:Expr}` slot has to be replaced in the text before the
// body goes on the wire. This file is that replacement and the record of where
// each value landed, which §SD6's validation reads to decide whether a parse
// error belongs to a field or to the query.

// exprSplice records one substitution: which slot, where its placeholder was in
// the input, and where its VALUE landed in the output.
//
// Out excludes the parentheses an `Expr` is wrapped in, so it is the extent of
// the text the user typed and nothing else — which is what makes an error
// offset inside it map back to a field position by subtraction.
type exprSplice struct {
	Name  string
	Cat   paramExprCategoryE
	Src   nanopass.SourceRange
	Out   nanopass.SourceRange
	Value string
}

// spliceExprSlots replaces every filled `Expr` / `ExprList` placeholder in sql
// with its value, and reports where each value landed.
//
// Per §SD4, the two categories splice differently and the difference is not
// cosmetic:
//
//   - `Expr` is PARENTHESISED. `WHERE a = 1 AND {c:Expr}` with `b = 2 OR c = 3`
//     must become `… AND (b = 2 OR c = 3)`; spliced bare it silently
//     reassociates into a different query that still parses and still runs.
//   - `ExprList` is spliced BARE. A list cannot be parenthesised without
//     becoming a tuple.
//
// A slot with no value, or an empty one, is left alone: it is unfilled, the run
// gate holds it, and substituting nothing would produce `WHERE ()`.
//
// Degrades rather than fails, like every step of the client-side rewrite: an
// unparseable buffer comes back unchanged with the parse error, and the caller
// ships what the user wrote so the server reports the real problem.
func spliceExprSlots(sql string, values map[string]string) (out string, spl []exprSplice, err error) {
	out = sql
	if len(values) == 0 {
		return
	}
	pr, perr := nanopass.Parse(sql)
	if perr != nil {
		err = eh.Errorf("spliceExprSlots: %w", perr)
		return
	}
	occ := collectParamSlotOccurrences(pr)
	if len(occ) == 0 {
		return
	}
	sort.SliceStable(occ, func(i, j int) bool { return occ[i].Src.Start < occ[j].Src.Start })

	var b strings.Builder
	b.Grow(len(sql))
	prev := 0
	for _, s := range occ {
		cat := exprCategoryFor(s.Type)
		if !cat.spliced() {
			continue
		}
		v, held := values[s.Name]
		if !held || v == "" {
			continue
		}
		// Defensive: a range that starts inside the previous one would make
		// the output text nonsense. The CST cannot produce overlapping
		// placeholders, so skipping is unreachable rather than lossy.
		if s.Src.Start < prev || s.Src.End > len(sql) {
			continue
		}
		b.WriteString(sql[prev:s.Src.Start])
		if cat == exprCatExpr {
			b.WriteString("(")
		}
		// Taken after the opening paren, so Out is the value's own extent and
		// an error offset inside it subtracts to a field position directly.
		start := b.Len()
		b.WriteString(v)
		if cat == exprCatExpr {
			b.WriteString(")")
		}
		spl = append(spl, exprSplice{
			Name:  s.Name,
			Cat:   cat,
			Src:   s.Src,
			Out:   nanopass.SourceRange{Start: start, End: start + len(v)},
			Value: v,
		})
		prev = s.Src.End
	}
	if len(spl) == 0 {
		return
	}
	b.WriteString(sql[prev:])
	out = b.String()
	return
}

// exprMarkFor maps a parse error in the SUBSTITUTED buffer back onto the field
// it came from: the returned range is relative to that slot's VALUE, which is
// what the field is bound to.
//
// §SD6's attribution rule. An error position inside a spliced value is that
// field's error; one outside it belongs to the query, and the caller reports it
// as a query-level error rather than blaming a field for it. ANTLR's recovery
// can report at a distance from the real fault, so this degrades to "the query
// does not parse" rather than to underlining the wrong text.
//
// The mark runs from the error to the end of the value. A single offset would
// underline one character, which reads as a stray artefact rather than as a
// span; ending at the value's end says "from here on" without claiming to know
// where the fault stops.
func exprMarkFor(spl []exprSplice, errOffset int) (name string, mark nanopass.SourceRange, ok bool) {
	for _, s := range spl {
		if errOffset < s.Out.Start || errOffset > s.Out.End {
			continue
		}
		rel := errOffset - s.Out.Start
		if rel >= len(s.Value) {
			// The error sits at the value's closing edge — the fault is what
			// the value failed to finish, so mark the whole of it.
			rel = 0
		}
		return s.Name, nanopass.SourceRange{Start: rel, End: len(s.Value)}, true
	}
	return
}

// pinExprClaim is [PlayApp.pinParamClaim]'s arm for a SQL-valued slot
// (ADR-0187 (proposed) §SD3): it authors the `-- play: expr` line the prelude
// would otherwise have been, through the same writer a pinned drift uses so
// there is one definition of the syntax.
//
// The value is the store's — what the field is showing — falling back to the
// draft for a name the store never held. As with a `SET`, the store KEEPS its
// value: the directive shadows it at substitution time rather than replacing
// it, so pinning a predicate a panel publishes does not wipe the panel's, and
// unpinning finds it still there.
func (inst *PlayApp) pinExprClaim(subset []paramSlot) {
	values := make(map[string]string, len(inst.paramSyncedExprs)+len(subset))
	maps.Copy(values, inst.paramSyncedExprs)
	for _, s := range subset {
		v, held := inst.signalRawFor(s.Name)
		if !held {
			if ptr, has := inst.paramDrafts[s.Name]; has {
				v = *ptr
			}
		}
		if v == "" {
			// An empty declaration is not one (§SD3), so there is nothing to
			// pin — the slot stays unfilled and the control stays offered.
			continue
		}
		values[s.Name] = v
	}
	out, changed := syncExprDirectives(inst.sql, values)
	if !changed {
		return
	}
	inst.sql = out
	for _, s := range subset {
		v, ok := values[s.Name]
		if !ok {
			continue
		}
		// The tier bit flips now, not when the debounced parse catches up: the
		// frame in between must not read as live drift and write the value
		// straight back into the store it just left.
		inst.paramSyncedExprs[s.Name] = v
		delete(inst.paramLiveSeeded, s.Name)
		if ptr, has := inst.paramDrafts[s.Name]; has {
			*ptr = v
		}
	}
}

// unpinExprClaim is the same migration in reverse: the declaration goes, and
// the value it carried is seeded into the store so the field keeps showing it
// and the name is immediately live rather than unfilled.
func (inst *PlayApp) unpinExprClaim(subset []paramSlot) {
	values := make(map[string]string, len(inst.paramSyncedExprs))
	maps.Copy(values, inst.paramSyncedExprs)
	freed := make(map[string]string, len(subset))
	for _, s := range subset {
		v, declared := inst.paramSyncedExprs[s.Name]
		if !declared {
			continue
		}
		freed[s.Name] = v
		delete(values, s.Name)
	}
	if len(freed) == 0 {
		return
	}
	out, changed := syncExprDirectives(inst.sql, values)
	if !changed {
		return
	}
	inst.sql = out
	for _, s := range subset {
		v, ok := freed[s.Name]
		if !ok {
			continue
		}
		delete(inst.paramSyncedExprs, s.Name)
		inst.noteLiveSeeded(s.Name, v)
		if inst.graph != nil {
			inst.graph.setSignalRawFrom(s.Name, v, signalWriterParamWidget)
		}
	}
}

// exprDirectiveLine renders one `-- play: expr` declaration.
func exprDirectiveLine(name, value string) string {
	return "-- play: expr " + name + " = " + value + "\n"
}

// syncExprDirectives rewrites sql so its `-- play: expr` lines exactly match
// values, and returns the new buffer.
//
// The mirror ADR-0124 §SD4 establishes for the `SET` prelude, applied to the
// directive: a widget writes a draft, and exactly one place turns drafts into
// buffer text. Declarations for names in values are rewritten in place, so an
// author's line ordering and their position relative to the prelude survive
// editing; a name that gained a value with no line yet gets one appended after
// the last existing declaration, or immediately after the `SET` prelude when
// there is none — never above it, which would end the prelude before it starts
// (§SD3).
//
// A name whose value went empty loses its line: an empty declaration is not a
// declaration, and leaving `-- play: expr cond =` behind would read as one.
//
// Idempotent: returns (sql, false) when the buffer already says this.
func syncExprDirectives(sql string, values map[string]string) (out string, changed bool) {
	lines := strings.Split(sql, "\n")
	seen := make(map[string]bool, len(values))
	kept := make([]string, 0, len(lines)+len(values))
	lastDecl := -1
	for _, line := range lines {
		name, _, isDecl := parseExprDirectiveLine(line)
		if !isDecl {
			kept = append(kept, line)
			continue
		}
		v, wanted := values[name]
		if !wanted || v == "" || seen[name] {
			// Dropped: no value, an emptied value, or a duplicate whose first
			// occurrence already won (scanExprHints's rule).
			continue
		}
		seen[name] = true
		kept = append(kept, strings.TrimSuffix(exprDirectiveLine(name, v), "\n"))
		lastDecl = len(kept) - 1
	}
	// Append the declarations the buffer does not carry yet, in a stable order
	// so two runs of one edit produce one buffer.
	var fresh []string
	for name, v := range values {
		if v == "" || seen[name] {
			continue
		}
		fresh = append(fresh, name)
	}
	sort.Strings(fresh)
	if len(fresh) > 0 {
		at := lastDecl + 1
		if lastDecl < 0 {
			at = exprDirectiveInsertPoint(kept)
		}
		add := make([]string, 0, len(fresh))
		for _, name := range fresh {
			add = append(add, strings.TrimSuffix(exprDirectiveLine(name, values[name]), "\n"))
		}
		kept = append(kept[:at], append(add, kept[at:]...)...)
	}
	out = strings.Join(kept, "\n")
	return out, out != sql
}

// exprDirectiveInsertPoint is the first line index a declaration may occupy:
// past the `SET` prelude, so the declaration lands in the residual where §SD3
// puts it.
//
// The prelude it steps over is the one `dsl/env` defines — a leading run of
// blank, comment and `SET` lines, ending at the last line that carries a `SET`
// (ADR-0006, 2026-08-15 Update). Anchoring on the last SET rather than
// breaking at the first non-SET line is what keeps a declaration below a
// header comment: written above one, it would sit inside the prelude region
// and be re-emitted above the `SET` block on the next round-trip, which is the
// one placement §SD3 rules out.
//
// A buffer with no `SET` at all has no prelude, so the declaration goes to the
// top — line 0, above any header comment. That is the same answer the old
// first-non-SET-line scan gave, and it is deliberate: with nothing to sit
// below, the top is where the reader looks.
func exprDirectiveInsertPoint(lines []string) int {
	at := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToUpper(trimmed), "SET ") {
			at = i + 1
			continue
		}
		// Blank and comment lines are prelude only if a SET follows; they do
		// not move the insert point themselves, so a comment trailing the
		// prelude stays below the declaration rather than above it.
		if trimmed == "" || strings.HasPrefix(trimmed, "--") || strings.HasPrefix(trimmed, "//") {
			continue
		}
		break
	}
	return at
}

// parseExprDirectiveLine recognises one declaration line, with the same rules
// [scanExprHints] reads it by — one definition of the syntax, so the writer and
// the reader cannot drift apart.
func parseExprDirectiveLine(line string) (name, value string, ok bool) {
	ln := strings.TrimSpace(line)
	if !strings.HasPrefix(ln, "--") {
		return
	}
	ln = strings.TrimSpace(strings.TrimPrefix(ln, "--"))
	if !strings.HasPrefix(strings.ToLower(ln), exprMarkerPrefix) {
		return
	}
	rest := strings.TrimSpace(ln[len(exprMarkerPrefix):])
	name, value, ok = strings.Cut(rest, "=")
	if !ok {
		return "", "", false
	}
	name = strings.TrimSpace(name)
	value = strings.TrimSpace(value)
	if name == "" {
		return "", "", false
	}
	return name, value, true
}
