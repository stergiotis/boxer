package canonicaltypesummary

import (
	"strconv"
	"strings"
	"sync"

	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
)

// pkgParser is reused across parse calls — [canonicaltypes.Parser] resets its
// lexer / token-stream per call and is designed for reuse. egui rendering is
// single-threaded, but parseType still guards with pkgParserMu so a stray
// off-thread caller cannot corrupt the shared antlr state.
var (
	pkgParser   = canonicaltypes.NewParser()
	pkgParserMu sync.Mutex
)

// parseType parses a canonical string into an AST: a single primitive, a flat
// group ('-'-separated), or a signature (groups joined by the canonical '_'
// separator). The signature case is handled by splitting on the separator and
// parsing each segment as a primitive-or-group, which keeps this widget on the
// exported API (no grammar-internal walk); it assumes the canonical '_' form,
// which is what String() and the editor emit.
func parseType(s string) (ast canonicaltypes.AstNodeI, err error) {
	pkgParserMu.Lock()
	defer pkgParserMu.Unlock()
	if !strings.Contains(s, canonicaltypes.SignatureSeparator) {
		ast, err = pkgParser.ParsePrimitiveTypeOrGroupAst(s)
		return
	}
	parts := strings.Split(s, canonicaltypes.SignatureSeparator)
	members := make([]canonicaltypes.AstNodeI, 0, len(parts))
	for _, p := range parts {
		var n canonicaltypes.AstNodeI
		n, err = pkgParser.ParsePrimitiveTypeOrGroupAst(p)
		if err != nil {
			return
		}
		members = append(members, n)
	}
	ast = canonicaltypes.NewSignatureAstNode(members)
	return
}

// memberInfo is the decomposed, display-ready view of one primitive member,
// shared by the Layout strip, the Members table, and the footprint totals.
type memberInfo struct {
	canonical string // m.String()
	family    string // "string" | "numeric" | "temporal" | "network"
	base      string // human base, e.g. "uint", "utf8", "ipv4"
	width     int    // bit width, 0 when not applicable
	byteOrder string // "LE" | "BE" | ""
	scalar    string // "scalar" | "array" | "set"
	bytes     int    // fixed per-value footprint in bytes, 0 when variable/unknown
	variable  bool   // true when the per-value footprint is variable-length
	note      string // short qualifier, e.g. "variable-length", "× N elements"
}

// describeMember decomposes one primitive node into a [memberInfo] by
// type-switching on the concrete AST struct (their fields are exported). The
// per-value byte footprint uses [canonicaltypes.NetworkTypeAstNode.ByteWidth]
// for network types and width÷8 for fixed machine-numeric / temporal /
// fixed-width string types; everything else is variable-length. A non-scalar
// shape (array / set) makes the member variable-length overall regardless of
// its element footprint.
func describeMember(m canonicaltypes.PrimitiveAstNodeI) (info memberInfo) {
	info = memberInfo{canonical: m.String(), family: "?", base: "?", scalar: "scalar"}
	switch n := m.(type) {
	case canonicaltypes.StringAstNode:
		info.family = "string"
		switch n.BaseType {
		case canonicaltypes.BaseTypeStringUtf8:
			info.base = "utf8"
		case canonicaltypes.BaseTypeStringBytes:
			info.base = "bytes"
		case canonicaltypes.BaseTypeStringBool:
			info.base = "bool"
		default:
			info.base = n.BaseType.String()
		}
		info.scalar = scalarLabel(n.ScalarModifier)
		switch {
		case n.BaseType == canonicaltypes.BaseTypeStringBool:
			info.bytes = 1
			info.note = "bit-packable"
		case n.WidthModifier == canonicaltypes.WidthModifierFixed && n.Width > 0:
			info.width = int(n.Width)
			info.bytes = (int(n.Width) + 7) / 8
		default:
			info.variable = true
			info.note = "variable-length"
		}
	case canonicaltypes.MachineNumericTypeAstNode:
		info.family = "numeric"
		switch n.BaseType {
		case canonicaltypes.BaseTypeMachineNumericUnsigned:
			info.base = "uint"
		case canonicaltypes.BaseTypeMachineNumericSigned:
			info.base = "int"
		case canonicaltypes.BaseTypeMachineNumericFloat:
			info.base = "float"
		default:
			info.base = n.BaseType.String()
		}
		info.width = int(n.Width)
		info.byteOrder = byteOrderLabel(n.ByteOrderModifier)
		info.scalar = scalarLabel(n.ScalarModifier)
		if n.Width > 0 {
			info.bytes = (int(n.Width) + 7) / 8
		}
	case canonicaltypes.TemporalTypeAstNode:
		info.family = "temporal"
		switch n.BaseType {
		case canonicaltypes.BaseTypeTemporalUtcDatetime:
			info.base = "utc-datetime"
		case canonicaltypes.BaseTypeTemporalZonedDatetime:
			info.base = "zoned-datetime"
		case canonicaltypes.BaseTypeTemporalZonedTime:
			info.base = "zoned-time"
		default:
			info.base = n.BaseType.String()
		}
		info.width = int(n.Width)
		info.scalar = scalarLabel(n.ScalarModifier)
		if n.Width > 0 {
			info.bytes = (int(n.Width) + 7) / 8
		}
	case canonicaltypes.NetworkTypeAstNode:
		info.family = "network"
		info.base = n.BaseType.String() // "ipv4" / "ipv6"
		info.scalar = scalarLabel(n.ScalarModifier)
		info.bytes = n.ByteWidth()
		if n.CIDRModifier == canonicaltypes.CIDRModifierVariable {
			info.note = "CIDR (+1 B prefix)"
		}
	}
	if info.scalar != "scalar" {
		info.variable = true
		if info.note == "" {
			info.note = "× N elements"
		}
	}
	return
}

