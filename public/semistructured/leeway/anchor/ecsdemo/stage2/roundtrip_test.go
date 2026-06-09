package stage2

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

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
// (kindStatus/…/kindWindowBegin = 1..5 in dto.out.go; the geoPoint and timeRange
// groups each share one membership). The readback resolver must agree so its SQL
// queries the same ids the Arrow batch carries.
var droneLookup = marshallreflect.MapLookup{
	"droneStatus":  1,
	"droneBattery": 2,
	"droneTags":    3,
	"droneLoc":     4,
	"droneWindow":  5,
}

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
// All five sections round-trip: symbol (Status), u64Array unit (Battery),
// symbolArray (Tags), and the multi-sub-column geoPoint (Lat/Lng/Cell) and
// timeRange (WindowBegin/WindowEnd). projection ≙ Unmarshal, presence ≙ Presence,
// validator ≙ Validate — the same question stage 1 answers over json.
func TestRoundTripClickHouse(t *testing.T) {
	t0 := time.Unix(1_600_000_000, 0).UTC() // whole second -> exact DateTime64(9)
	original := []DroneEntity{
		{ID: 1001, Status: "IDLE", Battery: 9000, Tags: []string{"survey"},
			Lat: 47.5, Lng: 8.5, Cell: 12345, WindowBegin: t0, WindowEnd: t0.Add(time.Hour)},
		{ID: 1002, Status: "IN_TRANSIT", Battery: 8000, Tags: []string{"survey", "urgent"},
			Lat: 40.25, Lng: 12.5, Cell: 67890, WindowBegin: t0.Add(24 * time.Hour), WindowEnd: t0.Add(25 * time.Hour)},
		{ID: 1003, Status: "CHARGING", Battery: 150, Tags: []string{"idle"},
			Lat: 51.5, Lng: 0.5, Cell: 11111, WindowBegin: t0.Add(48 * time.Hour), WindowEnd: t0.Add(49 * time.Hour)},
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
	script := readback.HelperUDFsSQL() +
		"\nSELECT p.ID, p.Status, p.Battery, p.Lat, p.Lng, p.Cell, p.WindowBegin, p.WindowEnd, pres, val FROM (SELECT " +
		a.Projection + " AS p, " + a.Presence + " AS pres, " + a.Validator + " AS val FROM file('" +
		arrowPath + "', 'Arrow')) ORDER BY p.ID"
	out := runClickHouseLocal(t, script)

	var rows [][]string
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, "\t")
		require.Lenf(t, f, 10, "row %q\nscript:\n%s", line, script)
		rows = append(rows, f)
	}
	require.Lenf(t, rows, len(original), "output:\n%s", out)

	want := append([]DroneEntity(nil), original...)
	sort.Slice(want, func(i, j int) bool { return want[i].ID < want[j].ID })
	const chTime = "2006-01-02 15:04:05.000000000"
	for i, w := range want {
		r := rows[i]
		require.Equalf(t, fmt.Sprintf("%d", w.ID), r[0], "row %d id", i)
		require.Equalf(t, w.Status, r[1], "row %d status", i)
		// Battery is a u64Array attribute, so it projects as a 1-element array.
		require.Equalf(t, fmt.Sprintf("[%d]", w.Battery), r[2], "row %d battery", i)
		lat, _ := strconv.ParseFloat(r[3], 64)
		require.InDeltaf(t, float64(w.Lat), lat, 1e-6, "row %d lat", i)
		lng, _ := strconv.ParseFloat(r[4], 64)
		require.InDeltaf(t, float64(w.Lng), lng, 1e-6, "row %d lng", i)
		require.Equalf(t, fmt.Sprintf("%d", w.Cell), r[5], "row %d cell", i)
		require.Equalf(t, w.WindowBegin.Format(chTime), r[6], "row %d windowBegin", i)
		require.Equalf(t, w.WindowEnd.Format(chTime), r[7], "row %d windowEnd", i)
		require.Equalf(t, "1", r[8], "row %d presence", i)
		require.Equalf(t, "1", r[9], "row %d validator", i)
	}
}
