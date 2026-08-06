package datacatalog

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"io"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// QueryI is the one method the live fetcher needs from a ClickHouse client;
// [github.com/stergiotis/boxer/public/keelson/data/chclient.Client] satisfies
// it. Narrow on purpose, like [ExecI]: the fetcher is then testable against an
// httptest server, or against a canned body, with no client at all.
type QueryI interface {
	Query(ctx context.Context, sql string) (body io.ReadCloser, err error)
}

// ChFetcher discovers tables from a live server's system tables — the
// [FetcherI] a `refresh` runs with.
type ChFetcher struct {
	Query QueryI
}

var _ FetcherI = (*ChFetcher)(nil)

// NewChFetcher wraps a query-capable client.
func NewChFetcher(query QueryI) (inst *ChFetcher) {
	return &ChFetcher{Query: query}
}

// systemDatabaseExclusion renders the NOT IN list both probe queries carry.
// The names are literals rather than parameters because they are this package's
// own constants, and a parameterised IN list is more machinery than a fixed
// three-element tuple deserves.
func systemDatabaseExclusion() (clause string) {
	quoted := make([]string, 0, len(SystemDatabases))
	for _, db := range SystemDatabases {
		quoted = append(quoted, "'"+db+"'")
	}
	return "database NOT IN (" + strings.Join(quoted, ", ") + ")"
}

// TablesQuery and ColumnsQuery are the two probes one discovery pass runs.
//
// JSONEachRow rather than TabSeparated: a column name may contain a tab or a
// newline, and JSON puts the escaping question on the server's side of the
// wire where it is already answered. position is cast to UInt32 because
// ClickHouse quotes 64-bit integers in JSON output by default, which would make
// it a string here for no reason.
func TablesQuery() (sql string) {
	return `SELECT database, name, engine FROM system.tables WHERE ` +
		systemDatabaseExclusion() + ` ORDER BY database, name FORMAT JSONEachRow`
}

// ColumnsQuery fetches every non-system column in (database, table, position)
// order — position order matters, because it is the order the normalized schema
// string renders and the shape batteries are written against.
func ColumnsQuery() (sql string) {
	return `SELECT database, table, name, type, toUInt32(position) AS position FROM system.columns WHERE ` +
		systemDatabaseExclusion() + ` ORDER BY database, table, position FORMAT JSONEachRow`
}

type tablesRow struct {
	Database string `json:"database"`
	Name     string `json:"name"`
	Engine   string `json:"engine"`
}

type columnsRow struct {
	Database string `json:"database"`
	Table    string `json:"table"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Position uint32 `json:"position"`
}

// FetchTables runs both probes and joins them: one snapshot per table, columns
// attached in position order.
//
// A table that system.tables reports but system.columns has no rows for still
// appears, with no columns — it will classify as opaque with [ErrNoColumns],
// which is a visible row rather than a silent omission. A column whose table is
// not in the table list is dropped: the two queries are not atomic, and a table
// created between them has no inventory row to attach to.
func (inst *ChFetcher) FetchTables(ctx context.Context) (tables []TableSnapshot, err error) {
	var rawTables []tablesRow
	rawTables, err = fetchJsonRows[tablesRow](ctx, inst.Query, TablesQuery())
	if err != nil {
		err = eh.Errorf("unable to read system.tables: %w", err)
		return
	}
	var rawColumns []columnsRow
	rawColumns, err = fetchJsonRows[columnsRow](ctx, inst.Query, ColumnsQuery())
	if err != nil {
		err = eh.Errorf("unable to read system.columns: %w", err)
		return
	}

	tables = make([]TableSnapshot, 0, len(rawTables))
	byRef := make(map[TableRef]int, len(rawTables))
	for _, t := range rawTables {
		ref := TableRef{Database: t.Database, Name: t.Name}
		byRef[ref] = len(tables)
		tables = append(tables, TableSnapshot{Ref: ref, Engine: t.Engine})
	}
	for _, c := range rawColumns {
		idx, has := byRef[TableRef{Database: c.Database, Name: c.Table}]
		if !has {
			continue
		}
		tables[idx].Columns = append(tables[idx].Columns, ColumnMeta{
			Name:     c.Name,
			Type:     c.Type,
			Position: uint64(c.Position),
		})
	}
	return
}

// fetchJsonRows reads a JSONEachRow body one line at a time. The body is a
// stream of independent objects, not a JSON array, so it is decoded per line
// rather than whole — which also keeps a malformed row's diagnostic pointing at
// the row.
func fetchJsonRows[T any](ctx context.Context, q QueryI, sql string) (rows []T, err error) {
	var body io.ReadCloser
	body, err = q.Query(ctx, sql)
	if err != nil {
		err = eh.Errorf("unable to run query: %w", err)
		return
	}
	defer func() { _ = body.Close() }()

	rows = make([]T, 0, 256)
	sc := bufio.NewScanner(body)
	// A desc_json-sized line cannot occur here (these are system-table rows),
	// but a very wide Enum or Tuple type can outgrow bufio's 64 KiB default.
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
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
		return
	}
	return
}
