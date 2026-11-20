// Package runtime is the support code leeway-generated read sides import:
// the interfaces generated readers implement ([ColumnIndexHandlingI],
// [SectionMethodsI], one InAttributeMembership…I per membership channel),
// the index types ([EntityIdx], [AttributeIdx]) and the lookup accelerators
// that map an entity to its attributes in a loaded record.
//
// Application code holds the generated readers rather than this package.
// Two things about them come from here. Readers address columns by
// position: the generated defaults are the schema's own column order, so a
// record from a query that projects or reorders columns needs
// SetColumnIndices with that record's positions before LoadFromRecord. And
// readers retain the Arrow arrays they load ([ReleasableI]); call Release
// when done.
package runtime
