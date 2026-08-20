package play

import (
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stergiotis/boxer/public/keelson/data/passreg"
	passregdefaults "github.com/stergiotis/boxer/public/keelson/data/passreg/defaults"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect/providers"
	"github.com/stergiotis/boxer/public/keelson/runtime/sysmfacts"
	"github.com/stergiotis/boxer/public/semistructured/leeway/constructsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/componentsql"
)

// TestRegisterPassesOrdering pins the host set's apply order:
// canonicalisation first (50), then the standard entries (macro expansion
// 100, late-bound column resolution 200) consuming canonical shapes.
// TestExecuteArrowStreamCanonicalizesViaHostSet proves the behavioural
// consequence through the client.
func TestRegisterPassesOrdering(t *testing.T) {
	reg := passreg.NewRegistry()
	if err := passregdefaults.RegisterStandard(reg); err != nil {
		t.Fatalf("RegisterStandard: %v", err)
	}
	if err := RegisterPasses(reg); err != nil {
		t.Fatalf("RegisterPasses: %v", err)
	}

	rows := reg.Catalog()
	got := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.Stage == passreg.StagePreExecute {
			got = append(got, r.Name)
		}
	}
	// LwComponentExpand (110) sits between the identity macros (100) and the
	// extraction family (120), so one statement may mix a component read with
	// LW_GET and with an LW_ID_* macro around either (ADR-0189 §SD3).
	// LadingExpand (145) follows the display macros and precedes handle
	// resolution (200), so an fs(…) expansion's physical column names are
	// already what they will be when the resolver runs (ADR-0198 §SD7).
	want := []string{"CanonicalizeFull", "ExpandDescriptiveStatistics", "DocsearchExpand", "ExpandLwIdMacros", "LwComponentExpand", "LwExtractExpand", "LwConstructExpandTarget", "LwConstructExpand", "GlossExpand", "LadingExpand", "ResolveColumnNames"}
	if len(got) != len(want) {
		t.Fatalf("pre-execute catalog = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("pre-execute catalog order = %v, want %v", got, want)
		}
	}

	for _, r := range rows {
		if r.Name != "CanonicalizeFull" {
			continue
		}
		if r.LateBound {
			t.Error("CanonicalizeFull must be a concrete entry, not a factory")
		}
		if !r.Properties.Idempotent {
			t.Error("CanonicalizeFull catalog row must carry the Idempotent property")
		}
	}
}

// TestRegisterPassesExposesSubPasses pins the surface the Passes tab's detail
// panel reads (entryPassForRow → Pass.Children): the registered composite
// names its members in apply order.
func TestRegisterPassesExposesSubPasses(t *testing.T) {
	reg := passreg.NewRegistry()
	if err := RegisterPasses(reg); err != nil {
		t.Fatalf("RegisterPasses: %v", err)
	}
	p, ok := entryPassForRow(reg, passreg.CatalogRow{Stage: passreg.StagePreExecute, Name: "CanonicalizeFull"})
	if !ok {
		t.Fatal("CanonicalizeFull entry not resolvable")
	}
	want := []string{
		"CanonicalizeInsertWrapper",
		"CanonicalizeWhitespaceSingleLine",
		"CanonicalizeEquals",
		"CanonicalizeSugar",
		"FixedPoint(CanonicalizeConstructors)",
		"FixedPoint(CanonicalizeCaseConditionals)",
		"CanonicalizeMultiIf",
		"FixedPoint(CanonicalizeCasts)",
		"CanonicalizeJoin",
		"FixedPoint(CanonicalizeTernary)",
		"CanonicalizeKeywordCase",
		"CanonicalizeIdentifiers",
	}
	if len(p.Children) != len(want) {
		t.Fatalf("children = %d, want %d", len(p.Children), len(want))
	}
	for i := range want {
		if p.Children[i].Name != want[i] {
			t.Fatalf("child %d = %q, want %q", i, p.Children[i].Name, want[i])
		}
	}
}

