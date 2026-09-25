package jackstay

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"io"
	"slices"
	"strings"

	"github.com/stergiotis/boxer/public/gov/datacatalog"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// QueryI is the one method discovery needs from a ClickHouse client;
// [github.com/stergiotis/boxer/public/keelson/data/chclient.Client] satisfies
// it.
type QueryI interface {
	Query(ctx context.Context, sql string) (body io.ReadCloser, err error)
}

// ServerInfo is what a server says about itself. It is recorded in the plan so
// a later reader can see which versions a plan was made between.
type ServerInfo struct {
	// UUID is serverUUID(): it identifies the server itself, whatever URL
	// reached it.
	UUID          string `json:"uuid"`
	Version       string `json:"version"`
	UptimeSeconds uint64 `json:"uptimeSeconds"`
	Timezone      string `json:"timezone"`
}

type DatabaseInfo struct {
	Name   string `json:"name"`
	Engine string `json:"engine"`
}

// ColumnInfo is one column as system.columns reports it. DefaultKind is the
// server's spelling: empty, DEFAULT, MATERIALIZED, ALIAS or EPHEMERAL.
type ColumnInfo struct {
	Name              string `json:"name"`
	Type              string `json:"type"`
	Position          uint64 `json:"position"`
	DefaultKind       string `json:"default_kind"`
	DefaultExpression string `json:"default_expression"`
	CompressionCodec  string `json:"compression_codec"`
}

// IsInsertable reports whether an INSERT may carry a value for the column.
// MATERIALIZED columns are stored but computed by the server, and ALIAS and
// EPHEMERAL columns are not stored at all.
func (inst ColumnInfo) IsInsertable() (ok bool) {
	return inst.DefaultKind == "" || inst.DefaultKind == "DEFAULT"
}

// TableInfo is one table as system.tables and system.columns report it,
// columns in position order.
type TableInfo struct {
	Ref          datacatalog.TableRef
	Engine       string
	SortingKey   string
	PrimaryKey   string
	PartitionKey string
	SamplingKey  string
	TotalRows    uint64
	TotalBytes   uint64
	CreateQuery  string
	Columns      []ColumnInfo
}

func (inst *TableInfo) ColumnNames() (names []string) {
	names = make([]string, 0, len(inst.Columns))
	for _, c := range inst.Columns {
		names = append(names, c.Name)
	}
	return
}

func (inst *TableInfo) Column(name string) (col ColumnInfo, has bool) {
	for _, c := range inst.Columns {
		if c.Name == name {
			return c, true
		}
	}
	return
}

// Inventory is one server's discovered state (ADR-0259 §SD2). System
// databases are listed in Databases but their tables are not discovered.
type Inventory struct {
	Server    ServerInfo
	Databases []DatabaseInfo
	Tables    []TableInfo
}

func (inst *Inventory) HasDatabase(name string) (has bool) {
	return slices.ContainsFunc(inst.Databases, func(d DatabaseInfo) bool { return d.Name == name })
}

// Table looks a table up by reference. Inventories are small (hundreds of
// tables), so a scan is cheaper to keep correct than an index.
func (inst *Inventory) Table(ref datacatalog.TableRef) (tbl *TableInfo, has bool) {
	for i := range inst.Tables {
		if inst.Tables[i].Ref == ref {
			return &inst.Tables[i], true
		}
	}
	return
}

// UserDatabases returns the database names that are not system databases, in
// server order.
func (inst *Inventory) UserDatabases() (names []string) {
	names = make([]string, 0, len(inst.Databases))
	for _, d := range inst.Databases {
		if !datacatalog.IsSystemDatabase(d.Name) {
			names = append(names, d.Name)
		}
	}
	return
}

// jsonSettings makes 64-bit integers plain JSON numbers rather than quoted
// strings, so they decode into uint64 fields.
const jsonSettings = " SETTINGS output_format_json_quote_64bit_integers = 0 FORMAT JSONEachRow"

func systemExclusion(column string) (clause string) {
	quoted := make([]string, 0, len(datacatalog.SystemDatabases))
	for _, db := range datacatalog.SystemDatabases {
		quoted = append(quoted, "'"+db+"'")
	}
	return column + " NOT IN (" + strings.Join(quoted, ", ") + ")"
}

func ServerQuery() (sql string) {
	return "SELECT toString(serverUUID()) AS uuid, version() AS version, toUInt64(uptime()) AS uptime, timezone() AS timezone" + jsonSettings
}

func DatabasesQuery() (sql string) {
	return "SELECT name, engine FROM system.databases ORDER BY name" + jsonSettings
}

// TablesQuery excludes temporary tables: they belong to a session and cannot
// be reached by a sync.
func TablesQuery() (sql string) {
	return "SELECT database, name, engine, sorting_key, primary_key, partition_key, sampling_key, " +
		"ifNull(total_rows, 0) AS total_rows, ifNull(total_bytes, 0) AS total_bytes, create_table_query " +
		"FROM system.tables WHERE " + systemExclusion("database") + " AND NOT is_temporary " +
		"ORDER BY database, name" + jsonSettings
}

