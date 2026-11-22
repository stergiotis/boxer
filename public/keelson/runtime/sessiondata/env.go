package sessiondata

import "github.com/stergiotis/boxer/public/config/env"

// The capability's settings (ADR-0009). Each bounds one data session or one
// statement; a request past a bound is refused with the bound named.
var (
	// MaxRowsEnv bounds the rows of one data session.
	MaxRowsEnv = env.NewInt(env.Spec{
		Name:        "BOXER_SESSIONDATA_MAX_ROWS",
		Default:     "50000000",
		Description: "rows one session data set and its siblings may hold together on the ClickHouse server, per data session; a definition past it is refused and its table dropped",
		Category:    env.CategoryDatabase,
	})
	// MaxBytesEnv bounds the server bytes of one data session.
	MaxBytesEnv = env.NewInt(env.Spec{
		Name:        "BOXER_SESSIONDATA_MAX_BYTES",
		Default:     "4294967296",
		Description: "bytes the session data sets of one data session may take on the ClickHouse server together, as the server reports a table's total_bytes; a definition past it is refused and its table dropped",
		Category:    env.CategoryDatabase,
	})
	// MaxAttachBytesEnv bounds the ad-hoc data one statement attaches.
	MaxAttachBytesEnv = env.NewInt(env.Spec{
		Name:        "BOXER_SESSIONDATA_MAX_ATTACH_BYTES",
		Default:     "67108864",
		Description: "bytes of ad-hoc datasets one statement may attach to a ClickHouse server statement as external data, together",
		Category:    env.CategoryDatabase,
	})
	// SessionTimeoutEnv is the session_timeout the service sends.
	SessionTimeoutEnv = env.NewDuration(env.Spec{
		Name:        "BOXER_SESSIONDATA_SESSION_TIMEOUT",
		Default:     "15m",
		Description: "idle time after which the ClickHouse server drops a session holding session data sets (session_timeout, whole seconds; the server refuses more than its max_session_timeout); a later statement replays the definitions it reads",
		Category:    env.CategoryDatabase,
	})
	// BusyWaitEnv bounds how long one of the service's statements waits for
	// its server session to be free.
	BusyWaitEnv = env.NewDuration(env.Spec{
		Name:        "BOXER_SESSIONDATA_BUSY_WAIT",
		Default:     "10s",
		Description: "how long one of the session data service's own statements (a probe, a create, a replay, a drop) is tried again, with backoff, while the ClickHouse server refuses it because an earlier statement still runs in the session (error 373, SESSION_IS_LOCKED); 0 sends it once",
		Category:    env.CategoryDatabase,
	})
	// LeaseTimeoutEnv bounds a prepared statement's hold on its session.
	LeaseTimeoutEnv = env.NewDuration(env.Spec{
		Name:        "BOXER_SESSIONDATA_LEASE_TIMEOUT",
		Default:     "15m",
		Description: "how long a prepared statement may hold its data session's ClickHouse session before the hold lapses for a caller that never released it",
		Category:    env.CategoryDatabase,
	})
)
