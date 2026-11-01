package appstate

import (
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
)

// The operations catalog (ADR-0345 M4). Queries read the window's last
// read of keelson('app_state') — the poller's snapshot, never a statement
// of their own — and filter by their own arguments; the one command over
// the view selects an app, and refresh asks the poller to read again.
// Clearing an entry or forgetting an app writes the store and is the
// person's act at the window (ADR-0345 §SD4).

const (
	resSelection = "selection"
	resRead      = "read"

	opListEntries = "list_entries"
	opGetView     = "get_view"
	opSelectApp   = "select_app"
	opRefresh     = "refresh"
)

// Result bounds: what one inline result may carry.
const (
	opsMaxEntries     = 200
	opsDefaultEntries = 50
	opsMaxDetailLen   = 512
)

const unknownAppReason = "no app of that id kept any state at the last read; list_entries names the apps that did"

// readValue is the read resource's value: when the poller last landed a
// read and what it reported, comparable so the host can tell a new read
// from the last. The poller writes between frames, so the window latches
// it at the top of each frame (latchRead) and the change lands inside the
// frame, attributed to the app rather than to the person (ADR-0329 §SD3).
type readValue struct {
	Refreshed time.Time
	Error     string
}

func readValueOf(s snapshot) (v readValue) {
	v = readValue{Refreshed: s.refreshed, Error: s.lastError}
	return
}

// latchRead copies the poller's state into the frame.
func (inst *App) latchRead() {
	inst.latchedRead = readValueOf(inst.snapshot())
}

// opsSnap is what queries read: the poller's last read and the view.
type opsSnap struct {
	read     snapshot
	selected string
}

func (inst *App) opsSnapshot() (sn opsSnap) {
	sn.read = inst.snapshot()
	sn.selected = inst.selected
	return
}

// ListEntriesArgs filters the last read; every field is optional.
type ListEntriesArgs struct {
	App   string `desc:"case-insensitive substring of the app id"`
	Kind  string `desc:"the entry kind, exactly: persist, workingset or column_width; empty keeps every kind"`
	Limit int    `desc:"entries to return, in table order; default 50, at most 200"`
}

// Entry is one row of keelson('app_state') as the window shows it.
type Entry struct {
	Kind         string `desc:"persist, workingset or column_width"`
	App          string `desc:"the app id"`
	Key          string `desc:"the entry's key within its kind"`
	EntityId     string `desc:"the store's id for the entry"`
	PayloadBytes int64  `desc:"the payload's size; 0 for a column width"`
	Detail       string `desc:"the table's one-line detail, cut at 512 characters"`
	WrittenAt    string `desc:"when the entry was written, as the table renders it"`
	RunId        string `desc:"the run that wrote it"`
	InstanceKey  uint64 `desc:"the window that wrote it; 0 when none"`
}

// ListEntriesResult is list_entries's result.
type ListEntriesResult struct {
	Matched int       `desc:"entries of the last read that match the arguments"`
	Entries []Entry   `desc:"the first limit matches, in the table's order: app, kind, key"`
	ReadAt  time.Time `desc:"when the window last read the table; zero before the first read"`
}

// View is get_view's result: the selection and the state of the window's
// read.
type View struct {
	Selected  string    `desc:"the app whose entries the right pane lists; empty for none"`
	Entries   int       `desc:"entries in the last read"`
	Apps      int       `desc:"apps with at least one entry in the last read"`
	ReadAt    time.Time `desc:"when the window last read the table; zero before the first read"`
	Reads     bool      `desc:"the window has a reader; false where the host minted no bus for it"`
	ReadError string    `desc:"the last read's or verb's error, empty when none"`
	Note      string    `desc:"the last verb's note"`
	Inflight  int       `desc:"verbs in flight"`
}

// SelectAppArgs selects the app whose entries the right pane lists.
type SelectAppArgs struct {
	App string `desc:"an app id from list_entries; empty clears the selection"`
}

func cutText(s string, n int) (out string) {
	out = s
	if len(out) > n {
		out = out[:n] + "…"
	}
	return
}

func entryOf(r entryRow) (e Entry) {
	e = Entry{Kind: r.Kind, App: r.AppId, Key: r.Key, EntityId: r.EntityId, PayloadBytes: r.PayloadBytes,
		Detail: cutText(r.Detail, opsMaxDetailLen), WrittenAt: r.WrittenAt, RunId: r.RunId, InstanceKey: r.InstanceKey}
	return
}

