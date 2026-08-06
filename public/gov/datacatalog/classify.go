package datacatalog

import (
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
)

// ErrNoColumns is why a table with no columns classifies as opaque. Discovery
// itself accepts an empty name list and returns an empty table — vacuously a
// valid leeway schema — but a table that carries no evidence is not a leeway
// table, and calling it one would put an attribute-less row into
// boxer.tables_leeway and an empty-set node into every pair it takes part in.
var ErrNoColumns = eh.Errorf("no columns to classify")

// KindE is how a table's physical column names classified under the leeway
// naming grammar. There are exactly two answers because the grammar itself
// gives exactly two: discovery succeeded, or it did not.
type KindE uint8

const (
	// KindOpaque: the column names do not parse as leeway physical names. An
	// expected, fully supported answer — most tables anyone writes by hand are
	// opaque, and only opaque tables are matched against the panel shapes.
	KindOpaque KindE = iota
	// KindLeeway: the names parsed and a [common.TableDesc] was rebuilt from
	// them.
	KindLeeway
)

// AllKinds is the enum's domain, in the order the catalog's Enum8 column
// declares it.
var AllKinds = []KindE{KindOpaque, KindLeeway}

func (inst KindE) String() (s string) {
	switch inst {
	case KindOpaque:
		return "opaque"
	case KindLeeway:
		return "leeway"
	}
	return "invalid"
}

// Classification is the outcome of one probe. Table, RowConfig and Convention
// are meaningful only when Kind is [KindLeeway]; Err is non-nil exactly when it
// is not, and carries why the names did not parse.
//
// Convention is handed back because a caller that goes on to build an IR or a
// streamreadaccess driver needs the very convention discovery ran under — the
// separator it was constructed with is part of the answer, not a detail.
type Classification struct {
	Kind       KindE
	Separator  string
	Table      *common.TableDesc
	RowConfig  common.TableRowConfigE
	Convention common.NamingConventionI
	Err        error
}

// Detail renders Err for the catalog's classify_detail column: empty when the
// table classified as leeway, the parse failure otherwise. It is a diagnostic —
// "looks leeway but does not parse" is a readable row rather than an absence —
// and not something to match on.
func (inst Classification) Detail() (s string) {
	if inst.Err == nil {
		return ""
	}
	return inst.Err.Error()
}

// SniffSeparator picks the naming-convention separator a set of physical column
// names is authored under.
//
// The leeway flat-name format spells nested tags as `<head><sep><tag>` (e.g.
// `metric:env`); the canonical separator is `:`, but ClickHouse table dumps
// mangle it to `_` because `:` is illegal in CH column identifiers. The first
// non-leading-underscore column settles the question: a `:` anywhere in the
// name picks the canonical convention, otherwise the CH-mangled fallback `_`.
//
// Columns whose name starts with `_` are reserved for later / implicit /
// opaque schema columns that aren't authored under either convention, so they
// can't be used as evidence either way — they are skipped and the next column
// is looked at. A name list that is empty, or entirely `_`-prefixed, yields the
// fallback.
func SniffSeparator(columnNames []string) (separator string) {
	separator = "_"
	for _, n := range columnNames {
		if strings.HasPrefix(n, "_") {
			continue
		}
		if strings.ContainsRune(n, ':') {
			separator = ":"
		}
		break
	}
	return
}

// Classify probes a set of physical column names against the leeway naming
// grammar: sniff the separator ([SniffSeparator]), attempt
// DiscoverTableFromColumnNames, and call the table leeway iff discovery
// succeeds.
//
// This is the single classifier both the catalog and play's card driver run,
// so "is this result set leeway-shaped" has one answer in the codebase rather
// than two that drift. A failure is not a fault: an aggregation, a join, or any
// arbitrary SQL result is opaque and expected, which is why the error is
// returned in the Classification instead of raised.
//
// A table that would parse under both separators takes the sniffed one. That is
// deliberate — the sniff, not a search, is the contract, so the same names
// classify the same way wherever they are seen.
//
// An empty column list is opaque with [ErrNoColumns]; see there for why the
// discovery layer's vacuous success is not taken at face value.
func Classify(columnNames []string) (cl Classification) {
	cl.Separator = SniffSeparator(columnNames)
	if len(columnNames) == 0 {
		cl.Err = ErrNoColumns
		return
	}
	conv, err := ddl.NewHumanReadableNamingConvention(cl.Separator)
	if err != nil {
		cl.Err = err
		return
	}
	tblDesc, rowConfig, err := conv.DiscoverTableFromColumnNames(columnNames)
	if err != nil {
		cl.Err = err
		return
	}
	cl.Kind = KindLeeway
	cl.Table = &tblDesc
	cl.RowConfig = rowConfig
	cl.Convention = conv
	return
}
