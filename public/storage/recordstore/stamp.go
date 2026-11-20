package recordstore

import (
	"encoding/hex"
	"slices"
	"strconv"
	"strings"

	"lukechampine.com/blake3"
)

// TableStampPrefix opens every table stamp, so a COMMENT an operator wrote
// by hand is told from one a store wrote.
const TableStampPrefix = "boxer-table/1"

// LeewayNamingVersion is the leeway physical-naming generation the stamp
// records: the v2 aspect segment codec of ADR-0182 §SD1. A table written
// under another generation names its columns differently throughout.
const LeewayNamingVersion = "2"

// DedupWindowSetting is the MergeTree setting that makes a non-replicated
// table remember recent inserts, so a retry carrying the same
// insert_deduplication_token is dropped rather than written twice.
const DedupWindowSetting = "non_replicated_deduplication_window"

// DedupWindowBlocks is the window newly created tables keep: the number of
// most recent insert blocks whose tokens the table remembers.
const DedupWindowBlocks = 1000

// TableStamp is the provenance a stored table carries in its COMMENT
// (ADR-0387): which schema created it, as digests a binary compares with
// its own. It follows the view stamps of the leeway catalog: a plain
// string a reader compares, no version marker of its own beyond the prefix.
type TableStamp struct {
	// Kind is the generated table name.
	Kind string
	// Columns is [ColumnsDigest] of the generated column list.
	Columns string
	// Naming is [LeewayNamingVersion].
	Naming string
	// Vocab is [VocabDigest] of the membership-id assignment, "" when the
	// store binds none.
	Vocab string
}

// String renders the stamp: the prefix, then space-separated key=value.
func (inst TableStamp) String() string {
	parts := []string{TableStampPrefix, "kind=" + inst.Kind, "columns=" + inst.Columns, "naming=" + inst.Naming}
	if inst.Vocab != "" {
		parts = append(parts, "vocab="+inst.Vocab)
	}
	return strings.Join(parts, " ")
}

// ParseTableStamp reads a COMMENT as a stamp; ok is false for anything a
// store did not write.
func ParseTableStamp(comment string) (s TableStamp, ok bool) {
	fields := strings.Fields(comment)
	if len(fields) == 0 || fields[0] != TableStampPrefix {
		return
	}
	for _, f := range fields[1:] {
		k, v, found := strings.Cut(f, "=")
		if !found {
			continue
		}
		switch k {
		case "kind":
			s.Kind = v
		case "columns":
			s.Columns = v
		case "naming":
			s.Naming = v
		case "vocab":
			s.Vocab = v
		}
	}
	ok = s.Columns != ""
	return
}

// ColumnsDigest is a short digest of a column list: names and normalized
// types, in order. Codecs are left out — a codec change that matters
// renames the column (ADR-0182), and one that does not changes nothing a
// reader sees.
func ColumnsDigest(cols []ColumnSpec) string {
	h := blake3.New(32, nil)
	for _, c := range cols {
		_, _ = h.Write([]byte(c.Name))
		_, _ = h.Write([]byte{0x1f})
		_, _ = h.Write([]byte(NormalizeType(c.Type)))
		_, _ = h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// VocabDigest is a short digest of a membership-id assignment (kind →
// membership → id); "" for an empty one.
func VocabDigest(ids map[string]map[string]uint64) string {
	if len(ids) == 0 {
		return ""
	}
	kinds := make([]string, 0, len(ids))
	for k := range ids {
		kinds = append(kinds, k)
	}
	slices.Sort(kinds)
	h := blake3.New(32, nil)
	for _, k := range kinds {
		names := make([]string, 0, len(ids[k]))
		for n := range ids[k] {
			names = append(names, n)
		}
		slices.Sort(names)
		for _, n := range names {
			_, _ = h.Write([]byte(k + "\x1f" + n + "\x1f" + strconv.FormatUint(ids[k][n], 10) + "\n"))
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// commentClausePrefix opens the COMMENT line the generator appends to a
// stamped CREATE TABLE.
const commentClausePrefix = "\nCOMMENT '"

// CommentClause renders the COMMENT clause a stamped CREATE TABLE ends
// with, on a line of its own.
func CommentClause(stamp string) string {
	return "\nCOMMENT " + quoteLiteral(stamp)
}

// AppendDDLTail appends a store's configured DDLTail to its CREATE TABLE.
// The tail continues the statement's SETTINGS list or adds clauses after
// it, and ClickHouse wants the COMMENT clause last, so the tail goes in
// before a trailing COMMENT line when the statement has one.
func AppendDDLTail(stmt string, tail string) string {
	if tail == "" {
		return stmt
	}
	if i := strings.LastIndex(stmt, commentClausePrefix); i >= 0 {
		return stmt[:i] + " " + tail + stmt[i:]
	}
	return stmt + " " + tail
}
