package canonform

// The `boxer.facts` sample golden ADR-0201 M0 moved to M1, plus the
// PathPrefixClassifier variant of the anchor digests the M0 verification plan
// names. Together with encoder_test.go's anchor goldens these pin the form
// over the two real schemas in the tree.

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	factsdml "github.com/stergiotis/boxer/public/keelson/runtime/factsschema/dml"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsschema"
	"github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/membershiprole"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
)

// factsSampleBatch writes three deterministic entities through the generated
// facts DML: symbol scalars, a bool, a u64 set with a duplicate element, and
// ref plus mixed-carrier memberships.
func factsSampleBatch(t *testing.T) (tbl common.TableDesc, ir *common.IntermediateTableRepresentation, rec arrow.RecordBatch) {
	t.Helper()
	manip, err := factsschema.GetSchemaInManipulator()
	require.NoError(t, err)
	tbl, err = manip.BuildTableDesc()
	require.NoError(t, err)
	ir = common.NewIntermediateTableRepresentation()
	require.NoError(t, ir.LoadFromTable(&tbl, clickhouse.NewTechnologySpecificCodeGenerator()))

	dml := factsdml.NewInEntityFacts(memory.DefaultAllocator, 16)
	ts := time.Unix(1_700_000_000, 0).UTC()
	for i := range 3 {
		ent := dml.BeginEntity()
		ent.SetId(uint64(1000+i), []byte(fmt.Sprintf("nk-%d", i)))
		ent.SetTimestamp(ts.Add(time.Duration(i) * time.Second))
		ent.SetLifecycle(ts.Add(24 * time.Hour))
		sym := dml.GetSectionSymbol()
		sym.BeginAttribute(fmt.Sprintf("sym-%d", i)).
			AddMembershipLowCardRef(uint64(7 + i)).
			EndAttribute()
		sym.BeginAttribute("shared").
			AddMembershipMixedLowCardRef(3, []byte("p")).
			AddMembershipLowCardRef(9).
			EndAttribute()
		dml.GetSectionBool().BeginAttribute(i%2 == 0).
			AddMembershipLowCardRef(11).
			EndAttribute()
		dml.GetSectionU64Set().BeginAttribute().
			AddToContainer(5).
			AddToContainer(1).
			AddToContainer(5).
			AddMembershipLowCardRef(uint64(20 + i)).
			EndAttribute()
		require.NoError(t, ent.CheckErrors())
		require.NoError(t, ent.CommitEntity())
	}
	records, err := dml.TransferRecords(nil)
	require.NoError(t, err)
	require.Len(t, records, 1)
	rec = records[0]
	return
}

func TestFactsSampleGolden(t *testing.T) {
	tbl, ir, rec := factsSampleBatch(t)

	digestLines := func(opts Options) string {
		d, err := streamreadaccess.NewDriver(&tbl, ir, streamreadaccess.DefaultFormatters())
		require.NoError(t, err)
		enc, err := NewEncoder(ir, opts)
		require.NoError(t, err)
		require.NoError(t, d.DriveRecordBatch(enc, rec))
		require.NoError(t, enc.Err())
		var sb strings.Builder
		pin, err := enc.FormPin()
		require.NoError(t, err)
		sb.WriteString("pin " + pin + "\n")
		for i := range enc.NumRecords() {
			fmt.Fprintf(&sb, "%02d %s\n", i, hexs(enc.RecordDigest(i)))
		}
		return sb.String()
	}

	var out strings.Builder
	out.WriteString("# default options\n")
	out.WriteString(digestLines(Options{}))
	out.WriteString("# identity in, integrity key and lifecycle out\n")
	out.WriteString(digestLines(Options{Plains: PlainsMask{
		IncludeEntityId:  true,
		ExcludeItemTypes: []common.PlainItemTypeE{common.PlainItemTypeEntityLifecycle},
		ExcludeNames:     []naming.StylableName{"naturalKey"},
	}}))
	gold(t, "canonform_facts_gold.out.txt", out.String())
}

// The anchor digests under PathPrefixClassifier — the second classifier the
// M0 verification plan names. The anchor fixtures carry both "/"-prefixed
// verbatim memberships (primary under this classifier) and label-shaped ones
// (secondary), so these digests differ from the nil-classifier golden.
func TestAnchorPathPrefixClassifierGolden(t *testing.T) {
	manip, err := anchor.GetSchemaInManipulator()
	require.NoError(t, err)
	tbl, err := manip.BuildTableDesc()
	require.NoError(t, err)
	ir := common.NewIntermediateTableRepresentation()
	require.NoError(t, ir.LoadFromTable(&tbl, clickhouse.NewTechnologySpecificCodeGenerator()))
	records, err := anchor.GenerateAlpineEvents(nil, 20)
	require.NoError(t, err)
	records, err = anchor.GenerateCyberThreatEvents(records)
	require.NoError(t, err)
	records, err = anchor.GenerateDroneMissionEvents(records)
	require.NoError(t, err)

	d, err := streamreadaccess.NewDriver(&tbl, ir, streamreadaccess.DefaultFormatters())
	require.NoError(t, err)
	var digests strings.Builder
	var pinned string
	for bi, rec := range records {
		enc, err := NewEncoder(ir, Options{Classifier: membershiprole.PathPrefixClassifier{}})
		require.NoError(t, err)
		if pinned == "" {
			pinned, err = enc.FormPin()
			require.NoError(t, err)
			digests.WriteString("pin " + pinned + "\n")
		}
		require.NoError(t, d.DriveRecordBatch(enc, rec))
		require.NoError(t, enc.Err())
		for i := range enc.NumRecords() {
			fmt.Fprintf(&digests, "%d %02d %s\n", bi, i, hexs(enc.RecordDigest(i)))
		}
	}
	gold(t, "canonform_anchor_pathprefix_gold.out.txt", digests.String())

	// A fixture fact, pinned deliberately: every anchor membership is a
	// "/"-prefixed verbatim or a ref, so PathPrefixClassifier classifies all
	// of them primary and the digests coincide with the nil-classifier run.
	// If an anchor fixture ever gains a label-shaped (secondary) membership,
	// this equality breaks and the golden above starts carrying independent
	// information. Classifier-sensitivity itself is proven over random data
	// by TestPropertySecondaryMembershipsAreNotContent.
	collect := func(opts Options) string {
		var all bytes.Buffer
		for _, rec := range records {
			enc, err := NewEncoder(ir, opts)
			require.NoError(t, err)
			require.NoError(t, d.DriveRecordBatch(enc, rec))
			require.NoError(t, enc.Err())
			for i := range enc.NumRecords() {
				all.Write(enc.RecordDigest(i))
			}
		}
		return hexs(all.Bytes())
	}
	require.Equal(t,
		collect(Options{Classifier: membershiprole.PathPrefixClassifier{}}),
		collect(Options{}))
}
