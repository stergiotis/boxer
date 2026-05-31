//go:build llm_generated_opus47

package marshallgen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/mappingplan"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshallgen"
)

// tryParse writes src to a temp file and calls marshallgen.ParsePlan.
// The synthetic source only needs to be syntactically valid Go for
// go/parser; it does not need to import-resolve.
func tryParse(t *testing.T, src string) (*mappingplan.Plan, error) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "dto.go")
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	return marshallgen.ParsePlan(path)
}

func assertErrContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("expected error containing %q, got: %v", want, err)
	}
}

// --- Happy-path: flag combinations on each shape. ---

func TestParse_ScalarT_NoFlags(t *testing.T) {
	plan, err := tryParse(t, `package foo
type MyDTO struct {
	_   struct{}  `+"`kind:\"my\"`"+`
	Id  uint64    `+"`lw:\",id\"`"+`
	Ts  time.Time `+"`lw:\",ts\"`"+`
	Src string    `+"`lw:\"src,symbol\"`"+`
}
`)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if len(plan.Fields) != 1 {
		t.Fatalf("expected 1 tagged field, got %d", len(plan.Fields))
	}
	if plan.Fields[0].Flags.Unit || plan.Fields[0].Flags.Explode {
		t.Fatalf("expected no flags, got %+v", plan.Fields[0].Flags)
	}
}

func TestParse_ScalarT_Unit(t *testing.T) {
	plan, err := tryParse(t, `package foo
type MyDTO struct {
	_   struct{}  `+"`kind:\"my\"`"+`
	Id  uint64    `+"`lw:\",id\"`"+`
	Ts  time.Time `+"`lw:\",ts\"`"+`
	Src string    `+"`lw:\"src,symbolArray,unit\"`"+`
}
`)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if !plan.Fields[0].Flags.Unit || plan.Fields[0].Flags.Explode {
		t.Fatalf("expected Unit-only, got %+v", plan.Fields[0].Flags)
	}
}

func TestParse_OptionT_Unit(t *testing.T) {
	plan, err := tryParse(t, `package foo
type MyDTO struct {
	_   struct{}              `+"`kind:\"my\"`"+`
	Id  uint64                `+"`lw:\",id\"`"+`
	Ts  time.Time             `+"`lw:\",ts\"`"+`
	Src option.Option[string]  `+"`lw:\"src,symbolArray,unit\"`"+`
}
`)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if !plan.Fields[0].IsOption || !plan.Fields[0].Flags.Unit {
		t.Fatalf("expected Option + Unit, got %+v", plan.Fields[0])
	}
}

func TestParse_SliceT_Default(t *testing.T) {
	plan, err := tryParse(t, `package foo
type MyDTO struct {
	_     struct{}  `+"`kind:\"my\"`"+`
	Id    uint64    `+"`lw:\",id\"`"+`
	Ts    time.Time `+"`lw:\",ts\"`"+`
	Scope []string  `+"`lw:\"scope,stringArray\"`"+`
}
`)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if !plan.Fields[0].IsSlice || plan.Fields[0].Flags.Explode {
		t.Fatalf("expected slice default (no flags), got %+v", plan.Fields[0])
	}
}

func TestParse_SliceT_Explode(t *testing.T) {
	plan, err := tryParse(t, `package foo
type MyDTO struct {
	_    struct{}   `+"`kind:\"my\"`"+`
	Id   uint64     `+"`lw:\",id\"`"+`
	Ts   time.Time  `+"`lw:\",ts\"`"+`
	Tags []string   `+"`lw:\"tag,symbol,explode\"`"+`
}
`)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if !plan.Fields[0].Flags.Explode || plan.Fields[0].Flags.Unit {
		t.Fatalf("expected Explode-only, got %+v", plan.Fields[0].Flags)
	}
}

func TestParse_SliceT_ExplodeUnit(t *testing.T) {
	plan, err := tryParse(t, `package foo
type MyDTO struct {
	_    struct{}   `+"`kind:\"my\"`"+`
	Id   uint64     `+"`lw:\",id\"`"+`
	Ts   time.Time  `+"`lw:\",ts\"`"+`
	Tags []string   `+"`lw:\"tag,symbolArray,explode,unit\"`"+`
}
`)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if !plan.Fields[0].Flags.Explode || !plan.Fields[0].Flags.Unit {
		t.Fatalf("expected Explode+Unit, got %+v", plan.Fields[0].Flags)
	}
}

func TestParse_Roaring_Default(t *testing.T) {
	plan, err := tryParse(t, `package foo
type MyDTO struct {
	_    struct{}        `+"`kind:\"my\"`"+`
	Id   uint64          `+"`lw:\",id\"`"+`
	Ts   time.Time       `+"`lw:\",ts\"`"+`
	Bits *roaring.Bitmap `+"`lw:\"bits,u32Array\"`"+`
}
`)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if !plan.Fields[0].IsRoaring || plan.Fields[0].Flags.Explode {
		t.Fatalf("expected roaring default, got %+v", plan.Fields[0])
	}
}

