// Package panelshapes is the vocabulary of result contracts a play panel can
// render, written as RE2 patterns over the data catalog's normalized schema
// string (ADR-0170 §SD4, §SD5).
//
// A shape is a *battery*: several patterns that must all match. RE2 has no
// lookahead, so a conjunction cannot live inside one regular expression — the
// position-independent claim "has a `lane` column and a `title` column" is two
// patterns, not one. That is the whole reason a shape is a small struct rather
// than a string.
//
// The vocabulary has two faces and one definition. [Shapes] is imported
// directly by the catalog run, which evaluates the batteries in Go; the
// `panel_shapes` provider in
// [github.com/stergiotis/boxer/public/keelson/runtime/introspect/providers]
// serves the same list as a keelson table, so a live session can join the
// vocabulary against boxer.tables_catalog without the catalog having
// materialized that join. Neither face is allowed to have shapes the other does
// not, which is what that package's test pins.
//
// The batteries are matched against **opaque** tables only. A leeway table's
// physical names are the naming grammar's, and a pattern like `;value:` would
// match them for reasons that have nothing to do with what the column means;
// leeway tables reach panels through column handles and the UDF read forms
// instead.
//
// The seed set is small and will be wrong in places. It is data: refining it is
// editing this file and re-running `datacatalog refresh`.
package panelshapes

