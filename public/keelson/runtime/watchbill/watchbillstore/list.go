package watchbillstore

import "strings"

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
