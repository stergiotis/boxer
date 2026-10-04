package jackstay

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/gov/datacatalog"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
)

// PlanFormatVersion is the plan document's format version. [LoadPlan] refuses
// any other.
const PlanFormatVersion = uint32(1)

// Selection is what the operator chose on the Databases step: which source
// databases take part, where each lands on the target, and whether only
// leeway tables are planned.
type Selection struct {
	// Databases are source database names; empty selects every non-system
	// database.
	Databases []string `json:"databases"`
	// DatabaseMap renames a source database on the target. A database with no
	// entry keeps its name.
	DatabaseMap map[string]string `json:"databaseMap,omitempty"`
	LeewayOnly  bool              `json:"leewayOnly"`
	// Filters are row filters by source table ("database.name"): each is a
	// boolean expression over the copied columns, and the table is planned,
	// diffed and synced as the slice it selects (ADR-0271 §SD1). A key is
	// matched against the source's tables, not split at a dot, so dotted
	// names resolve.
	Filters map[string]string `json:"filters,omitempty"`
}

func (inst *Selection) TargetDatabase(source string) (target string) {
	if t, has := inst.DatabaseMap[source]; has && t != "" {
		return t
	}
	return source
}

// PlanTable is one source table's entry in the plan.
type PlanTable struct {
	Source       datacatalog.TableRef `json:"source"`
	Target       datacatalog.TableRef `json:"target"`
	Engine       string               `json:"engine"`
	TargetEngine string               `json:"targetEngine,omitempty"`
	SortingKey   string               `json:"sortingKey"`
	PartitionKey string               `json:"partitionKey"`
	// TargetPartitionKey decides whether a partition chunk can be dropped
	// on the target as a partition.
	TargetPartitionKey string `json:"targetPartitionKey,omitempty"`
	Rows               uint64 `json:"rows"`
	Bytes              uint64 `json:"bytes"`
	// Filter is the row filter the table was planned with: the operator's
	// from [Selection.Filters], or the one a pack source was exported under.
	Filter       string `json:"filter,omitempty"`
	TableVerdict `json:",inline"`
	// Chunking is set by the first diff and carried over by later plans of
	// the same table (see [Plan.CarryOver]).
	Chunking *Chunking `json:"chunking,omitempty"`
	// Diff is the latest content diff; a new structure step drops it.
	Diff *TableDiff `json:"diff,omitempty"`
	// Sync is the operator's choice on the Sync step; SyncReport the latest
	// outcome of the run named by [Plan.SyncRun].
	Sync       *TableSync       `json:"sync,omitempty"`
	SyncReport *TableSyncReport `json:"syncReport,omitempty"`
}

// IsDiffable reports whether the table's content can be compared: both sides
// hold it with the same key and every copied column, so no DDL is pending.
func (inst *PlanTable) IsDiffable() (ok bool) {
	return inst.Verdict == VerdictIdentical || inst.Verdict == VerdictNarrower
}

// Plan is the unit of work of ADR-0259 §SD1. Each step adds its section; this
// milestone writes the discovery and structure sections.
type Plan struct {
	FormatVersion uint32     `json:"formatVersion"`
	CreatedAt     time.Time  `json:"createdAt"`
	Source        Endpoint   `json:"source"`
	Target        Endpoint   `json:"target"`
	SourceServer  ServerInfo `json:"sourceServer"`
	TargetServer  ServerInfo `json:"targetServer"`
	Selection     Selection  `json:"selection"`
	// Notes are plan-wide facts, such as a version gap between the servers.
	Notes []string `json:"notes,omitempty"`
	// DatabaseDDL creates the target databases the tables' DDL needs. It runs
	// before any table DDL.
	DatabaseDDL []string    `json:"databaseDdl,omitempty"`
	Tables      []PlanTable `json:"tables"`
	// SyncRun names the sync run the journal beside the plan belongs to.
	SyncRun *SyncRun `json:"syncRun,omitempty"`
}

// CountVerdicts tallies the tables per verdict.
func (inst *Plan) CountVerdicts() (counts map[VerdictE]int) {
	counts = make(map[VerdictE]int, len(AllVerdicts))
	for _, t := range inst.Tables {
		counts[t.Verdict]++
	}
	return
}

// HasPendingDDL reports whether applying the structure step would run any
// statement.
func (inst *Plan) HasPendingDDL() (has bool) {
	if len(inst.DatabaseDDL) > 0 {
		return true
	}
	for _, t := range inst.Tables {
		if len(t.DDL) > 0 {
			return true
		}
	}
	return false
}

