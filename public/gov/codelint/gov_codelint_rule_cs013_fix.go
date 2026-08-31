package codelint

// The mechanical half of CS013: given a flagged call, decide whether the
// rewrite can be made without a human, and if so what it is.
//
// This is the acceptance rule from the scripted first pass over the backlog,
// kept because the judgment in it is not obvious and does not survive in a
// diff. Roughly a quarter of the backlog cleared mechanically; the reasons the
// rest did not are the interesting part, and they are encoded here rather than
// rediscovered by the next person who tries.

import (
	"go/ast"
	"go/printer"
	"go/token"
	"go/types"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/tools/go/analysis"
)

// dropDelimiters are the characters a directive may abut on its right and
// still be droppable.
//
// The value was formatted onto the tail of a clause, so deleting it leaves the
// clause intact: "open %s: %w" becomes "open: %w". A directive followed by a
// word is a value in the middle of a sentence — "tier needs %d files, %s holds
// %d" reduces to "tier needs files, holds" — and removing it needs the sentence
// rewritten, which is not mechanical. A comma is excluded for the same reason a
// word is: a value inside a comma-separated list leaves "stored, computed: %w".
const dropDelimiters = ":;)."

// messageArtifacts are substrings whose presence in the rewritten message means
// the removal left broken punctuation. Cheaper and more reliable than trying to
// predict them: rewrite, then look.
var messageArtifacts = []string{
	",,", ",)", ",:", "::", "()", "( ", " )", ",;", "[]", "=,", ":,",
	" ,", " :", " ;", "  ", "''", `""`, "..", "//",
}

// tailFunctionWords are words a message must not end on once the value after
// them is gone. "unable to open the staged %s" would become "unable to open the
// staged"; the sentence still parses and says the wrong thing, which is worse
// than leaving the directive in place.
var tailFunctionWords = map[string]struct{}{
	"a": {}, "an": {}, "the": {}, "in": {}, "on": {}, "at": {}, "to": {},
	"of": {}, "for": {}, "from": {}, "by": {}, "with": {}, "into": {},
	"after": {}, "before": {}, "than": {}, "is": {}, "was": {}, "are": {},
	"were": {}, "and": {}, "or": {}, "not": {}, "no": {}, "as": {}, "via": {},
	"per": {}, "over": {}, "under": {}, "has": {}, "have": {}, "had": {},
	"be": {}, "been": {}, "got": {}, "gets": {}, "expected": {}, "want": {},
	"wants": {}, "need": {}, "needs": {}, "holds": {}, "only": {}, "must": {},
	"up": {}, "down": {}, "out": {}, "above": {}, "below": {}, "containing": {},
	"exceeds": {}, "matching": {}, "named": {}, "between": {}, "using": {},
	"against": {},
}

// shortFieldKeys are the one- and two-letter field names that carry meaning on
// their own. Any other key that short is a loop variable or an abbreviation
// only the surrounding function explains, and a field nobody can interpret is
// no better than the prose it came from.
var shortFieldKeys = map[string]struct{}{
	"id": {}, "db": {}, "wd": {}, "ip": {}, "os": {}, "fd": {}, "ok": {},
}

// Decline reasons. These are the shapes the remaining backlog is made of, and
// they name the kind of work a site needs rather than restating the finding —
// "the sentence has to be rewritten" and "somebody has to name this field" are
// different jobs, batched differently.
const (
	DeclineNeedsMessageRewrite = "message needs rewriting"
	DeclineNeedsFieldName      = "field name needs choosing"
	DeclineNoFieldForVerb      = "no eb field preserves this verb"
	DeclineFormattedError      = "wraps a formatted error; %w is a semantic change"
	DeclineNotMechanical       = "not mechanically decidable"
)

// printfDirective locates one verb in a format string.
type printfDirective struct {
	start, end int // byte range in the unquoted format
	verb       rune
}

