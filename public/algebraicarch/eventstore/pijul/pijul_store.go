//go:build llm_generated_gemini3pro

package pijul

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// 1. Data Models & Store
// ---------------------------------------------------------------------------

var BaseDir = filepath.Join(os.TempDir(), "pijul-demo")

type DemoStore struct {
	mu sync.RWMutex

	Actors           map[string]*ActorState
	Server           *ActorState
	Inbox            []PatchEnvelope
	EditInputs       map[string]*string // e.g., "Alice_/contact/email" -> "jane@example.com"
	ChecklistState   map[string]*bool
	PendingOverrides map[*string]string

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
	CliLogs     []string
}

type KVLine struct {
	Path         string
	Value        string
	Conflict     *ConflictData
	CreditHash   string // NEW: Cell-level provenance hash
	CreditAuthor string // NEW: Cell-level provenance author
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
	Action func() (actors []string, err error)
	OnDone func(err error)
}

// PijulLogEntry maps the output of `pijul log --output-format json`
type PijulLogEntry struct {
	Hash       string    `json:"hash"`
	Authors    []string  `json:"authors"`
	Timestamp  string    `json:"timestamp"`
	Message    string    `json:"message"`
	ParsedTime time.Time // Used to calculate the true original author
}

// ---------------------------------------------------------------------------
// 2. Core Initializer & Task Runner
// ---------------------------------------------------------------------------

func NewDemoStore() *DemoStore {
	store := &DemoStore{
		Actors:           make(map[string]*ActorState),
		EditInputs:       make(map[string]*string),
		ChecklistState:   make(map[string]*bool),
		PendingOverrides: make(map[*string]string),
		TaskQueue:        make(chan Task, 100),
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
		s.mu.Lock()
		s.IsProcessing = true
		s.mu.Unlock()

		// 1. Execute and get targets
		affectedActors, err := task.Action()

		time.Sleep(300 * time.Millisecond)

		s.mu.Lock()
		task.OnDone(err)

		s.ReloadAllActors() // Parse disk for everyone, strictly safe.

		// 2. Targeted Cache Override (Fixes the State Bleed!)
		for _, actorName := range affectedActors {
			if state, ok := s.Actors[actorName]; ok {
				for _, line := range state.Lines {
					if line.Conflict == nil {
						inputKey := state.Name + "_" + line.Path
						if valPtr, exists := s.EditInputs[inputKey]; exists {
							if *valPtr != line.Value {
								// Queue the pointer for a synchronous override
								s.PendingOverrides[valPtr] = line.Value
							}
						} else {
							newVal := line.Value
							s.EditInputs[inputKey] = &newVal
						}
					}
				}
			}
		}

		s.IsProcessing = false
		s.mu.Unlock()
	}
}

func (s *DemoStore) EnqueueTask(action func() ([]string, error), onDone func(err error)) {
	s.TaskQueue <- Task{Action: action, OnDone: onDone}
}

// ---------------------------------------------------------------------------
// 3. Pijul OS/Exec Wrappers
// ---------------------------------------------------------------------------

func (s *DemoStore) InitSystem() ([]string, error) {
	_ = os.RemoveAll(BaseDir)
	_ = os.MkdirAll(s.Server.Path, 0755)
	// In InitSystem(), update the record command:

	_, err := s.runCmd("Server", s.Server.Path, "pijul", "init")
	if err != nil {
		return nil, err
	}

	baseText := "/id \"CUST-100\"\n/contact/name \"Jane Doe\"\n/contact/email \"jane@example.com\"\n/contact/phone \"555-0000\"\n/account/status \"Active\"\n/account/created \"2023-01-01\"\n/company/name \"Acme Corp\"\n/company/address \"123 Main St\"\n"
	_ = os.WriteFile(filepath.Join(s.Server.Path, "customer.txt"), []byte(baseText), 0644)

	_, _ = s.runCmd("Server", s.Server.Path, "pijul", "add", "customer.txt")
	_, _ = s.runCmd("Server", s.Server.Path, "pijul", "record", "--author", "System", "-am", "Init Base Record")

	for _, actor := range []string{"Alice", "Bob", "Charlie"} {
		_, err := s.runCmd(actor, BaseDir, "pijul", "clone", s.Server.Path, strings.ToLower(actor))
		if err != nil {
			return nil, err
		}
	}
	return []string{"Server", "Alice", "Bob", "Charlie"}, nil
}

func (s *DemoStore) SaveEdit(actor string, pathKey string, value string) ([]string, error) {
	// 1. Lock around memory mutation and file writing
	s.mu.Lock()
	state := s.Actors[actor]
	for i, l := range state.Lines {
		if l.Path == pathKey {
			state.Lines[i].Value = value
			state.Lines[i].Conflict = nil
			break
		}
	}
	err := s.SaveStateToFile(actor)
	s.mu.Unlock()

	if err != nil {
		return nil, err
	}

	// 2. Execute the CLI command (this acquires its own lock internally)
	_, err = s.runCmd(actor, state.Path, "pijul", "record", "--author", actor, "-am", fmt.Sprintf("Updated %s", pathKey))

	return []string{actor}, err
}

