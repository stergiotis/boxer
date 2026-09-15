package watchbillstore

import (
	"strconv"
	"strings"
)

// ListFilter narrows a read of the job table (ADR-0234 §SD2): any of the
// states, any of the kinds, any of the queues, one owner app. Every empty
// field matches everything, so the zero filter is the whole table. It is
// the read side River's JobList offers, kept to the columns a person or a
// management surface filters by; a richer question is a query over
// keelson('watchbill').
type ListFilter struct {
	States     []string
	Kinds      []string
	Queues     []string
	OwnerAppId string
}

// ListPredicate is the ScanOpts.ExtraPredicate for f, empty when f is
// zero. The generated scan ANDs it with its own filter, so it starts
// without a conjunction.
func ListPredicate(f ListFilter) (pred string) {
	var sb strings.Builder
	sb.WriteString(inClause("jobState", f.States))
	sb.WriteString(inClause("jobKind", f.Kinds))
	sb.WriteString(inClause("jobQueue", f.Queues))
	if f.OwnerAppId != "" {
		sb.WriteString(" AND " + elem("jobOwnerApp") + " = " + strLit(f.OwnerAppId))
	}
	return strings.TrimPrefix(sb.String(), " AND ")
}

// ListSQL reads the ids of the rows f selects, newest request first — the
// envelope ts is the request instant and never changes — at most limit.
// The bound applies after the order, so a list of N is the newest N; the
// generated scan orders ascending and cannot say otherwise, which is why
// the ids come from here and the rows from the scan.
func ListSQL(layout Layout, f ListFilter, limit int) (sql string) {
	var sb strings.Builder
	sb.WriteString("SELECT " + JobColKey + " FROM " + layout.JobTable())
	if pred := ListPredicate(f); pred != "" {
		sb.WriteString(" WHERE " + pred)
	}
	sb.WriteString(" ORDER BY " + JobColOrder + " DESC")
	if limit > 0 {
		sb.WriteString(" LIMIT " + strconv.Itoa(limit))
	}
	return sb.String()
}

// IdsPredicate is the ScanOpts.ExtraPredicate that reads a set of jobs.
func IdsPredicate(ids []string) (pred string) {
	var sb strings.Builder
	sb.WriteString(JobColKey + " IN (")
	for i, id := range ids {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(strLit(id))
	}
	sb.WriteString(")")
	return sb.String()
}
