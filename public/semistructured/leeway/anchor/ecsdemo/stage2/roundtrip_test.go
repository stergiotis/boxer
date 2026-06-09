package stage2

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/readback"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
)

// droneLookup maps each membership name to the id the generated codec writes
// (kindStatus/kindBattery/kindTags = 1/2/3 in dto.out.go). The readback resolver
// must agree so its SQL queries the same ids the Arrow batch carries.
var droneLookup = marshallreflect.MapLookup{"droneStatus": 1, "droneBattery": 2, "droneTags": 3}

func buildDroneIR(t *testing.T) *readback.InformationRetrieval {
	t.Helper()
	td := droneTableDesc(t)
	conv, err := ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	ir := common.NewIntermediateTableRepresentation()
	require.NoError(t, ir.LoadFromTable(&td, clickhouse.NewTechnologySpecificCodeGenerator()))
	info := readback.NewInformationRetrieval(conv)
	require.NoError(t, info.LoadTable(ir, TableRowConfig))
	return info
}

func writeArrowFile(t *testing.T, rec arrow.RecordBatch) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "drone.arrow")
	f, err := os.Create(path)
	require.NoError(t, err)
	w, err := ipc.NewFileWriter(f, ipc.WithSchema(rec.Schema()), ipc.WithAllocator(memory.NewGoAllocator()))
	require.NoError(t, err)
	require.NoError(t, w.Write(rec))
	require.NoError(t, w.Close())
	require.NoError(t, f.Close())
	return path
}

func runClickHouseLocal(t *testing.T, script string) string {
	t.Helper()
	bin, err := exec.LookPath("clickhouse-local")
	if err != nil {
		t.Skipf("clickhouse-local not on PATH: %v", err)
	}
	cmd := exec.Command(bin, "--multiquery", "--output-format", "TSV")
	cmd.Stdin = strings.NewReader(script)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Run(), "clickhouse-local stderr:\n%s", stderr.String())
	return stdout.String()
}

// TestRoundTripClickHouse is the stage-2 mirror of stage-1's Presence/Validate:
// it marshals DroneEntity rows to a real Arrow batch via the marshallgen codec
// (DroneEntityBuildEntities) over the bespoke DML target, then runs the readback
// Presence/Validator/Projection over that batch in clickhouse-local and asserts
// the read-back equals the originals with presence=validator=1 on every row.
//
// This closes the loop: the same "can this be unserialized into this shape?"
// that stage 1 answers over a json document, stage 2 answers as SQL over the
// columnar Arrow encoding — projection ≙ Unmarshal, presence ≙ Presence,
// validator ≙ Validate.
func TestRoundTripClickHouse(t *testing.T) {
	original := []DroneEntity{
		{ID: 1001, Status: "IDLE", Battery: 9000, Tags: []string{"survey"}},
		{ID: 1002, Status: "IN_TRANSIT", Battery: 8000, Tags: []string{"survey", "urgent"}},
		{ID: 1003, Status: "CHARGING", Battery: 150, Tags: []string{"idle"}},
	}

	// marshal Go -> Arrow via the generated marshallgen codec + bespoke DML target
	cols := &DroneEntityColumns{}
	for _, row := range original {
		cols.Append(row)
	}
	table := NewInEntityDroneTable(memory.NewGoAllocator(), cols.Len())
	require.NoError(t, DroneEntityBuildEntities(table, cols))
	recs, err := table.TransferRecords(nil)
	require.NoError(t, err)
	defer func() {
		for _, r := range recs {
			r.Release()
		}
	}()
	require.NotEmpty(t, recs)
	arrowPath := writeArrowFile(t, recs[0])

	// generate the Presence/Validator/Projection SQL for DroneEntity
	plan, err := marshallreflect.PlanFor[DroneEntity]()
	require.NoError(t, err)
	g := readback.NewGenerator(buildDroneIR(t), readback.NewLookupResolver(droneLookup))
	a, err := g.Generate(plan)
	require.NoError(t, err)

	// run them over the Arrow batch in clickhouse-local
	script := readback.HelperUDFsSQL() + "\nSELECT p.ID, p.Status, p.Battery, pres, val FROM (SELECT " +
		a.Projection + " AS p, " + a.Presence + " AS pres, " + a.Validator + " AS val FROM file('" +
		arrowPath + "', 'Arrow')) ORDER BY p.ID"
	out := runClickHouseLocal(t, script)

	type row struct{ id, status, battery, pres, val string }
	var got []row
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, "\t")
		require.Lenf(t, f, 5, "row %q\nscript:\n%s", line, script)
		got = append(got, row{f[0], f[1], f[2], f[3], f[4]})
	}
	require.Lenf(t, got, len(original), "output:\n%s", out)

	want := make([]DroneEntity, len(original))
	copy(want, original)
	sort.Slice(want, func(i, j int) bool { return want[i].ID < want[j].ID })
	for i, w := range want {
		r := got[i]
		require.Equalf(t, fmt.Sprintf("%d", w.ID), r.id, "row %d id", i)
		require.Equalf(t, w.Status, r.status, "row %d status", i)
		// Battery lives in the u64Array section, so it projects as a 1-element
		// array even though the unit modifier wrote it single-valued.
		require.Equalf(t, fmt.Sprintf("[%d]", w.Battery), r.battery, "row %d battery", i)
		require.Equalf(t, "1", r.pres, "row %d presence", i)
		require.Equalf(t, "1", r.val, "row %d validator", i)
	}
}
