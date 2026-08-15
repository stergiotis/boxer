package anchor

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"encoding/json/jsontext"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	card2 "github.com/stergiotis/boxer/public/semistructured/leeway/card"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/membership"
	"github.com/stergiotis/boxer/public/semistructured/leeway/membershiprole"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
)

// anchorRefFormatter renders the demo domain's membership ref ids as names —
// a demo-local stand-in for the deferred registry-backed formatter that is
// the membership.Renderer seam's intended first-class injector (ADR-0072:
// producers emit identities, consumers render them). The write side already
// resolves names to ids; this is the read-side inverse, keyed on the id
// ranges the anchor data generators use.
type anchorRefFormatter struct{}

func (anchorRefFormatter) FormatRef(ref uint64) string {
	switch {
	case ref == 5:
		return "model:AeroQuad"
	case ref >= 999000 && ref < 1000000:
		return fmt.Sprintf("customer:%d", ref-999000)
	case ref <= 65535:
		return fmt.Sprintf("port:%d", ref)
	default:
		return fmt.Sprintf("0x%x", ref)
	}
}

// buildMembershipDemoBatch commits two small entities through the generated
// DML builder: a drone mission whose symbol attribute carries a low-card ref
// (the drone model) and whose geoPoint carries a high-card ref (the
// customer), and a cyber incident whose symbol attributes carry port refs.
func buildMembershipDemoBatch(t *testing.T) []arrow.RecordBatch {
	t.Helper()
	table := NewInEntityTestTable(memory.NewGoAllocator(), 2)

	table.BeginEntity().SetId(1, []byte("TRK-DEMO-1"))
	table.GetSectionSymbol().
		BeginAttribute("IN_TRANSIT").
		AddMembershipLowCardRef(5).
		EndAttribute().
		EndSection()
	table.GetSectionGeoPoint().
		BeginAttribute(47.3769, 8.5417, 61029384).
		AddMembershipHighCardRef(999042).
		EndAttribute().
		EndSection()
	require.NoError(t, table.CommitEntity())

	table.BeginEntity().SetId(2, []byte("INC-DEMO-2"))
	sec := table.GetSectionSymbol()
	sec.BeginAttribute("PORT_SCAN").AddMembershipLowCardRef(22).EndAttribute()
	sec.BeginAttribute("SQL_INJECTION").AddMembershipLowCardRef(443).EndAttribute()
	sec.EndSection()
	table.GetSectionTimeRange().
		BeginAttribute(time.Unix(1773269000, 0).UTC(), time.Unix(1773269600, 0).UTC()).
		EndAttribute().
		EndSection()
	require.NoError(t, table.CommitEntity())

	recs, err := table.TransferRecords(nil)
	require.NoError(t, err)
	require.NotEmpty(t, recs)
	return recs
}

// TestMembershipRendererGeneration emits the same two-entity batch through
// the JSON card emitter twice — once with the default renderer (hex refs)
// and once with the anchor domain formatter injected via WithRenderer — and
// asserts the swap end to end: the default output keys memberships by hex id,
// the injected output by domain name, on identical wire bytes. Naming an id
// is a read-side, per-consumer decision (ADR-0072).
// card_anchor_membership_renderer.out.md records the rendering table; the
// full card JSON is asserted, not dumped.
func TestMembershipRendererGeneration(t *testing.T) {
	tblDesc, err := GetAnchorTableDesc()
	require.NoError(t, err)
	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	ir := common.NewIntermediateTableRepresentation()
	require.NoError(t, ir.LoadFromTable(&tblDesc, tech))

	driver, err := streamreadaccess.NewDriver(&tblDesc, ir, streamreadaccess.DefaultFormatters())
	require.NoError(t, err)

	recs := buildMembershipDemoBatch(t)
	defer func() {
		for _, r := range recs {
			r.Release()
		}
	}()

	emit := func(opts ...card2.JsonCardEmitterOption) string {
		b := bytes.NewBuffer(nil)
		enc := jsontext.NewEncoder(b, jsontext.Multiline(true), jsontext.WithIndent("  "))
		sink := card2.NewJsonCardEmitter(enc, ir, opts...)
		require.NoError(t, driver.DriveRecordBatch(sink, recs[0]))
		return b.String()
	}

	renderer := membership.NewRenderer(anchorRefFormatter{}, nil, nil)
	plain := emit()
	named := emit(card2.WithRenderer(renderer))
	for _, hexKey := range []string{`"0x5"`, `"0xf3e82"`, `"0x16"`, `"0x1bb"`} {
		require.Contains(t, plain, hexKey, "default renderer keys memberships by hex id")
		require.NotContains(t, named, hexKey, "injected formatter must replace the hex id")
	}
	for _, name := range []string{`"model:AeroQuad"`, `"customer:42"`, `"port:22"`, `"port:443"`} {
		require.Contains(t, named, name, "injected formatter keys memberships by domain name")
		require.NotContains(t, plain, name)
	}

	doc := &strings.Builder{}
	doc.WriteString(dqlDocHeader("anchor — membership rendering: ids on the wire, names at read time", "TestMembershipRendererGeneration"))
	doc.WriteString("The batch carries membership ids; how an id displays is decided at read\n")
	doc.WriteString("time by the consumer's renderer (ADR-0072). The test drives the same batch\n")
	doc.WriteString("through the JSON card emitter with the default renderer and with an anchor\n")
	doc.WriteString("domain formatter injected via WithRenderer, and asserts the card's\n")
	doc.WriteString("membership keys swap from the hex column to the named column below. The\n")
	doc.WriteString("formatter is a demo-local stand-in for the deferred registry-backed\n")
	doc.WriteString("ref-to-name formatter (the seam's intended first-class injector).\n\n")
	doc.WriteString("| ref id (wire) | default renderer | anchor formatter |\n|---|---|---|\n")
	def := membership.DefaultRenderer()
	for _, ref := range []uint64{5, 999042, 22, 443, 0xdeadbeef} {
		fmt.Fprintf(doc, "| %d | `%s` | `%s` |\n", ref, def.RenderRef(ref), renderer.RenderRef(ref))
	}
	writeFile("./card_anchor_membership_renderer.out.md", doc.String(), t)
}

