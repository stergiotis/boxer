package play

// play_completion_catalog.go is the endpoint half of the completion engine's
// providers (ADR-0190 §SD12's B rows): what `system.*` says about the buffer's
// own endpoint.
//
// Every one of these is an ADR-0147 §SD6 probe — off the frame thread, cached,
// and "not yet" until it answers, which is a different answer from "empty". The
// distinction is the whole reason the provider signature carries a ready flag:
// a pane that showed nothing for an unanswered probe would say the domain has
// no members, which is ADR-0174's `?`-never-`MISSING` rule inverted.
//
// Endpoint dependence is structural rather than implemented here. Every lane
// runs through a nodeLane over clientExecutor — the machinery the docs lookup
// and the vocabulary probe already use — so it inherits endpoint routing, auth
// and the pre-execute stage, and retargeting (ADR-0134) changes what it is
// asking without this file knowing.

import (
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/db/clickhouse/chtype"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlcomplete"
)

// catalogProbeTimeout bounds one catalog listing. Like the vocabulary probe's:
// a slow server must delay a pane, never a query.
const catalogProbeTimeout = 10 * time.Second

// catalogRowLimit bounds a listing that a large endpoint could make unbounded.
//
// It applies to the two per-scope listings — a database's tables, a table's
// columns — and is generous enough that a truncation is a pathological schema
// rather than an ordinary one. When it does bite, the pane says so through the
// row it adds rather than silently offering a prefix of the domain.
const catalogRowLimit = 2000

// catalogProbe holds one lane per question, created on first demand, and a memo
// of the answers.
//
// A nodeLane serves one (SQL, params) pair at a time, so a question with a
// parameter — a database's tables, a table's columns — gets one lane and a memo
// that accumulates. Asking for a second parameter supersedes the in-flight run
// for the first; the answer already banked stays banked.
type catalogProbe struct {
	client *Client
	lanes  map[string]*nodeLane
	memo   map[string][]sqlcomplete.Item
	// types is the column-type memo the typer reads, keyed
	// "<table>\x00<column>". Filled by the same rows the column listing
	// decodes, because parsing the type string twice would be the only other
	// way to have both.
	types map[string]chtype.Type
}

func newCatalogProbe(client *Client) (inst *catalogProbe) {
	return &catalogProbe{
		client: client,
		lanes:  make(map[string]*nodeLane, 8),
		memo:   make(map[string][]sqlcomplete.Item, 16),
		types:  make(map[string]chtype.Type, 256),
	}
}

// demand asks one question, requesting it on first ask.
//
// lane names the question (one nodeLane per name); key identifies the answer in
// the memo, which for a parameterised question includes the parameter.
func (inst *catalogProbe) demand(lane string, key string, sql string, decode func(view laneView) []sqlcomplete.Item) (items []sqlcomplete.Item, ready bool) {
	if inst == nil || inst.client == nil {
		return
	}
	if got, hit := inst.memo[key]; hit {
		return got, true
	}
	l, hit := inst.lanes[lane]
	if !hit {
		l = newNodeLane(clientExecutor{client: inst.client, opts: newExecOptions("completion-" + lane)},
			memory.NewGoAllocator(), catalogProbeTimeout)
		inst.lanes[lane] = l
	}
	view := l.demand(compiledNode{SQL: sql})
	if view.rec == nil || view.rec.NumRows() == 0 {
		// Zero rows is indistinguishable here from an unanswered probe, and
		// both read as not-ready. That errs toward silence, which is the side
		// §SD1 asks to err on.
		return nil, false
	}
	items = decode(view)
	inst.memo[key] = items
	return items, true
}

// oneColumn decodes a single-column listing into plain items.
func oneColumn(kind sqlcomplete.ItemKindE, source string) func(laneView) []sqlcomplete.Item {
	return func(view laneView) (items []sqlcomplete.Item) {
		col := view.rec.Column(0)
		n := int(view.rec.NumRows())
		items = make([]sqlcomplete.Item, 0, n)
		for row := range n {
			items = append(items, sqlcomplete.Item{
				Text: col.ValueStr(row), Kind: kind, Source: source,
			})
		}
		return
	}
}

// nameAndDoc decodes a two-column listing where the second column is prose.
func nameAndDoc(kind sqlcomplete.ItemKindE, source string) func(laneView) []sqlcomplete.Item {
	return func(view laneView) (items []sqlcomplete.Item) {
		names := view.rec.Column(0)
		docs := view.rec.Column(1)
		n := int(view.rec.NumRows())
		items = make([]sqlcomplete.Item, 0, n)
		for row := range n {
			it := sqlcomplete.Item{Text: names.ValueStr(row), Kind: kind, Source: source}
			if docs != nil {
				it.Doc = catalogDocLine(docs.ValueStr(row))
			}
			items = append(items, it)
		}
		return
	}
}