// printfDirectives locates every verb in format. A doubled %% is an escape and
// yields none. Malformed directives are skipped: go vet's printf analyzer is
// the authority on those.
func printfDirectives(format string) (out []printfDirective) {
	b := []byte(format)
	for i := 0; i < len(b); i++ {
		if b[i] != '%' {
			continue
		}
		start := i
		i++
		if i >= len(b) {
			break
		}
		if b[i] == '%' {
			continue
		}
		// Flags, argument index, width and precision. None can be a letter, so
		// the first rune that is not one of these is the verb.
		for i < len(b) && strings.IndexByte("+-# 0123456789.*[]", b[i]) >= 0 {
			i++
		}
		if i >= len(b) {
			break
		}
		r := rune(b[i])
		if !unicode.IsLetter(r) {
			continue
		}
		out = append(out, printfDirective{start: start, end: i + 1, verb: r})
	}
	return
}

// MessageWithoutDirectives removes every non-%w directive from format, together
// with one preceding space, and reports whether what is left is publishable
// prose.
//
// ok is false when the result would read worse than the original — a value
// glued to an operator ("n=%d"), a value mid-sentence, leftover punctuation, or
// a sentence ending on the word that introduced the value. Those are the sites
// a human has to rewrite; there is no partial credit, because a message is read
// by whoever is debugging at the time.
func MessageWithoutDirectives(format string) (msg string, ok bool) {
	dirs := printfDirectives(format)
	b := []byte(format)
	// Back to front, so earlier offsets stay valid.
	for i := len(dirs) - 1; i >= 0; i-- {
		d := dirs[i]
		if d.verb == 'w' {
			continue
		}
		if d.start == 0 || b[d.start-1] != ' ' {
			// Glued to a word or an operator: "n=%d" leaves "n=".
			return "", false
		}
		rest := b[d.end:]
		if len(rest) > 0 && strings.IndexByte(dropDelimiters, rest[0]) < 0 {
			return "", false
		}
		b = append(b[:d.start-1], b[d.end:]...)
	}
	msg = string(b)
	msg = strings.ReplaceAll(msg, " :", ":")
	msg = strings.ReplaceAll(msg, " ,", ",")
	for strings.Contains(msg, "  ") {
		msg = strings.ReplaceAll(msg, "  ", " ")
	}
	msg = strings.TrimRight(msg, " ")
	if msg == "" {
		return "", false
	}
	for _, a := range messageArtifacts {
		if strings.Contains(msg, a) {
			return "", false
		}
	}
	switch msg[len(msg)-1] {
	case ',', '(', '[', '=', '-', '/':
		return "", false
	case ':':
		// A trailing ':' introduces something; only %w still can.
		if !strings.Contains(msg, "%w") {
			return "", false
		}
	}
	switch msg[0] {
	case ',', ':', ')', ']':
		return "", false
	}
	if endsOnFunctionWord(msg) {
		return "", false
	}
	ok = true
	return
}

func endsOnFunctionWord(msg string) (bad bool) {
	t := strings.TrimRight(msg, " :")
	t = strings.TrimSuffix(t, "%w")
	t = strings.TrimRight(t, " :")
	fs := strings.Fields(t)
	if len(fs) == 0 {
		return
	}
	last := strings.Trim(fs[len(fs)-1], "(),;\"'")
	if _, hit := tailFunctionWords[strings.ToLower(last)]; hit {
		bad = true
		return
	}
	// "… the staged", "… an entry": an article plus one word is a noun phrase
	// whose noun was the value that just went away.
	if len(fs) >= 2 {
		switch strings.ToLower(fs[len(fs)-2]) {
		case "the", "a", "an", "this", "that":
			bad = true
		}
	}
	return
}

// UsefulFieldKey reports whether key is worth persisting as a field name.
func UsefulFieldKey(key string) (ok bool) {
	if len(key) >= 3 {
		return true
	}
	_, ok = shortFieldKeys[strings.ToLower(key)]
	return
}

