package leewaywidgets

import (
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
)

// TestGeoCoSectionMerges verifies the fixture's "geo" co-section group
// (geoPoint + geoArea, equal cardinality) is actually merged by the driver
// into one BeginCoSectionGroup / wide BeginSection — through BOTH the dense
// NewDriver path (the leewaywidgets demo) and the NewDriverFromSchema discovery
// path (the play app's detail pane).
//
// buildCoGroups keys off CoSectionGroup, which fixture_schema.go now sets on
// both geo sections (alongside the orthogonal StreamingGroup). Before that the
// sections only shared a streaming group, so no co-group formed and they
// rendered standalone — the "geo co-section error".
func TestGeoCoSectionMerges(t *testing.T) {
	batches, err := BuildFixtureBatches(memory.NewGoAllocator())
	if err != nil {
		t.Fatalf("build fixture batches: %v", err)
	}
	if len(batches) == 0 {
		t.Fatal("no fixture batches")
	}
	rec := batches[0]
	schema := rec.Schema()

	tblDesc, err := BuildFixtureTableDesc()
	if err != nil {
		t.Fatalf("build table desc: %v", err)
	}
	tech := clickhouse.NewTechnologySpecificCodeGenerator()

	// The merged co-group must (a) open a BeginCoSectionGroup("geo") and (b)
	// carry value columns from BOTH sections — geoPoint (lat/lng) and geoArea
	// (poly/code) — in one merged BeginSection.
	assertGeoMerged := func(t *testing.T, label, out string) {
		t.Helper()
		if !strings.Contains(out, `BeginCoSectionGroup("geo")`) {
			t.Errorf("%s: no BeginCoSectionGroup(\"geo\") — co-group did not form\n%s",
				label, sectionLines(out))
			return
		}
		if !strings.Contains(out, `"lat"`) || !strings.Contains(out, `"code"`) {
			t.Errorf("%s: merged section missing columns from both geo sections\n%s",
				label, sectionLines(out))
		}
	}

	// Dense path (NewDriver) — what the demo / RunFixture uses.
	{
		ir := common.NewIntermediateTableRepresentation()
		if err = ir.LoadFromTable(&tblDesc, tech); err != nil {
			t.Fatalf("dense ir load: %v", err)
		}
		d, err := streamreadaccess.NewDriver(&tblDesc, ir, streamreadaccess.DefaultFormatters())
		if err != nil {
			t.Fatalf("dense driver: %v", err)
		}
		sink := streamreadaccess.NewStructuredOutputRecorder()
		if err = d.DriveRecordBatch(sink, rec); err != nil {
			t.Fatalf("dense drive: %v", err)
		}
		assertGeoMerged(t, "dense", sink.String())
	}

	// Schema path (NewDriverFromSchema) — what the play app's CardDriver uses.
	{
		nFields := schema.NumFields()
		colNames := make([]string, 0, nFields)
		for i := range nFields {
			colNames = append(colNames, schema.Field(i).Name)
		}
		// Fixture physical names are canonical ':'-separated.
		conv, err := ddl.NewHumanReadableNamingConvention(":")
		if err != nil {
			t.Fatalf("naming convention: %v", err)
		}
		discTbl, trc, err := conv.DiscoverTableFromColumnNames(colNames)
		if err != nil {
			t.Fatalf("discover table: %v", err)
		}
		ir := common.NewIntermediateTableRepresentation()
		if err = ir.LoadFromTable(&discTbl, tech); err != nil {
			t.Fatalf("disc ir load: %v", err)
		}
		d, err := streamreadaccess.NewDriverFromSchema(
			&discTbl, ir, streamreadaccess.DefaultFormatters(), schema, conv, trc)
		if err != nil {
			t.Fatalf("schema driver: %v", err)
		}
		sink := streamreadaccess.NewStructuredOutputRecorder()
		if err = d.DriveRecordBatch(sink, rec); err != nil {
			t.Fatalf("schema drive: %v", err)
		}
		assertGeoMerged(t, "schema", sink.String())
	}
}

// sectionLines extracts the structural Begin/End section lines from a DebugSink
// dump, for readable failure messages.
func sectionLines(out string) string {
	var b strings.Builder
	for _, line := range strings.Split(out, "\n") {
		s := strings.TrimSpace(line)
		if strings.Contains(s, "Section") || strings.Contains(s, "CoSection") {
			b.WriteString(s)
			b.WriteByte('\n')
		}
	}
	return b.String()
}
