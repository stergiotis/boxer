package agentrun

import (
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
)

// ServiceAppId is the identity the host's journal service holds on the
// bus.
const ServiceAppId app.AppIdT = "runtime.agentrun"

// The family (ADR-0308 §SD10; ADR-0303 §SD8, superseded by ADR-0308): one
// request/reply subject
// per verb of the store seam, every call scoped to Msg.Sender — an app
// reaches only the runs it owns.
const (
	SubjectPrefix    = "runtime.agentrun."
	SubjectAll       = "runtime.agentrun.*"
	SubjectCreate    = SubjectPrefix + "create"
	SubjectUpdate    = SubjectPrefix + "update"
	SubjectGet       = SubjectPrefix + "get"
	SubjectList      = SubjectPrefix + "list"
	SubjectAppend    = SubjectPrefix + "append"
	SubjectEntries   = SubjectPrefix + "entries"
	SubjectTombstone = SubjectPrefix + "tombstone"
	// SubjectErase destroys the content of named entries of a run after
	// their erased entries are in the chain (ADR-0320 §SD4).
	SubjectErase = SubjectPrefix + "erase"
)

// TableRuns and TableEntries are the introspection tables (§SD10).
const (
	TableRuns    = "agent_runs"
	TableEntries = "agent_run_entries"
)

// ServiceCaps are the service's subject grants.
func ServiceCaps() (caps []app.SubjectFilter) {
	return []app.SubjectFilter{
		{Pattern: SubjectAll, Direction: app.CapDirectionSub, Reason: "agentrun: serve runtime.agentrun requests"},
		{Pattern: inprocbus.InboxPrefix + ">", Direction: app.CapDirectionPub, Reason: "agentrun: reply to inboxes"},
	}
}

// ClientCaps are what an app declares to write and read its own runs.
func ClientCaps(reason string) (caps []app.SubjectFilter) {
	return []app.SubjectFilter{{Pattern: SubjectAll, Direction: app.CapDirectionPub, Reason: reason, Sticky: true}}
}