func (s *DemoStore) PushPull(actor, action string) ([]string, error) {
	state := s.Actors[actor]
	_, err := s.runCmd(actor, state.Path, "pijul", action, "--all", s.Server.Path)

	affected := []string{actor}
	if action == "push" {
		affected = append(affected, "Server")
	}
	return affected, err
}

func (s *DemoStore) ResolveConflict(actor, pathKey, winningValue string) ([]string, error) {
	return s.SaveEdit(actor, pathKey, winningValue)
}

func (s *DemoStore) ApplyPatch(actor string, patchPath string) ([]string, error) {
	state := s.Actors[actor]
	_, err := s.runCmd(actor, state.Path, "pijul", "apply", patchPath)
	return []string{actor}, err
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

		cmdLog := exec.Command("pijul", "log", "--output-format", "json")
		cmdLog.Dir = state.Path
		outLog, _ := cmdLog.Output()

		logMap := make(map[string]PijulLogEntry)
		state.Logs = parsePijulLogJSON(outLog, logMap)

		if !state.HasConflict {
			cmdCredit := exec.Command("pijul", "credit", "customer.txt")
			cmdCredit.Dir = state.Path
			outCredit, _ := cmdCredit.Output()

			state.Lines = applyCreditToLines(string(outCredit), state.Lines, logMap)
		}
	}
}

func parsePijulLogJSON(logOut []byte, logMap map[string]PijulLogEntry) []string {
	if len(logOut) == 0 {
		return nil
	}

	var entries []PijulLogEntry
	if err := json.Unmarshal(logOut, &entries); err != nil {
		return []string{fmt.Sprintf("[Error parsing Pijul JSON log: %v]", err)}
	}

	var formattedLogs []string
	for i, entry := range entries {
		// Default Author
		if len(entry.Authors) == 0 || entry.Authors[0] == "" {
			entries[i].Authors = []string{"System"}
		}

		// Parse Time
		t, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
		if err == nil {
			entries[i].ParsedTime = t
		}

		logMap[entry.Hash] = entries[i]

		block := fmt.Sprintf("Change %s\nAuthor: %s\nDate: %s\n\n    %s",
			entry.Hash, entries[i].Authors[0], t.Local().Format("2006-01-02 15:04:05 (MST)"), entry.Message)
		formattedLogs = append(formattedLogs, block)
	}
	return formattedLogs
}

// Maps block-based `pijul credit` output using graph-theory age resolution
func applyCreditToLines(creditOut string, lines []KVLine, logMap map[string]PijulLogEntry) []KVLine {
	contentToEntry := make(map[string]PijulLogEntry)
	var currentHashes []string

	scanner := bufio.NewScanner(strings.NewReader(creditOut))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "> ") {
			content := strings.TrimSpace(strings.TrimPrefix(line, "> "))

			// Find the OLDEST patch among the introducing edges
			var oldestEntry PijulLogEntry
			found := false

			for _, shortHash := range currentHashes {
				shortHash = strings.TrimSpace(shortHash)
				for fullHash, entry := range logMap {
					// Pijul Rust source confirmed: credit outputs exactly the first 12 chars
					if strings.HasPrefix(fullHash, shortHash) {
						if !found || entry.ParsedTime.Before(oldestEntry.ParsedTime) {
							oldestEntry = entry
							found = true
						}
					}
				}
			}

			if found {
				contentToEntry[content] = oldestEntry
			}
		} else {
			// It's a header block: "EYPWGEPXCHFD, JYS6SYSP25AS"
			currentHashes = strings.Split(line, ",")
		}
	}

	for i, l := range lines {
		if l.Conflict != nil {
			continue
		}

		expectedContent := fmt.Sprintf(`%s "%s"`, l.Path, l.Value)
		if entry, ok := contentToEntry[expectedContent]; ok {
			lines[i].CreditHash = entry.Hash[:8] // Short UI hash
			lines[i].CreditAuthor = entry.Authors[0]
		}
	}
	return lines
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

		// Matches >>>>>>> and >>>>>>>>
		if strings.HasPrefix(line, ">>>>>>>") {
			inConflict = true
			hasConflict = true
			hash := strings.TrimSpace(strings.TrimLeft(line, ">"))
			cd = &ConflictData{AliceHash: hash}
			continue
		}
		if inConflict {
			if cd == nil {
				cd = &ConflictData{}
			}

			if strings.HasPrefix(line, "=======") {
				continue
			}
			if strings.HasPrefix(line, "<<<<<<<") {
				hash := strings.TrimSpace(strings.TrimLeft(line, "<"))
				cd.BobHash = hash
				lines = append(lines, KVLine{Path: conflictPath, Conflict: cd})

				inConflict = false
				cd = nil
				conflictPath = ""
				continue
			}

			// Extract KVs within the conflict block
			parts := strings.SplitN(line, " ", 2)
			if len(parts) >= 2 {
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
			if len(parts) >= 2 {
				lines = append(lines, KVLine{Path: parts[0], Value: strings.Trim(parts[1], `"`)})
			}
		}
	}
	return lines, hasConflict
}