// EbFieldMethod maps an argument type and the verb that consumed it onto the
// typed ErrorBuilder method that carries it, plus a conversion to spell when
// the type is a named version of the method's parameter type
// (Uint8("kind", uint8(k)) for a named uint8).
//
// ok is false where no method fits, which is a statement about eb's surface and
// not about the site: a rendering-specific verb (%x on an integer, %c, %p) has
// no field that preserves what the message showed, and a formatted error has to
// become %w, which changes unwrap semantics and is therefore not mechanical.
func EbFieldMethod(t types.Type, verb rune) (method string, conv string, ok bool) {
	if verb == 'T' {
		return "Type", "", true
	}
	errIface, _ := types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
	if errIface != nil {
		if types.Implements(t, errIface) || types.Implements(types.NewPointer(t), errIface) {
			return "", "", false
		}
	}
	if verb == 's' || verb == 'v' || verb == 'q' {
		if hasStringMethod(t) {
			return "Stringer", "", true
		}
	}
	_, named := t.(*types.Named)
	switch u := t.Underlying().(type) {
	case *types.Basic:
		m, base, bok := basicFieldMethod(u, verb)
		if !bok {
			return "", "", false
		}
		if named {
			return m, base, true
		}
		return m, "", true
	case *types.Slice:
		if named {
			return "", "", false
		}
		m, sok := sliceFieldMethod(u, verb)
		return m, "", sok
	case *types.Struct:
		if n, isNamed := t.(*types.Named); isNamed {
			o := n.Obj()
			if o.Pkg() != nil && o.Pkg().Path() == "time" && o.Name() == "Time" {
				return "Time", "", true
			}
		}
	}
	return "", "", false
}

// declineForType separates the two reasons EbFieldMethod says no, because they
// are different work: a formatted error becomes %w by hand (and changes what
// errors.Is traverses), while a rendering-specific verb needs a decision about
// what the message was for.
func declineForType(t types.Type) (reason string) {
	errIface, _ := types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
	if errIface != nil && (types.Implements(t, errIface) || types.Implements(types.NewPointer(t), errIface)) {
		return DeclineFormattedError
	}
	return DeclineNoFieldForVerb
}

func hasStringMethod(t types.Type) (ok bool) {
	ms := types.NewMethodSet(t)
	for i := 0; i < ms.Len(); i++ {
		f, _ := ms.At(i).Obj().(*types.Func)
		if f == nil || f.Name() != "String" {
			continue
		}
		sig, _ := f.Type().(*types.Signature)
		if sig == nil || sig.Params().Len() != 0 || sig.Results().Len() != 1 {
			continue
		}
		if b, isBasic := sig.Results().At(0).Type().(*types.Basic); isBasic && b.Kind() == types.String {
			return true
		}
	}
	return
}

func basicFieldMethod(b *types.Basic, verb rune) (method string, base string, ok bool) {
	numeric := func(m string, bs string) (string, string, bool) {
		if verb == 'd' || verb == 'v' {
			return m, bs, true
		}
		return "", "", false
	}
	float := func(m string, bs string) (string, string, bool) {
		if verb == 'f' || verb == 'v' || verb == 'g' || verb == 'e' {
			return m, bs, true
		}
		return "", "", false
	}
	switch b.Kind() {
	case types.String:
		if verb == 's' || verb == 'v' || verb == 'q' {
			return "Str", "string", true
		}
	case types.Bool:
		if verb == 'v' || verb == 't' {
			return "Bool", "bool", true
		}
	case types.Int:
		return numeric("Int", "int")
	case types.Int8:
		return numeric("Int8", "int8")
	case types.Int16:
		return numeric("Int16", "int16")
	case types.Int32:
		return numeric("Int32", "int32")
	case types.Int64:
		return numeric("Int64", "int64")
	case types.Uint:
		return numeric("Uint", "uint")
	case types.Uint8:
		return numeric("Uint8", "uint8")
	case types.Uint16:
		return numeric("Uint16", "uint16")
	case types.Uint32:
		return numeric("Uint32", "uint32")
	case types.Uint64:
		return numeric("Uint64", "uint64")
	case types.Float32:
		return float("Float32", "float32")
	case types.Float64:
		return float("Float64", "float64")
	}
	return "", "", false
}

func sliceFieldMethod(s *types.Slice, verb rune) (method string, ok bool) {
	if _, elemNamed := s.Elem().(*types.Named); elemNamed {
		return "", false
	}
	b, isBasic := s.Elem().Underlying().(*types.Basic)
	if !isBasic {
		return "", false
	}
	textual := verb == 's' || verb == 'q' || verb == 'v'
	numeric := verb == 'd' || verb == 'v'
	switch b.Kind() {
	case types.Uint8:
		if verb == 'x' || verb == 'X' {
			return "Hex", true
		}
		if textual {
			return "Bytes", true
		}
	case types.String:
		if textual {
			return "Strs", true
		}
	case types.Int:
		if numeric {
			return "Ints", true
		}
	case types.Int32:
		if numeric {
			return "Ints32", true
		}
	case types.Int64:
		if numeric {
			return "Ints64", true
		}
	case types.Uint32:
		if numeric {
			return "Uints32", true
		}
	case types.Uint64:
		if numeric {
			return "Uints64", true
		}
	case types.Float64:
		if verb == 'f' || verb == 'v' || verb == 'g' || verb == 'e' {
			return "Floats64", true
		}
	}
	return "", false
}

