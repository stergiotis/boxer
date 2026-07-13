// Package lwsql bridges leeway physical column names to the nanopass SQL
// pipeline. Its Resolver maps human-friendly column handles — a section name,
// or a quoted `section:column` composite — onto the technical physical column
// names leeway generates (e.g. `tv:geoPoint:lat:val:f64:…`), so that a
// nanopass ResolveColumnNames pass can rewrite a readable query into the
// physical one ClickHouse stores. BuildLabels is the inverse, used to show
// friendly labels for result columns without touching the SQL sent to the
// server.
//
// Only value columns are exposed as handles. Support columns (length, ref,
// cardinality) are named after their role and excluded — a table's value
// columns are the authority, taken from the reconstructed TableDesc. The
// friendly vocabulary here is exactly the one the schemaview widget shows, so
// the browser and the query resolver speak the same names.
//
// Scope note (v1): the descriptive identity of a membership-packed field
// (e.g. `droneStatus` sharing the `symbol` column) is row data, not part of
// the physical name, and is out of scope here — see the ADR.
package lwsql

import (
	"strings"
	"sync"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// Resolver resolves friendly leeway column handles to physical names for one
// endpoint. It implements passes.ColumnResolverI. Per-table indexes are built
// lazily from a passes.SchemaProviderI (which supplies the physical column
// list) and cached for the session; negatives (non-leeway tables) are cached
// too so a table is probed at most once.
type Resolver struct {
	provider passes.SchemaProviderI

	mu    sync.Mutex
	cache map[string]*tableIndex // key: db\x00table; nil value == not leeway
}

// tableIndex maps a folded handle to the physical column name(s) it selects.
// A bare section with more than one value column maps to several names — that
// is ambiguous and deliberately left unresolved (the user must pick a column
// via `section:column`).
type tableIndex struct {
	byHandle map[string][]string
}

// NewResolver builds a Resolver over a schema provider. The provider is
// expected to be caching (see passes.NewCachingSchemaProvider) — the Resolver
// caches the derived indexes, not the raw column lists.
func NewResolver(provider passes.SchemaProviderI) *Resolver {
	return &Resolver{
		provider: provider,
		cache:    make(map[string]*tableIndex, 8),
	}
}

var _ passes.ColumnResolverI = (*Resolver)(nil)

// Resolve implements passes.ColumnResolverI. It returns ok=false for a handle
// that names no value column, or that is ambiguous — the pass then leaves the
// identifier untouched.
func (inst *Resolver) Resolve(dbName string, tableName string, handle string) (physical string, ok bool) {
	idx := inst.indexFor(dbName, tableName)
	if idx == nil {
		return
	}
	phys := idx.byHandle[foldHandle(handle)]
	if len(phys) == 1 {
		return phys[0], true
	}
	return
}

// Reset clears the cached indexes — call when the endpoint or its schema may
// have changed (e.g. after switching the target server).
func (inst *Resolver) Reset() {
	inst.mu.Lock()
	inst.cache = make(map[string]*tableIndex, 8)
	inst.mu.Unlock()
}

func (inst *Resolver) indexFor(dbName string, tableName string) *tableIndex {
	key := dbName + "\x00" + tableName
	inst.mu.Lock()
	idx, hit := inst.cache[key]
	inst.mu.Unlock()
	if hit {
		return idx
	}
	// Build outside the lock — building fetches columns, which can hit the
	// network. A concurrent duplicate build is harmless (idempotent).
	idx = inst.build(dbName, tableName)
	inst.mu.Lock()
	if existing, hit := inst.cache[key]; hit {
		idx = existing
	} else {
		inst.cache[key] = idx
	}
	inst.mu.Unlock()
	return idx
}

// build derives the friendly index for one table. Any failure to parse the
// columns as leeway physical names (a plain SQL table, an aggregation result,
// an unreachable server) yields a nil index — the table simply has no handles.
func (inst *Resolver) build(dbName string, tableName string) *tableIndex {
	cols, n, found := inst.provider.GetColumns(dbName, tableName)
	if !found || n == 0 {
		return nil
	}
	names := make([]string, 0, n)
	for c := range cols {
		names = append(names, c)
	}
	infos, ok := classifyColumns(names)
	if !ok {
		return nil // not leeway-shaped
	}

	idx := &tableIndex{byHandle: make(map[string][]string, len(infos))}
	for _, ci := range infos {
		if !ci.isValue {
			continue
		}
		if ci.section == "" {
			// Plain / backbone column (id, ts, naturalKey, …) — bare name.
			fc := fold(ci.column)
			idx.byHandle[fc] = append(idx.byHandle[fc], ci.physical)
			continue
		}
		// A section resolves bare (all its value columns) and by `section:column`.
		fs, fc := fold(ci.section), fold(ci.column)
		idx.byHandle[fs] = append(idx.byHandle[fs], ci.physical)
		idx.byHandle[fs+":"+fc] = append(idx.byHandle[fs+":"+fc], ci.physical)
	}
	if len(idx.byHandle) == 0 {
		return nil
	}
	return idx
}

// columnInfo is one physical column decomposed into its section (empty for a
// plain/backbone column), its leeway column name, and whether it is a
// user-facing value column. Support columns (length, ref, cardinality) are
// named after their role and are not value columns. The Resolver and
// BuildLabels both key off this, so they expose exactly the same vocabulary.
type columnInfo struct {
	physical string
	section  string
	column   string
	isValue  bool
}

// classifyColumns parses a table's physical column names and classifies each.
// ok is false when the names are not leeway-shaped (a plain SQL table, an
// aggregation result, an unreachable server) — there are then no handles or
// labels to offer.
func classifyColumns(names []string) (infos []columnInfo, ok bool) {
	conv, err := ddl.NewHumanReadableNamingConvention(detectSeparator(names))
	if err != nil {
		return nil, false
	}
	phys, err := conv.ParseColumns(names)
	if err != nil {
		return nil, false
	}
	table, _, err := conv.DiscoverTableFromPhysicalColumns(phys)
	if err != nil {
		return nil, false
	}
	// The reconstructed TableDesc is the authority for which (section, column)
	// pairs are value columns; support columns are excluded.
	valueCols := make(map[string]struct{}, len(table.TaggedValuesSections))
	for _, sec := range table.TaggedValuesSections {
		fs := fold(string(sec.Name))
		for _, vcn := range sec.ValueColumnNames {
			valueCols[fs+"\x00"+fold(string(vcn))] = struct{}{}
		}
	}
	infos = make([]columnInfo, 0, len(phys))
	for i, phy := range phys {
		col, colErr := conv.ExtractLeewayColumnName(phy)
		if colErr != nil {
			continue
		}
		sec, secErr := conv.ExtractSectionName(phy)
		if secErr != nil {
			continue
		}
		ci := columnInfo{physical: names[i], section: string(sec), column: string(col)}
		if sec == "" {
			ci.isValue = true // plain/backbone columns are user-facing
		} else {
			_, ci.isValue = valueCols[fold(string(sec))+"\x00"+fold(string(col))]
		}
		infos = append(infos, ci)
	}
	return infos, true
}

// foldHandle canonicalises a user-typed handle to the same key form the index
// is built with: split on the first ':' into section[:column] and fold each
// part to LowerSpinalCase so any naming style the user types matches.
func foldHandle(handle string) string {
	if sec, col, found := strings.Cut(handle, ":"); found {
		return fold(sec) + ":" + fold(col)
	}
	return fold(handle)
}

// fold renders a name component to LowerSpinalCase, the style-independent
// canonical form (naming.Compare uses the same reduction), so `geoPoint`,
// `geo_point`, and `geo-point` all collapse to one key.
func fold(s string) string {
	return naming.ConvertNameStyle(strings.TrimSpace(s), naming.LowerSpinalCase)
}

// detectSeparator mirrors the play CardDriver's heuristic: leeway physical
// names join components with ':', but a ClickHouse table dump can mangle that
// to '_'. The first non-underscore-prefixed column with a ':' settles it.
func detectSeparator(names []string) string {
	for _, n := range names {
		if strings.HasPrefix(n, "_") {
			continue
		}
		if strings.ContainsRune(n, ':') {
			return ":"
		}
		break
	}
	return "_"
}
