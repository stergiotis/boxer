//go:build llm_generated_opus47

package marshallgen_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/marshallgen"
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
	mustNotContain(t, out, "AddToContainer(value")  // chain method (no P)
	mustNotContain(t, out, "EndAttribute()")        // chain method (no P)
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
	mustContain(t, out, "kindA uint64 = 1")
	mustContain(t, out, "kindB uint64 = 2")
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
	if !strings.Contains(err.Error(), "mixes `,verbatim`") {
		t.Fatalf("expected mixed-verbatim error, got: %v", err)
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
	mustNotContain(t, out, ".AddToContainerP(")             // explode does NOT use container append
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
