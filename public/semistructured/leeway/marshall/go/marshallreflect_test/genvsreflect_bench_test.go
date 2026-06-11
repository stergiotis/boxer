package marshallreflect_test

import (
	"fmt"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"

	anchor "github.com/stergiotis/boxer/public/semistructured/leeway/anchor"
	codecdemo "github.com/stergiotis/boxer/public/semistructured/leeway/anchor/codecdemo"
	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
)

// The two benchmarks below marshal the same DTO batch through the two
// front-ends against the same anchor table, quantifying the reflection
// tax the dual-front-end design exists to trade against. The gen path
// includes the SoA staging Append, since that is its public API shape.

func benchDronesGen(n int) []codecdemo.DroneMission {
	rows := make([]codecdemo.DroneMission, n)
	for i := range rows {
		rows[i] = codecdemo.DroneMission{
			ID:       uint64(1000 + i),
			Tracking: []byte("TRK-A"),
			Status:   "IN_TRANSIT",
			Battery:  uint64(8500 - i),
		}
	}
	return rows
}

func benchDronesReflect(n int) []reflectDrone {
	rows := make([]reflectDrone, n)
	for i := range rows {
		rows[i] = reflectDrone{
			ID:       uint64(1000 + i),
			Tracking: []byte("TRK-A"),
			Status:   "IN_TRANSIT",
			Battery:  uint64(8500 - i),
		}
	}
	return rows
}

func BenchmarkMarshalGen(b *testing.B) {
	pool := memory.NewGoAllocator()
	for _, n := range []int{1, 1000} {
		rows := benchDronesGen(n)
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				cols := &codecdemo.DroneMissionColumns{}
				for _, r := range rows {
					cols.Append(r)
				}
				table := anchor.NewInEntityTestTable(pool, cols.Len())
				if err := codecdemo.DroneMissionBuildEntities(table, cols); err != nil {
					b.Fatal(err)
				}
				recs, err := table.TransferRecords(nil)
				if err != nil {
					b.Fatal(err)
				}
				for _, r := range recs {
					r.Release()
				}
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(n), "ns/row")
		})
	}
}

func BenchmarkMarshalReflect(b *testing.B) {
	pool := memory.NewGoAllocator()
	lookup := marshallreflect.MapLookup{
		"droneStatus": 1,
		"battery":     2,
	}
	for _, n := range []int{1, 1000} {
		rows := benchDronesReflect(n)
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				table := anchor.NewInEntityTestTable(pool, len(rows))
				if err := marshallreflect.Marshal(table, rows, lookup); err != nil {
					b.Fatal(err)
				}
				recs, err := table.TransferRecords(nil)
				if err != nil {
					b.Fatal(err)
				}
				for _, r := range recs {
					r.Release()
				}
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(n), "ns/row")
		})
	}
}