func TestParse_BlobScalar_OptionByteSlice(t *testing.T) {
	plan, err := tryParse(t, `package foo
type MyDTO struct {
	_     struct{}              `+"`kind:\"my\"`"+`
	Id    uint64                `+"`lw:\",id\"`"+`
	Ts    time.Time             `+"`lw:\",ts\"`"+`
	Data  option.Option[[]byte]  `+"`lw:\"data,blobArray,unit\"`"+`
}
`)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if plan.Fields[0].GoType != "[]byte" || !plan.Fields[0].IsOption {
		t.Fatalf("expected Option[[]byte] scalar blob, got %+v", plan.Fields[0])
	}
}

// --- Flag-rejection rules. ---

func TestParse_RejectsExplodeOnScalar(t *testing.T) {
	_, err := tryParse(t, `package foo
type MyDTO struct {
	_   struct{}  `+"`kind:\"my\"`"+`
	Id  uint64    `+"`lw:\",id\"`"+`
	Ts  time.Time `+"`lw:\",ts\"`"+`
	Src string    `+"`lw:\"src,symbol,explode\"`"+`
}
`)
	assertErrContains(t, err, "`explode` requires a multi-element shape")
}

func TestParse_RejectsExplodeOnOption(t *testing.T) {
	_, err := tryParse(t, `package foo
type MyDTO struct {
	_   struct{}              `+"`kind:\"my\"`"+`
	Id  uint64                `+"`lw:\",id\"`"+`
	Ts  time.Time             `+"`lw:\",ts\"`"+`
	Src option.Option[string]  `+"`lw:\"src,symbol,explode\"`"+`
}
`)
	assertErrContains(t, err, "`explode` requires a multi-element shape")
}

func TestParse_RejectsUnitOnSliceWithoutExplode(t *testing.T) {
	_, err := tryParse(t, `package foo
type MyDTO struct {
	_    struct{}  `+"`kind:\"my\"`"+`
	Id   uint64    `+"`lw:\",id\"`"+`
	Ts   time.Time `+"`lw:\",ts\"`"+`
	Tags []string  `+"`lw:\"tag,stringArray,unit\"`"+`
}
`)
	assertErrContains(t, err, "`unit` on a multi-element shape requires `explode`")
}

func TestParse_RejectsUnknownFlag(t *testing.T) {
	_, err := tryParse(t, `package foo
type MyDTO struct {
	_   struct{}  `+"`kind:\"my\"`"+`
	Id  uint64    `+"`lw:\",id\"`"+`
	Ts  time.Time `+"`lw:\",ts\"`"+`
	Src string    `+"`lw:\"src,symbol,bogus\"`"+`
}
`)
	assertErrContains(t, err, "unknown flag token")
}

func TestParse_RejectsDuplicateFlag(t *testing.T) {
	_, err := tryParse(t, `package foo
type MyDTO struct {
	_   struct{}  `+"`kind:\"my\"`"+`
	Id  uint64    `+"`lw:\",id\"`"+`
	Ts  time.Time `+"`lw:\",ts\"`"+`
	Src string    `+"`lw:\"src,symbolArray,unit,unit\"`"+`
}
`)
	assertErrContains(t, err, "flag declared twice")
}

func TestParse_RejectsFlagsOnPlain(t *testing.T) {
	_, err := tryParse(t, `package foo
type MyDTO struct {
	_   struct{}  `+"`kind:\"my\"`"+`
	Id  uint64    `+"`lw:\",id,unit\"`"+`
	Ts  time.Time `+"`lw:\",ts\"`"+`
}
`)
	assertErrContains(t, err, "plain field cannot carry channel / `unit` / `explode`")
}

// --- Shape rejection (carried over from current codegen/parse_test.go). ---

func TestParse_RejectsDuplicateMembershipPerDTO(t *testing.T) {
	_, err := tryParse(t, `package foo
type MyDTO struct {
	_  struct{}  `+"`kind:\"my\"`"+`
	Id uint64    `+"`lw:\",id\"`"+`
	Ts time.Time `+"`lw:\",ts\"`"+`
	A  string    `+"`lw:\"src,symbol\"`"+`
	B  string    `+"`lw:\"src,symbol\"`"+`
}
`)
	assertErrContains(t, err, "appears on two DTO fields")
}

func TestParse_AllowsSharedMembershipDistinctSubColumns(t *testing.T) {
	plan, err := tryParse(t, `package foo
type MyDTO struct {
	_       struct{}  `+"`kind:\"my\"`"+`
	Id      uint64    `+"`lw:\",id\"`"+`
	Ts      time.Time `+"`lw:\",ts\"`"+`
	RangeLo uint32    `+"`lw:\"validity,u32Range:beginIncl\"`"+`
	RangeHi uint32    `+"`lw:\"validity,u32Range:endExcl\"`"+`
}
`)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if len(plan.Fields) != 2 {
		t.Fatalf("expected 2 fields sharing validity membership, got %d", len(plan.Fields))
	}
}

