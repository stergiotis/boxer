---
type: tutorial
audience: newcomer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Tutorial: your first leeway table

By the end of this tutorial you will have, in a module of your own, a
leeway table description, the Go code generated from it, a program that
writes two rows into an Arrow record and reads them back, and a SQL query
over the same rows. No database server is involved.

The same code, under boxer's own module, is
[tutorial/firsttable](../../public/semistructured/leeway/tutorial/firsttable/),
which compiles and runs with boxer's tests; when this page and that package
disagree, the package is right. Why the table looks the way it does is in
[the leeway explanation](../../public/semistructured/leeway/EXPLANATION.md).

## Prerequisites

- Go. Boxer's `go.mod` names the version it needs; with the default
  `GOTOOLCHAIN=auto` the `go` command downloads that version on first use.
- Optional, for the last step: the `clickhouse` binary on `PATH`
  (`clickhouse local` runs a query over a file, without a server).

## Step 1 — Create the module and pin boxer

Boxer's `main` branch moves quickly and its tags are infrequent, so pin a
commit rather than a tag:

```sh
mkdir lwfirst && cd lwfirst
go mod init example.com/lwfirst
go get github.com/stergiotis/boxer@<commit>
mkdir events
```

## Step 2 — Describe the table

A leeway table has **plain** columns, present on every row, and
**tagged-value sections**. A section holds values of one canonical type;
each value carries a **membership**, the label saying which logical field it
is. This table has an id and a timestamp, a section of strings and a section
of 64-bit floats. Its memberships are *verbatim*: the label is stored as
text, so no id registry is needed.

Create **events/schema.go**:

```go
package events

import (
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
)

// TableRowConfig is the row layout the generated code is built for: one row
// per entity, each section carrying any number of attributes.
const TableRowConfig = common.TableRowConfigMultiAttributesPerRow

// NewSchema describes the events table: an id and a timestamp on every row,
// a section of strings and a section of 64-bit floats. Both sections carry
// low-cardinality verbatim memberships, so an attribute's label is stored as
// text beside its value and no id registry is needed.
func NewSchema() (manip *common.TableManipulator, err error) {
	manip, err = common.NewTableManipulator()
	if err != nil {
		err = eh.Errorf("unable to create table manipulator: %w", err)
		return
	}
	manip.SetTableName("events")
	manip.PlainValueColumn(common.PlainItemTypeEntityId, "id", ctabb.U64)
	manip.PlainValueColumn(common.PlainItemTypeEntityTimestamp, "ts", ctabb.Z64)
	manip.TaggedValueSection("symbol").
		AddSectionMembership(common.MembershipSpecLowCardVerbatim).
		TaggedValueColumn("value", ctabb.S)
	manip.TaggedValueSection("f64").
		AddSectionMembership(common.MembershipSpecLowCardVerbatim).
		TaggedValueColumn("value", ctabb.F64)
	return
}
```

## Step 3 — Generate the write and read sides

The table's builder (the write side) and its typed readers (the read side)
are generated from the description. Boxer's own packages run the
generators from a test, so that `go test` regenerates them; do the same.

Create **events/gen_test.go**:

```go
package events

import (
	"os"
	"testing"

	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/dml"
	"github.com/stergiotis/boxer/public/semistructured/leeway/gocodegen"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/readaccess"
	"github.com/stretchr/testify/require"
)

// TestGenerate writes the generated write side (events_dml.out.go) and read
// side (events_ra.out.go) of the events table. Running the package's tests
// regenerates both; a diff after `go test` means the schema or a generator
// changed.
func TestGenerate(t *testing.T) {
	manip, err := NewSchema()
	require.NoError(t, err)
	tbl, err := manip.BuildTableDesc()
	require.NoError(t, err)
	conv, err := ddl.NewHumanReadableNamingConvention(":")
	require.NoError(t, err)
	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	namer := gocodegen.NewMultiTablePerPackageGoClassNamer()
	name := naming.MustBeValidStylableName("events")

	code, _, err := dml.NewGoCodeGeneratorDriver(conv, tech).GenerateGoClasses("events", name, tbl, TableRowConfig, namer)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile("events_dml.out.go", code, 0o644))

	code, _, err = readaccess.NewGoCodeGeneratorDriver(conv, tech, true).GenerateGoClasses("events", name, tbl, TableRowConfig, namer)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile("events_ra.out.go", code, 0o644))
}
```

Run it:

```sh
go mod tidy
go test -run TestGenerate ./events
```

Two files appear beside the schema: `events_dml.out.go`, with
`NewInEntityEvents`, and `events_ra.out.go`, with `NewReadAccessEvents`.
They are generated: change the schema and rerun the test rather than edit
them.

## Step 4 — Say what a row means

The generated code knows sections, not fields. A DTO's `lw:` struct tags map
its fields onto them: `membership,section,channel`, and an empty membership
for a plain column.

Create **events/checkout.go**:

