package tally

import (
	"context"
	"fmt"
	"io/fs"
	"strconv"
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/fs/lading/ladingschema"
	"github.com/stergiotis/boxer/public/fs/lading/ladingsql"
	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// infoRow is one attribute of the selected entry as the Info pane shows it.
type infoRow struct {
	name  string
	value string
}

// infoSQL is the entry's row through the store's own SQL surface, pinned to
// the pane's snapshot. The macro carries the cutoff and the visibility; the
// snapshot is the Unix-nanosecond spelling, which is the one that does not
// depend on a server time zone.
func infoSQL(mount identifier.TaggedId, snap time.Time, p string) string {
	return fmt.Sprintf(
		"SELECT node_kind, content, size, mtime, mode, link_target, text, blocks, block_size, "+
			"lower(hex(content_hash)) AS content_hash, err, snap, expires_at "+
			"FROM fs(%d, %d) WHERE path = %s",
		mount.Value(), snap.UnixNano(), ladingschema.QuoteLiteral(p))
}

var infoVisibility = ladingsql.Config{Visibility: ladingsql.VisibleAll{}}

// loadInfo runs the entry query off the render thread and flattens the one
// row into attribute/value pairs. A path with no row (a directory the walker
// never stat'ed, a name that does not exist) is an empty table, not an error.
func loadInfo(ctx context.Context, exec recordstore.ExecutorI, mount identifier.TaggedId, snap time.Time, p string) (rows []infoRow, err error) {
	sql, err := ladingsql.Expand(infoVisibility, infoSQL(mount, snap, p))
	if err != nil {
		return
	}
	for rec, qerr := range exec.QueryArrow(ctx, sql) {
		if qerr != nil {
			err = qerr
			return
		}
		if rec.NumRows() == 0 {
			rec.Release()
			continue
		}
		rows = flattenRow(rec, 0, rows)
		rec.Release()
		break
	}
	if ctx.Err() != nil {
		err = eh.Errorf("%w", ctx.Err())
	}
	return
}

// flattenRow renders every column of one Arrow row as text, in schema order,
// with the two values a reader would otherwise have to decode by hand made
// legible beside their raw form: the mode as Go prints it, the size in
// binary units.
func flattenRow(rec arrow.RecordBatch, row int64, dst []infoRow) []infoRow {
	schema := rec.Schema()
	for i, f := range schema.Fields() {
		raw := gloss.FormatArrowElem(rec.Column(i), row)
		dst = append(dst, infoRow{name: f.Name, value: prettyValue(f.Name, raw)})
	}
	return dst
}

// prettyValue decorates the columns whose raw form hides what they say.
func prettyValue(name, raw string) string {
	switch name {
	case "mode":
		if v, perr := strconv.ParseUint(raw, 10, 32); perr == nil {
			return fs.FileMode(v).String() + "  (" + raw + ")"
		}
	case "size", "block_size":
		if v, perr := strconv.ParseInt(raw, 10, 64); perr == nil && v >= 1024 {
			return humanSize(v) + "  (" + raw + ")"
		}
	}
	return raw
}
