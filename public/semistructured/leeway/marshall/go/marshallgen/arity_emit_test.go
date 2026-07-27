package marshallgen_test

// ADR-0146 M2b: the emitted decode gates every slot on the read contract's
// arity. These tests pin the emitted shape per field class, because the
// container and const gates are new and nothing else in the emit suite would
// notice if they stopped being written.

import (
	"strings"
	"testing"
)

func TestEmitArity_ContainerGetsSurplusGate(t *testing.T) {
	out := generate(t, `package demo
type MyDTO struct {
	_    struct{} `+"`kind:\"my\"`"+`
	Id   uint64   `+"`lw:\",id\"`"+`
	Tags []string `+"`lw:\"tags,symbolArray\"`"+`
}
`)
	parseGo(t, out)
	// The counter and its per-attribute dedup marker.
	mustContain(t, out, "var symbolArrayTagsCount int")
	mustContain(t, out, "var symbolArrayTagsLastAttr int64")
	mustContain(t, out, "if symbolArrayTagsLastAttr != attrJ+1 {")
	// A container is [0..1]: surplus is refused, absence is not.
	mustContain(t, out, "if symbolArrayTagsCount > 1 {")
	mustNotContain(t, out, "if symbolArrayTagsCount != 1 {")
	// The message names the slot, not just the Go field.
	mustContain(t, out, `Str("section", "symbolArray")`)
	mustContain(t, out, `Str("membership", "tags")`)
}

func TestEmitArity_MandatoryScalarRequiresExactlyOne(t *testing.T) {
	out := generate(t, `package demo
type MyDTO struct {
	_      struct{} `+"`kind:\"my\"`"+`
	Id     uint64   `+"`lw:\",id\"`"+`
	Status string   `+"`lw:\"status,symbol\"`"+`
}
`)
	parseGo(t, out)
	mustContain(t, out, "if symbolStatusCount != 1 {")
	mustContain(t, out, `Str("section", "symbol")`)
	mustContain(t, out, `Str("membership", "status")`)
}

func TestEmitArity_OptionAllowsAbsenceRefusesSurplus(t *testing.T) {
	out := generate(t, `package demo
type MyDTO struct {
	_      struct{}              `+"`kind:\"my\"`"+`
	Id     uint64                `+"`lw:\",id\"`"+`
	Status option.Option[string] `+"`lw:\"status,symbol\"`"+`
}
`)
	parseGo(t, out)
	mustContain(t, out, "if symbolStatusCount > 1 {")
	mustNotContain(t, out, "if symbolStatusCount != 1 {")
}

// A const projects no Go field, so its slot has no append tail to carry the
// gate; it is checked right after the match loops. Its counter is named by
// position because a verbatim membership need not be a Go identifier.
func TestEmitArity_ConstSlotIsCounted(t *testing.T) {
	out := generate(t, `package demo
type MyDTO struct {
	_   struct{} `+"`kind:\"my\"`"+`
	_   struct{} `+"`lw:\"my-app,symbol,verbatim,const=my-app\"`"+`
	Id  uint64   `+"`lw:\",id\"`"+`
}
`)
	parseGo(t, out)
	mustContain(t, out, "Count int")
	mustContain(t, out, "LastAttr int64")
	mustContain(t, out, `Str("membership", "my-app")`)
	// Positional naming — the membership is not a Go identifier.
	mustNotContain(t, out, "symbolConstMy-app")
}

// ReadRow reads an OPTIONAL component, so a missing slot is absence (reported
// through `present`), never an error — only surplus is refused, for every
// shape including containers.
func TestEmitArity_ReadRowRefusesSurplusOnly(t *testing.T) {
	out := generate(t, `package demo
type MyDTO struct {
	_      struct{} `+"`kind:\"my\"`"+`
	Id     uint64   `+"`lw:\",id\"`"+`
	Status string   `+"`lw:\"status,symbol\"`"+`
	Tags   []string `+"`lw:\"tags,symbolArray\"`"+`
}
`)
	parseGo(t, out)
	readRow := out[indexOfReadRow(t, out):]
	mustContain(t, readRow, "if symbolStatusCount > 1 {")
	mustContain(t, readRow, "if symbolArrayTagsCount > 1 {")
	// The batch-strict "exactly 1" belongs to FillFromArrow, not ReadRow.
	mustNotContain(t, readRow, "if symbolStatusCount != 1 {")
}

// indexOfReadRow returns the offset of the emitted <Kind>ReadRow helper so a
// test can assert against that half of the file alone.
func indexOfReadRow(t *testing.T, out string) int {
	t.Helper()
	const marker = "ReadRow reads row i as one optional"
	i := strings.Index(out, marker)
	if i < 0 {
		t.Fatalf("emitted source has no ReadRow helper:\n%s", out)
	}
	return i
}
