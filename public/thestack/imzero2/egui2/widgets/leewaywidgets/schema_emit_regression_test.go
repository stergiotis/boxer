//go:build llm_generated_opus47

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

// TestSchemaPathEmitsTaggedSections guards the data layer behind the play
// app's leeway detail pane: discovering a TableDesc from Arrow column names
// (as CardDriver.EnsureFor does), then driving the fixture batch through a
// Table2CardEmitter, must buffer rows for the tagged / co-sections too — not
// just the plain section.
//
// This exercises the schema-resolution path (NewDriverFromSchema), the one the
// play app actually uses; the demo uses the dense NewDriver path. It
// complements apps/play TestAnchorDriverRoundTrip, which only checks that
// physical names resolve, not that the emitter produces tagged-section content.
//
// Note: the user-reported "only plain value sections are shown" pane bug was an
// egui layout regression — the self-scrolling card table was nested in an outer
// ScrollArea, which crops its tail rows (the tagged/co sections come after the
// plain one). That fix lives in apps/play/play_detail.go and can't be exercised
// without a live egui context; this test guards the orthogonal data-layer
// invariant that the rows reach the emitter in the first place.
func TestSchemaPathEmitsTaggedSections(t *testing.T) {
	batches, err := BuildFixtureBatches(memory.NewGoAllocator())
	if err != nil {
		t.Fatalf("build fixture batches: %v", err)
	}
	if len(batches) == 0 {
		t.Fatal("no fixture batches")
	}
	rec := batches[0]
	schema := rec.Schema()

	// Replicate CardDriver.EnsureFor: separator detection → discovery →
	// IR load → NewDriverFromSchema.
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
	if err != nil {
		t.Fatalf("naming convention: %v", err)
	}
	tblDesc, tableRowConfig, err := conv.DiscoverTableFromColumnNames(colNames)
	if err != nil {
		t.Fatalf("discover table: %v", err)
	}
	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	ir := common.NewIntermediateTableRepresentation()
	if err = ir.LoadFromTable(&tblDesc, tech); err != nil {
		t.Fatalf("ir load: %v", err)
	}
	driver, err := streamreadaccess.NewDriverFromSchema(
		&tblDesc, ir, streamreadaccess.DefaultFormatters(),
		schema, conv, tableRowConfig)
	if err != nil {
		t.Fatalf("schema driver: %v", err)
	}

	// Drive the real emitter, but swallow EndBatch so flushUnified (which needs
	// a live egui context) is skipped, leaving `unified` populated for
	// inspection.
	emitter := NewTable2CardEmitter(nil, ColorPaletteViridis, nil)
	if err = driver.DriveRecordBatch(noFlushSink{emitter}, rec); err != nil {
		t.Fatalf("drive: %v", err)
	}

	var plain, tagged int
	for i := range emitter.unified {
		row := &emitter.unified[i]
		if row.kind != rowKindData {
			continue
		}
		switch row.sectionType {
		case sectionTypePlain:
			plain++
		case sectionTypeTagged, sectionTypeCo:
			tagged++
		}
	}
	if plain == 0 {
		t.Error("no plain data rows buffered")
	}
	if tagged == 0 {
		t.Error("no tagged/co data rows buffered — the detail pane would show only plain sections")
	}
	t.Logf("buffered data rows: plain=%d tagged/co=%d", plain, tagged)
}

// noFlushSink wraps a Table2CardEmitter and swallows EndBatch so flushUnified
// (which needs a live egui context) is skipped, leaving `unified` populated for
// inspection.
type noFlushSink struct{ *Table2CardEmitter }

func (noFlushSink) EndBatch() (err error) { return nil }
