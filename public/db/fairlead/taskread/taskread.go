// Package taskread is the fairlead pass that lets a statement a task
// supplied leave the process only as a read (ADR-0403).
//
// It judges statements whose [fairlead.Statement.Task] is set and lets every
// other statement through untouched. The mark is the sender's declaration
// ([fairlead.WithTask]), so a sender that runs a task's text without
// declaring it is not stopped here; the per-app checks and the server's own
// user are the barriers that do not depend on it.
//
// Read and not-read is the only question asked. Whether a read reaches past
// the server — a table function such as url(), s3(), remote() or file() —
// is the security class's question, which the apps ask before they send.
package taskread

import (
	"context"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/db/fairlead"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Pass refuses a statement a task supplied unless the statement-kind
// classifier proves it a read. A statement the classifier cannot place —
// unparsed, an unrecognised keyword, a script — is refused with it, which is
// the default-deny of [analysis.ClassifyStatementKind].
//
// It never rewrites a statement and holds no state.
type Pass struct{}

var _ fairlead.PassI = Pass{}

func (inst Pass) Lead(_ context.Context, st fairlead.Statement) (sql string, err error) {
	if st.Task == "" {
		sql = st.SQL
		return
	}
	kind := analysis.ClassifyStatementKind(st.SQL)
	switch kind {
	case analysis.KindReadOnly:
		sql = st.SQL
	case analysis.KindMutating:
		err = eb.Build().Str("task", st.Task).Stringer("kind", kind).
			Errorf("a task supplied this statement and it changes server state; a statement a task wrote leaves the process only as a read")
	default:
		err = eb.Build().Str("task", st.Task).Stringer("kind", kind).
			Errorf("a task supplied this statement and it cannot be shown to be a read; a statement a task wrote leaves the process only as a read")
	}
	return
}

// Install installs [Pass] at the fairlead and returns the function that
// removes it again.
func Install() (remove func()) {
	remove = fairlead.Install(Pass{})
	return
}
