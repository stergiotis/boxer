//go:build llm_generated_gemini3pro

package pijul

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func getKV(state *ActorState, pathKey string) string {
	for _, l := range state.Lines {
		if l.Path == pathKey {
			return l.Value
		}
	}
	return ""
}

func getConflict(state *ActorState, pathKey string) *ConflictData {
	for _, l := range state.Lines {
		if l.Path == pathKey && l.Conflict != nil {
			return l.Conflict
		}
	}
	return nil
}

func setupTestStore(t *testing.T, testName string) *DemoStore {
	if _, err := exec.LookPath("pijul"); err != nil {
		t.Skip("Pijul CLI not found in PATH. Skipping test.")
	}

	BaseDir = filepath.Join(os.TempDir(), "pijul-test-"+testName)

	store := &DemoStore{
		Actors:     make(map[string]*ActorState),
		EditInputs: make(map[string]*string),
	}

	for _, name := range []string{"Server", "Alice", "Bob", "Charlie"} {
		store.Actors[name] = &ActorState{
			Name: name,
			Path: filepath.Join(BaseDir, strings.ToLower(name)),
		}
	}
	store.Server = store.Actors["Server"]

	if _, err := store.InitSystem(); err != nil {
		t.Fatalf("InitSystem failed: %v", err)
	}

	store.ReloadAllActors()
	return store
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestPlaybook1_CommutativeMergeAndConflicts(t *testing.T) {
	store := setupTestStore(t, "playbook1")
	defer os.RemoveAll(BaseDir)

	t.Log("Step 1: Alice edits email, Bob edits company name")
	_, err := store.SaveEdit("Alice", "/contact/email", "alice@example.com")
	if err != nil {
		t.Fatalf("Alice SaveEdit failed: %v", err)
	}

	_, err = store.SaveEdit("Bob", "/company/name", "Bob Corp")
	if err != nil {
		t.Fatalf("Bob SaveEdit failed: %v", err)
	}

	t.Log("Step 2: Alice Pushes, Bob Pulls")
	if _, err := store.PushPull("Alice", "push"); err != nil {
		t.Fatalf("Alice Push failed: %v", err)
	}
	if _, err := store.PushPull("Bob", "pull"); err != nil {
		t.Fatalf("Bob Pull failed: %v", err)
	}

	store.ReloadAllActors()

	t.Log("Step 3: Verify Commutative Merge")
	bobState := store.Actors["Bob"]
	if bobState.HasConflict {
		content, _ := os.ReadFile(filepath.Join(bobState.Path, "customer.txt"))
		t.Fatalf("Bob should NOT have a conflict! The edits were spaced far apart.\n--- File contents:\n%s\n---", string(content))
	}
	if getKV(bobState, "/contact/email") != "alice@example.com" {
		t.Errorf("Bob did not receive Alice's email edit.")
	}
	if getKV(bobState, "/company/name") != "Bob Corp" {
		t.Errorf("Bob's company name was overwritten! Expected 'Bob Corp'.")
	}

	t.Log("Step 4: Both edit the EXACT same key to trigger a conflict")
	_, err = store.SaveEdit("Alice", "/account/status", "Suspended")
	if err != nil {
		t.Fatalf("Alice Save 4 failed: %v", err)
	}
	_, err = store.SaveEdit("Bob", "/account/status", "Pending Approval")
	if err != nil {
		t.Fatalf("Bob Save 4 failed: %v", err)
	}

	_, err = store.PushPull("Alice", "push")
	if err != nil {
		t.Fatalf("Alice Push 4 failed: %v", err)
	}

	// Pijul exits with status 1 when conflicts are found! We ignore the error here.
	_, _ = store.PushPull("Bob", "pull")

	store.ReloadAllActors()

	t.Log("Step 5: Verify Conflict Injection")
	bobState = store.Actors["Bob"]
	if !bobState.HasConflict {
		content, _ := os.ReadFile(filepath.Join(bobState.Path, "customer.txt"))
		t.Fatalf("Bob SHOULD have a conflict on /account/status, but none was detected.\n--- File contents:\n%s\n---", string(content))
	}

	conflict := getConflict(bobState, "/account/status")
	if conflict == nil {
		t.Fatal("Could not find parsed conflict struct for /account/status")
	}

	hasSuspended := conflict.AliceValue == "Suspended" || conflict.BobValue == "Suspended"
	hasPending := conflict.AliceValue == "Pending Approval" || conflict.BobValue == "Pending Approval"

	if !hasSuspended || !hasPending {
		t.Errorf("Conflict values parsed incorrectly. Got Alice: %s, Bob: %s", conflict.AliceValue, conflict.BobValue)
	}

	t.Log("Playbook 1 Passed!")
}

func TestPlaybook2_DecentralizedSyncAndDependencies(t *testing.T) {
	store := setupTestStore(t, "playbook2")
	defer os.RemoveAll(BaseDir)

	t.Log("Step 1: Bob edits and exports an email patch")
	_, _ = store.SaveEdit("Bob", "/company/name", "Bob Global")
	_, err := store.EmailPatch("Bob")
	if err != nil || len(store.Inbox) == 0 {
		t.Fatalf("Failed to export Bob's patch: %v", err)
	}
	bobPatchPath := store.Inbox[0].PatchPath

	t.Log("Step 2: Alice applies Bob's patch (P2P Sync)")
	_, err = store.ApplyPatch("Alice", bobPatchPath)
	if err != nil {
		t.Fatalf("Alice failed to apply Bob's patch: %v", err)
	}
	store.ReloadAllActors()
	if getKV(store.Actors["Alice"], "/company/name") != "Bob Global" {
		t.Errorf("Alice did not correctly receive Bob's peer-to-peer patch")
	}

	t.Log("Step 3: Charlie applies Bob's patch, edits the SAME line, and exports")
	_, _ = store.ApplyPatch("Charlie", bobPatchPath)
	_, _ = store.SaveEdit("Charlie", "/company/name", "Charlie Megacorp")
	_, _ = store.EmailPatch("Charlie")

	if len(store.Inbox) < 2 {
		t.Fatalf("Failed to export Charlie's patch")
	}
	charliePatchPath := store.Inbox[1].PatchPath

	t.Log("Step 4: Server attempts to apply Charlie's patch BEFORE Bob's patch")
	_, err = store.ApplyPatch("Server", charliePatchPath)

	if err == nil {
		t.Fatal("CRITICAL FAILURE: Pijul applied a patch without its prerequisites!")
	} else {
		t.Logf("Success! Pijul blocked the invalid merge. Error caught: %v", err)
	}

	t.Log("Playbook 2 Passed!")
}
func TestCreditParserGraphResolution(t *testing.T) {
	// 1. Mock JSON output: Base Patch (Old) and Context Patch (New)
	mockJSONLog := []byte(`[
	  {
		"hash": "EYPWGEPXCHFDYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYYY",
		"authors": ["Alice"],
		"timestamp": "2026-02-22T17:00:00.000000000Z",
		"message": "Added Context Edge"
	  },
	  {
		"hash": "JYS6SYSP25ASXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX",
		"authors": ["System"],
		"timestamp": "2026-02-22T16:00:00.000000000Z",
		"message": "Init Base Record"
	  }
	]`)

	// 2. Mock exactly the output the user provided
	mockCreditOut := `EYPWGEPXCHFD

EYPWGEPXCHFD, JYS6SYSP25AS
> /id "CUST-100"

JYS6SYSP25AS
> /contact/name "Jane Doe AAAA"

EYPWGEPXCHFD, JYS6SYSP25AS
> /contact/email "jane@example.com"
`

	// 3. The lines we expect to map to
	lines := []KVLine{
		{Path: "/id", Value: "CUST-100"},
		{Path: "/contact/name", Value: "Jane Doe AAAA"},
		{Path: "/contact/email", Value: "jane@example.com"},
	}

	logMap := make(map[string]PijulLogEntry)
	_ = parsePijulLogJSON(mockJSONLog, logMap)

	processedLines := applyCreditToLines(mockCreditOut, lines, logMap)

	// 4. Verify the OLDEST hash (JYS6SYSP25AS / System) won out on the multi-edge lines!
	idLine := processedLines[0]
	if idLine.CreditAuthor != "System" {
		t.Errorf("Expected True Author to be 'System', Got: '%s'. Failed to resolve oldest edge.", idLine.CreditAuthor)
	}

	nameLine := processedLines[1]
	if nameLine.CreditAuthor != "System" {
		t.Errorf("Expected True Author to be 'System', Got: '%s'", nameLine.CreditAuthor)
	}

	emailLine := processedLines[2]
	if emailLine.CreditAuthor != "System" {
		t.Errorf("Expected True Author to be 'System', Got: '%s'. Failed to resolve oldest edge.", emailLine.CreditAuthor)
	}
}