// RegisterComponents wires the stores play can read through LW_COMPONENT
// (ADR-0189 §SD7/M3). This is the seam where the generated Set, the registry
// and the pass meet: the three are written in different packages, and the
// only way to see that they agree is to expand a real kind.
func TestRegisterComponentsResolvesARealKind(t *testing.T) {
	reg := componentsql.NewRegistry()
	if err := RegisterComponents(reg); err != nil {
		t.Fatalf("RegisterComponents: %v", err)
	}

	out, err := constructsql.ComponentExpandPass(reg, "").
		Run("SELECT LW_COMPONENT('SysMem') AS m FROM boxer.facts")
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if strings.Contains(out, "LW_COMPONENT") {
		t.Fatalf("a component call survived expansion: %s", out)
	}
	// The projection and the filter its exactness depends on, from the real
	// generated artefacts rather than a stand-in.
	if !strings.Contains(out, "CAST(tuple(") {
		t.Fatalf("no named-tuple projection in: %s", out)
	}
	if !strings.Contains(out, " WHERE ") || !strings.Contains(out, "countEqual(") {
		t.Fatalf("the conformance filter was not injected: %s", out)
	}
}

// keelson('lw_components') answers with what play registered (ADR-0189 §SD8).
// The provider and the registration are written in different packages and
// neither imports the other, so this is where they can be seen to agree —
// and it is the query a person actually runs to discover what LW_COMPONENT
// will accept here.
func TestKeelsonLwComponentsPublishesWhatPlayRegistered(t *testing.T) {
	componentsql.Default.Reset()
	t.Cleanup(componentsql.Default.Reset)
	if err := RegisterComponents(componentsql.Default); err != nil {
		t.Fatalf("RegisterComponents: %v", err)
	}

	reg := introspect.NewRegistry()
	if err := providers.RegisterStatic(reg); err != nil {
		t.Fatalf("RegisterStatic: %v", err)
	}
	p, ok := reg.Lookup("lw_components")
	if !ok {
		t.Fatal("keelson('lw_components') is not registered")
	}

	batch, err := p.Snapshot(introspect.AllColumns())
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	defer batch.Release()

	// Thirteen kinds, the same count the store publishes — the table must not
	// quietly show a subset of what a query can actually reach.
	if got := int(batch.NumRows()); got != len(sysmfacts.SysmetricsComponentSQL.Kinds) {
		t.Fatalf("lw_components has %d rows, the store publishes %d kinds",
			got, len(sysmfacts.SysmetricsComponentSQL.Kinds))
	}

	kinds := batch.Column(0).(*array.String)
	tables := batch.Column(2).(*array.String)
	for i := range int(batch.NumRows()) {
		if _, known := sysmfacts.SysmetricsComponentSQL.Kinds[kinds.Value(i)]; !known {
			t.Fatalf("lw_components names a kind the store does not publish: %s", kinds.Value(i))
		}
		if tables.Value(i) != sysmfacts.SysmetricsTableName {
			t.Fatalf("%s: table = %s, want %s", kinds.Value(i), tables.Value(i), sysmfacts.SysmetricsTableName)
		}
	}
}

// TestNamedTupleAccessReachesTheComponentExpansion is ADR-0190 §SD11 end to
// end through play's own pass order.
//
// The dot form the grammar now takes has to survive canonicalisation into
// `tupleElement(LW_COMPONENT('SysMem'), 'TotalBytes')` and then the component
// expansion, which runs later (CanonicalizeFull at 50, LwComponentExpand at
// 110). The order matters in one direction only: if the component call were
// expanded first, the dot would sit on a CAST and canonicalise the same way —
// but the projection it names would already have been spliced, so the pass
// order is what keeps the readable spelling readable all the way to the wire.
func TestNamedTupleAccessReachesTheComponentExpansion(t *testing.T) {
	canon, err := passes.CanonicalizeConstructors(passes.ConstructorFormFunction).
		Run("SELECT LW_COMPONENT('SysMem').TotalBytes FROM boxer.facts")
	if err != nil {
		t.Fatalf("canonicalize: %v", err)
	}
	if !strings.Contains(canon, "tupleElement(LW_COMPONENT('SysMem'), 'TotalBytes')") {
		t.Fatalf("the dot form did not canonicalise: %s", canon)
	}

	reg := componentsql.NewRegistry()
	if err = RegisterComponents(reg); err != nil {
		t.Fatalf("RegisterComponents: %v", err)
	}
	out, err := constructsql.ComponentExpandPass(reg, "").Run(canon)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if strings.Contains(out, "LW_COMPONENT") {
		t.Fatalf("a component call survived expansion: %s", out)
	}
	if !strings.Contains(out, "tupleElement(CAST(tuple(") {
		t.Fatalf("the field read is not over the projection: %s", out)
	}
	if !strings.Contains(out, " WHERE ") || !strings.Contains(out, "countEqual(") {
		t.Fatalf("the conformance filter was not injected: %s", out)
	}
}