// BuildPlan runs discovery's comparison and the structure step: every selected
// source table is judged against the target (ADR-0259 §SD2, §SD3). The
// inventories are the caller's, so the same function serves a fresh plan and
// [Restate]'s re-check.
func BuildPlan(ops *common.TableOperations, srcEp Endpoint, dstEp Endpoint, src *Inventory, dst *Inventory, sel Selection, now time.Time) (plan Plan, err error) {
	sel, err = resolveSelection(src, sel)
	if err != nil {
		return
	}
	plan = Plan{
		FormatVersion: PlanFormatVersion,
		CreatedAt:     now.UTC(),
		Source:        srcEp,
		Target:        dstEp,
		SourceServer:  src.Server,
		TargetServer:  dst.Server,
		Selection:     sel,
	}
	if note := versionGapNote(src.Server.Version, dst.Server.Version); note != "" {
		plan.Notes = append(plan.Notes, note)
	}

	sameServer := IsSameServer(srcEp, dstEp, src.Server, dst.Server)
	targets := make(map[datacatalog.TableRef]datacatalog.TableRef, len(src.Tables))
	needDatabase := make([]string, 0, 4)
	plan.Tables = make([]PlanTable, 0, len(src.Tables))
	for i := range src.Tables {
		st := &src.Tables[i]
		if !slices.Contains(sel.Databases, st.Ref.Database) {
			continue
		}
		target := datacatalog.TableRef{Database: sel.TargetDatabase(st.Ref.Database), Name: st.Ref.Name}
		if sameServer && target == st.Ref {
			err = eb.Build().Str("table", st.Ref.String()).Errorf("source and target are the same table; map the database to another name")
			return
		}
		dt, _ := dst.Table(target)
		targetEngine, targetPartitionKey := "", ""
		if dt != nil {
			targetEngine, targetPartitionKey = dt.Engine, dt.PartitionKey
		}
		var v TableVerdict
		v, err = Judge(ops, st, dt, target)
		if err != nil {
			err = eb.Build().Str("table", st.Ref.String()).Errorf("unable to judge table: %w", err)
			return
		}
		v.HashAsText = jsonColumns(st, v.CopyColumns)
		if sel.LeewayOnly && !v.Leeway {
			continue
		}
		var filter string
		filter, err = tableFilter(st, sel, &v)
		if err != nil {
			return
		}
		if other, taken := targets[target]; taken {
			err = eb.Build().Str("table", st.Ref.String()).Str("other", other.String()).Str("target", target.String()).
				Errorf("two source tables map onto one target table")
			return
		}
		targets[target] = st.Ref
		if v.Verdict == VerdictCreate && !dst.HasDatabase(target.Database) && !slices.Contains(needDatabase, target.Database) {
			needDatabase = append(needDatabase, target.Database)
		}
		plan.Tables = append(plan.Tables, PlanTable{
			Source:             st.Ref,
			Target:             target,
			Engine:             st.Engine,
			TargetEngine:       targetEngine,
			TargetPartitionKey: targetPartitionKey,
			SortingKey:         st.SortingKey,
			PartitionKey:       st.PartitionKey,
			Rows:               st.TotalRows,
			Bytes:              st.TotalBytes,
			Filter:             filter,
			TableVerdict:       v,
		})
	}
	if sameServer {
		// A chain (a.t → b.t and b.t → c.t) writes a table the same plan
		// reads, so no diff or sync of either step would hold still.
		for i := range plan.Tables {
			if j := slices.IndexFunc(plan.Tables, func(t PlanTable) bool { return t.Source == plan.Tables[i].Target }); j >= 0 {
				err = eb.Build().Str("table", plan.Tables[i].Source.String()).Str("target", plan.Tables[i].Target.String()).
					Errorf("the target table is also a source of this plan; map the databases so no table is both")
				return
			}
		}
	}
	for _, db := range needDatabase {
		plan.DatabaseDDL = append(plan.DatabaseDDL, CreateDatabaseDDL(db))
	}
	for ref := range sel.Filters {
		found := false
		for i := range plan.Tables {
			if plan.Tables[i].Source.String() == ref {
				found = true
				break
			}
		}
		if !found {
			err = eb.Build().Str("table", ref).Errorf("a filter names a table the plan does not hold")
			return
		}
	}
	return
}

