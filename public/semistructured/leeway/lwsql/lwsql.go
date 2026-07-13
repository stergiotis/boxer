// Package lwsql bridges leeway physical column names to the nanopass SQL
// pipeline. Its Resolver maps human-friendly column handles onto the technical
// physical column names leeway generates (e.g. `tv:geoPoint:pointLat:val:f32:…`),
// so a nanopass ResolveColumnNames pass can rewrite a readable query into the
// physical one ClickHouse stores. BuildLabels is the inverse, for showing
// friendly labels on result columns without touching the SQL sent to the server.
//
// Handle syntax (a colon is the sole marker; a bare identifier is ordinary SQL):
//
//   - `section:column`  — one column. Sections are the tagged sections
//     (`geoPoint`, `symbol`, …) and six plain/backbone sections derived from
//     the physical item type: id, routing, timestamp, lifecycle, transaction,
//     opaque (so `id:id`, `routing:naturalKey`, …). Any column resolves —
//     value or support — so a `section:column` never false-warns.
//   - `section:*`        — all of the section's *value* columns (the data;
//     support columns are excluded). Expanded wherever it appears, including
//     ARRAY JOIN.
//
// Scope note (v1): a membership-packed field's descriptive identity (e.g.
// `droneStatus` sharing the `symbol` column) is row data, not part of the
// physical name, and is out of scope here — see the ADR.
package lwsql

import (
	"strings"
	"sync"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
)

// Resolver resolves leeway column handles for one endpoint. It implements
// passes.ColumnResolverI. Per-table indexes are built lazily from a
// passes.SchemaProviderI (the physical column list) and cached for the session;
// negatives (non-leeway tables) are cached too, so a table is probed at most
// once.
type Resolver struct {
	provider passes.SchemaProviderI

	mu    sync.Mutex
	cache map[string]*tableIndex // key: db\x00table; nil value == not leeway
}

// tableIndex maps each section (by its style-folded name) to its columns.
type tableIndex struct {
	sections map[string]*sectionIndex
}

// sectionIndex holds one section's columns. byColumn covers ALL of them (value
// and support) so a specific `section:column` never mistakenly reports "no such
// column"; valueCols is the ordered value-column subset used for `section:*`
// expansion and for candidate suggestions.
type sectionIndex struct {
	display   string            // section name as authored, e.g. "geoPoint"
	byColumn  map[string]string // folded column name → physical
	valueCols []columnRef       // value columns only, in order
}

type columnRef struct {
	display  string // column name as authored, e.g. "pointLat"
	physical string
}

// NewResolver builds a Resolver over a schema provider (expected to be caching;
// the Resolver caches the derived indexes, not the raw column lists).
func NewResolver(provider passes.SchemaProviderI) *Resolver {
	return &Resolver{
		provider: provider,
		cache:    make(map[string]*tableIndex, 8),
	}
}

var _ passes.ColumnResolverI = (*Resolver)(nil)