```go
package events

import "time"

// Checkout is the logical shape of one event.
type Checkout struct {
	_     struct{}  `kind:"checkout"`
	Id    uint64    `lw:",id"`
	Ts    time.Time `lw:",ts"`
	Event string    `lw:"event,symbol,verbatim"`
	Geo   string    `lw:"user.geo,symbol,verbatim"`
	Total float64   `lw:"cart.total,f64,verbatim"`
}
```

## Step 5 — Write rows and read them back

Create **cmd/roundtrip/main.go**. It marshals two rows into the generated
table, takes the Arrow record the table produces, reads the rows back
through the generated readers, and writes the record to a file:

```go
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stergiotis/boxer/public/semistructured/leeway/marshall/go/marshallreflect"

	"example.com/lwfirst/events"
)

func main() {
	rows := []events.Checkout{
		{Id: 1, Ts: time.Date(2026, 6, 27, 12, 34, 56, 0, time.UTC), Event: "checkout", Geo: "EU", Total: 24.48},
		{Id: 2, Ts: time.Date(2026, 6, 27, 12, 35, 10, 0, time.UTC), Event: "checkout", Geo: "US", Total: 4.5},
	}
	table := events.NewInEntityEvents(memory.NewGoAllocator(), len(rows))
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

	ra := events.NewReadAccessEvents()
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
	var got []events.Checkout
	err = marshallreflect.Unmarshal(readers, &got, marshallreflect.NoLookup{})
	if err != nil {
		panic(err)
	}
	for _, r := range got {
		fmt.Println(r.Id, r.Ts.UTC().Format(time.RFC3339), r.Event, r.Geo, r.Total)
	}

	f, err := os.Create("events.arrow")
	if err != nil {
		panic(err)
	}
	w, err := ipc.NewFileWriter(f, ipc.WithSchema(rec.Schema()))
	if err != nil {
		panic(err)
	}
	err = w.Write(rec)
	if err != nil {
		panic(err)
	}
	err = w.Close()
	if err != nil {
		panic(err)
	}
	err = f.Close()
	if err != nil {
		panic(err)
	}
}
```

Run it:

```sh
go mod tidy
go run ./cmd/roundtrip
```

Expected output:

```text
1 2026-06-27T12:34:56Z checkout EU 24.48
2 2026-06-27T12:35:10Z checkout US 4.5
```

and a file `events.arrow` in the current directory.

## Step 6 — Query the rows from SQL

The record's columns are the physical columns a ClickHouse table of this
schema would have; their names encode section, role and canonical type.
Look at them:

```sh
clickhouse local --query "DESCRIBE file('events.arrow', 'Arrow')"
```

The query below selects the rows that carry each field of `Checkout` exactly
once. Its `WHERE` clause is not hand-written: it is the filter boxer's
read-back generator emits for the `Checkout` DTO
(`ExampleGenerator_Generate` in
[readback/example_test.go](../../public/semistructured/leeway/marshall/clickhouse/readback/example_test.go)).

```sh
clickhouse local --query '
SELECT
    "id:id:u64:::0:" AS id,
    "tv:symbol:lv:lv:y:124:::0::" AS labels,
    "tv:symbol:value:val:s::::0::" AS strings
FROM file('\''events.arrow'\'', '\''Arrow'\'')
WHERE hasAll("tv:symbol:lv:lv:y:124:::0::", ['\''event'\'', '\''user.geo'\'']) AND has("tv:f64:lv:lv:y:124:::0::", '\''cart.total'\'') AND countEqual("tv:symbol:lv:lv:y:124:::0::", '\''event'\'') = 1 AND countEqual("tv:symbol:lv:lv:y:124:::0::", '\''user.geo'\'') = 1 AND countEqual("tv:f64:lv:lv:y:124:::0::", '\''cart.total'\'') = 1
'
```

Expected output:

```text
1	['event','user.geo']	['checkout','EU']
2	['event','user.geo']	['checkout','US']
```

Each row's `labels` and `strings` are parallel arrays: the string section's
memberships and its values.

## What you've learned

- A table is described once; the DDL, the write side and the read side are
  generated from that description.
- Values are stored by type, in sections, and told apart by membership; a
  new field of an existing type is a new membership, not a new column.
- A DTO's `lw:` tags are the mapping between the logical shape and the
  sections, and the same mapping drives the Go read path and the generated
  SQL.

## Where to go next

- **Why it is shaped this way**: [the leeway explanation](../../public/semistructured/leeway/EXPLANATION.md).
- **Reading leeway tables from SQL without spelling physical names**:
  [the SQL read surface](../explanation/leeway-sql-read-surface.md).
- **Components and shared fact tables**: the
  [leeway-components skill](../skills/leeway-components/SKILL.md).
- **The DDL for a ClickHouse server**: `ExampleComposeCreateTable` in
  [ddl/clickhouse/example_test.go](../../public/semistructured/leeway/ddl/clickhouse/example_test.go).
