package claudemine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClassify(t *testing.T) {
	cl := newClassifier("/home/spx/repo",
		map[string]string{"/opt/vendored/x": "myrepo"},
		[]string{"/home/spx/.claude"})

	cases := []struct {
		path, repo, rel string
	}{
		{"/home/spx/repo/boxer/public/a/b.go", "boxer", "public/a/b.go"},
		{"/home/spx/repo/testrepo/parser.go", "testrepo", "parser.go"},
		{"/home/spx/repo/testrepo", "testrepo", ""},
		{"/opt/vendored/x/deep/y.go", "myrepo", "deep/y.go"}, // explicit beats segment
		{"/home/spx/.claude/projects/p/memory/m.md", "claude-meta", ""},
		{"/somewhere/else/z.go", "other", ""},
		{"", "other", ""},
	}
	for _, c := range cases {
		repo, rel := cl.classify(c.path)
		if repo != c.repo || rel != c.rel {
			t.Errorf("classify(%q) = (%q,%q), want (%q,%q)", c.path, repo, rel, c.repo, c.rel)
		}
	}
}

// one synthetic session transcript exercising every emitted kind. cwd is a
// literal repo path (the classifier works on the strings in the data, not the
// temp fixture dir).
const sampleTranscript = `{"type":"ai-title","aiTitle":"Test session","sessionId":"sess1"}
{"type":"user","timestamp":"2026-06-01T10:00:00.000Z","cwd":"/home/spx/repo/testrepo","gitBranch":"main","version":"2.1.0","promptSource":"typed","permissionMode":"normal","uuid":"u1","message":{"role":"user","content":"Please refactor the parser"}}
{"type":"user","timestamp":"2026-06-01T10:00:01.000Z","cwd":"/home/spx/repo/testrepo","promptSource":"system","uuid":"u1b","message":{"role":"user","content":"injected system note"}}
{"type":"assistant","timestamp":"2026-06-01T10:00:05.000Z","cwd":"/home/spx/repo/testrepo","gitBranch":"main","version":"2.1.0","uuid":"a1","requestId":"req1","message":{"model":"claude-opus-4-8","stop_reason":"tool_use","usage":{"input_tokens":100,"output_tokens":50,"cache_read_input_tokens":2000,"cache_creation_input_tokens":300,"service_tier":"standard"},"content":[{"type":"thinking","thinking":"hmm"},{"type":"tool_use","id":"tu_read","name":"Read","input":{"file_path":"/home/spx/repo/testrepo/parser.go"}}]}}
{"type":"user","timestamp":"2026-06-01T10:00:06.000Z","cwd":"/home/spx/repo/testrepo","uuid":"u2","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_read","content":"file contents"}]},"toolUseResult":{"type":"text","file":{"filePath":"/home/spx/repo/testrepo/parser.go","content":"package main\nfunc x(){}\n"}}}
{"type":"assistant","timestamp":"2026-06-01T10:00:10.000Z","cwd":"/home/spx/repo/testrepo","uuid":"a2","requestId":"req2","message":{"model":"claude-opus-4-8","stop_reason":"tool_use","usage":{"input_tokens":120,"output_tokens":60,"cache_read_input_tokens":2500,"cache_creation_input_tokens":0,"service_tier":"standard"},"content":[{"type":"tool_use","id":"tu_edit","name":"Edit","input":{"file_path":"/home/spx/repo/testrepo/parser.go"}}]}}
{"type":"user","timestamp":"2026-06-01T10:00:11.000Z","cwd":"/home/spx/repo/testrepo","uuid":"u3","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_edit"}]},"toolUseResult":{"filePath":"/home/spx/repo/testrepo/parser.go","oldString":"a","newString":"b","structuredPatch":[{"oldStart":1,"oldLines":2,"newStart":1,"newLines":3,"lines":[" ctx","-old","+new1","+new2"]}]}}
{"type":"assistant","timestamp":"2026-06-01T10:00:20.000Z","cwd":"/home/spx/repo/testrepo","uuid":"a3","requestId":"req3","message":{"model":"claude-opus-4-8","stop_reason":"tool_use","usage":{"input_tokens":130,"output_tokens":40,"cache_read_input_tokens":3000,"cache_creation_input_tokens":0,"service_tier":"standard"},"content":[{"type":"tool_use","id":"tu_commit","name":"Bash","input":{"command":"git commit -m 'fix parser'"}},{"type":"tool_use","id":"tu_show","name":"Bash","input":{"command":"git show abc1234 --stat"}}]}}
{"type":"user","timestamp":"2026-06-01T10:00:21.000Z","cwd":"/home/spx/repo/testrepo","uuid":"u4","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_commit"}]},"toolUseResult":{"stdout":"[main def5678] fix parser\n 2 files changed, 5 insertions(+), 1 deletion(-)","stderr":"","gitOperation":{"commit":{"sha":"def5678","kind":"committed"}}}}
`

func parseSample(t *testing.T) []eventRow {
	t.Helper()
	projects := t.TempDir()
	sessDir := filepath.Join(projects, "-home-spx-repo-testrepo")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessDir, "sess1.jsonl"), []byte(sampleTranscript), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	cl := newClassifier("/home/spx/repo", nil, []string{"/home/spx/.claude"})
	rows, err := ParseCorpus(projects, cl)
	if err != nil {
		t.Fatalf("ParseCorpus: %v", err)
	}
	return rows
}

