package tally

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/stergiotis/boxer/public/fs/lading/ladingschema"
	"github.com/stergiotis/boxer/public/fs/lading/ladingsql"
	"github.com/stergiotis/boxer/public/hmi/gloss"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/storage/recordstore"
)

// location is one pane's pinned place in the store: a mount and a resolved
// snapshot instant.
type location struct {
	mount identifier.TaggedId
	snap  time.Time
}

func (l location) key() string {
	return fmt.Sprintf("%x@%d", l.mount.Value(), l.snap.UnixNano())
}

// scopePredicate restricts a relation to a directory subtree: everything
// under dir, or everything when dir is the root.
func scopePredicate(dir string) string {
	if dir == "" || dir == "." {
		return "1"
	}
	return "startsWith(path, " + ladingschema.QuoteLiteral(dir+"/") + ")"
}

// diffSQL is ADR-0198 §7's diff between two snapshots — or two mounts — as
// the Diff tab runs it: newer side n, older side o, a full outer join on the
// path, classified added / removed / modified, unchanged paths left out.
// Both sides are optionally cut to one directory subtree first.
func diffSQL(newer, older location, dir string) string {
	pred := scopePredicate(dir)
	return fmt.Sprintf(`SELECT if(n.path != '', n.path, o.path) AS path,
       multiIf(o.path = '', 'added', n.path = '', 'removed',
               n.content_hash != o.content_hash OR n.mtime != o.mtime, 'modified', 'same') AS change,
       if(o.path = '', '', toString(o.size)) AS size_before,
       if(n.path = '', '', toString(n.size)) AS size_after,
       if(o.path = '', '', toString(o.mtime)) AS mtime_before,
       if(n.path = '', '', toString(n.mtime)) AS mtime_after,
       if(o.path = '', '', lower(hex(o.content_hash))) AS hash_before,
       if(n.path = '', '', lower(hex(n.content_hash))) AS hash_after
FROM (SELECT * FROM fs(%d, %d) WHERE %s) AS n
FULL OUTER JOIN (SELECT * FROM fs(%d, %d) WHERE %s) AS o ON n.path = o.path
WHERE change != 'same'
ORDER BY path
LIMIT 5000`,
		newer.mount.Value(), newer.snap.UnixNano(), pred,
		older.mount.Value(), older.snap.UnixNano(), pred)
}

// historySQL is one path across every complete snapshot of a mount, oldest
// first: ADR-0198 §7's history, the Versions tab of a cloud browser.
func historySQL(mount identifier.TaggedId, p string) string {
	return fmt.Sprintf(`SELECT snap, node_kind, size, mtime, lower(hex(content_hash)) AS content_hash, content, expires_at
FROM fs(%d, '*')
WHERE path = %s
ORDER BY snap`, mount.Value(), ladingschema.QuoteLiteral(p))
}

// openInPlaySQL is what "Open in play" hands over: the pane's directory as a
// query the reader can edit — pasteable-complete, the mount and snapshot
// pinned as literals so the buffer stands on its own.
func openInPlaySQL(loc location, dir string) string {
	where := scopePredicate(dir)
	return fmt.Sprintf(`-- tally: %s at %s
SELECT path, is_dir, size, mtime, ext, content, text, lower(hex(content_hash)) AS hash
FROM fs(0x%X, %d)
WHERE %s
ORDER BY is_dir DESC, path
LIMIT 1000`,
		hexID(loc.mount), loc.snap.UTC().Format(time.RFC3339Nano),
		loc.mount.Value(), loc.snap.UnixNano(), where)
}

// rcloneMountCommand is the rclone invocation that mounts the pane's location
// read-only through the SFTP head (ADR-0198 §SD9).
func rcloneMountCommand(loc location, followLatest bool) string {
	snap := "latest"
	if !followLatest {
		snap = loc.snap.UTC().Format("20060102T150405.000000000Z")
	}
	return fmt.Sprintf(`rclone mount --read-only ':sftp,ssh="boxer fs sftp-stdio --mount 0x%X",shell_type=unix:/%s/%s' /mnt/tally`,
		loc.mount.Value(), hexID(loc.mount), snap)
}

// tableResult is a lane's result shaped for stringTable.
type tableResult struct {
	headers []string
	rows    [][]string
}

// runTable expands sql through the store's macros and flattens every row to
// text. Off the render thread.
func runTable(ctx context.Context, exec recordstore.ExecutorI, sql string) (out tableResult, err error) {
	expanded, err := ladingsql.Expand(infoVisibility, sql)
	if err != nil {
		return
	}
	for rec, qerr := range exec.QueryArrow(ctx, expanded) {
		if qerr != nil {
			err = qerr
			return
		}
		if out.headers == nil {
			for _, f := range rec.Schema().Fields() {
				out.headers = append(out.headers, f.Name)
			}
		}
		out.rows = appendRows(rec, out.rows)
		rec.Release()
		if ctx.Err() != nil {
			err = eh.Errorf("%w", ctx.Err())
			return
		}
	}
	return
}

func appendRows(rec arrow.RecordBatch, dst [][]string) [][]string {
	n := int(rec.NumRows())
	cols := int(rec.NumCols())
	for r := 0; r < n; r++ {
		row := make([]string, cols)
		for ci := 0; ci < cols; ci++ {
			row[ci] = gloss.FormatArrowElem(rec.Column(ci), int64(r))
		}
		dst = append(dst, row)
	}
	return dst
}

// columnIndex finds a header by name, -1 when absent.
func columnIndex(headers []string, name string) int {
	for i, h := range headers {
		if strings.EqualFold(h, name) {
			return i
		}
	}
	return -1
}