// deriveFieldKey turns an argument into a field key.
//
// First choice is the argument's own name — the last identifier segment,
// lower-camelised, so PkgPath reads as pkgPath. When that name is too short to
// mean anything to whoever reads the field later ("h", "i", "s"), the
// argument's type name is tried instead: a repo.PatchHash reads as patchHash,
// which says more than "h" ever did. An unnamed basic type offers nothing
// worth having — Int("int", n) is not an improvement — so those decline.
//
// An argument that is not name-shaped (a call, an index, an expression) has no
// name to borrow at either level, and inventing one is the human's job.
func deriveFieldKey(e ast.Expr, t types.Type) (key string, ok bool) {
	if name, nok := exprName(e); nok {
		if k := lowerCamel(name); UsefulFieldKey(k) {
			return k, true
		}
	}
	if name, nok := namedTypeName(t); nok {
		if k := lowerCamel(name); UsefulFieldKey(k) {
			return k, true
		}
	}
	return "", false
}

// exprName is the identifier a name-shaped argument ends in.
func exprName(e ast.Expr) (name string, ok bool) {
	switch v := e.(type) {
	case *ast.Ident:
		name = v.Name
	case *ast.SelectorExpr:
		if !nameShaped(v.X) {
			return "", false
		}
		name = v.Sel.Name
	default:
		return "", false
	}
	if name == "" || name == "_" {
		return "", false
	}
	return name, true
}

// namedTypeName is the declared name of a named type, pointers dereferenced.
// An unnamed type, or a named type whose name is one of the predeclared basics,
// yields nothing: those describe the representation, not the value.
func namedTypeName(t types.Type) (name string, ok bool) {
	if t == nil {
		return
	}
	if p, isPtr := t.(*types.Pointer); isPtr {
		t = p.Elem()
	}
	n, isNamed := t.(*types.Named)
	if !isNamed {
		return
	}
	name = n.Obj().Name()
	if name == "" {
		return "", false
	}
	if types.Universe.Lookup(name) != nil {
		// A type literally named after a predeclared identifier tells the
		// reader nothing the value does not already say.
		return "", false
	}
	return name, true
}

// lowerCamel lowers the leading run of capitals up to the next word, so
// PkgPath becomes pkgPath and ID becomes id.
func lowerCamel(name string) (out string) {
	r := []rune(name)
	if len(r) == 0 || !unicode.IsUpper(r[0]) {
		return name
	}
	i := 0
	for i < len(r) && unicode.IsUpper(r[i]) {
		i++
	}
	if i > 1 && i < len(r) {
		i--
	}
	for j := 0; j < i; j++ {
		r[j] = unicode.ToLower(r[j])
	}
	return string(r)
}

func nameShaped(e ast.Expr) (ok bool) {
	switch v := e.(type) {
	case *ast.Ident:
		return true
	case *ast.SelectorExpr:
		return nameShaped(v.X)
	case *ast.StarExpr:
		return nameShaped(v.X)
	case *ast.ParenExpr:
		return nameShaped(v.X)
	}
	return
}

// chainFieldKeys collects the fields an existing eb chain already carries, as
// key -> rendered argument, so a value already on the builder is not added a
// second time and a key already in use is not reused for a different value.
func chainFieldKeys(fset *token.FileSet, e ast.Expr) (byKey map[string]string, byArg map[string]string) {
	byKey = map[string]string{}
	byArg = map[string]string{}
	for {
		call, isCall := e.(*ast.CallExpr)
		if !isCall {
			return
		}
		sel, isSel := call.Fun.(*ast.SelectorExpr)
		if !isSel {
			return
		}
		if len(call.Args) == 2 {
			if lit, isLit := call.Args[0].(*ast.BasicLit); isLit && lit.Kind == token.STRING {
				if key, uerr := strconv.Unquote(lit.Value); uerr == nil {
					arg := renderExpr(fset, call.Args[1])
					byKey[key] = arg
					byArg[arg] = key
				}
			}
		}
		e = sel.X
	}
}

