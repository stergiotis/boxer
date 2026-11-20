// Package common holds the leeway table model every other stage reads: the
// table description, the intermediate representation derived from it, and
// the enumerations that name plain-item lanes, membership channels and
// column roles.
//
// A table is described with a [TableManipulator] from [NewTableManipulator]:
// [TableManipulator.PlainValueColumn] adds a column present on every row,
// [TableManipulator.TaggedValueSection] a section of values labelled by
// membership, and [TableManipulator.BuildTableDesc] validates and returns
// the [TableDesc]. [IntermediateTableRepresentation.LoadFromTable] expands a
// TableDesc into one entry per physical column for a given target, which is
// what the DDL, DML and read-access generators consume. The examples show
// both steps; the leeway EXPLANATION.md shows where they sit in the
// pipeline.
//
// The manipulator's fluent methods do not return errors; problems surface
// from BuildTableDesc, which runs the validator. Check its error rather than
// assuming a chain of calls succeeded.
package common
