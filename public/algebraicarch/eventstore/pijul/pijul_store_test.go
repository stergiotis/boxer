package pijul

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Helper to get a specific KV value from an actor's parsed state
func getKV(state *ActorState, pathKey string) string {
	for _, l := range state.Lines {
		if l.Path == pathKey {
			return l.Value
		}
	}
	return ""
}

// Helper to get conflict data from an actor's parsed state
func getConflict(state *ActorState, pathKey string) *ConflictData {
	for _, l := range state.Lines {
		if l.Path == pathKey && l.Conflict != nil {
			return l.Conflict
		}
	}
	return nil
}

// Synchronous setup for testing (bypasses the async queue)
func setupTestStore(t *testing.T, testName string) *DemoStore {
	// Skip test if Pijul isn't installed on the testing machine
	if _, err := exec.LookPath("pijul"); err != nil {
		t.Skip("Pijul CLI not found in PATH. Skipping test.")
	}

	// Use an isolated temp directory for tests
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

	if err := store.InitSystem(); err != nil {
		t.Fatalf("InitSystem failed: %v", err)
	}

	store.ReloadAllActors()
	return store
}
func TestPlaybook1_CommutativeMergeAndConflicts(t *testing.T) {
	store := setupTestStore(t, "playbook1")
	defer os.RemoveAll(BaseDir)

	t.Log("Step 1: Alice edits email, Bob edits company name")
	err := store.SaveEdit("Alice", "/contact/email", "alice@example.com")
	if err != nil {
		t.Fatalf("Alice SaveEdit failed: %v", err)
	}

	err = store.SaveEdit("Bob", "/company/name", "Bob Corp")
	if err != nil {
		t.Fatalf("Bob SaveEdit failed: %v", err)
	}

	t.Log("Step 2: Alice Pushes, Bob Pulls")
	if err := store.PushPull("Alice", "push"); err != nil {
		t.Fatalf("Alice Push failed: %v", err)
	}
	if err := store.PushPull("Bob", "pull"); err != nil {
		t.Fatalf("Bob Pull failed: %v", err)
	}

	store.ReloadAllActors()

	t.Log("Step 3: Verify Commutative Merge")
	bobState := store.Actors["Bob"]
	if bobState.HasConflict {
		// DUMP FILE CONTENTS FOR DEBUGGING!
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
	err = store.SaveEdit("Alice", "/account/status", "Suspended")
	if err != nil {
		t.Fatalf("Alice Save 4 failed: %v", err)
	}
	err = store.SaveEdit("Bob", "/account/status", "Pending Approval")
	if err != nil {
		t.Fatalf("Bob Save 4 failed: %v", err)
	}

	err = store.PushPull("Alice", "push")
	if err != nil {
		t.Fatalf("Alice Push 4 failed: %v", err)
	}

	// Pijul exits with status 1 when conflicts are found! We ignore the error here.
	_ = store.PushPull("Bob", "pull")

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
	_ = store.SaveEdit("Bob", "/company/name", "Bob Global")
	err := store.EmailPatch("Bob")
	if err != nil || len(store.Inbox) == 0 {
		t.Fatalf("Failed to export Bob's patch")
	}
	bobPatchPath := store.Inbox[0].PatchPath

	t.Log("Step 2: Alice applies Bob's patch (P2P Sync)")
	err = store.ApplyPatch("Alice", bobPatchPath)
	if err != nil {
		t.Fatalf("Alice failed to apply Bob's patch: %v", err)
	}
	store.ReloadAllActors()
	if getKV(store.Actors["Alice"], "/company/name") != "Bob Global" {
		t.Errorf("Alice did not correctly receive Bob's peer-to-peer patch")
	}

	t.Log("Step 3: Charlie applies Bob's patch, edits the SAME line, and exports")
	_ = store.ApplyPatch("Charlie", bobPatchPath) // Get Bob's context first
	_ = store.SaveEdit("Charlie", "/company/name", "Charlie Megacorp")
	_ = store.EmailPatch("Charlie")

	if len(store.Inbox) < 2 {
		t.Fatalf("Failed to export Charlie's patch")
	}
	charliePatchPath := store.Inbox[1].PatchPath

	t.Log("Step 4: Server attempts to apply Charlie's patch BEFORE Bob's patch")
	// The Server hasn't received anything yet (nobody pushed).
	// If it tries to apply Charlie's patch, it should fail because Charlie's
	// patch mathematically depends on Bob's patch!
	err = store.ApplyPatch("Server", charliePatchPath)

	if err == nil {
		t.Fatal("CRITICAL FAILURE: Pijul applied a patch without its prerequisites! Event sourcing property violated.")
	} else {
		t.Logf("Success! Pijul blocked the invalid merge. Error caught: %v", err)
	}

	t.Log("Playbook 2 Passed!")
}