// tableFilter is the row filter a table is planned with. A pack source holds
// only the slice it was exported under ([TableInfo.Filter]); the operator may
// repeat that filter but not choose another. Either filter is validated
// against the copy column list when the table can be synced at all, since a
// pack's manifest is as open to editing as a plan, and its notes join the
// verdict's.
func tableFilter(st *TableInfo, sel Selection, v *TableVerdict) (filter string, err error) {
	filter = strings.TrimSpace(sel.Filters[st.Ref.String()])
	if st.Filter != "" {
		if filter != "" && filter != st.Filter {
			err = eb.Build().Str("table", st.Ref.String()).Str("filter", filter).Str("exported", st.Filter).
				Errorf("the pack holds only the rows of the filter it was exported under")
			return
		}
		filter = st.Filter
		v.Notes = append(v.Notes, "rows as exported under the filter "+filter)
	}
	if filter == "" || !v.Verdict.IsSyncable() {
		return
	}
	columns := v.CopyColumns
	if st.Filter != "" {
		columns = packFilterColumns(columns, st.SortingKey)
	}
	var notes []string
	notes, err = ValidateFilter(filter, columns)
	if err != nil {
		err = eb.Build().Str("table", st.Ref.String()).Errorf("invalid filter: %w", err)
		return
	}
	v.Notes = append(v.Notes, notes...)
	return
}

// IsSameServer reports whether source and target are one server: the same URL,
// or the same serverUUID() behind two URLs (localhost and 127.0.0.1, say).
func IsSameServer(srcEp Endpoint, dstEp Endpoint, src ServerInfo, dst ServerInfo) (same bool) {
	const nilUUID = "00000000-0000-0000-0000-000000000000"
	if srcEp.Pack != "" || dstEp.Pack != "" {
		return false
	}
	if srcEp.URL == dstEp.URL {
		return true
	}
	return src.UUID != "" && src.UUID != nilUUID && src.UUID == dst.UUID
}

// CarryOver copies each table's chunk layout from old, a previous plan, when
// the table's source keys are unchanged: keeping the layout is what lets a
// later diff compare chunk for chunk with an earlier one (ADR-0259 §SD4).
// With diffs, it also carries the diff, sync settings and sync report of each
// table whose layout it kept and whose copied columns are unchanged, since
// the digests a diff holds were taken over those columns; the sync run comes
// with them. A structure step passes false: it makes every diff stale.
func (inst *Plan) CarryOver(old *Plan, withDiffs bool) {
	if old == nil {
		return
	}
	prev := make(map[datacatalog.TableRef]*PlanTable, len(old.Tables))
	for i := range old.Tables {
		prev[old.Tables[i].Source] = &old.Tables[i]
	}
	for i := range inst.Tables {
		t := &inst.Tables[i]
		o := prev[t.Source]
		if o == nil || o.Chunking == nil || o.SortingKey != t.SortingKey || o.PartitionKey != t.PartitionKey {
			continue
		}
		// Range bounds sampled over the whole table stay correct for any
		// slice of it, so the layout survives a filter change; the digests
		// below do not.
		c := *o.Chunking
		c.Bounds = slices.Clone(o.Chunking.Bounds)
		c.Exprs = slices.Clone(o.Chunking.Exprs)
		t.Chunking = &c
		if !withDiffs || !slices.Equal(o.CopyColumns, t.CopyColumns) || o.Filter != t.Filter {
			continue
		}
		if o.Diff != nil {
			d := *o.Diff
			t.Diff = &d
		}
		if o.Sync != nil {
			sy := *o.Sync
			t.Sync = &sy
		}
		if o.SyncReport != nil {
			r := *o.SyncReport
			t.SyncReport = &r
		}
	}
	if withDiffs && old.SyncRun != nil {
		run := *old.SyncRun
		inst.SyncRun = &run
	}
}