func TestParse_RejectsOptionOfSlice(t *testing.T) {
	_, err := tryParse(t, `package foo
type MyDTO struct {
	_   struct{}                `+"`kind:\"my\"`"+`
	Id  uint64                  `+"`lw:\",id\"`"+`
	Ts  time.Time               `+"`lw:\",ts\"`"+`
	X   option.Option[[]string]  `+"`lw:\"src,stringArray\"`"+`
}
`)
	assertErrContains(t, err, "Option[[]T] is forbidden")
}

func TestParse_RejectsSliceOfOption(t *testing.T) {
	_, err := tryParse(t, `package foo
type MyDTO struct {
	_  struct{}                `+"`kind:\"my\"`"+`
	Id uint64                  `+"`lw:\",id\"`"+`
	Ts time.Time               `+"`lw:\",ts\"`"+`
	X  []option.Option[string]  `+"`lw:\"tag,stringArray\"`"+`
}
`)
	assertErrContains(t, err, "[]Option[T] is forbidden")
}

func TestParse_RejectsPointer_NonRoaring(t *testing.T) {
	_, err := tryParse(t, `package foo
type MyDTO struct {
	_  struct{}  `+"`kind:\"my\"`"+`
	Id uint64    `+"`lw:\",id\"`"+`
	Ts time.Time `+"`lw:\",ts\"`"+`
	X  *int64    `+"`lw:\"src,symbol\"`"+`
}
`)
	assertErrContains(t, err, "pointer types forbidden except *roaring.Bitmap")
}

func TestParse_RejectsMultiNameField(t *testing.T) {
	_, err := tryParse(t, `package foo
type MyDTO struct {
	_    struct{}  `+"`kind:\"my\"`"+`
	Id   uint64    `+"`lw:\",id\"`"+`
	Ts   time.Time `+"`lw:\",ts\"`"+`
	A, B uint64    `+"`lw:\"src,symbol\"`"+`
}
`)
	assertErrContains(t, err, "multi-name or anonymous struct field")
}

func TestParse_RejectsMissingKind(t *testing.T) {
	_, err := tryParse(t, `package foo
type MyDTO struct {
	Id uint64    `+"`lw:\",id\"`"+`
	Ts time.Time `+"`lw:\",ts\"`"+`
}
`)
	// Without `_` the kind name is never collected; parser surfaces a
	// missing-`_` diagnostic.
	if err == nil {
		t.Fatalf("expected error for DTO without `_` field")
	}
}

func TestParse_RejectsPlainUnknownColumn(t *testing.T) {
	// Empty membership + non-fixed section name → not a valid plain
	// column. Plain sections are restricted to id / ts / naturalKey /
	// expiresAt.
	_, err := tryParse(t, `package foo
type MyDTO struct {
	_  struct{}  `+"`kind:\"my\"`"+`
	Id uint64    `+"`lw:\",id\"`"+`
	Ts time.Time `+"`lw:\",ts\"`"+`
	X  string    `+"`lw:\",symbol\"`"+`
}
`)
	assertErrContains(t, err, "unknown plain column")
}

func TestParse_RejectsPlainUnsupportedType(t *testing.T) {
	// Strict 1:1: a plain column's Go type IS its entity-setter argument
	// type, but it must still be a type the codec can project to/from an
	// Arrow array (see mappingplan.PlainArrowArrayType). complex128 is not.
	// (A string id, by contrast, is now accepted — that is the point of 1:1.)
	_, err := tryParse(t, `package foo
type MyDTO struct {
	_  struct{}   `+"`kind:\"my\"`"+`
	Id complex128 `+"`lw:\",id\"`"+`
	Ts time.Time  `+"`lw:\",ts\"`"+`
}
`)
	assertErrContains(t, err, "unsupported plain column Go type")
}

func TestParse_RejectsPlainOption(t *testing.T) {
	// Plain (entity-header) columns are mandatory under strict 1:1;
	// Option[T] (and slices / roaring) are forbidden on plain fields.
	_, err := tryParse(t, `package foo
type MyDTO struct {
	_  struct{}              `+"`kind:\"my\"`"+`
	Id option.Option[uint64] `+"`lw:\",id\"`"+`
	Ts time.Time             `+"`lw:\",ts\"`"+`
}
`)
	assertErrContains(t, err, "plain field must be a scalar T")
}

func TestParse_RejectsMissingId(t *testing.T) {
	_, err := tryParse(t, `package foo
type MyDTO struct {
	_  struct{}  `+"`kind:\"my\"`"+`
	Ts time.Time `+"`lw:\",ts\"`"+`
}
`)
	assertErrContains(t, err, "missing required plain column `id`")
}

func TestParse_RejectsUnderscoreLegacyPlainMap(t *testing.T) {
	_, err := tryParse(t, `package foo
type MyDTO struct {
	_  struct{}  `+"`kind:\"my\" plain:\"id=Id\"`"+`
	Id uint64    `+"`lw:\",id\"`"+`
	Ts time.Time `+"`lw:\",ts\"`"+`
}
`)
	assertErrContains(t, err, "`plain:` map is retired")
}
