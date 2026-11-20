// Package dml generates the write side of a leeway table: Go types that
// append entities, their plain values and their tagged-value sections to
// Arrow record builders, and hand back finished records.
//
// [NewGoCodeGeneratorDriver] takes a naming convention and a target
// technology; its GenerateGoClasses renders the source for one table, which
// a package commits as a *.out.go file and regenerates from a test. The
// generated code imports the dml/runtime package. [WriteArrowRecords]
// writes the records a generated table transfers to an Arrow IPC file. The
// tutorial/firsttable package is the smallest complete use.
//
// Generated builders record errors instead of returning them from each
// call (the runtime's ErrorHandlingI); the error surfaces from CommitEntity
// or CheckErrors, not at the call that caused it, so check the error
// CommitEntity returns for every entity.
package dml