func resolveSelection(src *Inventory, sel Selection) (out Selection, err error) {
	out = Selection{LeewayOnly: sel.LeewayOnly}
	if len(sel.Databases) == 0 {
		out.Databases = src.UserDatabases()
	} else {
		out.Databases = slices.Clone(sel.Databases)
		slices.Sort(out.Databases)
		out.Databases = slices.Compact(out.Databases)
		for _, db := range out.Databases {
			if datacatalog.IsSystemDatabase(db) {
				err = eb.Build().Str("database", db).Errorf("system databases are not synced")
				return
			}
			if !src.HasDatabase(db) {
				err = eb.Build().Str("database", db).Errorf("database not found on the source")
				return
			}
		}
	}
	for from, to := range sel.DatabaseMap {
		if !slices.Contains(out.Databases, from) {
			err = eb.Build().Str("database", from).Errorf("mapped database is not selected")
			return
		}
		if to == "" || datacatalog.IsSystemDatabase(to) {
			err = eb.Build().Str("database", from).Str("target", to).Errorf("invalid target database name")
			return
		}
		if to != from {
			if out.DatabaseMap == nil {
				out.DatabaseMap = make(map[string]string, len(sel.DatabaseMap))
			}
			out.DatabaseMap[from] = to
		}
	}
	for key, f := range sel.Filters {
		if strings.TrimSpace(f) == "" {
			continue
		}
		var tbl datacatalog.TableRef
		tbl, err = filterTable(src, out.Databases, key)
		if err != nil {
			return
		}
		if !slices.Contains(out.Databases, tbl.Database) {
			err = eb.Build().Str("table", key).Errorf("a filter names a table outside the selected databases")
			return
		}
		if out.Filters == nil {
			out.Filters = make(map[string]string, len(sel.Filters))
		}
		out.Filters[key] = strings.TrimSpace(f)
	}
	return
}

// filterTable resolves a [Selection.Filters] key. The key is
// [datacatalog.TableRef.String], database and name joined by a dot, which
// cannot be split back when either holds a dot; it is matched against the
// source's tables instead, those of the selected databases first. A key two
// selected tables share is refused. A key no table matches is resolved by
// its first dot, so the caller can say where it points.
func filterTable(src *Inventory, databases []string, key string) (tbl datacatalog.TableRef, err error) {
	var selected, other []datacatalog.TableRef
	for i := range src.Tables {
		r := src.Tables[i].Ref
		if r.String() != key {
			continue
		}
		if slices.Contains(databases, r.Database) {
			selected = append(selected, r)
		} else {
			other = append(other, r)
		}
	}
	switch {
	case len(selected) == 1:
		tbl = selected[0]
	case len(selected) > 1:
		err = eb.Build().Str("table", key).Str("one", selected[0].Database+" / "+selected[0].Name).Str("other", selected[1].Database+" / "+selected[1].Name).
			Errorf("a filter key names two selected tables, their database and table names joined by a dot read alike")
	case len(other) > 0:
		tbl = other[0]
	default:
		db, name, _ := strings.Cut(key, ".")
		tbl = datacatalog.TableRef{Database: db, Name: name}
	}
	return
}

// versionGapNote describes a difference in the servers' year.month release, the
// granularity at which ClickHouse's formats and functions move. The premise of
// ADR-0259 is close versions; the note leaves the judgement to the operator.
func versionGapNote(src string, dst string) (note string) {
	release := func(v string) string {
		parts := strings.SplitN(v, ".", 3)
		if len(parts) < 2 {
			return v
		}
		return parts[0] + "." + parts[1]
	}
	if release(src) == release(dst) {
		return
	}
	return "server releases differ: source " + src + ", target " + dst + "; hashes and Native encodings are assumed to agree"
}

// Marshal is the plan document's bytes, as [Plan.Save] writes them.
func (inst *Plan) Marshal() (data []byte, err error) {
	data, err = json.Marshal(inst, json.Deterministic(true), jsontext.Multiline(true), jsontext.WithIndent("  "))
	if err != nil {
		err = eh.Errorf("unable to encode plan: %w", err)
		return
	}
	data = append(data, '\n')
	return
}

// Save writes the plan atomically: to a temporary file in the same directory,
// then renamed over path.
func (inst *Plan) Save(path string) (err error) {
	return inst.SaveIn(OsFiles{}, path)
}

// SaveIn writes the plan to name in files, atomically.
func (inst *Plan) SaveIn(files FilesI, name string) (err error) {
	var data []byte
	data, err = inst.Marshal()
	if err != nil {
		return
	}
	err = files.WriteFile(name, data)
	if err != nil {
		err = eb.Build().Str("name", name).Errorf("unable to write plan: %w", err)
	}
	return
}

// LoadPlan reads a plan written by [Plan.Save].
func LoadPlan(path string) (plan Plan, err error) {
	return LoadPlanIn(OsFiles{}, path)
}

// LoadPlanIn reads a plan written by [Plan.SaveIn]. A missing file is an
// error wrapping fs.ErrNotExist.
func LoadPlanIn(files FilesI, name string) (plan Plan, err error) {
	var data []byte
	data, err = files.ReadFile(name)
	if err != nil {
		err = eb.Build().Str("name", name).Errorf("unable to read plan: %w", err)
		return
	}
	plan, err = ParsePlan(data)
	if err != nil {
		err = eb.Build().Str("name", name).Errorf("unable to load plan: %w", err)
	}
	return
}

