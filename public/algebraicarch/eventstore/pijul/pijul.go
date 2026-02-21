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
