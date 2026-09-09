// Package launchcfg is tally's launch configuration (ADR-0200 §SD9): the
// leeway-declared DTO a launch request may carry (ADR-0135) and a workingset
// restores (ADR-0148) — the two panes' locations, the synchronized-browsing
// flag and the target pane. The codec is generated into launchcfg.out.go by
// the golden test; the memberships are the tallyLaunch cohort in vdd.
package launchcfg

import "time"

// AppId is tally's durable app id — the target of a launch request.
const AppId = "github.com/stergiotis/boxer/apps/tally"

// Kind is the config kind the manifest declares and a request must name.
const Kind = "tallyLaunch"

// TallyLaunch is what reproduces a tally window. A mount is its id in hex
// (the SFTP directory spelling), a snapshot is RFC 3339 with nanoseconds or
// "" for "follow latest", a directory is an io/fs path ("." the root).
type TallyLaunch struct {
	_ struct{} `kind:"tallyLaunch"`

	FactId     uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	At         time.Time `lw:",ts"`

	MountA string `lw:"tallyLaunchMountA,textArray"`
	SnapA  string `lw:"tallyLaunchSnapA,textArray"`
	DirA   string `lw:"tallyLaunchDirA,textArray"`
	MountB string `lw:"tallyLaunchMountB,textArray"`
	SnapB  string `lw:"tallyLaunchSnapB,textArray"`
	DirB   string `lw:"tallyLaunchDirB,textArray"`
	// Sync is synchronized browsing; Target is "A" or "B".
	Sync   bool   `lw:"tallyLaunchSync,bool"`
	Target string `lw:"tallyLaunchTarget,symbol"`

	// SelA and SelB are the file each pane has selected, as an io/fs path
	// under that pane's snapshot; "" selects nothing. A path that is not in
	// the snapshot leaves the pane unselected rather than refusing the
	// config.
	SelA string `lw:"tallyLaunchSelA,textArray"`
	SelB string `lw:"tallyLaunchSelB,textArray"`

	// Tab is the dock tab to raise, by the slug TabIds lists; "" leaves the
	// dock as it opens. An unknown slug is ignored, not an error.
	Tab string `lw:"tallyLaunchTab,symbol"`

	// Sql is a path-set query over the lading surface (ADR-0222 §SD3): a
	// read yielding a `path` column, optionally `mount` and `snap`. It is
	// run on mount and its rows become the Results pane; a statement that
	// does not classify read-only is refused there with a message.
	Sql string `lw:"tallyLaunchSql,textArray"`
	// SqlLabel names the result set in the Results pane — what the caller
	// would call it ("flagged files"), not the query. Empty is a generic
	// label.
	SqlLabel string `lw:"tallyLaunchSqlLabel,textArray"`

	// Database is the ClickHouse database the lading store's tables live in
	// (ADR-0222 Updates 2026-09-09) — a ladingschema.Layout by its one
	// degree of freedom. Every mount, snapshot and query in this config is
	// read there; "" is the default store. A caller whose store is not
	// boxer's own has to say so, because nothing about a mount id or a path
	// says which tables hold it.
	Database string `lw:"tallyLaunchDatabase,symbol"`
}

// Tab slugs the Tab field accepts. They name the dock tabs a caller has a
// reason to raise; the browse panes are named by side, matching Target.
const (
	TabPaneA    = "paneA"
	TabPaneB    = "paneB"
	TabPreview  = "preview"
	TabInfo     = "info"
	TabHistory  = "history"
	TabDiff     = "diff"
	TabFind     = "find"
	TabDu       = "du"
	TabProblems = "problems"
	// TabResults is the pane a Sql config fills. It exists only while the
	// window carries a query.
	TabResults = "results"
)

// TabIds is every slug Tab accepts, in the order the dock lays the tabs out.
func TabIds() (ids []string) {
	return []string{
		TabPaneA, TabPaneB, TabResults, TabPreview, TabInfo,
		TabHistory, TabDiff, TabFind, TabDu, TabProblems,
	}
}