// ParsePlan decodes and validates a plan document.
func ParsePlan(data []byte) (plan Plan, err error) {
	err = json.Unmarshal(data, &plan)
	if err != nil {
		err = eh.Errorf("unable to decode plan: %w", err)
		return
	}
	plan.dropUnorderedRanges()
	err = plan.Validate()
	if err != nil {
		err = eh.Errorf("invalid plan: %w", err)
	}
	return
}

// dropUnorderedRanges forgets a range layout this version cannot order: a
// plan written before range chunking was limited to the key types whose
// bound text orders as the values do (a UUID, an IP address, an Enum) or
// sampled bounds that do not strictly ascend. Such a layout could assign a
// row to a chunk whose predicate selects nothing. The layout is derived again
// by the next comparison, and the comparison it described goes with it.
func (inst *Plan) dropUnorderedRanges() {
	for i := range inst.Tables {
		t := &inst.Tables[i]
		if t.Chunking == nil || t.Chunking.Kind != ChunkingRange || t.Chunking.validate() == nil {
			continue
		}
		t.Chunking, t.Diff = nil, nil
		inst.Notes = append(inst.Notes, t.Source.String()+": its range chunk layout cannot be ordered and was dropped; compare the content again")
	}
}

// Validate checks what a hand-edited or foreign plan could get wrong and the
// steps would otherwise act on silently: the format version, the endpoints,
// every table's references and row filter, and each chunk layout's bounds
// and leaf count.
func (inst *Plan) Validate() (err error) {
	if inst.FormatVersion != PlanFormatVersion {
		return eb.Build().Uint64("formatVersion", uint64(inst.FormatVersion)).Uint64("supported", uint64(PlanFormatVersion)).
			Errorf("unsupported plan format version")
	}
	if (inst.Source.URL == "") == (inst.Source.Pack == "") || inst.Target.URL == "" {
		return eh.Errorf("plan names no source (a server or a pack) or no target server")
	}
	for i := range inst.Tables {
		t := &inst.Tables[i]
		if t.Source.Database == "" || t.Source.Name == "" || t.Target.Database == "" || t.Target.Name == "" {
			return eb.Build().Int("table", i).Errorf("table entry names no source or no target table")
		}
		if c := t.Chunking; c != nil {
			if e := c.validate(); e != nil {
				return eb.Build().Str("table", t.Source.String()).Errorf("invalid chunk layout: %w", e)
			}
		}
		if f := inst.Selection.Filters[t.Source.String()]; f != "" && f != t.Filter {
			return eb.Build().Str("table", t.Source.String()).Errorf("table's filter differs from the selection's")
		}
		if t.Filter != "" && t.Verdict.IsSyncable() {
			columns := t.CopyColumns
			if inst.Source.Pack != "" {
				columns = packFilterColumns(columns, t.SortingKey)
			}
			if _, e := ValidateFilter(t.Filter, columns); e != nil {
				return eb.Build().Str("table", t.Source.String()).Errorf("invalid filter: %w", e)
			}
		}
		if t.Sync != nil && t.Sync.Mode == SyncModeSample && (t.Sync.SampleDen == 0 || t.Sync.SampleNum == 0 || t.Sync.SampleNum > t.Sync.SampleDen) {
			return eb.Build().Str("table", t.Source.String()).Errorf("invalid sample fraction")
		}
	}
	return
}

// Clone deep-copies the plan through its JSON form, so a background step can
// work on a copy while the front end keeps drawing the original.
func (inst *Plan) Clone() (out Plan, err error) {
	var data []byte
	data, err = json.Marshal(inst)
	if err != nil {
		err = eh.Errorf("unable to copy plan: %w", err)
		return
	}
	err = json.Unmarshal(data, &out)
	if err != nil {
		err = eh.Errorf("unable to copy plan: %w", err)
	}
	return
}

// jsonType matches a column type that is or holds a JSON value.
var jsonType = regexp.MustCompile(`\bJSON\b|\bObject\(`)

// jsonColumns are the copy columns whose type is or holds JSON
// ([TableVerdict.HashAsText]).
func jsonColumns(src *TableInfo, copyColumns []string) (cols []string) {
	for _, name := range copyColumns {
		if c, has := src.Column(name); has && jsonType.MatchString(c.Type) {
			cols = append(cols, name)
		}
	}
	return
}