func ColumnsQuery() (sql string) {
	return "SELECT database, table, name, type, position, default_kind, default_expression, compression_codec " +
		"FROM system.columns WHERE " + systemExclusion("database") + " ORDER BY database, table, position" + jsonSettings
}

type serverRow struct {
	UUID     string `json:"uuid"`
	Version  string `json:"version"`
	Uptime   uint64 `json:"uptime"`
	Timezone string `json:"timezone"`
}

type tableRow struct {
	Database     string `json:"database"`
	Name         string `json:"name"`
	Engine       string `json:"engine"`
	SortingKey   string `json:"sorting_key"`
	PrimaryKey   string `json:"primary_key"`
	PartitionKey string `json:"partition_key"`
	SamplingKey  string `json:"sampling_key"`
	TotalRows    uint64 `json:"total_rows"`
	TotalBytes   uint64 `json:"total_bytes"`
	CreateQuery  string `json:"create_table_query"`
}

type columnRow struct {
	Database   string `json:"database"`
	Table      string `json:"table"`
	ColumnInfo `json:",inline"`
}

// Discover reads a server's inventory: four queries over system tables, none
// of which touches table data (ADR-0259 §SD2). The table and column queries are
// not atomic; a column whose table appeared between them is dropped, and a
// table whose columns vanished between them is kept with none.
func Discover(ctx context.Context, q QueryI) (inv Inventory, err error) {
	var servers []serverRow
	servers, err = queryRows[serverRow](ctx, q, ServerQuery())
	if err != nil {
		err = eh.Errorf("unable to read server identity: %w", err)
		return
	}
	if len(servers) != 1 {
		err = eb.Build().Int("rows", len(servers)).Errorf("server identity query returned an unexpected row count")
		return
	}
	inv.Server = ServerInfo{UUID: servers[0].UUID, Version: servers[0].Version, UptimeSeconds: servers[0].Uptime, Timezone: servers[0].Timezone}

	inv.Databases, err = queryRows[DatabaseInfo](ctx, q, DatabasesQuery())
	if err != nil {
		err = eh.Errorf("unable to read system.databases: %w", err)
		return
	}
	var tables []tableRow
	tables, err = queryRows[tableRow](ctx, q, TablesQuery())
	if err != nil {
		err = eh.Errorf("unable to read system.tables: %w", err)
		return
	}
	var columns []columnRow
	columns, err = queryRows[columnRow](ctx, q, ColumnsQuery())
	if err != nil {
		err = eh.Errorf("unable to read system.columns: %w", err)
		return
	}

	inv.Tables = make([]TableInfo, 0, len(tables))
	byRef := make(map[datacatalog.TableRef]int, len(tables))
	for _, t := range tables {
		ref := datacatalog.TableRef{Database: t.Database, Name: t.Name}
		byRef[ref] = len(inv.Tables)
		inv.Tables = append(inv.Tables, TableInfo{
			Ref:          ref,
			Engine:       t.Engine,
			SortingKey:   t.SortingKey,
			PrimaryKey:   t.PrimaryKey,
			PartitionKey: t.PartitionKey,
			SamplingKey:  t.SamplingKey,
			TotalRows:    t.TotalRows,
			TotalBytes:   t.TotalBytes,
			CreateQuery:  t.CreateQuery,
		})
	}
	for _, c := range columns {
		idx, has := byRef[datacatalog.TableRef{Database: c.Database, Name: c.Table}]
		if !has {
			continue
		}
		inv.Tables[idx].Columns = append(inv.Tables[idx].Columns, c.ColumnInfo)
	}
	return
}

// queryRows decodes a JSONEachRow body one line at a time, so a malformed row's
// diagnostic points at the row.
func queryRows[T any](ctx context.Context, q QueryI, sql string) (rows []T, err error) {
	var body io.ReadCloser
	body, err = q.Query(ctx, sql)
	if err != nil {
		err = eh.Errorf("unable to run query: %w", err)
		return
	}
	defer func() { _ = body.Close() }()

	rows = make([]T, 0, 64)
	sc := bufio.NewScanner(body)
	// create_table_query of a wide table can outgrow bufio's 64 KiB default.
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	line := 0
	for sc.Scan() {
		line++
		raw := sc.Bytes()
		if len(strings.TrimSpace(string(raw))) == 0 {
			continue
		}
		var row T
		err = json.Unmarshal(raw, &row)
		if err != nil {
			err = eb.Build().Int("line", line).Errorf("unable to decode row: %w", err)
			rows = nil
			return
		}
		rows = append(rows, row)
	}
	err = sc.Err()
	if err != nil {
		err = eh.Errorf("unable to read query response: %w", err)
		rows = nil
	}
	return
}
