package marshallgen_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallgen"
)

// generate parses src + emits the .out.go bytes using NoOpWrapper.
// The returned string is gofmt-formatted Go source.
func generate(t *testing.T, src string) string {
	t.Helper()
	out, err := generateMay(t, src)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return out
}

// generateMay returns the error instead of fatalling — for tests that
// expect Generate to fail (parse-level rejection).
func generateMay(t *testing.T, src string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	in := filepath.Join(dir, "dto.go")
	if err := os.WriteFile(in, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := marshallgen.Generate(in, "", marshallgen.NoOpWrapper{})
	return string(out), err
}

// parseGo confirms the emitted source is valid Go syntactically. Type
// checking against the actual DML / RA interfaces is the responsibility
// of the consumer's go build; this assertion catches gofmt-clean-but-
// invalid output (missing imports, malformed type params, etc.).
func parseGo(t *testing.T, src string) {
	t.Helper()
	fset := token.NewFileSet()
	_, err := parser.ParseFile(fset, "out.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("emitted output failed go/parser: %v\n---\n%s\n---", err, src)
	}
}

func mustContain(t *testing.T, src, want string) {
	t.Helper()
	if !strings.Contains(src, want) {
		t.Fatalf("expected emitted output to contain %q, got:\n%s", want, src)
	}
}

func mustNotContain(t *testing.T, src, unwant string) {
	t.Helper()
	if strings.Contains(src, unwant) {
		t.Fatalf("expected emitted output NOT to contain %q, got:\n%s", unwant, src)
	}
}

// --- shape tests, one per fieldBeginShape branch. ---

func TestEmit_ScalarBegin_NoFlag(t *testing.T) {
	out := generate(t, `package demo
type MyDTO struct {
	_     struct{} `+"`kind:\"my\"`"+`
	Id    uint64   `+"`lw:\",id\"`"+`
	Ts    time.Time `+"`lw:\",ts\"`"+`
	Color string   `+"`lw:\"color,symbol\"`"+`
}
`)
	parseGo(t, out)
	mustContain(t, out, "type MyDTOSymbolAttrI interface {")
	mustContain(t, out, "type MyDTOSymbolSecI[Attr any, Ent any] interface {")
	mustContain(t, out, "BeginAttribute(value string) Attr")
	mustContain(t, out, "symbolSec.BeginAttribute(c.Color[i])")
	mustContain(t, out, ".AddMembershipLowCardRefP(kindColor)")
	mustContain(t, out, ".EndAttributeP()")
	mustContain(t, out, "const (")
	mustContain(t, out, "kindColor uint64 = 1")
	mustNotContain(t, out, "\tAddToContainerP(")
	mustNotContain(t, out, ".AddToContainerP(")
	mustNotContain(t, out, "BeginAttributeSingle")
}

// TestEmit_PlainOnly pins the 2026-06-14 review fix: a DTO with no tagged
// sections (only plain columns) must emit valid Go. Previously FillFromArrow
// got an empty type-parameter list `func F[](…)` — invalid Go, rejected by
// gofmt — and the section-only imports (iter / dmlruntime / raruntime / eb)
// were emitted unconditionally and went unused (go build error). FillFromArrow
// is now non-generic and those imports are omitted.
func TestEmit_PlainOnly(t *testing.T) {
	out := generate(t, `package demo
type MyDTO struct {
	_  struct{}  `+"`kind:\"my\"`"+`
	Id uint64    `+"`lw:\",id\"`"+`
	Ts time.Time `+"`lw:\",ts\"`"+`
}
`)
	parseGo(t, out)                                    // would fail pre-fix: "empty type parameter list"
	mustContain(t, out, "func MyDTOFillFromArrow(")    // non-generic
	mustNotContain(t, out, "func MyDTOFillFromArrow[") // no empty type-param list
	// BuildEntities keeps its Ent / DML params even with zero sections.
	mustContain(t, out, "func MyDTOBuildEntities[")
	// Section-only imports must be omitted (else unused-import build error).
	mustNotContain(t, out, "\"iter\"")
	mustNotContain(t, out, "observability/eh/eb")
	mustNotContain(t, out, "dml/runtime")
	mustNotContain(t, out, "readaccess/runtime")
	// Plain-path imports stay.
	mustContain(t, out, "arrow/array")
	mustContain(t, out, "observability/eh\"")
}

func TestEmit_ScalarBegin_Unit(t *testing.T) {
	out := generate(t, `package demo
type MyDTO struct {
	_       struct{} `+"`kind:\"my\"`"+`
	Id      uint64   `+"`lw:\",id\"`"+`
	Ts      time.Time `+"`lw:\",ts\"`"+`
	Battery uint64   `+"`lw:\"battery,u64Array,unit\"`"+`
}
`)
	parseGo(t, out)
	mustContain(t, out, "BeginAttributeSingle(value uint64) Attr")
	mustContain(t, out, "u64ArraySec.BeginAttributeSingle(c.Battery[i])")
	mustNotContain(t, out, "\tAddToContainerP(")
	mustNotContain(t, out, ".AddToContainerP(")
}

func TestEmit_OptionBegin_Unit(t *testing.T) {
	out := generate(t, `package demo
type MyDTO struct {
	_     struct{}            `+"`kind:\"my\"`"+`
	Id    uint64              `+"`lw:\",id\"`"+`
	Ts    time.Time           `+"`lw:\",ts\"`"+`
	Eta   option.Option[int64] `+"`lw:\"eta,i64Array,unit\"`"+`
}
`)
	parseGo(t, out)
	mustContain(t, out, "BeginAttributeSingle(value int64) Attr")
	mustContain(t, out, "if c.EtaHas[i] {")
	mustContain(t, out, "i64ArraySec.BeginAttributeSingle(c.EtaVal[i])")
}

func TestEmit_Container_Slice(t *testing.T) {
	out := generate(t, `package demo
type MyDTO struct {
	_     struct{}  `+"`kind:\"my\"`"+`
	Id    uint64    `+"`lw:\",id\"`"+`
	Ts    time.Time `+"`lw:\",ts\"`"+`
	Tags  []string  `+"`lw:\"tags,stringArray\"`"+`
}
`)
	parseGo(t, out)
	mustContain(t, out, "AddToContainerP(value string)")
	mustContain(t, out, "BeginAttribute() Attr")
	mustContain(t, out, "if len(c.Tags[i]) > 0 {")
	mustContain(t, out, "for _, v := range c.Tags[i] {")
	mustContain(t, out, ".AddToContainerP(v)")
	mustNotContain(t, out, "BeginAttributeSingle")
}

func TestEmit_Container_Roaring(t *testing.T) {
	out := generate(t, `package demo
type MyDTO struct {
	_     struct{}        `+"`kind:\"my\"`"+`
	Id    uint64          `+"`lw:\",id\"`"+`
	Ts    time.Time       `+"`lw:\",ts\"`"+`
	Bits  *roaring.Bitmap `+"`lw:\"bits,u32Array\"`"+`
}
`)
	parseGo(t, out)
	mustContain(t, out, "AddToContainerP(value uint32)")
	mustContain(t, out, "if c.Bits[i] != nil && !c.Bits[i].IsEmpty() {")
	mustContain(t, out, "it := c.Bits[i].Iterator()")
	mustContain(t, out, ".AddToContainerP(it.Next())")
}

func TestEmit_Explode_Slice(t *testing.T) {
	out := generate(t, `package demo
type MyDTO struct {
	_     struct{}  `+"`kind:\"my\"`"+`
	Id    uint64    `+"`lw:\",id\"`"+`
	Ts    time.Time `+"`lw:\",ts\"`"+`
	Ids   []string  `+"`lw:\"foreignId,symbol,explode\"`"+`
}
`)
	parseGo(t, out)
	mustContain(t, out, "BeginAttribute(value string) Attr")
	mustContain(t, out, "for _, v := range c.Ids[i] {")
	mustContain(t, out, "symbolSec.BeginAttribute(v)")
	mustNotContain(t, out, "\tAddToContainerP(")
	mustNotContain(t, out, ".AddToContainerP(")
}

func TestEmit_Explode_Slice_Unit(t *testing.T) {
	out := generate(t, `package demo
type MyDTO struct {
	_     struct{}  `+"`kind:\"my\"`"+`
	Id    uint64    `+"`lw:\",id\"`"+`
	Ts    time.Time `+"`lw:\",ts\"`"+`
	Ids   []string  `+"`lw:\"foreignId,stringArray,explode,unit\"`"+`
}
`)
	parseGo(t, out)
	mustContain(t, out, "BeginAttributeSingle(value string) Attr")
	mustContain(t, out, "stringArraySec.BeginAttributeSingle(v)")
}

// --- structural tests. ---

// TestEmit_ScalarFirstOrdering confirms ADR-0008 D2: within one
// section, scalar-shaped fields emit before container-shaped fields
// regardless of DTO declaration order. Bits (container) is declared
// first; Battery (scalar) is declared second; the BuildEntities body
// must reference Battery before Bits in the section's emit run.
func TestEmit_ScalarFirstOrdering(t *testing.T) {
	out := generate(t, `package demo
type MyDTO struct {
	_       struct{}        `+"`kind:\"my\"`"+`
	Id      uint64          `+"`lw:\",id\"`"+`
	Ts      time.Time       `+"`lw:\",ts\"`"+`
	Bits    *roaring.Bitmap `+"`lw:\"bits,u32Array\"`"+`
	Battery uint32          `+"`lw:\"battery,u32Array,unit\"`"+`
}
`)
	parseGo(t, out)
	scalarMarker := "c.Battery[i]"
	containerMarker := "c.Bits[i].Iterator()"
	sIdx := strings.Index(out, scalarMarker)
	cIdx := strings.Index(out, containerMarker)
	if sIdx < 0 || cIdx < 0 {
		t.Fatalf("expected both markers; scalar=%d container=%d\n---\n%s\n---", sIdx, cIdx, out)
	}
	if sIdx > cIdx {
		t.Fatalf("expected scalar emit (%q at %d) to precede container emit (%q at %d):\n%s",
			scalarMarker, sIdx, containerMarker, cIdx, out)
	}
}

func TestEmit_NoFBoundedSelfOnAttrI(t *testing.T) {
	// AttrI must use P-methods (void) — never carry an [Self] type
	// parameter. This is the load-bearing simplification from the
	// boxer AddToContainerP / EndAttributeP additions.
	out := generate(t, `package demo
type MyDTO struct {
	_     struct{}  `+"`kind:\"my\"`"+`
	Id    uint64    `+"`lw:\",id\"`"+`
	Ts    time.Time `+"`lw:\",ts\"`"+`
	Color string    `+"`lw:\"color,symbol\"`"+`
}
`)
	mustNotContain(t, out, "AttrI[Self any")
	mustNotContain(t, out, "AddToContainer(value") // chain method (no P)
	mustNotContain(t, out, "EndAttribute()")       // chain method (no P)
}

func TestEmit_AnchorStyleKindConsts(t *testing.T) {
	// NoOpWrapper emits kindXxx as package-local consts in declaration
	// order, no init(), no vdd lookups, no buscodec.
	out := generate(t, `package demo
type MyDTO struct {
	_     struct{}  `+"`kind:\"my\"`"+`
	Id    uint64    `+"`lw:\",id\"`"+`
	Ts    time.Time `+"`lw:\",ts\"`"+`
	A     string    `+"`lw:\"alpha,symbol\"`"+`
	B     string    `+"`lw:\"beta,symbol\"`"+`
}
`)
	mustContain(t, out, "const (")
	// Keyed on the membership name (schema-global), not the Go field name,
	// so kind vars stay unique across kinds generated into one package.
	mustContain(t, out, "kindAlpha uint64 = 1")
	mustContain(t, out, "kindBeta")
	mustContain(t, out, "uint64 = 2")
	mustNotContain(t, out, "func init() {")
	mustNotContain(t, out, "vdd.Memb")
	mustNotContain(t, out, "buscodec.Register")
}

func TestEmit_Verbatim(t *testing.T) {
	// ,verbatim flips the membership channel to LowCardVerbatim:
	//   - AttrI embeds InAttributeMembershipLowCardVerbatimPI
	//   - emit calls AddMembershipLowCardVerbatimP([]byte("name"))
	//   - read uses GetMembValueLowCardVerbatim → iter.Seq[[]byte]
	//   - case match switches on `string(membBytes)` literal
	// No kindXxx const is emitted for verbatim memberships.
	out := generate(t, `package demo
type MyDTO struct {
	_   struct{}  `+"`kind:\"my\"`"+`
	Id  uint64    `+"`lw:\",id\"`"+`
	Ts  time.Time `+"`lw:\",ts\"`"+`
	App string    `+"`lw:\"my-app,symbol,verbatim\"`"+`
}
`)
	parseGo(t, out)
	mustContain(t, out, "dmlruntime.InAttributeMembershipLowCardVerbatimPI")
	mustContain(t, out, `AddMembershipLowCardVerbatimP([]byte("my-app"))`)
	mustContain(t, out, "GetMembValueLowCardVerbatim")
	mustContain(t, out, "iter.Seq[[]byte]")
	mustContain(t, out, "switch string(membBytes)")
	mustContain(t, out, `case "my-app":`)
	mustNotContain(t, out, "kindApp")
	mustNotContain(t, out, ".AddMembershipLowCardRefP(") // doc-comment mentions both methods; assert no call site
}

// TestEmit_HighCardRef confirms ADR-0008 D3's HighCardRef channel flag.
// The emit shape mirrors LowCardRef but routes through the high-card
// AddMembership / GetMembValue methods.
func TestEmit_HighCardRef(t *testing.T) {
	out := generate(t, `package demo
type MyDTO struct {
	_   struct{}  `+"`kind:\"my\"`"+`
	Id  uint64    `+"`lw:\",id\"`"+`
	Ts  time.Time `+"`lw:\",ts\"`"+`
	App string    `+"`lw:\"myApp,symbol,highCardRef\"`"+`
}
`)
	parseGo(t, out)
	mustContain(t, out, "dmlruntime.InAttributeMembershipHighCardRefPI")
	mustContain(t, out, ".AddMembershipHighCardRefP(kindMyApp)")
	mustContain(t, out, "GetMembValueHighCardRef")
	mustContain(t, out, "iter.Seq[uint64]")
	mustContain(t, out, "kindMyApp uint64 = 1")
}

// TestEmit_HighCardVerbatim confirms ADR-0008 D3's HighCardVerbatim
// channel flag. Mirrors LowCardVerbatim but routes through the high-
// card accessors; no kindXxx is declared.
func TestEmit_HighCardVerbatim(t *testing.T) {
	out := generate(t, `package demo
type MyDTO struct {
	_   struct{}  `+"`kind:\"my\"`"+`
	Id  uint64    `+"`lw:\",id\"`"+`
	Ts  time.Time `+"`lw:\",ts\"`"+`
	App string    `+"`lw:\"my-app,symbol,highCardVerbatim\"`"+`
}
`)
	parseGo(t, out)
	mustContain(t, out, "dmlruntime.InAttributeMembershipHighCardVerbatimPI")
	mustContain(t, out, `AddMembershipHighCardVerbatimP([]byte("my-app"))`)
	mustContain(t, out, "GetMembValueHighCardVerbatim")
	mustContain(t, out, "iter.Seq[[]byte]")
	mustNotContain(t, out, "kindApp")
}

// TestParse_RejectsStagedChannels confirms the four complex channel
// flags (lowCardRefParametrized, highCardRefParametrized,
// mixedLowCardRef, mixedLowCardVerbatim) are recognised but rejected
// at parse time with a clear ADR-0008 pointer per the staged rollout.
func TestParse_RejectsCarrierChannelWithoutSibling(t *testing.T) {
	// All eight channels are implemented (ADR-0008 Cut-2 complete). A carrier
	// channel value field declared without its marshalltypes sibling is
	// rejected — there is no longer a "not yet implemented" staged set.
	for _, flag := range []string{"mixedLowCardRef", "mixedLowCardVerbatim", "lowCardRefParametrized", "highCardRefParametrized"} {
		src := `package demo
type MyDTO struct {
	_   struct{}  ` + "`kind:\"my\"`" + `
	Id  uint64    ` + "`lw:\",id\"`" + `
	Ts  time.Time ` + "`lw:\",ts\"`" + `
	X   string    ` + "`lw:\"x,symbol," + flag + "\"`" + `
}
`
		_, err := generateMay(t, src)
		if err == nil {
			t.Fatalf("flag %q: expected rejection (no carrier sibling), got success", flag)
		}
		if !strings.Contains(err.Error(), "needs a sibling carrier field") {
			t.Fatalf("flag %q: expected `needs a sibling carrier field` error, got: %v", flag, err)
		}
	}
}

func TestParse_RejectsMixedVerbatimRef(t *testing.T) {
	_, err := generateMay(t, `package demo
type MyDTO struct {
	_   struct{}  `+"`kind:\"my\"`"+`
	Id  uint64    `+"`lw:\",id\"`"+`
	Ts  time.Time `+"`lw:\",ts\"`"+`
	A   string    `+"`lw:\"alpha,symbol,verbatim\"`"+`
	B   string    `+"`lw:\"beta,symbol\"`"+`
}
`)
	if err == nil {
		t.Fatalf("expected error for mixed verbatim/ref in same section, got success")
	}
	if !strings.Contains(err.Error(), "mixes membership channels") {
		t.Fatalf("expected mixed-channel error, got: %v", err)
	}
}

func TestEmit_Const_Ref(t *testing.T) {
	// Const on `_` with ref channel: emits BeginAttribute("literal") +
	// AddMembershipLowCardRefP(kindXxx) on every row. No Go-side
	// storage; Append/Row/Columns don't carry the constant.
	out := generate(t, `package demo
type MyDTO struct {
	_   struct{}  `+"`kind:\"my\"`"+`
	_   struct{}  `+"`lw:\"appId,symbol,const=my-app\"`"+`
	Id  uint64    `+"`lw:\",id\"`"+`
	Ts  time.Time `+"`lw:\",ts\"`"+`
}
`)
	parseGo(t, out)
	mustContain(t, out, `symbolSec.BeginAttribute("my-app")`)
	mustContain(t, out, "AddMembershipLowCardRefP(kindAppId)")
	mustContain(t, out, "kindAppId uint64 = 1")
}

func TestEmit_Const_Verbatim(t *testing.T) {
	out := generate(t, `package demo
type MyDTO struct {
	_   struct{}  `+"`kind:\"my\"`"+`
	_   struct{}  `+"`lw:\"my-app,symbol,verbatim,const=my-app\"`"+`
	Id  uint64    `+"`lw:\",id\"`"+`
	Ts  time.Time `+"`lw:\",ts\"`"+`
}
`)
	parseGo(t, out)
	mustContain(t, out, `symbolSec.BeginAttribute("my-app")`)
	mustContain(t, out, `AddMembershipLowCardVerbatimP([]byte("my-app"))`)
	mustNotContain(t, out, "kindMy-app") // no kindXxx for verbatim
}

func TestEmit_Const_UnitFlag(t *testing.T) {
	// Const + unit + non-scalar section → BeginAttributeSingle.
	out := generate(t, `package demo
type MyDTO struct {
	_   struct{}  `+"`kind:\"my\"`"+`
	_   struct{}  `+"`lw:\"facet,symbolArray,unit,const=audit\"`"+`
	Id  uint64    `+"`lw:\",id\"`"+`
	Ts  time.Time `+"`lw:\",ts\"`"+`
}
`)
	parseGo(t, out)
	mustContain(t, out, `symbolArraySec.BeginAttributeSingle("audit")`)
}

func TestParse_RejectsConstOnNonUnderscore(t *testing.T) {
	_, err := generateMay(t, `package demo
type MyDTO struct {
	_   struct{}  `+"`kind:\"my\"`"+`
	Id  uint64    `+"`lw:\",id\"`"+`
	Ts  time.Time `+"`lw:\",ts\"`"+`
	Src string    `+"`lw:\"src,symbol,const=foo\"`"+`
}
`)
	if err == nil {
		t.Fatalf("expected error for const on non-`_` field")
	}
	if !strings.Contains(err.Error(), "only valid on `_` blank-identifier fields") {
		t.Fatalf("expected const-non-underscore error, got: %v", err)
	}
}

func TestParse_RejectsUnderscoreLWWithoutConst(t *testing.T) {
	_, err := generateMay(t, `package demo
type MyDTO struct {
	_   struct{}  `+"`kind:\"my\"`"+`
	_   struct{}  `+"`lw:\"appId,symbol\"`"+`
	Id  uint64    `+"`lw:\",id\"`"+`
	Ts  time.Time `+"`lw:\",ts\"`"+`
}
`)
	if err == nil {
		t.Fatalf("expected error for `_` with bare lw: (no const=)")
	}
	if !strings.Contains(err.Error(), "must declare `,const=") {
		t.Fatalf("expected `_`-without-const error, got: %v", err)
	}
}

func TestEmit_Option_Verbatim(t *testing.T) {
	// Option[T] + ,verbatim: scalar Has-guarded emit through the
	// LowCardVerbatim channel. AttrI embeds the verbatim PI; the per-
	// row driver emits the literal []byte at the call site; no kindXxx
	// const is declared for the membership.
	out := generate(t, `package demo
type MyDTO struct {
	_     struct{}              `+"`kind:\"my\"`"+`
	Id    uint64                `+"`lw:\",id\"`"+`
	Ts    time.Time             `+"`lw:\",ts\"`"+`
	Trace option.Option[string] `+"`lw:\"traceId,symbol,verbatim\"`"+`
}
`)
	parseGo(t, out)
	mustContain(t, out, "dmlruntime.InAttributeMembershipLowCardVerbatimPI")
	mustContain(t, out, "if c.TraceHas[i] {")
	mustContain(t, out, "symbolSec.BeginAttribute(c.TraceVal[i])")
	mustContain(t, out, `AddMembershipLowCardVerbatimP([]byte("traceId"))`)
	mustNotContain(t, out, "kindTrace") // verbatim → no var
	mustNotContain(t, out, ".AddMembershipLowCardRefP(")
}

func TestEmit_Roaring_Explode(t *testing.T) {
	// *roaring.Bitmap + ,explode: per-bit BeginAttribute(uint32) loop
	// (vs the default container path's single BeginAttribute() + N×
	// AddToContainerP). Empty/nil bitmap = zero iterations = zero attrs.
	out := generate(t, `package demo
type MyDTO struct {
	_    struct{}        `+"`kind:\"my\"`"+`
	Id   uint64          `+"`lw:\",id\"`"+`
	Ts   time.Time       `+"`lw:\",ts\"`"+`
	Bits *roaring.Bitmap `+"`lw:\"bits,u32,explode\"`"+`
}
`)
	parseGo(t, out)
	mustContain(t, out, "if c.Bits[i] != nil {")
	mustContain(t, out, "it := c.Bits[i].Iterator()")
	mustContain(t, out, "for it.HasNext() {")
	mustContain(t, out, "u32Sec.BeginAttribute(it.Next())")
	mustNotContain(t, out, ".AddToContainerP(")              // explode does NOT use container append
	mustContain(t, out, "BeginAttribute(value uint32) Attr") // scalar section signature
}

func TestEmit_MultiSubColumnSection(t *testing.T) {
	// u32Range with beginIncl + endExcl sub-columns sharing one
	// membership — SecI emits BeginAttribute(beginIncl T1, endExcl T2).
	out := generate(t, `package demo
type MyDTO struct {
	_       struct{}  `+"`kind:\"my\"`"+`
	Id      uint64    `+"`lw:\",id\"`"+`
	Ts      time.Time `+"`lw:\",ts\"`"+`
	RangeLo uint32    `+"`lw:\"validity,u32Range:beginIncl\"`"+`
	RangeHi uint32    `+"`lw:\"validity,u32Range:endExcl\"`"+`
}
`)
	parseGo(t, out)
	mustContain(t, out, "BeginAttribute(beginIncl uint32, endExcl uint32) Attr")
	mustContain(t, out, "u32RangeSec.BeginAttribute(c.RangeLo[i], c.RangeHi[i])")
}

func TestEmit_MixedLowCardRef(t *testing.T) {
	// Cut-2 mixedLowCardRef: a value field paired with a
	// marshalltypes.MixedLowCardRef carrier on one (membership, section,
	// channel) triple. The carrier gets its own SoA column and supplies the
	// per-row (id, params) to AddMembershipMixedLowCardRefP; the read side
	// reconstructs it from the Seq2 combined accessor.
	out := generate(t, `package demo
type MyDTO struct {
	_        struct{}                      `+"`kind:\"my\"`"+`
	Id       uint64                        `+"`lw:\",id\"`"+`
	Ts       time.Time                     `+"`lw:\",ts\"`"+`
	Reading  string                        `+"`lw:\"sensor,symbol,mixedLowCardRef\"`"+`
	ReadingC marshalltypes.MixedLowCardRef `+"`lw:\"sensor,symbol,mixedLowCardRef\"`"+`
}
`)
	parseGo(t, out)
	// Carrier gets its own SoA column; the package is imported.
	mustContain(t, out, "ReadingC []marshalltypes.MixedLowCardRef")
	mustContain(t, out, "leeway/marshall/marshalltypes")
	// AttrI composes the runtime mixed-ref constraint (no per-DTO method).
	mustContain(t, out, "dmlruntime.InAttributeMembershipMixedLowCardRefPI")
	// MembsReadI exposes the Seq2 combined accessor, not the plain Seq.
	mustContain(t, out, "GetMembValueLowCardRefHighCardParams(entityIdx raruntime.EntityIdx, attrIdx raruntime.AttributeIdx) iter.Seq2[uint64, []byte]")
	// Write driver: value + per-row carrier id/params, no kindXxx lookup.
	mustContain(t, out, "symbolSecAttr_Reading := symbolSec.BeginAttribute(c.Reading[i])")
	mustContain(t, out, "AddMembershipMixedLowCardRefP(c.ReadingC[i].Id, c.ReadingC[i].Params)")
	mustNotContain(t, out, "kindReading")
	// Read side: Seq2 accessor + carrier reconstruction.
	mustContain(t, out, "marshalltypes.MixedLowCardRef{Id: mv, Params:")
}

func TestEmit_MixedLowCardVerbatim(t *testing.T) {
	// Cut-2 mixedLowCardVerbatim: like mixedLowCardRef but the carrier's
	// membership value is a []byte Name (embedded verbatim) rather than a
	// uint64 Id — so the read copies it out of the Arrow buffer.
	out := generate(t, `package demo
type MyDTO struct {
	_        struct{}                           `+"`kind:\"my\"`"+`
	Id       uint64                             `+"`lw:\",id\"`"+`
	Ts       time.Time                          `+"`lw:\",ts\"`"+`
	Reading  string                             `+"`lw:\"sensor,symbol,mixedLowCardVerbatim\"`"+`
	ReadingC marshalltypes.MixedLowCardVerbatim `+"`lw:\"sensor,symbol,mixedLowCardVerbatim\"`"+`
}
`)
	parseGo(t, out)
	mustContain(t, out, "ReadingC []marshalltypes.MixedLowCardVerbatim")
	mustContain(t, out, "dmlruntime.InAttributeMembershipMixedLowCardVerbatimPI")
	mustContain(t, out, "GetMembValueLowCardVerbatimHighCardParams(entityIdx raruntime.EntityIdx, attrIdx raruntime.AttributeIdx) iter.Seq2[[]byte, []byte]")
	mustContain(t, out, "AddMembershipMixedLowCardVerbatimP(c.ReadingC[i].Name, c.ReadingC[i].Params)")
	// Name is []byte → defensively copied out of the Arrow buffer on read.
	mustContain(t, out, "marshalltypes.MixedLowCardVerbatim{Name: append([]byte(nil), mv...), Params:")
}

func TestEmit_Parametrized(t *testing.T) {
	// Cut-2 parametrized channels: the membership is an opaque params blob
	// (a marshalltypes.Parametrized carrier — no id/name). One write arg, a
	// single Seq[[]byte] read accessor.
	for _, c := range []struct{ flag, suffix string }{
		{"lowCardRefParametrized", "LowCardRefParametrized"},
		{"highCardRefParametrized", "HighCardRefParametrized"},
	} {
		out := generate(t, `package demo
type MyDTO struct {
	_        struct{}                   `+"`kind:\"my\"`"+`
	Id       uint64                     `+"`lw:\",id\"`"+`
	Ts       time.Time                  `+"`lw:\",ts\"`"+`
	Reading  string                     `+"`lw:\"sensor,symbol,"+c.flag+"\"`"+`
	ReadingC marshalltypes.Parametrized `+"`lw:\"sensor,symbol,"+c.flag+"\"`"+`
}
`)
		parseGo(t, out)
		mustContain(t, out, "ReadingC []marshalltypes.Parametrized")
		mustContain(t, out, "dmlruntime.InAttributeMembership"+c.suffix+"PI")
		// Single Seq read accessor (not the mixed channels' Seq2).
		mustContain(t, out, "GetMembValue"+c.suffix+"(entityIdx raruntime.EntityIdx, attrIdx raruntime.AttributeIdx) iter.Seq[[]byte]")
		// Write driver: one arg (the params blob only).
		mustContain(t, out, "AddMembership"+c.suffix+"P(c.ReadingC[i].Params)")
		// Read reconstruction: params only.
		mustContain(t, out, "marshalltypes.Parametrized{Params: append([]byte(nil), params...)}")
		mustNotContain(t, out, "iter.Seq2")
	}
}

// --- ADR-0008 OQ#4: carrier value shapes beyond scalar T. ---

func TestEmit_CarrierOption_ScalarCarrier(t *testing.T) {
	// An Option carrier value keeps a scalar carrier (one per attribute,
	// emitted only when Has); the carrier column is appended every row.
	out := generate(t, `package demo
type MyDTO struct {
	_        struct{}                      `+"`kind:\"my\"`"+`
	Id       uint64                        `+"`lw:\",id\"`"+`
	Reading  option.Option[uint32]         `+"`lw:\"sensor,symbol,mixedLowCardRef\"`"+`
	ReadingC marshalltypes.MixedLowCardRef `+"`lw:\"sensor,symbol,mixedLowCardRef\"`"+`
}
`)
	parseGo(t, out)
	// Scalar carrier column (gofmt aligns the Val/Has/C struct fields, so
	// match the type, not the name+spacing); not a slice carrier.
	mustContain(t, out, "[]marshalltypes.MixedLowCardRef")
	mustNotContain(t, out, "[][]marshalltypes.MixedLowCardRef")
	mustContain(t, out, "if c.ReadingHas[i] {")
	mustContain(t, out, "AddMembershipMixedLowCardRefP(c.ReadingC[i].Id, c.ReadingC[i].Params)")
	// Read: present → Has=true, and the carrier column is appended every row.
	mustContain(t, out, "c.ReadingHas = append(c.ReadingHas, true)")
	mustContain(t, out, "c.ReadingC = append(c.ReadingC,")
}

func TestEmit_CarrierContainer_ScalarCarrier(t *testing.T) {
	// A container ([]T) carrier value emits one attribute (N values) paired
	// with a single scalar carrier.
	out := generate(t, `package demo
type MyDTO struct {
	_         struct{}                      `+"`kind:\"my\"`"+`
	Id        uint64                        `+"`lw:\",id\"`"+`
	Readings  []uint32                      `+"`lw:\"sensor,u32Array,mixedLowCardRef\"`"+`
	ReadingsC marshalltypes.MixedLowCardRef `+"`lw:\"sensor,u32Array,mixedLowCardRef\"`"+`
}
`)
	parseGo(t, out)
	mustContain(t, out, "ReadingsC []marshalltypes.MixedLowCardRef")
	mustContain(t, out, "if len(c.Readings[i]) > 0 {")
	mustContain(t, out, "AddToContainerP")
	// One carrier for the whole container attribute (scalar index).
	mustContain(t, out, "AddMembershipMixedLowCardRefP(c.ReadingsC[i].Id, c.ReadingsC[i].Params)")
}

func TestEmit_CarrierExplode_SliceCarrier(t *testing.T) {
	// An exploded ([]T,explode) carrier value pairs a []marshalltypes.X slice
	// carrier, one element per emitted attribute, with a runtime length guard.
	out := generate(t, `package demo
type MyDTO struct {
	_         struct{}                        `+"`kind:\"my\"`"+`
	Id        uint64                          `+"`lw:\",id\"`"+`
	Readings  []uint32                        `+"`lw:\"sensor,u32Array,explode,mixedLowCardRef\"`"+`
	ReadingsC []marshalltypes.MixedLowCardRef `+"`lw:\"sensor,u32Array,mixedLowCardRef\"`"+`
}
`)
	parseGo(t, out)
	// Slice carrier column (extra slice dimension).
	mustContain(t, out, "ReadingsC [][]marshalltypes.MixedLowCardRef")
	// Runtime length guard before the per-element loop.
	mustContain(t, out, "if len(c.ReadingsC[i]) != len(c.Readings[i]) {")
	mustContain(t, out, "different lengths")
	// Per-element value loop + element-indexed carrier.
	mustContain(t, out, "for k, v := range c.Readings[i] {")
	mustContain(t, out, "AddMembershipMixedLowCardRefP(c.ReadingsC[i][k].Id, c.ReadingsC[i][k].Params)")
	// Read accumulates parallel value + carrier slices.
	mustContain(t, out, "c.ReadingsC = append(c.ReadingsC,")
}
