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

var BaseDir = filepath.Join(os.TempDir(), "pijul-demo")

type DemoStore struct {
	mu sync.RWMutex

	Actors     map[string]*ActorState
	Server     *ActorState
	Inbox      []PatchEnvelope
	EditInputs map[string]*string // e.g., "Alice_/contact/email" -> "jane@example.com"

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
		EditInputs: make(map[string]*string),
		TaskQueue:  make(chan Task, 100),
	}

	for _, name := range []string{"Server", "Alice", "Bob", "Charlie"} {
		store.Actors[name] = &ActorState{
			Name: name,
			Path: filepath.Join(BaseDir, strings.ToLower(name)),
		}
	}
	store.Server = store.Actors["Server"]

	// Start Background Worker
	go store.WorkerLoop()

	// Enqueue Initial System Setup
	store.EnqueueTask(store.InitSystem, func(err error) {
		if err != nil {
			fmt.Printf("Init Error: %v\n", err)
			store.Server.LastError = fmt.Sprintf("Init Error:\n%v", err)
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
	// Clean slate
	_ = os.RemoveAll(BaseDir)

	// Create the Server directory (which also creates BaseDir)
	if err := os.MkdirAll(s.Server.Path, 0755); err != nil {
		return fmt.Errorf("mkdir server: %v", err)
	}

	if out, err := runCmd(s.Server.Path, "pijul", "init"); err != nil {
		return fmt.Errorf("pijul init failed: %v\nOutput: %s", err, out)
	}

	baseText := `/id "CUST-100"
/contact/email "jane@example.com"
/account/status "Active"
/company/name "Acme Corp"`

	if err := os.WriteFile(filepath.Join(s.Server.Path, "customer.txt"), []byte(baseText), 0644); err != nil {
		return fmt.Errorf("write base file failed: %v", err)
	}

	if out, err := runCmd(s.Server.Path, "pijul", "add", "customer.txt"); err != nil {
		return fmt.Errorf("pijul add failed: %v\nOutput: %s", err, out)
	}

	if out, err := runCmd(s.Server.Path, "pijul", "record", "-am", "Init Base Record"); err != nil {
		return fmt.Errorf("pijul record failed (Are you missing a Pijul identity?): %v\nOutput: %s", err, out)
	}

	// Clone to Actors
	for _, actor := range []string{"Alice", "Bob", "Charlie"} {
		// Pijul clone expects the target directory to NOT exist yet.
		// It will create /tmp/pijul-demo/{actor} itself.
		if out, err := runCmd(BaseDir, "pijul", "clone", s.Server.Path, strings.ToLower(actor)); err != nil {
			return fmt.Errorf("pijul clone %s failed: %v\nOutput: %s", actor, err, out)
		}
	}
	return nil
}

// Safely rebuilds customer.txt from internal parsed state so we don't accidentally mangle injected conflict lines
func (s *DemoStore) SaveStateToFile(actor string) error {
	state := s.Actors[actor]
	file := filepath.Join(state.Path, "customer.txt")
	var out []string

	for _, l := range state.Lines {
		if l.Conflict != nil {
			out = append(out, ">>>>>>>> "+l.Conflict.AliceHash)
			out = append(out, fmt.Sprintf(`%s "%s"`, l.Path, l.Conflict.AliceValue))
			out = append(out, "========")
			out = append(out, fmt.Sprintf(`%s "%s"`, l.Path, l.Conflict.BobValue))
			out = append(out, "<<<<<<<< "+l.Conflict.BobHash)
		} else {
			out = append(out, fmt.Sprintf(`%s "%s"`, l.Path, l.Value))
		}
	}
	// append trailing newline
	return os.WriteFile(file, []byte(strings.Join(out, "\n")+"\n"), 0644)
}

func (s *DemoStore) SaveEdit(actor string, pathKey string, value string) error {
	state := s.Actors[actor]

	// Update memory state
	for i, l := range state.Lines {
		if l.Path == pathKey {
			state.Lines[i].Value = value
			state.Lines[i].Conflict = nil // Force resolution if user just edits it
			break
		}
	}

	if err := s.SaveStateToFile(actor); err != nil {
		return err
	}

	_, err := runCmd(state.Path, "pijul", "record", "-am", fmt.Sprintf("Updated %s", pathKey))
	return err
}

func (s *DemoStore) PushPull(actor, action string) error {
	state := s.Actors[actor]
	_, err := runCmd(state.Path, "pijul", action, s.Server.Path)
	return err
}

func (s *DemoStore) ResolveConflict(actor, pathKey, winningValue string) error {
	return s.SaveEdit(actor, pathKey, winningValue)
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

	inboxDir := filepath.Join(BaseDir, "inbox")
	_ = os.MkdirAll(inboxDir, 0755)
	patchPath := filepath.Join(inboxDir, latestHash+".patch")

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
	return err
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
		state.Logs = strings.Split(logOut, "\n\n")
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
			hash := strings.TrimSpace(strings.TrimPrefix(line, ">>>>>>>>"))
			cd = &ConflictData{AliceHash: hash}
			continue
		}
		if inConflict {
			if cd == nil {
				cd = &ConflictData{}
			} // failsafe init

			if strings.HasPrefix(line, "========") {
				continue
			}
			if strings.HasPrefix(line, "<<<<<<<<") {
				cd.BobHash = strings.TrimSpace(strings.TrimPrefix(line, "<<<<<<<<"))
				lines = append(lines, KVLine{Path: conflictPath, Conflict: cd})

				// Reset state machine for subsequent keys
				inConflict = false
				cd = nil
				conflictPath = ""
				continue
			}

			// Extract KVs within the conflict block
			parts := strings.SplitN(line, " ", 2)
			if len(parts) == 2 {
				val := strings.Trim(parts[1], `"`)
				if conflictPath == "" {
					conflictPath = parts[0]
				}
				if cd.AliceValue == "" {
					cd.AliceValue = val
				} else if cd.BobValue == "" && cd.AliceValue != "" {
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

func RenderWindow(store *DemoStore) {
	store.mu.RLock()
	defer store.mu.RUnlock()

	ids := c.NewWidgetIdStack()

	// 1. Draw Actor Windows
	actors := []string{"Alice", "Bob", "Charlie"}
	for _, name := range actors {
		for range c.Window(ids.PrepareStr("win_"+name), c.WidgetText().Text(name+" Node").Keep()).DefaultOpen(true).KeepIter() {
			renderActorWindow(store, ids, name)
		}
	}

	// 2. Draw Server Window
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
						if c.Button(ids.PrepareStr(actorName+"_keep_a_"+line.Path), c.Atoms().Text("Keep: "+line.Conflict.AliceValue).Keep()).SendResp().HasPrimaryClicked() {
							val := line.Conflict.AliceValue
							store.EnqueueTask(func() error { return store.ResolveConflict(actorName, line.Path, val) }, func(err error) {})
						}
						if c.Button(ids.PrepareStr(actorName+"_keep_b_"+line.Path), c.Atoms().Text("Keep: "+line.Conflict.BobValue).Keep()).SendResp().HasPrimaryClicked() {
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

					// 1. Get or create a STABLE pointer for this input
					valPtr, ok := store.EditInputs[inputKey]
					if !ok {
						newVal := line.Value // Copy string from file state
						valPtr = &newVal     // Create a stable heap pointer
						store.EditInputs[inputKey] = valPtr
					}

					// 2. Dereference for the initial text value
					edit := c.TextEdit(ids.PrepareStr("edit_"+inputKey), *valPtr).DesiredWidth(200)
					if store.IsProcessing {
						edit = edit.Interactive(false)
					}

					// 3. Pass the stable pointer.
					// The framework will mutate *valPtr directly in the background!
					edit.SendRespVal(valPtr)

					// Save Button
					btn := c.Button(ids.PrepareStr("btn_save_"+inputKey), c.Atoms().Text("Save").Keep())
					if !store.IsProcessing && btn.SendResp().HasPrimaryClicked() {
						// Dereference the stable pointer to get the latest updated value
						capturedVal := *valPtr
						capturedPath := line.Path
						store.EnqueueTask(func() error {
							return store.SaveEdit(actorName, capturedPath, capturedVal)
						}, func(err error) {})
					}
				}
			}
		}

		c.Separator().Horizontal().Send()

		// 3. Error Display
		if state.LastError != "" {
			c.Label("🔴 Pijul Output/Error:").Send()
			for range c.ScrollArea().KeepIter() {
				c.Label(state.LastError).Send()
			}
			c.Separator().Horizontal().Send()
		}

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

	if state.LastError != "" {
		c.Label("🔴 Server Fatal Error: " + state.LastError).Send()
		c.Separator().Horizontal().Send()
	}

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

				// Buttons to apply patch manually
				for _, peer := range []string{"Alice", "Bob", "Charlie"} {
					if peer == patch.FromActor {
						continue
					}

					btnID := fmt.Sprintf("btn_apply_%d_%s", i, peer)
					btn := c.Button(ids.PrepareStr(btnID), c.Atoms().Text("Apply to "+peer).Keep())

					if !store.IsProcessing && btn.SendResp().HasPrimaryClicked() {
						capturedPeer := peer
						capturedPatch := patch.PatchPath
						store.EnqueueTask(func() error {
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
