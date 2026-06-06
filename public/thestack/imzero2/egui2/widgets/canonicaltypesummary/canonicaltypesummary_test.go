package canonicaltypesummary

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/inspector"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// describeOne parses a single-primitive canonical string and returns its
// decomposed memberInfo, asserting exactly one member and a clean parse +
// canonical round-trip (String() == input for the canonical forms used here).
func describeOne(t *testing.T, s string) memberInfo {
	t.Helper()
	ast, err := parseType(s)
	require.NoError(t, err, "parse %q", s)
	require.Equal(t, s, ast.String(), "round-trip %q", s)
	var got memberInfo
	n := 0
	for m := range ast.IterateMembers() {
		got = describeMember(m)
		n++
	}
	require.Equal(t, 1, n, "expected exactly one member for %q", s)
	return got
}

// TestDescribeMemberFootprint pins the per-family classification and the
// hand-rolled byte-footprint maths — the part of this widget most prone to
// silent drift. width÷8 for fixed numeric/temporal/string, ByteWidth for
// network (incl. the CIDR +1 byte), variable for unbounded strings and any
// array/set shape.
func TestDescribeMemberFootprint(t *testing.T) {
	cases := []struct {
		in        string
		family    string
		base      string
		width     int
		byteOrder string
		scalar    string
		bytes     int
		variable  bool
	}{
		{"u32", "numeric", "uint", 32, "", "scalar", 4, false},
		{"u32l", "numeric", "uint", 32, "LE", "scalar", 4, false},
		{"i64n", "numeric", "int", 64, "BE", "scalar", 8, false},
		{"f64", "numeric", "float", 64, "", "scalar", 8, false},
		{"i8", "numeric", "int", 8, "", "scalar", 1, false},
		{"z64", "temporal", "utc-datetime", 64, "", "scalar", 8, false},
		{"d32", "temporal", "zoned-datetime", 32, "", "scalar", 4, false},
		{"sx128", "string", "utf8", 128, "", "scalar", 16, false},
		{"s", "string", "utf8", 0, "", "scalar", 0, true},
		{"y", "string", "bytes", 0, "", "scalar", 0, true},
		{"b", "string", "bool", 0, "", "scalar", 1, false},
		{"v", "network", "ipv4", 0, "", "scalar", 4, false},
		{"vc", "network", "ipv4", 0, "", "scalar", 5, false},
		{"w", "network", "ipv6", 0, "", "scalar", 16, false},
		{"wc", "network", "ipv6", 0, "", "scalar", 17, false},
		// Collections are variable-length overall regardless of element size.
		{"sh", "string", "utf8", 0, "", "array", 0, true},
		{"u8m", "numeric", "uint", 8, "", "set", 1, true},
	}
	for _, tc := range cases {
		info := describeOne(t, tc.in)
		assert.Equal(t, tc.in, info.canonical, "canonical %q", tc.in)
		assert.Equal(t, tc.family, info.family, "family %q", tc.in)
		assert.Equal(t, tc.base, info.base, "base %q", tc.in)
		assert.Equal(t, tc.width, info.width, "width %q", tc.in)
		assert.Equal(t, tc.byteOrder, info.byteOrder, "byteOrder %q", tc.in)
		assert.Equal(t, tc.scalar, info.scalar, "scalar %q", tc.in)
		assert.Equal(t, tc.bytes, info.bytes, "bytes %q", tc.in)
		assert.Equal(t, tc.variable, info.variable, "variable %q", tc.in)
	}
}

// TestFootprintGroupTotals locks the group aggregation: fixed members sum,
// any variable member flips anyVar, and count tracks the member tally.
func TestFootprintGroupTotals(t *testing.T) {
	// u32 (4, fixed) + i16 (2, fixed) → 6 B, no variable.
	ast, err := parseType("u32-i16")
	require.NoError(t, err)
	fixedBytes, anyVar, count := footprint(ast)
	assert.Equal(t, 6, fixedBytes)
	assert.False(t, anyVar)
	assert.Equal(t, 2, count)

	// u32 (4) + s (variable) + v (4) → 8 B fixed + variable.
	ast, err = parseType("u32-s-v")
	require.NoError(t, err)
	fixedBytes, anyVar, count = footprint(ast)
	assert.Equal(t, 8, fixedBytes)
	assert.True(t, anyVar)
	assert.Equal(t, 3, count)
}

// TestParseTypeRejectsGarbage confirms the parse-error path that drives the
// level-1 red dot and the in-window error banner.
func TestParseTypeRejectsGarbage(t *testing.T) {
	_, err := parseType("q!!q")
	assert.Error(t, err)
}

