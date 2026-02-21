package pijul

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	c "github.com/stergiotis/pebble2impl/src/go/public/thestack/imzero2/egui2/components"
)

// ---------------------------------------------------------------------------
// 1. Data Models & Store
// ---------------------------------------------------------------------------

type DemoStore struct {
	mu sync.RWMutex

	Actors     map[string]*ActorState
	Server     *ActorState
	Inbox      []PatchEnvelope
	EditInputs map[string]string // e.g., "Alice_/contact/email" -> "jane@example.com"

	// Global Queue to keep UI thread unblocked
	TaskQueue    chan Task
	IsProcessing bool
}

type ActorState struct {
	Name        string
	Path        string
	Lines       []KVLine
	HasConflict bool
	Logs        []string
	LastError   string
}

type KVLine struct {
	Path     string
	Value    string
	Conflict *ConflictData
}

type ConflictData struct {
	AliceHash  string
	AliceValue string
	BobHash    string
	BobValue   string
}

type PatchEnvelope struct {
	FromActor string
	Hash      string
	PatchPath string
}

type Task struct {
	Action func() error
	OnDone func(err error)
}

// ---------------------------------------------------------------------------
// 2. Core Initializer & Task Runner
// ---------------------------------------------------------------------------

func NewDemoStore() *DemoStore {
	store := &DemoStore{
		Actors:     make(map[string]*ActorState),
		EditInputs: make(map[string]string),
		TaskQueue:  make(chan Task, 100),
	}

	for _, name := range []string{"Server", "Alice", "Bob", "Charlie"} {
		store.Actors[name] = &ActorState{
			Name: name,
			Path: filepath.Join("/tmp/pijul-demo", strings.ToLower(name)),
		}
	}
	store.Server = store.Actors["Server"]

	// Start Background Worker
	go store.WorkerLoop()

	// Enqueue Initial System Setup
	store.EnqueueTask(store.InitSystem, func(err error) {
		if err != nil {
			fmt.Println("Init Error:", err)
		}
	})

	return store
}

func (s *DemoStore) WorkerLoop() {
	for task := range s.TaskQueue {
		// Lock to indicate UI processing
		s.mu.Lock()
		s.IsProcessing = true
		s.mu.Unlock()

		err := task.Action()

		s.mu.Lock()
		task.OnDone(err)
		s.ReloadAllActors()
		s.IsProcessing = false
		s.mu.Unlock()
	}
}

func (s *DemoStore) EnqueueTask(action func() error, onDone func(err error)) {
	s.TaskQueue <- Task{Action: action, OnDone: onDone}
}

// ---------------------------------------------------------------------------
// 3. Pijul OS/Exec Wrappers
// ---------------------------------------------------------------------------

func runCmd(dir string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("CMD Error: %v\nStderr: %s", err, errBuf.String())
	}
	return outBuf.String(), nil
}

func (s *DemoStore) InitSystem() error {
	_ = os.RemoveAll("/tmp/pijul-demo")
	_ = os.MkdirAll("/tmp/pijul-demo/server", 0755)

	// Init Server
	if _, err := runCmd(s.Server.Path, "pijul", "init"); err != nil {
		return err
	}

	baseText := `/id "CUST-100"
/contact/email "jane@example.com"
/account/status "Active"
/company/name "Acme Corp"`

	_ = os.WriteFile(filepath.Join(s.Server.Path, "customer.txt"), []byte(baseText), 0644)

	runCmd(s.Server.Path, "pijul", "add", "customer.txt")
	runCmd(s.Server.Path, "pijul", "record", "-am", "Init Base Record")

	// Clone to Actors
	for _, actor := range []string{"Alice", "Bob", "Charlie"} {
		path := s.Actors[actor].Path
		_ = os.MkdirAll(path, 0755)
		if _, err := runCmd("/tmp/pijul-demo", "pijul", "clone", s.Server.Path, strings.ToLower(actor)); err != nil {
			return err
		}
	}
	return nil
}

func (s *DemoStore) SaveEdit(actor string, pathKey string, value string) error {
	state := s.Actors[actor]
	file := filepath.Join(state.Path, "customer.txt")

	// Poor-man's flat KV updater (assuming no active conflict markers here)
	content, _ := os.ReadFile(file)
	lines := strings.Split(string(content), "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, pathKey+" ") {
			lines[i] = fmt.Sprintf(`%s "%s"`, pathKey, value)
			break
		}
	}
	_ = os.WriteFile(file, []byte(strings.Join(lines, "\n")), 0644)

	_, err := runCmd(state.Path, "pijul", "record", "-am", fmt.Sprintf("Updated %s", pathKey))
	return err
}

func (s *DemoStore) PushPull(actor, action string) error {
	state := s.Actors[actor]
	_, err := runCmd(state.Path, "pijul", action, s.Server.Path)
	return err
}

