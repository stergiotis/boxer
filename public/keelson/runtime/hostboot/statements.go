package hostboot

import (
	"sync"

	"github.com/stergiotis/boxer/public/db/fairlead/taskread"
)

// installStatementPasses installs the fairlead passes every host runs
// (ADR-0403): a statement whose text a task supplied leaves the process only
// as a read. It runs once per process, before the first statement, and is
// not removed when a Runtime closes — it is the process's policy, and a
// second Boot in one process would otherwise install it twice.
var installStatementPasses = sync.OnceFunc(func() {
	_ = taskread.Install()
})
