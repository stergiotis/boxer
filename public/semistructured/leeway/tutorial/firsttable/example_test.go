package firsttable_test

import (
	"fmt"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"
	"github.com/stergiotis/boxer/public/semistructured/leeway/tutorial/firsttable"
)

// Checkout is the logical shape of one event. Each lw: tag names the
// membership (the attribute's label), the section the value is stored in and
// the membership channel; an empty membership marks a plain column.
type Checkout struct {
	_     struct{}  `kind:"checkout"`
	Id    uint64    `lw:",id"`
	Ts    time.Time `lw:",ts"`
	Event string    `lw:"event,symbol,verbatim"`
	Geo   string    `lw:"user.geo,symbol,verbatim"`
	Total float64   `lw:"cart.total,f64,verbatim"`
}

// Example writes two rows through the generated write side, takes the
// resulting Arrow record, and reads the rows back through the generated read
// side. No database is involved: the record is what a ClickHouse INSERT would
// carry.
func Example() {
	rows := []Checkout{
		{Id: 1, Ts: time.Date(2026, 6, 27, 12, 34, 56, 0, time.UTC), Event: "checkout", Geo: "EU", Total: 24.48},
		{Id: 2, Ts: time.Date(2026, 6, 27, 12, 35, 10, 0, time.UTC), Event: "checkout", Geo: "US", Total: 4.5},
	}

	// Write: marshal the DTOs into the generated table and take the record.
	table := firsttable.NewInEntityEvents(memory.NewGoAllocator(), len(rows))
	err := marshallreflect.Marshal(table, rows, marshallreflect.NoLookup{})
	if err != nil {
		panic(err)
	}
	recs, err := table.TransferRecords(nil)
	if err != nil {
		panic(err)
	}
	rec := recs[0]
	defer rec.Release()

	// Read: load the record into the generated readers and unmarshal.
	ra := firsttable.NewReadAccessEvents()
	defer ra.Release()
	ra.SetColumnIndices(ra.GetColumnIndices())
	err = ra.LoadFromRecord(rec)
	if err != nil {
		panic(err)
	}
	readers := marshallreflect.NewSectionReaders(ra.GetNumberOfEntities()).
		PlainColumn("id", ra.EntityId.ValueId).
		PlainColumn("ts", ra.EntityTimestamp.ValueTs).
		Section("symbol", ra.Symbol.GetAttributes(), ra.Symbol.GetMemberships()).
		Section("f64", ra.F64.GetAttributes(), ra.F64.GetMemberships())
	var got []Checkout
	err = marshallreflect.Unmarshal(readers, &got, marshallreflect.NoLookup{})
	if err != nil {
		panic(err)
	}
	for _, r := range got {
		fmt.Println(r.Id, r.Ts.UTC().Format(time.RFC3339), r.Event, r.Geo, r.Total)
	}
	// Output:
	// 1 2026-06-27T12:34:56Z checkout EU 24.48
	// 2 2026-06-27T12:35:10Z checkout US 4.5
}