// Resolve implements passes.ColumnResolverI.
func (inst *Resolver) Resolve(dbName string, tableName string, handle string) passes.ResolveResult {
	section, column, isHandle := splitHandle(handle)
	if !isHandle {
		return passes.ResolveResult{Kind: passes.ResolveNotAHandle} // no colon → ordinary SQL
	}
	idx := inst.indexFor(dbName, tableName)
	if idx == nil {
		return passes.ResolveResult{Kind: passes.ResolveNotAHandle} // table not leeway-shaped → can't judge
	}
	si, ok := idx.sections[fold(section)]
	if !ok {
		return passes.ResolveResult{Kind: passes.ResolveUnknownSection, Section: section}
	}
	if column == "*" {
		phys := make([]string, len(si.valueCols))
		for i, c := range si.valueCols {
			phys[i] = c.physical
		}
		return passes.ResolveResult{Kind: passes.ResolveOK, Physical: phys, Section: si.display}
	}
	if p, ok := si.byColumn[fold(column)]; ok {
		return passes.ResolveResult{Kind: passes.ResolveOK, Physical: []string{p}, Section: si.display, Column: column}
	}
	return passes.ResolveResult{
		Kind:       passes.ResolveUnknownColumn,
		Section:    si.display,
		Column:     column,
		Candidates: valueColumnNames(si),
	}
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
	idx = inst.build(dbName, tableName) // build outside the lock (may hit the network)
	inst.mu.Lock()
	if existing, hit := inst.cache[key]; hit {
		idx = existing
	} else {
		inst.cache[key] = idx
	}
	inst.mu.Unlock()
	return idx
}

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

	idx := &tableIndex{sections: make(map[string]*sectionIndex, 8)}
	for _, ci := range infos {
		if ci.section == "" {
			continue
		}
		fs := fold(ci.section)
		si := idx.sections[fs]
		if si == nil {
			si = &sectionIndex{display: ci.section, byColumn: make(map[string]string, 4)}
			idx.sections[fs] = si
		}
		si.byColumn[fold(ci.column)] = ci.physical
		if ci.isValue {
			si.valueCols = append(si.valueCols, columnRef{display: ci.column, physical: ci.physical})
		}
	}
	if len(idx.sections) == 0 {
		return nil
	}
	return idx
}

func valueColumnNames(si *sectionIndex) []string {
	out := make([]string, len(si.valueCols))
	for i, c := range si.valueCols {
		out[i] = c.display
	}
	return out
}

// columnInfo is one physical column decomposed into its section, its column
// name, and whether it is a value column (vs a support column — length, ref,
// cardinality). Plain/backbone columns carry the section name derived from
// their item type and are always value columns. The Resolver and BuildLabels
// both key off this.
type columnInfo struct {
	physical string
	section  string
	column   string
	isValue  bool
}

// classifyColumns parses a table's physical column names and classifies each.
// ok is false when the names are not leeway-shaped (a plain SQL table, an
// aggregation result, an unreachable server).
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
	// pairs are value columns; support columns are excluded from that set.
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
		ci := columnInfo{physical: names[i], column: string(col)}
		if sec != "" {
			ci.section = string(sec)
			_, ci.isValue = valueCols[fold(string(sec))+"\x00"+fold(string(col))]
		} else {
			// Plain/backbone column — its section is its item-type name.
			pit, pErr := conv.ExtractPlainItemType(phy)
			if pErr != nil {
				continue
			}
			ci.section = plainSectionName(pit)
			if ci.section == "" {
				continue // unmapped item type
			}
			ci.isValue = true
		}
		infos = append(infos, ci)
	}
	return infos, true
}

// plainSectionName maps a plain/backbone item type to its user-facing section
// name (the six TableDescDto plain groups). Empty for PlainItemTypeNone.
func plainSectionName(pit common.PlainItemTypeE) string {
	switch pit {
	case common.PlainItemTypeEntityId:
		return "id"
	case common.PlainItemTypeEntityTimestamp:
		return "timestamp"
	case common.PlainItemTypeEntityRouting:
		return "routing"
	case common.PlainItemTypeEntityLifecycle:
		return "lifecycle"
	case common.PlainItemTypeTransaction:
		return "transaction"
	case common.PlainItemTypeOpaque:
		return "opaque"
	}
	return ""
}

// splitHandle splits `section:column` on its single ':'. isHandle is false
// unless there is exactly one colon: a bare identifier (none) is ordinary SQL,
// and a physical name typed verbatim (`tv:symbol:value:…`, many colons) is not
// a handle either — it must pass through untouched, not warn as an "unknown
// section". Section and column names cannot themselves contain a colon, so
// exactly one is the rule.
func splitHandle(handle string) (section string, column string, isHandle bool) {
	if strings.Count(handle, ":") != 1 {
		return "", "", false
	}
	sec, col, _ := strings.Cut(handle, ":")
	return sec, col, true
}

// fold renders a name component to LowerSpinalCase, the style-independent
// canonical form (naming.Compare uses the same reduction), so `geoPoint`,
// `geo_point`, and `geo-point` collapse to one key.
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
