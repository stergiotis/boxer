package sessiondata

import (
	"context"
	"errors"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
)

// ServiceAppId is the identity the capability speaks under on the bus.
const ServiceAppId app.AppIdT = "runtime.sessiondata"

// MaxDatasetsPerSession bounds how many data sets one data session holds.
const MaxDatasetsPerSession = 64

// Identity is a window: the bus envelope's sender app and instance. The
// zero Identity is the runtime itself, an in-process caller.
type Identity struct {
	App      app.AppIdT
	Instance uint64
}

// Caller is who asks: a window, and the agent's call the request is work
// of, nil for the person's.
type Caller struct {
	By         Identity
	OnBehalfOf *app.OnBehalfOf
}

// Endpoint is a ClickHouse HTTP endpoint and the identity the caller
// reaches it under. A server session is per endpoint and identity.
type Endpoint struct {
	URL      string
	User     string
	Password string
	// ServesConfined is the caller's declaration that this endpoint may
	// see the plaintext of sealed data (ADR-0145 §SD4, §SD5): the same
	// discipline gate the server engine takes at construction. Without it
	// a statement that would send an ad-hoc dataset to the server, or read
	// a data set derived from one, is refused.
	ServesConfined bool
}

// DatasetStateE is where a data set's table stands, as the capability last
// saw it.
type DatasetStateE uint8

const (
	// DatasetStateUnspecified is the zero value, never a data set's state.
	DatasetStateUnspecified DatasetStateE = iota
	// DatasetStateLive — the table was there when the capability last
	// looked.
	DatasetStateLive
	// DatasetStateExpired — the server session was lost, or the table was
	// gone from it; the next statement that reads it replays it.
	DatasetStateExpired
	// DatasetStateStale — a data set it reads was redefined; the next
	// statement that reads it rebuilds it from the new one.
	DatasetStateStale
	// DatasetStateFailed — its last build failed; Reason says why. The next
	// statement that reads it tries again.
	DatasetStateFailed
)

// AllDatasetStates lists every DatasetStateE value.
var AllDatasetStates = []DatasetStateE{DatasetStateUnspecified, DatasetStateLive, DatasetStateExpired, DatasetStateStale, DatasetStateFailed}

func (inst DatasetStateE) String() (s string) {
	switch inst {
	case DatasetStateLive:
		s = "live"
	case DatasetStateExpired:
		s = "expired"
	case DatasetStateStale:
		s = "stale"
	case DatasetStateFailed:
		s = "failed"
	default:
		s = "unspecified"
	}
	return
}

// Column is one column of a data set, as the server describes it.
type Column struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// DatasetInfo is one data set as the catalog shows it.
type DatasetInfo struct {
	Name string `json:"name"`
	// SQL is the defining statement as its definer wrote it.
	SQL          string   `json:"sql"`
	Dependencies []string `json:"dependencies,omitempty"`
	// Attachments are the ad-hoc datasets the definition reads, by alias.
	Attachments []string `json:"attachments,omitempty"`
	OrderBy     []string `json:"orderBy,omitempty"`
	// Revision counts definitions; Builds counts the times its table was
	// made, Replays the builds that re-made it without a new definition.
	Revision uint64        `json:"revision"`
	Builds   uint64        `json:"builds"`
	Replays  uint64        `json:"replays"`
	State    DatasetStateE `json:"state"`
	Reason   string        `json:"reason,omitempty"`
	Rows     uint64        `json:"rows"`
	Bytes    uint64        `json:"bytes"`
	Columns  []Column      `json:"columns,omitempty"`
	// Confined is set when it was derived from sealed data (ADR-0145): a
	// statement reading it goes only to an endpoint declared for sealed
	// data.
	Confined bool `json:"confined,omitempty"`
	// OwnClear is set, with Confined, when everything sealed its table
	// was last made from is the own publication at TLP:CLEAR of the task
	// that asks: that task's reading of it is not labelled. It is false
	// for the person, for another caller, and once a replay read anything
	// else; a reader that does not look takes the data set as sealed.
	OwnClear bool `json:"ownClear,omitempty"`
	// Endpoint is the host it lives on, as a grant names it.
	Endpoint string `json:"endpoint"`
	// Table is its server-side table in the session.
	Table     string    `json:"table"`
	DefinedAt time.Time `json:"definedAt"`
	BuiltAt   time.Time `json:"builtAt,omitzero"`
	// Task is the task whose call defined it, empty for the person.
	Task string `json:"task,omitempty"`
}