// ---------------------------------------------------------------------------
// Backend Execution & File Helpers
// ---------------------------------------------------------------------------

// runCmd executes a shell command with a 15-second timeout, captures the output,
// and safely routes the formatted execution log to the specified actor's UI window.
func (s *DemoStore) runCmd(actorName string, dir string, name string, args ...string) (string, error) {
	// 1. Context with 15-second Timeout
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmdStr := fmt.Sprintf("$ %s %s", name, strings.Join(args, " "))

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()

	// 2. Format Observability Log
	logEntry := cmdStr
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			logEntry += "\n[TIMEOUT 15s]"
		} else {
			logEntry += fmt.Sprintf("\n[ERROR] %v\n%s", err, errBuf.String())
		}
	} else {
		out := strings.TrimSpace(outBuf.String())
		if out != "" {
			logEntry += fmt.Sprintf("\n%s", out)
		} else {
			logEntry += "\n[OK]"
		}
	}

	// 3. Thread-safely append to the Actor's window
	s.mu.Lock()
	if state, ok := s.Actors[actorName]; ok {
		state.CliLogs = append(state.CliLogs, logEntry)
		// Keep log tidy (cap at last 30 commands)
		if len(state.CliLogs) > 30 {
			state.CliLogs = state.CliLogs[1:]
		}
	}
	s.mu.Unlock()

	// 4. Return robust errors
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("command timed out: %s", cmdStr)
		}
		return "", fmt.Errorf("CMD Error: %v\nStderr: %s", err, errBuf.String())
	}
	return outBuf.String(), nil
}

// EmailPatch grabs the latest binary change file from Pijul's internal directory
// and exports it to the shared global Inbox.
func (s *DemoStore) EmailPatch(actor string) ([]string, error) {
	state := s.Actors[actor]

	// 1. Find the newest binary patch file in .pijul/changes
	// (Pijul splits hashes into folders, so checking ModTime is the most robust lookup)
	changeDir := filepath.Join(state.Path, ".pijul", "changes")
	var srcFile string
	var maxTime time.Time

	err := filepath.Walk(changeDir, func(p string, info os.FileInfo, e error) error {
		if e == nil && !info.IsDir() {
			if info.ModTime().After(maxTime) {
				maxTime = info.ModTime()
				srcFile = p
			}
		}
		return nil
	})

	if err != nil || srcFile == "" {
		return []string{actor}, fmt.Errorf("could not find binary patch file in %s", changeDir)
	}

	// 2. Get the hash for the UI label using our robust runCmd
	logOut, _ := s.runCmd(actor, state.Path, "pijul", "log", "--hash-only")
	hashes := strings.Split(strings.TrimSpace(logOut), "\n")
	latestHash := "unknown"
	if len(hashes) > 0 && hashes[0] != "" {
		latestHash = strings.TrimSpace(hashes[0])
	}

	// 3. Copy the raw binary file to our inbox
	inboxDir := filepath.Join(BaseDir, "inbox")
	_ = os.MkdirAll(inboxDir, 0755)
	patchPath := filepath.Join(inboxDir, latestHash+".patch")

	data, err := os.ReadFile(srcFile)
	if err != nil {
		return []string{actor}, err
	}
	if err := os.WriteFile(patchPath, data, 0644); err != nil {
		return []string{actor}, err
	}

	// PROTECT THE SLICE MUTATION
	s.mu.Lock()
	s.Inbox = append(s.Inbox, PatchEnvelope{
		FromActor: actor,
		Hash:      latestHash,
		PatchPath: patchPath,
	})
	s.mu.Unlock()

	return []string{actor}, nil
}

// SaveStateToFile safely rebuilds customer.txt from internal parsed state so we
// don't accidentally mangle injected conflict lines or lose trailing EOF newlines.
func (s *DemoStore) SaveStateToFile(actor string) error {
	state := s.Actors[actor]
	file := filepath.Join(state.Path, "customer.txt")
	var out []string

	for _, l := range state.Lines {
		if l.Conflict != nil {
			// Restore the Pijul conflict block exactly as it was injected
			out = append(out, ">>>>>>> "+l.Conflict.AliceHash)
			out = append(out, fmt.Sprintf(`%s "%s"`, l.Path, l.Conflict.AliceValue))
			out = append(out, "=======")
			out = append(out, fmt.Sprintf(`%s "%s"`, l.Path, l.Conflict.BobValue))
			out = append(out, "<<<<<<< "+l.Conflict.BobHash)
		} else {
			// Write standard KV line
			out = append(out, fmt.Sprintf(`%s "%s"`, l.Path, l.Value))
		}
	}

	// Crucial Fix: Append the trailing newline so Pijul's patch graph
	// doesn't mistakenly flag EOF overlaps as structural conflicts!
	finalContent := strings.Join(out, "\n") + "\n"

	return os.WriteFile(file, []byte(finalContent), 0644)
}
