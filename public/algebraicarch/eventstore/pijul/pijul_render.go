package pijul

import (
	"fmt"

	c "github.com/stergiotis/pebble2impl/src/go/public/thestack/imzero2/egui2/components"
)

// ---------------------------------------------------------------------------
// 5. ImZero2 UI View Architecture
// ---------------------------------------------------------------------------

// A single, global ID stack for the entire application that survives frames
var GlobalIDs = c.NewWidgetIdStack()

func RenderWindow(store *DemoStore) {
	// 1. Process Pending Overrides SYNCHRONOUSLY within the frame!
	store.mu.Lock()
	if len(store.PendingOverrides) > 0 {
		for ptr, val := range store.PendingOverrides {
			*ptr = val
			// This is now safe because we are inside the active UI frame lifecycle!
			c.CurrentApplicationState.StateManager.OverrideDatabindingSPtr(ptr)
		}
		// Clear the queue
		store.PendingOverrides = make(map[*string]string)
	}
	store.mu.Unlock()

	// 2. Standard Render Pass
	store.mu.RLock()
	defer store.mu.RUnlock()

	// A. Draw Actor Windows
	actors := []string{"Alice", "Bob", "Charlie"}
	for _, name := range actors {
		for range c.IdScope(GlobalIDs.PrepareStr("scope_win_" + name)) {
			for range c.Window(GlobalIDs.PrepareStr("window"), c.WidgetText().Text(name+" Node").Keep()).DefaultOpen(true).KeepIter() {
				renderActorWindow(store, GlobalIDs, name)
			}
		}
	}

	// B. Draw Server Window
	for range c.IdScope(GlobalIDs.PrepareStr("scope_server")) {
		for range c.Window(GlobalIDs.PrepareStr("window"), c.WidgetText().Text("Server & Global Inbox").Keep()).DefaultOpen(true).KeepIter() {
			renderServerWindow(store, GlobalIDs)
		}
	}

	// C. Draw Storyboard Window
	for range c.IdScope(GlobalIDs.PrepareStr("scope_storyboard")) {
		for range c.Window(GlobalIDs.PrepareStr("window"), c.WidgetText().Text("Demo Storyboard").Keep()).DefaultOpen(true).KeepIter() {
			renderStoryboardWindow(store, GlobalIDs)
		}
	}
}

func renderActorWindow(store *DemoStore, ids *c.WidgetIdStack, actorName string) {
	state := store.Actors[actorName]

	for range c.Vertical().KeepIter() {

		// 1. Toolbar
		for range c.IdScope(ids.PrepareStr("toolbar")) {
			for range c.Horizontal().KeepIter() {
				renderActionButton(store, ids, actorName, "Push", func() ([]string, error) { return store.PushPull(actorName, "push") })
				renderActionButton(store, ids, actorName, "Pull", func() ([]string, error) { return store.PushPull(actorName, "pull") })
				renderActionButton(store, ids, actorName, "Email Patch", func() ([]string, error) { return store.EmailPatch(actorName) })
			}
		}
		c.Separator().Horizontal().Send()

		// 2. KV Data Visualizer
		c.Label("Data Record: customer.txt").Send()
		for _, line := range state.Lines {

			// ID Scope per row prevents dynamic keys from causing ID drift below them
			for range c.IdScope(ids.PrepareStr("line_" + line.Path)) {
				if line.Conflict != nil {
					// CONFLICT BLOCK
					for range c.Vertical().KeepIter() {
						c.Label("⚠️ CONFLICT ON: " + line.Path).Send()
						for range c.Horizontal().KeepIter() {

							btnA := c.Button(ids.PrepareStr("keep_a"), c.Atoms().Text("Keep: "+line.Conflict.AliceValue).Keep())
							if btnA.SendResp().HasPrimaryClicked() && !store.IsProcessing {
								val := line.Conflict.AliceValue
								store.EnqueueTask(func() ([]string, error) {
									return store.ResolveConflict(actorName, line.Path, val)
								}, func(err error) {})
							}

							btnB := c.Button(ids.PrepareStr("keep_b"), c.Atoms().Text("Keep: "+line.Conflict.BobValue).Keep())
							if btnB.SendResp().HasPrimaryClicked() && !store.IsProcessing {
								val := line.Conflict.BobValue
								store.EnqueueTask(func() ([]string, error) {
									return store.ResolveConflict(actorName, line.Path, val)
								}, func(err error) {})
							}
						}
					}
				} else {
					// STANDARD KV EDIT BLOCK
					for range c.Horizontal().KeepIter() {
						c.Label(line.Path).Send()

						inputKey := actorName + "_" + line.Path
						valPtr, ok := store.EditInputs[inputKey]
						if !ok {
							newVal := line.Value
							valPtr = &newVal
							store.EditInputs[inputKey] = valPtr
						}

						edit := c.TextEdit(ids.PrepareStr("edit"), *valPtr).DesiredWidth(200)
						if store.IsProcessing {
							edit = edit.Interactive(false)
						}
						edit.SendRespVal(valPtr)

						btn := c.Button(ids.PrepareStr("btn_save"), c.Atoms().Text("Save").Keep())
						// Evaluate SendResp FIRST so the button never vanishes due to short-circuiting
						if btn.SendResp().HasPrimaryClicked() && !store.IsProcessing {
							capturedVal := *valPtr
							capturedPath := line.Path
							store.EnqueueTask(func() ([]string, error) {
								return store.SaveEdit(actorName, capturedPath, capturedVal)
							}, func(err error) {})
						}
					}
				}
			}
		}

		c.Separator().Horizontal().Send()

		// 3. Error Display (Scoped so it doesn't drift the blocks below)
		if state.LastError != "" {
			for range c.IdScope(ids.PrepareStr("error_block")) {
				c.Label("🔴 Fatal Error:").Send()
				for range c.ScrollArea().KeepIter() {
					c.Label(state.LastError).Send()
				}
				c.Separator().Horizontal().Send()
			}
		}

		// 4. CLI Observability Log (New!)
		for range c.IdScope(ids.PrepareStr("cli_logs")) {
			c.Label("CLI Operations Log:").Send()
			for range c.ScrollArea().KeepIter() {
				// Iterate backwards to show newest commands at the top
				for i := len(state.CliLogs) - 1; i >= 0; i-- {
					c.Label(state.CliLogs[i]).Send()
					c.Separator().Horizontal().Send()
				}
			}
		}

		// 5. Pijul History
		for range c.IdScope(ids.PrepareStr("history_block")) {
			c.Label("Local Pijul History:").Send()
			for range c.ScrollArea().KeepIter() {
				for _, logItem := range state.Logs {
					c.Label(logItem).Send()
					c.Separator().Horizontal().Send()
				}
			}
		}
	}
}

