package adrcorpus

import (
	"os"
)

// AdrContent is one row of the `adrcontent` table: an ADR's markdown source,
// keyed by the number that joins it to [Adr].
//
// # Why this is not a column of `adr`
//
// It is the same corpus and the same one row per decision, so a column would
// be the obvious shape. The reason it is not is size, and the size is not
// marginal: measured on boxer's own corpus, the metadata for every decision
// snapshots to ~53 KB and the source to ~3.2 MiB — about 60× — on a table that
// is read per query and shipped uncompressed.
//
// Splitting them makes the cost follow the ask. `SELECT * FROM adr` — the
// first query anyone writes — stays what it was, and nothing pays for the
// bytes until a query names this table. As a column there would be no way to
// not pay: the introspection engine widens to all columns for any `*`, any
// aggregate over no named column, and any join, which is most of what is
// written against these tables.
//
// The cost that remains is honest and worth stating plainly: naming
// `adrcontent` at all reads every ADR, because the projection that prunes
// columns cannot prune rows — a `WHERE num = 42` is applied by ClickHouse
// after the snapshot has been built.
type AdrContent struct {
	Num int
	// Path is where the bytes came from, so a row identifies itself without
	// a join back to `adr`.
	Path string
	// Content is the file whole — frontmatter included — not the body [Adr]
	// derives its title and dates from. So length(Content) is Adr.BodyBytes
	// exactly; the two agree by construction rather than by coincidence.
	Content string
}

// ReadContents reads the source of each ADR in adrs, in the order given.
//
// An unreadable file drops its row rather than yielding an empty one. The
// difference matters here: an empty string is a legible answer — an ADR that
// really is empty — and a reader has no way to tell it from a file that
// vanished. A missing row says less, and says it accurately.
func ReadContents(adrs []Adr) (rows []AdrContent) {
	rows = make([]AdrContent, 0, len(adrs))
	for _, a := range adrs {
		src, err := os.ReadFile(a.Path)
		if err != nil {
			continue
		}
		rows = append(rows, AdrContent{Num: a.Num, Path: a.Path, Content: string(src)})
	}
	return rows
}

// LoadContents reads the source of every ADR in the resolved corpus.
//
// Unlike [Load] this is deliberately not memoised, and the bytes are not held
// on the [Adr] rows: keeping a corpus-sized string alive for the process
// lifetime would charge every reader of `adr` for a table most of them never
// query. The read is repeated per call instead — the files are in the page
// cache by then, so it costs a fraction of the parse [Load] already did.
//
// The identity of the rows comes from [Load]'s snapshot and the bytes are read
// after it, so an ADR edited in between shows its new source beside its old
// metadata. That is the same window the tables already share with one another
// (see [LoadWindow]), widened by the read rather than opened by it.
func LoadContents() (rows []AdrContent) {
	adrs, _, _ := Load()
	return ReadContents(adrs)
}