func hasApp(cols *entryCols, appId string) (ok bool) {
	for _, a := range cols.AppId {
		if a == appId {
			return true
		}
	}
	return
}

var ops = func() (s *appops.Set[*App, opsSnap]) {
	s = appops.NewSet((*App).opsSnapshot)

	s.Resource(resSelection, "the app whose entries the right pane lists; empty for none",
		func(inst *App) any { return inst.selected })
	s.Restorable(resSelection, func(inst *App, v any) bool {
		appId, ok := v.(string)
		if ok {
			inst.selected = appId
		}
		return ok
	})
	s.Resource(resRead, "the window's last read of keelson('app_state'): when it landed and what it reported; refresh asks for the next",
		func(inst *App) any { return inst.latchedRead })

	appops.Query(s, app.OperationSpec{Name: opListEntries, Version: 1,
		Summary: "list what apps keep across restarts, from the window's last read of keelson('app_state'), filtered by app and kind, at most 200",
		Agents:  true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, in ListEntriesArgs) (out ListEntriesResult, err error) {
			limit := in.Limit
			switch {
			case limit < 0:
				err = app.RefuseOperation("limit is not negative")
				return
			case limit == 0:
				limit = opsDefaultEntries
			case limit > opsMaxEntries:
				limit = opsMaxEntries
			}
			out.ReadAt = sn.read.refreshed
			appNeedle := strings.ToLower(in.App)
			for _, r := range sn.read.rows.All() {
				if appNeedle != "" && !strings.Contains(strings.ToLower(r.AppId), appNeedle) {
					continue
				}
				if in.Kind != "" && r.Kind != in.Kind {
					continue
				}
				out.Matched++
				if len(out.Entries) < limit {
					out.Entries = append(out.Entries, entryOf(r))
				}
			}
			return
		})

	appops.Query(s, app.OperationSpec{Name: opGetView, Version: 1,
		Summary: "read the selected app and the state of the window's read: when, any error, verbs in flight",
		Agents:  true, Untrusted: true, Fork: app.OperationForkReadShared},
		func(sn opsSnap, _ appops.None) (out View, err error) {
			out = View{Selected: sn.selected, Entries: sn.read.rows.Len(), Apps: len(summarize(&sn.read.rows)),
				ReadAt: sn.read.refreshed, Reads: sn.read.reads, ReadError: sn.read.lastError,
				Note: sn.read.lastNote, Inflight: sn.read.inflight}
			return
		})

	appops.Command(s, app.OperationSpec{Name: opSelectApp, Version: 1,
		Summary: "select the app whose entries the right pane lists; an empty id clears the selection",
		Effect:  app.OperationEffectView, Writes: []string{resSelection}, Agents: true,
		Gesture: "a row of the app list", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, in SelectAppArgs) (out appops.None, err error) {
			if in.App != "" {
				read := inst.snapshot()
				if !hasApp(&read.rows, in.App) {
					err = app.RefuseOperation(unknownAppReason)
					return
				}
			}
			inst.selected = in.App
			return
		})

	appops.Command(s, app.OperationSpec{Name: opRefresh, Version: 1,
		Summary: "ask the window to read keelson('app_state') again; the read lands in a later frame, and get_view's read_at says when",
		Effect:  app.OperationEffectView, Writes: []string{resRead}, Agents: true,
		Gesture: "the Refresh button", Rebuild: app.OperationRebuildVolatile},
		func(inst *App, _ app.OperationCall, _ appops.None) (out appops.None, err error) {
			inst.markDirty()
			return
		})

	return
}()

// Operations serves the catalog for this window (app.OperationsAppI).
func (inst *App) Operations() (h app.OperationsHandlerI) { return ops.Bind(inst) }

// gestureOr routes the person's control through the catalog (ADR-0345
// §SD3), so the command log sees it as theirs, and applies it directly
// where no host serves the catalog (tour, tests).
func gestureOr[In any](inst *App, op string, in In, direct func()) {
	if inst.frameCtx != nil {
		if _, err := appops.Gesture[In, appops.None](inst.frameCtx, op, in); err == nil {
			return
		}
	}
	direct()
}

func (inst *App) selectAppGesture(appId string) {
	gestureOr(inst, opSelectApp, SelectAppArgs{App: appId}, func() { inst.selected = appId })
}

func (inst *App) refreshGesture() {
	gestureOr(inst, opRefresh, appops.None{}, inst.markDirty)
}