import (
	"regexp"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// The type fragments a pattern is built from. They are ClickHouse spellings as
// [github.com/stergiotis/boxer/public/gov/datacatalog.NormalizeType] leaves
// them: `LowCardinality` already stripped, a `Nullable` already rewritten to a
// trailing `?` — which is why every fragment is followed by `\??` at the point
// of use rather than spelling nullability itself.
const (
	// NumericType matches a scalar number. Bool is excluded: a panel that asks
	// for a quantity and gets a flag draws a bar chart of zeroes and ones.
	NumericType = `(U?Int(8|16|32|64|128|256)|Float(32|64)|Decimal[0-9]*(\([^;]*\))?)`
	// TemporalType matches the Date/DateTime family, with or without a
	// precision or timezone argument.
	TemporalType = `(Date(Time)?(32|64)?(\([^;]*\))?)`
	// ArrayType matches any Array, whatever it holds.
	ArrayType = `(Array\([^;]*\))`
	// AnyType matches whatever a column carries, up to the terminating
	// sentinel.
	AnyType = `[^;]*`
)

// Shape is one named result contract: a battery of patterns that must all match
// a table's normalized schema string for the table to satisfy it.
//
// Note is prose for a reader of the `panel_shapes` table — what the shape is
// and which panel wants it — not something a consumer parses.
type Shape struct {
	Name     string
	Note     string
	Patterns []string
}

// NamedColumn is the pattern for "a column called name, of any type". The
// leading `;` is the sentinel that makes the name whole: without it, `value:`
// would also match a column called `myvalue`.
func NamedColumn(name string) (pattern string) {
	return `;` + regexp.QuoteMeta(name) + `:`
}

// NamedColumnOfType is the pattern for "a column called name whose type
// matches typeFragment". The trailing `\??;` admits the Nullable marker and
// pins the column's end, so `Float64` does not match the `Float64` inside
// `Array(Float64)`.
func NamedColumnOfType(name string, typeFragment string) (pattern string) {
	return `;` + regexp.QuoteMeta(name) + `:` + typeFragment + `\??;`
}

// AnyColumnOfType is the pattern for "some column, any name, whose type matches
// typeFragment" — what a shape uses when it needs a kind of column rather than
// a named one.
func AnyColumnOfType(typeFragment string) (pattern string) {
	return `;[^;]*:` + typeFragment + `\??;`
}

// Shapes returns the seed battery, in the order the catalog reports matches.
// The slice is freshly built per call, so a caller may sort or filter it
// without disturbing anyone else's copy.
//
// Every entry is grounded in a shipped panel's column contract rather than in
// an idea of what might be useful; the constants those contracts are declared
// with are cited so a rename shows up as a doc mismatch.
func Shapes() (shapes []Shape) {
	return []Shape{
		{
			Name: "series",
			Note: "a temporal column plus at least one number — the Series tab's lanes (ADR-0163). The one panel that detects rather than demands names.",
			Patterns: []string{
				AnyColumnOfType(TemporalType),
				AnyColumnOfType(NumericType),
			},
		},
		{
			Name: "sankey-flows",
			Note: "source, target and a numeric value — the Sankey tab's `flows` CTE (ADR-0159). The value is what separates a flow diagram from a node-link graph.",
			Patterns: []string{
				NamedColumn("source"),
				NamedColumn("target"),
				NamedColumnOfType("value", NumericType),
			},
		},
		{
			Name: "network-edges",
			Note: "source and target — the Network tab's `edges` CTE (ADR-0129). Every sankey-flows table satisfies this one too, which is correct: both panels would draw it.",
			Patterns: []string{
				NamedColumn("source"),
				NamedColumn("target"),
			},
		},
		{
			Name: "kanban-cards",
			Note: "lane and title — the Kanban tab's card contract (ADR-0122). Nothing but intent distinguishes a lane column from a title column, so the panel asks for the names.",
			Patterns: []string{
				NamedColumn("lane"),
				NamedColumn("title"),
			},
		},
		{
			Name: "hierarchy-nodes",
			Note: "id, parent and a numeric value — the node form of the treemap/icicle contract (ADR-0166). The only form in which an interior node carries a value of its own.",
			Patterns: []string{
				NamedColumn("id"),
				NamedColumn("parent"),
				NamedColumnOfType("value", NumericType),
			},
		},
		{
			Name: "hierarchy-folded",
			Note: "an Array-typed stack plus a numeric value — the folded form of the treemap/icicle contract (ADR-0160), which is what a pprof capture already is.",
			Patterns: []string{
				NamedColumnOfType("stack", ArrayType),
				NamedColumnOfType("value", NumericType),
			},
		},
		{
			Name: "distribution",
			Note: "series, n and the ps/qs quantile grid — the Distribution tab's contract (ADR-0161). The grid values are validated at fold time; this only claims the columns are there.",
			Patterns: []string{
				NamedColumn("series"),
				NamedColumnOfType("n", NumericType),
				NamedColumnOfType("ps", ArrayType),
				NamedColumnOfType("qs", ArrayType),
			},
		},
	}
}

// Battery is a compiled shape set, ready to match. Compilation is separated
// from [Shapes] because the provider serves the patterns as text and never
// needs to run them.
type Battery struct {
	shapes []Shape
	res    [][]*regexp.Regexp
}

// NewBattery compiles the seed set from [Shapes].
func NewBattery() (inst *Battery, err error) {
	return NewBatteryFrom(Shapes())
}

// NewBatteryFrom compiles an arbitrary shape set — the seam a test uses to
// exercise the matcher against a shape it authored.
//
// A shape with no patterns is rejected: an empty conjunction is vacuously true
// and would claim every opaque table in the instance.
func NewBatteryFrom(shapes []Shape) (inst *Battery, err error) {
	res := make([][]*regexp.Regexp, 0, len(shapes))
	for _, s := range shapes {
		if len(s.Patterns) == 0 {
			err = eb.Build().Str("shape", s.Name).Errorf("shape has no patterns")
			return
		}
		compiled := make([]*regexp.Regexp, 0, len(s.Patterns))
		for i, p := range s.Patterns {
			var re *regexp.Regexp
			re, err = regexp.Compile(p)
			if err != nil {
				err = eb.Build().Str("shape", s.Name).Int("ordinal", i).Str("pattern", p).
					Errorf("unable to compile pattern: %w", err)
				return
			}
			compiled = append(compiled, re)
		}
		res = append(res, compiled)
	}
	inst = &Battery{shapes: shapes, res: res}
	return
}

// Shapes returns the battery's shape set in declaration order.
func (inst *Battery) Shapes() (shapes []Shape) {
	return inst.shapes
}

// Match returns the names of every shape all of whose patterns match
// normalizedSchema, in the battery's declaration order. A table commonly
// satisfies more than one — the catalog stores a row per (table, shape) rather
// than picking a winner, because which panel a reader wants is not the
// catalog's call.
func (inst *Battery) Match(normalizedSchema string) (names []string) {
	names = make([]string, 0, 2)
	for i, res := range inst.res {
		all := true
		for _, re := range res {
			if !re.MatchString(normalizedSchema) {
				all = false
				break
			}
		}
		if all {
			names = append(names, inst.shapes[i].Name)
		}
	}
	return
}