func renderExpr(fset *token.FileSet, e ast.Expr) (s string) {
	var sb strings.Builder
	if err := printer.Fprint(&sb, fset, e); err != nil {
		return ""
	}
	return sb.String()
}

// suggestCS013Fix builds the mechanical rewrite for a flagged call, or reports
// ok=false when the site needs a human.
//
// The edit replaces the whole call expression. It assumes the eb import is
// present: whether a file's last eh use is going away is a file-level question
// this per-call fix cannot answer, so an applier has to reconcile imports
// itself.
func suggestCS013Fix(pass *analysis.Pass, call *ast.CallExpr, sel *ast.SelectorExpr, kind string, fmtIdx int, format string) (fix analysis.SuggestedFix, ok bool, decline string) {
	// ErrorfWithData already has a structured channel of its own; folding one
	// into a builder chain is a different change.
	if kind != "eh.Errorf" && kind != "eb.Build()…Errorf" {
		decline = DeclineNotMechanical
		return
	}
	if strings.Contains(format, "%[") {
		// Explicit argument index: positional mapping is not reliable.
		decline = DeclineNotMechanical
		return
	}
	dirs := printfDirectives(format)
	args := call.Args[fmtIdx+1:]
	if len(dirs) != len(args) {
		decline = DeclineNotMechanical
		return
	}
	newMessage, mok := MessageWithoutDirectives(format)
	if !mok {
		decline = DeclineNeedsMessageRewrite
		return
	}

	haveKey, haveArg := map[string]string{}, map[string]string{}
	if kind == "eb.Build()…Errorf" {
		haveKey, haveArg = chainFieldKeys(pass.Fset, sel.X)
	}

	type field struct{ method, key, arg string }
	var fields []field
	seen := map[string]struct{}{}
	var kept []ast.Expr
	for i, d := range dirs {
		if d.verb == 'w' {
			kept = append(kept, args[i])
			continue
		}
		a := args[i]
		key, kok := deriveFieldKey(a, pass.TypesInfo.Types[a].Type)
		if !kok {
			decline = DeclineNeedsFieldName
			return
		}
		if _, dup := seen[key]; dup {
			decline = DeclineNeedsFieldName
			return
		}
		arg := renderExpr(pass.Fset, a)
		if arg == "" {
			decline = DeclineNotMechanical
			return
		}
		if _, already := haveArg[arg]; already {
			continue // the chain carries this value; dropping the directive is the edit
		}
		if _, clash := haveKey[key]; clash {
			decline = DeclineNeedsFieldName
			return
		}
		tv, found := pass.TypesInfo.Types[a]
		if !found || tv.Type == nil {
			decline = DeclineNotMechanical
			return
		}
		method, conv, mok2 := EbFieldMethod(tv.Type, d.verb)
		if !mok2 {
			decline = declineForType(tv.Type)
			return
		}
		if conv != "" {
			arg = conv + "(" + arg + ")"
		}
		seen[key] = struct{}{}
		fields = append(fields, field{method, key, arg})
	}

	var sb strings.Builder
	if kind == "eh.Errorf" {
		sb.WriteString("eb.Build()")
	} else {
		sb.WriteString(renderExpr(pass.Fset, sel.X))
	}
	for _, f := range fields {
		sb.WriteString(".")
		sb.WriteString(f.method)
		sb.WriteString("(")
		sb.WriteString(strconv.Quote(f.key))
		sb.WriteString(", ")
		sb.WriteString(f.arg)
		sb.WriteString(")")
	}
	sb.WriteString(".Errorf(")
	sb.WriteString(strconv.Quote(newMessage))
	for _, a := range kept {
		sb.WriteString(", ")
		sb.WriteString(renderExpr(pass.Fset, a))
	}
	sb.WriteString(")")

	fix = analysis.SuggestedFix{
		Message: "CS013: move the formatted values onto eb.Build() fields",
		TextEdits: []analysis.TextEdit{{
			Pos:     call.Pos(),
			End:     call.End(),
			NewText: []byte(sb.String()),
		}},
	}
	ok = true
	return
}
