// Package ra is the generated read side of the boxer.facts table: typed
// readers over the table's plain columns and tagged-value sections
// (runtime_facts_ra.out.go), and the canonical wire encoder bound to them
// (runtime_facts_canonwire.out.go).
//
// Both files are written by the factsschema codegen package from the schema
// in factsschema; change the schema or the generator and regenerate rather
// than edit them. The readers follow the leeway readaccess/runtime
// contract: they address columns by position and retain what they load
// until Release.
package ra