// Binding is what [Service.Prepare] hands a caller to send a statement
// inside a data session: the statement with its macros replaced, the
// settings that put it in the server session, the external tables to
// attach, and its label. The caller sends it as its own statement — under
// its own checks, readonly level and task mark — and releases the lease
// once the response is drained or abandoned.
type Binding struct {
	// SQL is the statement with every session('…') and keelson('…') call
	// replaced by the table the server reads.
	SQL string `json:"sql"`
	// Settings put the statement in the server session; empty when it
	// reads no data set.
	Settings map[string]string `json:"settings,omitempty"`
	// External are the ad-hoc datasets the statement reads, attached as
	// external data.
	External []queryengine.ExternalTable `json:"external,omitempty"`
	// Sensitivity is confined when the statement reads sealed data, an
	// attached dataset or a data set derived from one: where it may be
	// sent, whoever published the data.
	Sensitivity queryengine.SensitivityE `json:"sensitivity,omitempty"`
	// OwnClear is set, with a confined Sensitivity, when everything sealed
	// the statement reads is the reading task's own publication at
	// TLP:CLEAR, as the dataset service answered each read: the statement
	// is sent as confined all the same, and its result need not be
	// labelled as derived from sealed data. False for every other
	// statement; a caller that does not look labels the result.
	OwnClear bool `json:"ownClear,omitempty"`
	// Lease holds the server session for this statement; empty when the
	// statement reads no data set. A server session runs one statement at
	// a time, so the next caller waits until it is released or expires.
	Lease string `json:"lease,omitempty"`
	// Datasets are the data sets the statement reads, as name@revision.
	Datasets []string `json:"datasets,omitempty"`
	// Replayed are the data sets re-made before the statement could run.
	Replayed []string `json:"replayed,omitempty"`
	// Ticket names the data session, to hand to another window.
	Ticket string `json:"ticket,omitempty"`
}

// ErrConflictingSetting is Apply's refusal of a request that already sets
// a setting the binding sets to another value.
var ErrConflictingSetting = errors.New("the request sets a session setting the binding sets otherwise")

// Apply adds the binding's settings, external tables and label to req. The
// statement is the caller's to place: it takes SQL before its own rewrites.
func (inst Binding) Apply(req *queryengine.Request) (err error) {
	if len(inst.Settings) > 0 && req.Settings == nil {
		req.Settings = make(map[string]string, len(inst.Settings))
	}
	for k, v := range inst.Settings {
		if old, set := req.Settings[k]; set && old != v {
			return errors.Join(ErrConflictingSetting, errors.New(k))
		}
		req.Settings[k] = v
	}
	req.External = append(req.External, inst.External...)
	if inst.Sensitivity == queryengine.SensitivityConfined {
		req.Sensitivity = queryengine.SensitivityConfined
	}
	return
}

// AdhocReaderI reads an ad-hoc dataset whole for a caller, under the
// dataset service's own grant rules and audit. The host's
// adhocdata.Service implements it.
type AdhocReaderI interface {
	Read(ctx context.Context, alias string, by adhocdata.Identity, obo *app.OnBehalfOf) (res adhocdata.ReadResult, err error)
}

// DispatcherI is what the capability asks the host's agent dispatcher: who
// an agent-caused request is work of, whether the task's grant lists a
// destination, and whether a task has ended.
type DispatcherI interface {
	app.DelegationI
	TaskLive(task string) (live bool)
}

// GrantError is a request an agent's call caused that its task's grant does
// not cover. Destination is what the grant would have to list; an app
// surfaces it with app.RefuseForDestinations.
type GrantError struct {
	Destination string
	Reason      string
}

func (inst *GrantError) Error() (s string) { return "agent limit: " + inst.Reason }

// DestinationSession is how a grant names the right to keep session tables
// on an endpoint, by host.
func DestinationSession(host string) (name string) { return "clickhouse-session:" + host }

// DestinationServer is how a grant names an endpoint a task's work may
// query, by host; play's agent limits use the same spelling.
func DestinationServer(host string) (name string) { return "clickhouse:" + host }