// catalogDocLine trims a server description to what a one-line cell can hold.
func catalogDocLine(s string) string {
	return strings.TrimSpace(firstLine(s))
}

func (inst *catalogProbe) databases() ([]sqlcomplete.Item, bool) {
	return inst.demand("databases", "databases",
		"SELECT name FROM system.databases ORDER BY name",
		oneColumn(sqlcomplete.ItemDatabase, "system.databases"))
}

func (inst *catalogProbe) tables(db string) ([]sqlcomplete.Item, bool) {
	where := "database = currentDatabase()"
	if db != "" {
		where = "database = " + marshalling.EscapeString(db)
	}
	return inst.demand("tables", "tables\x00"+db,
		"SELECT name, engine FROM system.tables WHERE "+where+
			" ORDER BY name LIMIT "+itoa(catalogRowLimit),
		nameAndDoc(sqlcomplete.ItemTable, "system.tables"))
}

// columns answers a table's columns and banks their types for the typer.
//
// table may be `db.name` or bare; bare resolves against the endpoint's current
// database, which is what an unqualified reference in the buffer means too.
func (inst *catalogProbe) columns(table string) (items []sqlcomplete.Item, ready bool) {
	db, name := splitQualified(table)
	if name == "" {
		return
	}
	where := "table = " + marshalling.EscapeString(name)
	if db == "" {
		where += " AND database = currentDatabase()"
	} else {
		where += " AND database = " + marshalling.EscapeString(db)
	}
	return inst.demand("columns", "columns\x00"+table,
		"SELECT name, type, comment FROM system.columns WHERE "+where+
			" ORDER BY position LIMIT "+itoa(catalogRowLimit),
		func(view laneView) (out []sqlcomplete.Item) {
			names := view.rec.Column(0)
			types := view.rec.Column(1)
			comments := view.rec.Column(2)
			n := int(view.rec.NumRows())
			out = make([]sqlcomplete.Item, 0, n)
			for row := range n {
				it := sqlcomplete.Item{
					Text: names.ValueStr(row), Kind: sqlcomplete.ItemColumn, Source: "system.columns",
				}
				if types != nil {
					it.Type = types.ValueStr(row)
					if t, err := chtype.Parse(it.Type); err == nil {
						inst.types[table+"\x00"+it.Text] = t
					}
				}
				if comments != nil {
					it.Doc = catalogDocLine(comments.ValueStr(row))
				}
				out = append(out, it)
			}
			return
		})
}

// columnType is the typer's rung for a column reference (§SD5). It reads the
// listing's own memo, demanding it when it is absent, so the first ask returns
// "unknown" and the next frame after the probe lands returns the type.
func (inst *catalogProbe) columnType(table string, column string) (t chtype.Type, ok bool) {
	if inst == nil || table == "" || column == "" {
		return
	}
	if got, hit := inst.types[table+"\x00"+column]; hit {
		return got, true
	}
	if _, ready := inst.columns(table); !ready {
		return
	}
	t, ok = inst.types[table+"\x00"+column]
	return
}

func (inst *catalogProbe) settings() ([]sqlcomplete.Item, bool) {
	return inst.demand("settings", "settings",
		"SELECT name, description FROM system.settings ORDER BY name",
		nameAndDoc(sqlcomplete.ItemSetting, "system.settings"))
}

func (inst *catalogProbe) typeNames() ([]sqlcomplete.Item, bool) {
	return inst.demand("typefamilies", "typefamilies",
		"SELECT name FROM system.data_type_families ORDER BY name",
		oneColumn(sqlcomplete.ItemTypeName, "system.data_type_families"))
}

func (inst *catalogProbe) timeZones() ([]sqlcomplete.Item, bool) {
	return inst.demand("timezones", "timezones",
		"SELECT time_zone FROM system.time_zones ORDER BY time_zone",
		oneColumn(sqlcomplete.ItemTimeZone, "system.time_zones"))
}

func (inst *catalogProbe) formats() ([]sqlcomplete.Item, bool) {
	return inst.demand("formats", "formats",
		"SELECT name FROM system.formats WHERE is_input OR is_output ORDER BY name",
		oneColumn(sqlcomplete.ItemFormat, "system.formats"))
}

func (inst *catalogProbe) dictionaries() ([]sqlcomplete.Item, bool) {
	return inst.demand("dictionaries", "dictionaries",
		"SELECT name, comment FROM system.dictionaries ORDER BY name",
		nameAndDoc(sqlcomplete.ItemDictionary, "system.dictionaries"))
}

// splitQualified splits `db.table` into its halves; a bare name yields an empty
// database, which resolves against the endpoint's current one.
func splitQualified(s string) (db string, name string) {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '.'); i > 0 && i < len(s)-1 {
		return s[:i], s[i+1:]
	}
	return "", s
}

// itoa keeps this file free of a strconv import for one constant.
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}