// TestParseTypeSignature pins the signature (group-joined-by-'_') parse path:
// "u32-s_v" is two groups (u32-s and v) and round-trips through String().
func TestParseTypeSignature(t *testing.T) {
	ast, err := parseType("u32-s_v")
	require.NoError(t, err)
	assert.True(t, ast.IsSignature())
	assert.Equal(t, "u32-s_v", ast.String())
}

// TestFootprintSignature confirms members are summed across group boundaries:
// u32(4) + s(var) + v(4) → 8 B fixed + variable, 3 members.
func TestFootprintSignature(t *testing.T) {
	ast, err := parseType("u32-s_v")
	require.NoError(t, err)
	fixedBytes, anyVar, count := footprint(ast)
	assert.Equal(t, 8, fixedBytes)
	assert.True(t, anyVar)
	assert.Equal(t, 3, count)
}

// TestGenerateGoSourceSignature pins the signature codegen shape: a
// NewSignatureAstNode wrapping a NewGroupAstNode and a bare primitive.
func TestGenerateGoSourceSignature(t *testing.T) {
	ast, err := parseType("u32-s_v")
	require.NoError(t, err)
	src := generateGoSource(ast)
	assert.True(t, strings.HasPrefix(src, "canonicaltypes.NewSignatureAstNode([]canonicaltypes.AstNodeI{"), "src=%q", src)
	assert.Contains(t, src, "canonicaltypes.NewGroupAstNode(")
	assert.Contains(t, src, "canonicaltypes.NetworkTypeAstNode{")
}

// TestStripItems pins the Layout-strip structural walk: a signature expands to
// segments interleaved with the right '-'/'_' boundary markers.
func TestStripItems(t *testing.T) {
	ast, err := parseType("u32-s_vc")
	require.NoError(t, err)
	items := stripItems(ast)
	require.Len(t, items, 5)
	assert.Equal(t, "u32", items[0].info.canonical)
	assert.Equal(t, "-", items[1].sep)
	assert.Equal(t, "s", items[2].info.canonical)
	assert.Equal(t, "_", items[3].sep)
	assert.Equal(t, "vc", items[4].info.canonical)
}

// TestStripItemsPrimitive: a bare primitive is a single segment, no boundaries.
func TestStripItemsPrimitive(t *testing.T) {
	ast, err := parseType("u32")
	require.NoError(t, err)
	items := stripItems(ast)
	require.Len(t, items, 1)
	assert.Equal(t, "", items[0].sep)
	assert.Equal(t, "u32", items[0].info.canonical)
}

// TestStripItemsGroup: a flat group uses only '-' boundaries.
func TestStripItemsGroup(t *testing.T) {
	ast, err := parseType("u32-s-v")
	require.NoError(t, err)
	items := stripItems(ast)
	require.Len(t, items, 5)
	assert.Equal(t, "-", items[1].sep)
	assert.Equal(t, "-", items[3].sep)
	assert.Equal(t, "v", items[4].info.canonical)
}

// TestGenerateGoSourcePrimitive pins the primitive codegen shape: one
// qualified struct literal carrying the decoded fields.
func TestGenerateGoSourcePrimitive(t *testing.T) {
	ast, err := parseType("u32l")
	require.NoError(t, err)
	src := generateGoSource(ast)
	assert.True(t, strings.HasPrefix(src, "canonicaltypes.MachineNumericTypeAstNode{"), "src=%q", src)
	assert.Contains(t, src, "Width: 32")
	assert.Contains(t, src, "ByteOrderModifier: 'l'")
}

// TestGenerateGoSourceGroup pins the group codegen shape: a NewGroupAstNode
// wrapper around qualified member literals.
func TestGenerateGoSourceGroup(t *testing.T) {
	ast, err := parseType("u32-s")
	require.NoError(t, err)
	src := generateGoSource(ast)
	assert.True(t, strings.HasPrefix(src, "canonicaltypes.NewGroupAstNode([]canonicaltypes.PrimitiveAstNodeI{"), "src=%q", src)
	assert.Contains(t, src, "canonicaltypes.MachineNumericTypeAstNode{")
	assert.Contains(t, src, "canonicaltypes.StringAstNode{")
}

