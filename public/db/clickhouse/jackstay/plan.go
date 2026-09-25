package jackstay

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"os"
	"path/filepath"
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
	TableVerdict       `json:",inline"`
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
		if other, taken := targets[target]; taken {
			err = eb.Build().Str("table", st.Ref.String()).Str("other", other.String()).Str("target", target.String()).
				Errorf("two source tables map onto one target table")
			return
		}
		targets[target] = st.Ref

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
		if sel.LeewayOnly && !v.Leeway {
			continue
		}
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
			TableVerdict:       v,
		})
	}
	for _, db := range needDatabase {
		plan.DatabaseDDL = append(plan.DatabaseDDL, CreateDatabaseDDL(db))
	}
	return
}

// IsSameServer reports whether source and target are one server: the same URL,
// or the same serverUUID() behind two URLs (localhost and 127.0.0.1, say).
func IsSameServer(srcEp Endpoint, dstEp Endpoint, src ServerInfo, dst ServerInfo) (same bool) {
	const nilUUID = "00000000-0000-0000-0000-000000000000"
	if srcEp.URL == dstEp.URL {
		return true
	}
	return src.UUID != "" && src.UUID != nilUUID && src.UUID == dst.UUID
}

// CarryDiffs copies each table's diff, sync settings and sync report from old
// where [Plan.CarryOver] kept the chunk layout they were computed under. The sync step uses it: repair acts only
// on what a diff showed, and a fresh structure check must not lose it.
func (inst *Plan) CarryDiffs(old *Plan) {
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
		if o == nil || t.Chunking == nil || o.Chunking == nil || !slices.Equal(o.Chunking.Bounds, t.Chunking.Bounds) || o.Chunking.Leaves != t.Chunking.Leaves {
			continue
		}
		t.Diff = o.Diff
		t.Sync = o.Sync
		t.SyncReport = o.SyncReport
	}
	inst.SyncRun = old.SyncRun
}

// CarryOver copies each table's chunk layout from old, a previous plan, when
// the table's source keys are unchanged. Keeping the layout is what lets a
// later diff compare chunk for chunk with an earlier one (ADR-0259 §SD4).
// Diffs are not carried: a new structure step makes them stale.
func (inst *Plan) CarryOver(old *Plan) {
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
		c := *o.Chunking
		t.Chunking = &c
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

// Save writes the plan atomically: to a temporary file in the same directory,
// then renamed over path.
func (inst *Plan) Save(path string) (err error) {
	var data []byte
	data, err = json.Marshal(inst, json.Deterministic(true), jsontext.Multiline(true), jsontext.WithIndent("  "))
	if err != nil {
		err = eh.Errorf("unable to encode plan: %w", err)
		return
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	var f *os.File
	f, err = os.CreateTemp(dir, ".jackstay-plan-*")
	if err != nil {
		err = eb.Build().Str("dir", dir).Errorf("unable to create temporary plan file: %w", err)
		return
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
		err = eb.Build().Str("path", path).Errorf("unable to write plan: %w", err)
	}
	return
}

// LoadPlan reads a plan written by [Plan.Save].
func LoadPlan(path string) (plan Plan, err error) {
	var data []byte
	data, err = os.ReadFile(path)
	if err != nil {
		err = eb.Build().Str("path", path).Errorf("unable to read plan: %w", err)
		return
	}
	err = json.Unmarshal(data, &plan)
	if err != nil {
		err = eb.Build().Str("path", path).Errorf("unable to decode plan: %w", err)
		return
	}
	if plan.FormatVersion != PlanFormatVersion {
		err = eb.Build().Str("path", path).Uint64("formatVersion", uint64(plan.FormatVersion)).
			Uint64("supported", uint64(PlanFormatVersion)).Errorf("unsupported plan format version")
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
