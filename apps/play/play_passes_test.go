package play

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/data/passreg"
	passregdefaults "github.com/stergiotis/boxer/public/keelson/data/passreg/defaults"
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
	want := []string{"CanonicalizeFull", "ExpandDescriptiveStatistics", "DocsearchExpand", "ExpandLwIdMacros", "LwComponentExpand", "LwExtractExpand", "LwConstructExpandTarget", "LwConstructExpand", "GlossExpand", "ResolveColumnNames"}
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
