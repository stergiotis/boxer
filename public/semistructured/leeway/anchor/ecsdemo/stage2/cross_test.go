package stage2

import (
	"maps"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/semistructured/leeway/anchor/ecsdemo"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/clickhouse/readback"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
)

// droneCore is DroneEntity minus its geoPoint section (the Located component) — a
// structural subset, the leeway analogue of stage-1's Flying ⊂ Operating.
// Marshalling it leaves geoPoint empty, so DroneEntity's validator (which
// requires it) reports the row invalid, exactly as stage-1's
// ArchetypeValidate(Operating) rejects an Entity that has no Located component.
type droneCore struct {
	_ struct{} `kind:"droneCore"`

	ID          uint64    `lw:",id"`
	Status      string    `lw:"droneStatus,symbol"`
	Battery     uint64    `lw:"droneBattery,u64Array,unit"`
	Tags        []string  `lw:"droneTags,symbolArray"`
	WindowBegin time.Time `lw:"droneWindow,timeRange:beginIncl"`
	WindowEnd   time.Time `lw:"droneWindow,timeRange:endExcl"`
}

type verdict struct{ presence, validator bool }

// leewayVerdicts marshals rows into the bespoke drone schema, then runs
// DroneEntity's readback presence/validator over the Arrow batch in
// clickhouse-local, returning one verdict per entity id. The marshal uses
// marshallreflect so full and subset DTOs go through one path (the marshallgen
// codec is exercised by TestRoundTripClickHouse; here the marshal is incidental
// to the check being cross-checked).
func leewayVerdicts[T any](t *testing.T, rows []T) map[uint64]verdict {
	t.Helper()
	table := NewInEntityDroneTable(memory.NewGoAllocator(), len(rows))
	require.NoError(t, marshallreflect.Marshal(table, rows, droneLookup))
	recs, err := table.TransferRecords(nil)
	require.NoError(t, err)
	defer func() {
		for _, r := range recs {
			r.Release()
		}
	}()
	require.NotEmpty(t, recs)
	arrowPath := writeArrowFile(t, recs[0])

	plan, err := marshallreflect.PlanFor[DroneEntity]()
	require.NoError(t, err)
	g := readback.NewGenerator(buildDroneIR(t), readback.NewLookupResolver(droneLookup))
	a, err := g.Generate(plan)
	require.NoError(t, err)

	script := readback.HelperUDFsSQL() + "\nSELECT p.ID, pres, val FROM (SELECT " +
		a.Projection + " AS p, " + a.Presence + " AS pres, " + a.Validator + " AS val FROM file('" +
		arrowPath + "', 'Arrow'))"
	out := runClickHouseLocal(t, script)

	verdicts := make(map[uint64]verdict, len(rows))
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, "\t")
		require.Lenf(t, f, 3, "row %q", line)
		id, err := strconv.ParseUint(f[0], 10, 64)
		require.NoError(t, err)
		verdicts[id] = verdict{presence: f[1] == "1", validator: f[2] == "1"}
	}
	return verdicts
}

type geo struct {
	lat, lng float32
	cell     uint64
}

// asEntity rebuilds the stage-1 Entity for a drone. A nil geo means the entity
// has no Located component (the Flying archetype), the stage-1 counterpart of an
// empty geoPoint section.
func asEntity(id uint64, status string, battery uint64, tags []string, g *geo, begin, end time.Time) ecsdemo.Entity {
	e := ecsdemo.Entity{
		ID:       ecsdemo.EntityID(id),
		Identity: &ecsdemo.Identity{Status: status},
		Battery:  &ecsdemo.Battery{Charge: battery},
		Tasked:   &ecsdemo.Tasked{Window: ecsdemo.TimeRange{BeginIncl: begin.UnixNano(), EndExcl: end.UnixNano()}, Tags: tags},
	}
	if g != nil {
		e.Located = &ecsdemo.Located{At: ecsdemo.GeoPoint{Lat: g.lat, Lng: g.lng, Cell: g.cell}}
	}
	return e
}

// TestCrossCheck_Stage1Stage2Agree asserts the two stages answer the same
// question — "is this a complete (Operating) drone?" — identically. For every
// entity, stage-1's json ArchetypePresence/ArchetypeValidate(Operating) must
// agree with stage-2's clickhouse-local presence/validator. It covers complete
// drones (both verdicts true) and a Located-less drone (both false), so the
// agreement is exercised in both directions, not just on a constant verdict.
func TestCrossCheck_Stage1Stage2Agree(t *testing.T) {
	t0 := time.Unix(1_600_000_000, 0).UTC()
	full := []DroneEntity{
		{ID: 1, Status: "IDLE", Battery: 9000, Tags: []string{"survey"}, Lat: 47.5, Lng: 8.5, Cell: 12345, WindowBegin: t0, WindowEnd: t0.Add(time.Hour)},
		{ID: 2, Status: "BUSY", Battery: 8000, Tags: []string{"urgent"}, Lat: 40.25, Lng: 12.5, Cell: 67890, WindowBegin: t0, WindowEnd: t0.Add(time.Hour)},
	}
	core := []droneCore{
		{ID: 3, Status: "IDLE", Battery: 9000, Tags: []string{"survey"}, WindowBegin: t0, WindowEnd: t0.Add(time.Hour)},
	}

	leeway := leewayVerdicts(t, full)
	maps.Copy(leeway, leewayVerdicts(t, core))

	check := func(e ecsdemo.Entity) {
		doc, err := ecsdemo.MarshalEntity(e)
		require.NoError(t, err)
		s1Present := ecsdemo.ArchetypePresence(doc, ecsdemo.Operating)
		s1Valid := ecsdemo.ArchetypeValidate(doc, ecsdemo.Operating) == nil
		v, ok := leeway[uint64(e.ID)]
		require.Truef(t, ok, "no leeway verdict for entity %d", e.ID)
		require.Equalf(t, s1Present, v.presence, "presence must agree for entity %d (stage1=%v stage2=%v)", e.ID, s1Present, v.presence)
		require.Equalf(t, s1Valid, v.validator, "validator must agree for entity %d (stage1=%v stage2=%v)", e.ID, s1Valid, v.validator)
	}

	for _, d := range full {
		check(asEntity(d.ID, d.Status, d.Battery, d.Tags, &geo{d.Lat, d.Lng, d.Cell}, d.WindowBegin, d.WindowEnd))
	}
	for _, d := range core {
		check(asEntity(d.ID, d.Status, d.Battery, d.Tags, nil, d.WindowBegin, d.WindowEnd))
	}
}