func onlyKind(rows []eventRow, kind string) []eventRow {
	var out []eventRow
	for i := range rows {
		if rows[i].Kind == kind {
			out = append(out, rows[i])
		}
	}
	return out
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
func derefI(p *int32) int32 {
	if p == nil {
		return -1
	}
	return *p
}
func derefI64(p *int64) int64 {
	if p == nil {
		return -1
	}
	return *p
}

func TestParseCorpus(t *testing.T) {
	rows := parseSample(t)

	want := map[string]int{
		"session": 1, "user_input": 1, "assistant": 3,
		"file_read": 1, "file_write": 0, "file_edit": 1, "commit": 2,
	}
	got := map[string]int{}
	for i := range rows {
		got[rows[i].Kind]++
	}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("kind %q: want %d rows, got %d", k, n, got[k])
		}
	}

	// session row
	s := onlyKind(rows, "session")[0]
	if deref(s.Title) != "Test session" {
		t.Errorf("session title: want %q, got %q", "Test session", deref(s.Title))
	}
	if s.ProjectRepo != "testrepo" || s.Seq != -1 {
		t.Errorf("session row: repo=%q seq=%d, want testrepo / -1", s.ProjectRepo, s.Seq)
	}

	// user_input: only the typed prompt (the promptSource=system one is excluded)
	ui := onlyKind(rows, "user_input")[0]
	if deref(ui.Text) != "Please refactor the parser" {
		t.Errorf("user_input text: got %q", deref(ui.Text))
	}

	// assistant: context_tokens = input + cache_read + cache_creation
	var a1 *eventRow
	for _, r := range onlyKind(rows, "assistant") {
		if deref(r.RequestID) == "req1" {
			rc := r
			a1 = &rc
		}
	}
	if a1 == nil {
		t.Fatal("assistant req1 not found")
	}
	if deref(a1.Model) != "claude-opus-4-8" {
		t.Errorf("model: got %q", deref(a1.Model))
	}
	if got := derefI64(a1.ContextTokens); got != 100+2000+300 {
		t.Errorf("context_tokens: want 2400, got %d", got)
	}
	if a1.HasThinking == nil || !*a1.HasThinking {
		t.Errorf("has_thinking: want true")
	}
	if got := derefI(a1.NToolUse); got != 1 {
		t.Errorf("n_tool_use: want 1, got %d", got)
	}

	// file_read: correlated to the Read tool, repo + ext set, no line counts
	fr := onlyKind(rows, "file_read")[0]
	if deref(fr.TargetRepo) != "testrepo" || deref(fr.FileRel) != "parser.go" || deref(fr.FileExt) != "go" {
		t.Errorf("file_read: repo=%q rel=%q ext=%q", deref(fr.TargetRepo), deref(fr.FileRel), deref(fr.FileExt))
	}
	if fr.LinesAdded != nil {
		t.Errorf("file_read lines_added: want nil, got %d", *fr.LinesAdded)
	}

	// file_edit: structuredPatch line counts (2 added, 1 removed)
	fe := onlyKind(rows, "file_edit")[0]
	if derefI(fe.LinesAdded) != 2 || derefI(fe.LinesRemoved) != 1 {
		t.Errorf("file_edit lines: +%d -%d, want +2 -1", derefI(fe.LinesAdded), derefI(fe.LinesRemoved))
	}

	// commits: one authoritative committed (with parsed stat), one mined referenced
	var committed, referenced *eventRow
	for _, r := range onlyKind(rows, "commit") {
		rc := r
		switch deref(r.CommitKind) {
		case "committed":
			committed = &rc
		case "referenced":
			referenced = &rc
		}
	}
	if committed == nil || referenced == nil {
		t.Fatalf("want both committed and referenced commit rows, got %+v", onlyKind(rows, "commit"))
	}
	if deref(committed.CommitSha) != "def5678" || deref(committed.CommitSubject) != "fix parser" {
		t.Errorf("committed: sha=%q subject=%q", deref(committed.CommitSha), deref(committed.CommitSubject))
	}
	if derefI(committed.CommitFilesChanged) != 2 || derefI(committed.CommitInsertions) != 5 || derefI(committed.CommitDeletions) != 1 {
		t.Errorf("committed stat: files=%d ins=%d del=%d, want 2/5/1",
			derefI(committed.CommitFilesChanged), derefI(committed.CommitInsertions), derefI(committed.CommitDeletions))
	}
	if deref(referenced.CommitSha) != "abc1234" || deref(referenced.TargetRepo) != "testrepo" {
		t.Errorf("referenced: sha=%q repo=%q", deref(referenced.CommitSha), deref(referenced.TargetRepo))
	}
}

func TestWriteEventsArrow(t *testing.T) {
	rows := parseSample(t)
	out := filepath.Join(t.TempDir(), eventsArrowName)
	if err := WriteEventsArrow(out, rows); err != nil {
		t.Fatalf("WriteEventsArrow: %v", err)
	}
	if fi, err := os.Stat(out); err != nil || fi.Size() == 0 {
		t.Fatalf("arrow not written or empty: %v", err)
	}
}