// TestFootprintStrings pins the terse byte summaries shown in the level-1
// trailer and the Layout-tab header.
func TestFootprintStrings(t *testing.T) {
	assert.Equal(t, "1 field · 4 B", footprintTrailer(1, 4, false))
	assert.Equal(t, "3 fields · 8 B+var", footprintTrailer(3, 8, true))
	assert.Equal(t, "1 field · var", footprintTrailer(1, 0, true))
	assert.Equal(t, "var", footprintBytes(0, true))
	assert.Equal(t, "4 B", footprintBytes(4, false))
}

// TestSmallHelpers covers the display-formatting helpers.
func TestSmallHelpers(t *testing.T) {
	assert.Equal(t, "32b", widthStr(memberInfo{width: 32}))
	assert.Equal(t, "—", widthStr(memberInfo{width: 0}))
	assert.Equal(t, "var", bytesStr(memberInfo{variable: true}))
	assert.Equal(t, "4", bytesStr(memberInfo{bytes: 4}))
	assert.Equal(t, "—", emptyDash(""))
	assert.Equal(t, "LE", emptyDash("LE"))
	assert.Equal(t, "first", firstLine("first\nsecond"))
}

// TestTruncate locks the rune-boundary truncation contract (cut never lands
// mid-codepoint; ellipsis counts toward the cap; n<1 clamps to default).
func TestTruncate(t *testing.T) {
	assert.Equal(t, "u32", truncate("u32", 48))
	assert.Equal(t, "abcd…", truncate("abcdefghij", 5))
	assert.Equal(t, "αβγδ…", truncate("αβγδεζηθικλ", 5))
	assert.Equal(t, "abcdef", truncate("abcdef", 0))
}

// TestCallScopeDeterministic locks the "idPrefix#<hex>" scope format and its
// stability + uniqueness contract.
func TestCallScopeDeterministic(t *testing.T) {
	a := callScope("col-type", 0xDEADBEEF)
	b := callScope("col-type", 0xDEADBEEF)
	c := callScope("col-type", 0xCAFEBABE)
	assert.Equal(t, a, b)
	assert.NotEqual(t, a, c)
	assert.Equal(t, "col-type#deadbeef", a)
}

// TestGetInstanceStateIdempotent locks LoadOrStore: the same scope returns
// the same pointer so per-instance state survives across frames.
func TestGetInstanceStateIdempotent(t *testing.T) {
	scope := callScope("idem", 0x1234)
	a := getInstanceState(scope)
	b := getInstanceState(scope)
	assert.Same(t, a, b)
	assert.NotSame(t, a, getInstanceState(callScope("idem", 0x5678)))
}

// TestRendererDefaults pins the documented constructor defaults, including
// the SurfaceInspector-derived popup envelope.
func TestRendererDefaults(t *testing.T) {
	r := New("t")
	assert.Equal(t, "t", r.idPrefix)
	assert.Equal(t, float32(styletokens.SurfaceInspector.W), r.popupWidth)
	assert.Equal(t, float32(styletokens.SurfaceInspector.H), r.popupHeight)
	assert.Equal(t, defaultNameMaxLen, r.nameMaxLen)
	assert.True(t, r.showIcon)
	assert.False(t, r.defaultOpen)
	assert.True(t, r.provenance.IsZero())
}

// TestRendererFluentSettersReturnCopies locks the value-receiver contract:
// the base is untouched and the returned copy carries the new values.
func TestRendererFluentSettersReturnCopies(t *testing.T) {
	base := New("t")
	mod := base.
		PopupSize(640, 480).
		NameMaxLen(64).
		ShowIcon(false).
		DefaultOpen(true).
		Provenance(inspector.Provenance{Subject: "schema.col"})
	assert.Equal(t, float32(styletokens.SurfaceInspector.W), base.popupWidth)
	assert.Equal(t, defaultNameMaxLen, base.nameMaxLen)
	assert.True(t, base.showIcon)
	assert.False(t, base.defaultOpen)
	assert.True(t, base.provenance.IsZero())
	assert.Equal(t, float32(640), mod.popupWidth)
	assert.Equal(t, float32(480), mod.popupHeight)
	assert.Equal(t, 64, mod.nameMaxLen)
	assert.False(t, mod.showIcon)
	assert.True(t, mod.defaultOpen)
	assert.False(t, mod.provenance.IsZero())
}

// TestNameMaxLenClamps confirms the "n<1 → default" guard.
func TestNameMaxLenClamps(t *testing.T) {
	assert.Equal(t, defaultNameMaxLen, New("t").NameMaxLen(0).nameMaxLen)
	assert.Equal(t, defaultNameMaxLen, New("t").NameMaxLen(-5).nameMaxLen)
}