func renderServerWindow(store *DemoStore, ids *c.WidgetIdStack) {
	state := store.Server

	if state.LastError != "" {
		for range c.IdScope(ids.PrepareStr("server_error")) {
			c.Label("🔴 Server Fatal Error: " + state.LastError).Send()
			c.Separator().Horizontal().Send()
		}
	}

	for range c.Vertical().KeepIter() {
		// 1. Origin State
		for range c.IdScope(ids.PrepareStr("origin_state")) {
			c.Label("=== Central Origin State ===").Send()
			for _, line := range state.Lines {
				c.Label(fmt.Sprintf("%s = %s", line.Path, line.Value)).Send()
			}
			c.Separator().Horizontal().Send()
		}

		// 2. Inbox
		for range c.IdScope(ids.PrepareStr("inbox_block")) {
			c.Label("=== Patch Inbox ===").Send()
			for i, patch := range store.Inbox {
				// Scope each patch row
				for range c.IdScope(ids.PrepareStr(fmt.Sprintf("patch_%d", i))) {
					for range c.Horizontal().KeepIter() {
						c.Label(fmt.Sprintf("Patch from %s (%s)", patch.FromActor, patch.Hash[:8])).Send()

						for _, peer := range []string{"Alice", "Bob", "Charlie"} {
							if peer == patch.FromActor {
								continue
							}

							btn := c.Button(ids.PrepareStr("btn_apply_"+peer), c.Atoms().Text("Apply to "+peer).Keep())

							if btn.SendResp().HasPrimaryClicked() && !store.IsProcessing {
								capturedPeer := peer
								capturedPatch := patch.PatchPath
								store.EnqueueTask(func() ([]string, error) {
									return store.ApplyPatch(capturedPeer, capturedPatch)
								}, func(err error) {
									if err != nil {
										store.Actors[capturedPeer].LastError = err.Error()
									} else {
										store.Actors[capturedPeer].LastError = ""
									}
								})
							}
						}
					}
				}
			}
			c.Separator().Horizontal().Send()
		}

		// 3. CLI Observability Log for Server Operations
		for range c.IdScope(ids.PrepareStr("cli_logs")) {
			c.Label("Server CLI Operations Log:").Send()
			for range c.ScrollArea().KeepIter() {
				for i := len(state.CliLogs) - 1; i >= 0; i-- {
					c.Label(state.CliLogs[i]).Send()
					c.Separator().Horizontal().Send()
				}
			}
		}
	}
}

func renderStoryboardWindow(store *DemoStore, ids *c.WidgetIdStack) {
	for range c.Vertical().KeepIter() {
		c.Label("Use this assistant to walk through the Event Sourcing capabilities.").Send()
		c.Separator().Horizontal().Send()

		renderPlaybook(store, ids)
	}
}


// ---------------------------------------------------------------------------
// 6. Helpers
// ---------------------------------------------------------------------------

func renderActionButton(store *DemoStore, ids *c.WidgetIdStack, actor, label string, action func() ([]string, error)) {
	btn := c.Button(ids.PrepareStr("btn_"+label), c.Atoms().Text(label).Keep())
	if btn.SendResp().HasPrimaryClicked() && !store.IsProcessing {
		store.EnqueueTask(action, func(err error) {
			if err != nil {
				store.Actors[actor].LastError = err.Error()
			} else {
				store.Actors[actor].LastError = ""
			}
		})
	}
}