// TestMembershipRoleClassifierGeneration tabulates the PathPrefixClassifier's
// verdicts for representative membership values against anchor sections in
// card_anchor_membership_roles.out.md. The same classifier runs inside the
// card emitters (it keys their byAttribute grouping); the table makes the
// default policy inspectable: refs are primary, verbatim names are primary
// only under the path prefix, parameters mean identity.
func TestMembershipRoleClassifierGeneration(t *testing.T) {
	tblDesc, err := GetAnchorTableDesc()
	require.NoError(t, err)

	secCtx := func(name string) membershiprole.SectionContext {
		for _, sec := range tblDesc.TaggedValuesSections {
			if string(sec.Name) == name {
				return membershiprole.SectionContext{Name: sec.Name, UseAspects: sec.UseAspects}
			}
		}
		t.Fatalf("section %q not in anchor schema", name)
		return membershiprole.SectionContext{}
	}

	cases := []struct {
		section string
		label   string
		mv      membership.MembershipValue
	}{
		{"symbol", "low-card ref 5 (drone model)", membership.MembershipValue{Kind: membership.IdentityRef, LowCard: true, Ref: 5}},
		{"geoPoint", "high-card ref 999042 (customer)", membership.MembershipValue{Kind: membership.IdentityRef, Ref: 999042}},
		{"symbol", "verbatim `/status/live` (under path prefix)", membership.MembershipValue{Kind: membership.IdentityVerbatim, Verbatim: "/status/live"}},
		{"symbol", "verbatim `unit` (no path prefix)", membership.MembershipValue{Kind: membership.IdentityVerbatim, Verbatim: "unit"}},
		{"foreignKey", "low-card ref 7 on the linking section", membership.MembershipValue{Kind: membership.IdentityRef, LowCard: true, Ref: 7}},
		{"symbol", "mixed ref 5 + params (the fourth anchor membership spec)", membership.MembershipValue{Kind: membership.IdentityPerRowId, Ref: 5, Params: "\x01\x00"}},
	}

	classifier := membershiprole.PathPrefixClassifier{}
	doc := &strings.Builder{}
	doc.WriteString(dqlDocHeader("anchor — membership role classification (PathPrefixClassifier)", "TestMembershipRoleClassifierGeneration"))
	doc.WriteString("| section | membership | role | param treatment |\n|---|---|---|---|\n")
	for _, c := range cases {
		role, pt := classifier.Classify(secCtx(c.section), c.mv)
		fmt.Fprintf(doc, "| `%s` | %s | %s | %s |\n", c.section, c.label, roleName(role), paramTreatmentName(pt))
	}
	writeFile("./card_anchor_membership_roles.out.md", doc.String(), t)
}

func roleName(r membershiprole.MembershipRoleE) string {
	switch r {
	case membershiprole.MembershipRolePrimary:
		return "primary"
	case membershiprole.MembershipRoleSecondary:
		return "secondary"
	}
	return "none"
}

func paramTreatmentName(p membershiprole.ParamTreatmentE) string {
	switch p {
	case membershiprole.ParamTreatmentIdentity:
		return "identity"
	case membershiprole.ParamTreatmentIndex:
		return "index"
	}
	return "none"
}