func (s *DemoStore) ResolveConflict(actor, pathKey, winningValue string) error {
	// Simple resolution: forces the file back to clean state for that key
	s.SaveEdit(actor, pathKey, winningValue)
	return nil
}

func (s *DemoStore) EmailPatch(actor string) error {
	state := s.Actors[actor]
	logOut, err := runCmd(state.Path, "pijul", "log", "--hash-only")
	if err != nil {
		return err
	}

	hashes := strings.Split(strings.TrimSpace(logOut), "\n")
	if len(hashes) == 0 {
		return nil
	}
	latestHash := hashes[0]

	_ = os.MkdirAll("/tmp/pijul-demo/inbox", 0755)
	patchPath := filepath.Join("/tmp/pijul-demo/inbox", latestHash+".patch")

	// Export Patch
	cmd := exec.Command("pijul", "change", latestHash)
	cmd.Dir = state.Path
	outFile, _ := os.Create(patchPath)
	cmd.Stdout = outFile
	_ = cmd.Run()
	outFile.Close()

	s.Inbox = append(s.Inbox, PatchEnvelope{
		FromActor: actor,
		Hash:      latestHash,
		PatchPath: patchPath,
	})
	return nil
}

func (s *DemoStore) ApplyPatch(actor string, patchPath string) error {
	state := s.Actors[actor]
	_, err := runCmd(state.Path, "pijul", "apply", patchPath)
	return err // We intentionally return this so the UI can catch dependency errors
}

// ---------------------------------------------------------------------------
// 4. File Parsing & Conflict State Machine
// ---------------------------------------------------------------------------

func (s *DemoStore) ReloadAllActors() {
	for _, state := range s.Actors {
		content, err := os.ReadFile(filepath.Join(state.Path, "customer.txt"))
		if err != nil {
			continue
		}

		state.Lines, state.HasConflict = ParsePijulFile(string(content))

		logOut, _ := runCmd(state.Path, "pijul", "log", "--description")
		state.Logs = strings.Split(logOut, "\n\n") // rough block separation
	}
}

func ParsePijulFile(content string) ([]KVLine, bool) {
	var lines []KVLine
	hasConflict := false
	scanner := bufio.NewScanner(strings.NewReader(content))

	inConflict := false
	var cd *ConflictData
	var conflictPath string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, ">>>>>>>>") {
			inConflict = true
			hasConflict = true
			cd = &ConflictData{AliceHash: strings.TrimSpace(strings.TrimPrefix(line, ">>>>>>>>"))}
			continue
		}
		if inConflict {
			if strings.HasPrefix(line, "========") {
				continue
			}
			if strings.HasPrefix(line, "<<<<<<<<") {
				cd.BobHash = strings.TrimSpace(strings.TrimPrefix(line, "<<<<<<<<"))
				lines = append(lines, KVLine{Path: conflictPath, Conflict: cd})
				inConflict = false
				cd = nil
				continue
			}
			// Parse KV during conflict block
			parts := strings.SplitN(line, " ", 2)
			if len(parts) == 2 {
				val := strings.Trim(parts[1], `"`)
				if conflictPath == "" {
					conflictPath = parts[0]
				}
				if cd.AliceValue == "" {
					cd.AliceValue = val
				} else if cd.BobValue == "" {
					cd.BobValue = val
				}
			}
		} else {
			parts := strings.SplitN(line, " ", 2)
			if len(parts) == 2 {
				lines = append(lines, KVLine{Path: parts[0], Value: strings.Trim(parts[1], `"`)})
			}
		}
	}
	return lines, hasConflict
}

// ---------------------------------------------------------------------------
// 5. ImZero2 UI View Architecture
// ---------------------------------------------------------------------------

var ids = c.NewWidgetIdStack()

func RenderWindow(store *DemoStore) {
	store.mu.RLock()
	defer store.mu.RUnlock()

	// 1. Draw Actor Windows
	actors := []string{"Alice", "Bob", "Charlie"}
	for _, name := range actors {
		for range c.Window(ids.PrepareStr("win_"+name), c.WidgetText().Text(name+" Node").Keep()).DefaultOpen(true).KeepIter() {
			renderActorWindow(store, ids, name)
		}
	}

	// 2. Draw Server & Inbox Window
	for range c.Window(ids.PrepareStr("win_server"), c.WidgetText().Text("Server & Global Inbox").Keep()).DefaultOpen(true).KeepIter() {
		renderServerWindow(store, ids)
	}
}