// footprint sums the fixed per-value byte footprints across all members and
// reports whether any member is variable-length, plus the member count.
func footprint(ast canonicaltypes.AstNodeI) (fixedBytes int, anyVar bool, count int) {
	for m := range ast.IterateMembers() {
		info := describeMember(m)
		count++
		if info.variable || info.bytes == 0 {
			anyVar = true
			continue
		}
		fixedBytes += info.bytes
	}
	return
}

// generateGoSource renders the AST as compilable Go. A signature becomes a
// [canonicaltypes.NewSignatureAstNode] over its group/primitive members; a
// group becomes a [canonicaltypes.NewGroupAstNode] over primitive literals; a
// bare primitive becomes a single qualified struct literal (each primitive via
// [canonicaltypes.PrimitiveAstNodeI.GenerateGoCode]).
func generateGoSource(ast canonicaltypes.AstNodeI) string {
	sig, ok := ast.(canonicaltypes.SignatureAstNode)
	if !ok {
		return goGroupOrPrimitive(ast)
	}
	var b strings.Builder
	b.WriteString("canonicaltypes.NewSignatureAstNode([]canonicaltypes.AstNodeI{\n")
	for g := range sig.IterateGroupMembers() {
		b.WriteString("\t")
		b.WriteString(goGroupOrPrimitive(g))
		b.WriteString(",\n")
	}
	b.WriteString("})")
	return b.String()
}

// goGroupOrPrimitive renders one signature element: a primitive as a qualified
// struct literal, or a group as a NewGroupAstNode over its primitive literals.
func goGroupOrPrimitive(ast canonicaltypes.AstNodeI) string {
	var b strings.Builder
	if p, ok := ast.(canonicaltypes.PrimitiveAstNodeI); ok {
		b.WriteString("canonicaltypes.")
		_ = p.GenerateGoCode(&b)
		return b.String()
	}
	b.WriteString("canonicaltypes.NewGroupAstNode([]canonicaltypes.PrimitiveAstNodeI{")
	first := true
	for m := range ast.IterateMembers() {
		if !first {
			b.WriteString(", ")
		}
		first = false
		b.WriteString("canonicaltypes.")
		_ = m.GenerateGoCode(&b)
	}
	b.WriteString("})")
	return b.String()
}

// footprintTrailer is the terse level-1 suffix, e.g. "1 field · 4 B",
// "3 fields · 9 B+var", or "1 field · var".
func footprintTrailer(count, fixedBytes int, anyVar bool) string {
	field := "fields"
	if count == 1 {
		field = "field"
	}
	return strconv.Itoa(count) + " " + field + " · " + footprintBytes(fixedBytes, anyVar)
}

// footprintHeader is the Layout-tab caption above the strip.
func footprintHeader(count, fixedBytes int, anyVar bool) string {
	field := "fields"
	if count == 1 {
		field = "field"
	}
	return "wire footprint: " + footprintBytes(fixedBytes, anyVar) + " · " + strconv.Itoa(count) + " " + field
}

// footprintBytes renders the byte summary shared by the trailer and header.
func footprintBytes(fixedBytes int, anyVar bool) string {
	switch {
	case fixedBytes > 0 && anyVar:
		return strconv.Itoa(fixedBytes) + " B+var"
	case fixedBytes > 0:
		return strconv.Itoa(fixedBytes) + " B"
	default:
		return "var"
	}
}

func scalarLabel(m canonicaltypes.ScalarModifierE) string {
	switch m {
	case canonicaltypes.ScalarModifierHomogenousArray:
		return "array"
	case canonicaltypes.ScalarModifierSet:
		return "set"
	default:
		return "scalar"
	}
}

func byteOrderLabel(m canonicaltypes.ByteOrderModifierE) string {
	switch m {
	case canonicaltypes.ByteOrderModifierLittleEndian:
		return "LE"
	case canonicaltypes.ByteOrderModifierBigEndian:
		return "BE"
	default:
		return ""
	}
}

func widthStr(info memberInfo) string {
	if info.width > 0 {
		return strconv.Itoa(info.width) + "b"
	}
	return "—"
}

func bytesStr(info memberInfo) string {
	if info.variable || info.bytes == 0 {
		return "var"
	}
	return strconv.Itoa(info.bytes)
}

func emptyDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// truncate caps s to maxLen runes, appending a single ellipsis on overflow.
// Counts in runes so multi-byte glyphs are not cut mid-codepoint.
func truncate(s string, maxLen int) string {
	if maxLen < 1 {
		maxLen = defaultNameMaxLen
	}
	r := []rune(s)
	if len(r) <= maxLen {
		return s
	}
	return string(r[:maxLen-1]) + "…"
}

// firstLine returns the first line of s, trimmed — parser errors can be
// multi-line and the level-2 banner shows only the headline.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
