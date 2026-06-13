package play

import (
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	"github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stretchr/testify/require"
)

// TestAnchorDriverRoundTrip is a regression check for the leeway
// detail-pane bug where sections such as `geoPoint` / `geoArea`
// vanished from the rendered card.
//
// Root cause: schema columns use the section's original casing
// (`tv:geoPoint:…`) but the IR re-styles StylableName components to
// LowerSpinalCase (`tv:geo-point:…`) when loading the TableDesc.
// `NewDriverFromSchema`'s name → arrow-index lookup used the raw schema
// name only, so every IR column for a re-styled section resolved to
// arrowIdx=-1, dropped out of the layout, made `sectionAttrCount` return
// zero, and silently suppressed the section.
//
// The fix lives in boxer: `NamingConventionFwdI.CanonicalizeSchemaName`
// re-styles the section/column-name components of a schema column to
// match what `MapIntermediateToPhysicalColumns` would emit, and
// `prepareFromSchema` adds the canonical form as a second key in the
// lookup map.
//
// This test exercises the round-trip end-to-end against the generated
// anchor schema (which contains the offending `geoPoint` and `geoArea`
// section names). If `CanonicalizeSchemaName` regresses, the missing
// count will climb back to the original 37 / 88.
func TestAnchorDriverRoundTrip(t *testing.T) {
	schema := anchor.CreateSchemaTestTable()

	ids := c.NewWidgetIdStack()
	cards := NewCardDriver(ids, memory.NewGoAllocator())
	require.True(t, cards.EnsureFor(schema), "anchor schema must be leeway-shaped")

	// Same separator detection as CardDriver.EnsureFor.
	nFields := schema.NumFields()
	colNames := make([]string, 0, nFields)
	for i := range nFields {
		colNames = append(colNames, schema.Field(i).Name)
	}
	sep := "_"
	for _, n := range colNames {
		if strings.HasPrefix(n, "_") {
			continue
		}
		if strings.ContainsRune(n, ':') {
			sep = ":"
		}
		break
	}

	conv, err := ddl.NewHumanReadableNamingConvention(sep)
	require.NoError(t, err)
	tblDesc, tableRowConfig, err := conv.DiscoverTableFromColumnNames(colNames)
	require.NoError(t, err)
	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	ir := common.NewIntermediateTableRepresentation()
	require.NoError(t, ir.LoadFromTable(&tblDesc, tech))

	// Build the same nameToIdx the boxer-side prepareFromSchema builds,
	// then walk the IR and confirm every physical name resolves.
	nameToIdx := make(map[string]int, nFields*2)
	for i := range nFields {
		n := schema.Field(i).Name
		nameToIdx[n] = i
		if canon := conv.CanonicalizeSchemaName(n); canon != n {
			nameToIdx[canon] = i
		}
	}

	missing := 0
	var physBuf []common.PhysicalColumnDesc
	for cc, cp := range ir.IterateColumnProps() {
		physBuf, err = conv.MapIntermediateToPhysicalColumns(cc, *cp, physBuf[:0], tableRowConfig)
		require.NoError(t, err)
		for j, phys := range physBuf {
			physName := phys.String()
			if _, ok := nameToIdx[physName]; !ok {
				missing++
				t.Logf("MISSING: %s (cc.PlainItem=%v cc.SubType=%v section=%v col=%v)",
					physName, cc.PlainItemType, cc.SubType, cc.SectionName, cp.Names[j])
			}
		}
	}
	require.Zero(t, missing, "every IR physical name should resolve to a schema column")

	// Cross-check by constructing the actual driver.
	_, err = streamreadaccess.NewDriverFromSchema(
		&tblDesc, ir, streamreadaccess.DefaultFormatters(),
		schema, conv, tableRowConfig)
	require.NoError(t, err)
}