func renderActorWindow(store *DemoStore, ids *c.WidgetIdStack, actorName string) {
	state := store.Actors[actorName]

	for range c.Vertical().KeepIter() {

		// 1. Toolbar
		for range c.Horizontal().KeepIter() {
			renderActionButton(store, ids, actorName, "Push", func() error { return store.PushPull(actorName, "push") })
			renderActionButton(store, ids, actorName, "Pull", func() error { return store.PushPull(actorName, "pull") })
			renderActionButton(store, ids, actorName, "Email Patch", func() error { return store.EmailPatch(actorName) })
		}
		c.Separator().Horizontal().Send()

		// 2. KV Data Visualizer
		c.Label("Data Record: customer.txt").Send()
		for _, line := range state.Lines {
			if line.Conflict != nil {
				// CONFLICT BLOCK
				for range c.Vertical().KeepIter() {
					c.Label("⚠️ CONFLICT ON: " + line.Path).Send()
					for range c.Horizontal().KeepIter() {
						if c.Button(ids.PrepareStr(actorName+"_keep_a"), c.Atoms().Text("Keep Alice's: "+line.Conflict.AliceValue).Keep()).SendResp().HasPrimaryClicked() {
							val := line.Conflict.AliceValue
							store.EnqueueTask(func() error { return store.ResolveConflict(actorName, line.Path, val) }, func(err error) {})
						}
						if c.Button(ids.PrepareStr(actorName+"_keep_b"), c.Atoms().Text("Keep Bob's: "+line.Conflict.BobValue).Keep()).SendResp().HasPrimaryClicked() {
							val := line.Conflict.BobValue
							store.EnqueueTask(func() error { return store.ResolveConflict(actorName, line.Path, val) }, func(err error) {})
						}
					}
				}
			} else {
				// STANDARD KV EDIT BLOCK
				for range c.Horizontal().KeepIter() {
					c.Label(line.Path).Send()

					inputKey := actorName + "_" + line.Path
					val, ok := store.EditInputs[inputKey]
					if !ok {
						val = line.Value // Default to file state
					}

					edit := c.TextEdit(ids.PrepareStr("edit_"+inputKey), val)
					if store.IsProcessing {
						edit = edit.Interactive(false) // Lock text edit during disk io
					}

					if edit.SendRespVal(&val).HasChanged() {
						store.EditInputs[inputKey] = val
					}

					// Save Button
					btn := c.Button(ids.PrepareStr("btn_save_"+inputKey), c.Atoms().Text("Save").Keep())
					if !store.IsProcessing && btn.SendResp().HasPrimaryClicked() {
						// Pass captures safely
						capturedVal := val
						capturedPath := line.Path
						store.EnqueueTask(func() error { return store.SaveEdit(actorName, capturedPath, capturedVal) }, func(err error) {})
					}
				}
			}
		}

		c.Separator().Horizontal().Send()

		// 3. Error Display (Playbook 2 Step 8)
		if state.LastError != "" {
			c.Label("🔴 Pijul Error:").Send()
			for range c.ScrollArea().KeepIter() {
				c.Label(state.LastError).Send()
			}
		}

		c.Separator().Horizontal().Send()

		// 4. Pijul History
		c.Label("Local Pijul History:").Send()
		for range c.ScrollArea().KeepIter() {
			for _, logItem := range state.Logs {
				c.Label(logItem).Send()
				c.Separator().Horizontal().Send()
			}
		}
	}
}

func renderServerWindow(store *DemoStore, ids *c.WidgetIdStack) {
	state := store.Server

	for range c.Vertical().KeepIter() {
		c.Label("=== Central Origin State ===").Send()
		for _, line := range state.Lines {
			c.Label(fmt.Sprintf("%s = %s", line.Path, line.Value)).Send()
		}

		c.Separator().Horizontal().Send()

		c.Label("=== Patch Inbox ===").Send()
		for i, patch := range store.Inbox {
			for range c.Horizontal().KeepIter() {
				c.Label(fmt.Sprintf("Patch from %s (%s)", patch.FromActor, patch.Hash[:8])).Send()

				// Buttons to apply patch manually to peers (Playbook 2, peer-to-peer sync)
				for _, peer := range []string{"Alice", "Bob", "Charlie"} {
					if peer == patch.FromActor {
						continue
					} // Can't apply to self

					btnID := fmt.Sprintf("btn_apply_%d_%s", i, peer)
					btn := c.Button(ids.PrepareStr(btnID), c.Atoms().Text("Apply to "+peer).Keep())

					if !store.IsProcessing && btn.SendResp().HasPrimaryClicked() {
						capturedPeer := peer
						capturedPatch := patch.PatchPath
						store.EnqueueTask(func() error {
							return store.ApplyPatch(capturedPeer, capturedPatch)
						}, func(err error) {
							if err != nil {
								// Feed the stderr text to the target actor's window directly
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
}

// ---------------------------------------------------------------------------
// 6. Helpers
// ---------------------------------------------------------------------------

func renderActionButton(store *DemoStore, ids *c.WidgetIdStack, actor, label string, action func() error) {
	btn := c.Button(ids.PrepareStr("btn_"+actor+"_"+label), c.Atoms().Text(label).Keep())
	if !store.IsProcessing && btn.SendResp().HasPrimaryClicked() {
		store.EnqueueTask(action, func(err error) {
			if err != nil {
				store.Actors[actor].LastError = err.Error()
			} else {
				store.Actors[actor].LastError = ""
			}
		})
	}
}
