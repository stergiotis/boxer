// Package claudemine is the `boxer claudemine` command: it mines Claude Code
// session transcripts (the JSONL under ~/.claude/projects) into a single
// denormalized, ClickHouse-queryable Arrow event table so token spend, context
// growth, model choice, typed prompts, file reads/writes/edits and commits can
// be inspected with SQL via clickhouse-local.
//
// Repository membership of every touched path is recorded per event
// (target_repo), so "references to a given repo's code" are answerable
// regardless of which session — in which working directory — made them. See
// ADR-0091.
package claudemine

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// rawEvent is the subset of a transcript line shared across event types.
type rawEvent struct {
	Type             string          `json:"type"`
	Timestamp        string          `json:"timestamp"`
	Cwd              string          `json:"cwd"`
	GitBranch        string          `json:"gitBranch"`
	Version          string          `json:"version"`
	UUID             string          `json:"uuid"`
	ParentUUID       string          `json:"parentUuid"`
	RequestID        string          `json:"requestId"`
	IsSidechain      bool            `json:"isSidechain"`
	IsMeta           bool            `json:"isMeta"`
	IsCompactSummary bool            `json:"isCompactSummary"`
	PromptSource     string          `json:"promptSource"`
	PermissionMode   string          `json:"permissionMode"`
	AiTitle          string          `json:"aiTitle"`
	Message          json.RawMessage `json:"message"`
	ToolUseResult    json.RawMessage `json:"toolUseResult"`
}

type assistantMessage struct {
	Model      string         `json:"model"`
	StopReason string         `json:"stop_reason"`
	Usage      *usage         `json:"usage"`
	Content    []contentBlock `json:"content"`
}

type usage struct {
	InputTokens              int64  `json:"input_tokens"`
	OutputTokens             int64  `json:"output_tokens"`
	CacheReadInputTokens     int64  `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64  `json:"cache_creation_input_tokens"`
	ServiceTier              string `json:"service_tier"`
}

type contentBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`          // tool_use
	Name      string          `json:"name"`        // tool_use
	Input     json.RawMessage `json:"input"`       // tool_use
	ToolUseID string          `json:"tool_use_id"` // tool_result
	Text      string          `json:"text"`        // text
}

type toolInput struct {
	FilePath string `json:"file_path"`
	Command  string `json:"command"`
}

// toolResult is the structured tool outcome (user event's top-level
// `toolUseResult`). Its shape varies by tool; the fields here cover the
// file-op and commit variants. `Content` is left raw because it is a string
// for a Write/create but an array for an Agent result.
type toolResult struct {
	Type            string          `json:"type"`
	FilePath        string          `json:"filePath"`
	File            *fileField      `json:"file"`
	Content         json.RawMessage `json:"content"`
	OldString       string          `json:"oldString"`
	StructuredPatch []patchHunk     `json:"structuredPatch"`
	GitOperation    *gitOperation   `json:"gitOperation"`
	Stdout          string          `json:"stdout"`
}

type fileField struct {
	FilePath string `json:"filePath"`
	Content  string `json:"content"`
}

type patchHunk struct {
	Lines []string `json:"lines"`
}

type gitOperation struct {
	Commit *struct {
		Sha  string `json:"sha"`
		Kind string `json:"kind"`
	} `json:"commit"`
}

type toolUseInfo struct {
	name     string
	filePath string
}

var (
	// Gate: a git subcommand that inspects an existing revision.
	gitInspectRe = regexp.MustCompile(`\bgit\b[^|;&]*?\b(show|log|diff|cherry-pick|revert|checkout|reset|rebase|switch|tag|branch|stash|blame|bisect)\b`)
	// A hex token long enough to plausibly be a commit sha.
	shaRe = regexp.MustCompile(`\b[0-9a-f]{7,40}\b`)
	// "N files changed, M insertions(+), K deletions(-)" git summary line.
	commitStatRe = regexp.MustCompile(`(\d+) files? changed(?:, (\d+) insertions?\(\+\))?(?:, (\d+) deletions?\(-\))?`)
	// "[branch sha] subject" prefix on a commit's stdout first line.
	commitHeadRe = regexp.MustCompile(`^\[[^\]]*\]\s*`)
)

// ParseCorpus walks every project directory under projectsDir, parses each
// *.jsonl session transcript, and returns the flat list of event rows.
func ParseCorpus(projectsDir string, cl *repoClassifier) (rows []eventRow, err error) {
	projects, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil, eh.Errorf("unable to read projects dir %q: %w", projectsDir, err)
	}
	for _, pe := range projects {
		if !pe.IsDir() {
			continue
		}
		dirPath := filepath.Join(projectsDir, pe.Name())
		files, derr := os.ReadDir(dirPath)
		if derr != nil {
			return nil, eh.Errorf("unable to read project dir %q: %w", dirPath, derr)
		}
		for _, fe := range files {
			if fe.IsDir() || !strings.HasSuffix(fe.Name(), ".jsonl") {
				continue
			}
			sp := &sessionParser{
				cl:         cl,
				sessionID:  strings.TrimSuffix(fe.Name(), ".jsonl"),
				projectDir: pe.Name(),
				toolUse:    map[string]toolUseInfo{},
				seenRefSha: map[string]struct{}{},
			}
			if perr := sp.parseFile(filepath.Join(dirPath, fe.Name())); perr != nil {
				return nil, perr
			}
			rows = append(rows, sp.rows...)
		}
	}
	return rows, nil
}

type sessionParser struct {
	cl         *repoClassifier
	sessionID  string
	projectDir string
	rows       []eventRow
	toolUse    map[string]toolUseInfo
	seenRefSha map[string]struct{}

	// session-level trackers
	title       string
	sessCwd     string
	sessBranch  string
	sessVersion string
	firstTs     time.Time
	haveFirst   bool
}

func (s *sessionParser) parseFile(path string) (err error) {
	var f *os.File
	if f, err = os.Open(path); err != nil {
		return eh.Errorf("unable to open transcript %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	// bufio.Reader (not Scanner) so arbitrarily long lines never overflow.
	br := bufio.NewReaderSize(f, 1<<20)
	seq := -1
	for {
		line, rerr := br.ReadBytes('\n')
		if len(line) > 0 {
			seq++
			s.handleLine(seq, line)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return eh.Errorf("unable to read transcript %q: %w", path, rerr)
		}
	}
	s.emitSessionRow()
	return nil
}

func (s *sessionParser) handleLine(seq int, line []byte) {
	var ev rawEvent
	if json.Unmarshal(line, &ev) != nil {
		return
	}
	if ev.Cwd != "" {
		s.sessCwd = ev.Cwd
	}
	if ev.GitBranch != "" {
		s.sessBranch = ev.GitBranch
	}
	if ev.Version != "" {
		s.sessVersion = ev.Version
	}
	ts := parseTs(ev.Timestamp)
	if !ts.IsZero() && (!s.haveFirst || ts.Before(s.firstTs)) {
		s.firstTs, s.haveFirst = ts, true
	}

	switch ev.Type {
	case "ai-title":
		if ev.AiTitle != "" {
			s.title = ev.AiTitle
		}
	case "user":
		s.handleUser(seq, ts, &ev)
	case "assistant":
		s.handleAssistant(seq, ts, &ev)
	}
}

func (s *sessionParser) handleUser(seq int, ts time.Time, ev *rawEvent) {
	// Genuine typed prompt (not a tool-result, meta, or compact summary).
	if ev.PromptSource == "typed" && !ev.IsMeta && !ev.IsCompactSummary {
		if text := userText(ev.Message); text != "" {
			r := s.base("user_input", seq, ts, ev)
			r.Text = &text
			r.PromptSource = optStr(ev.PromptSource)
			r.PermissionMode = optStr(ev.PermissionMode)
			s.rows = append(s.rows, r)
		}
	}
	// Structured tool outcome → file op / commit.
	if len(ev.ToolUseResult) > 0 {
		s.handleToolResult(seq, ts, ev)
	}
}

func (s *sessionParser) handleAssistant(seq int, ts time.Time, ev *rawEvent) {
	var m assistantMessage
	if json.Unmarshal(ev.Message, &m) != nil {
		return
	}
	nTool := int32(0)
	hasThinking := false
	for i := range m.Content {
		b := &m.Content[i]
		switch b.Type {
		case "tool_use":
			nTool++
			var in toolInput
			_ = json.Unmarshal(b.Input, &in)
			s.toolUse[b.ID] = toolUseInfo{name: b.Name, filePath: in.FilePath}
			if b.Name == "Bash" && in.Command != "" {
				s.mineRefCommits(seq, ts, ev, in.Command)
			}
		case "thinking", "redacted_thinking":
			hasThinking = true
		}
	}
	if m.Usage == nil {
		return // API-error placeholder etc. — no token row.
	}
	r := s.base("assistant", seq, ts, ev)
	r.Model = optStr(m.Model)
	r.InputTokens = i64(m.Usage.InputTokens)
	r.OutputTokens = i64(m.Usage.OutputTokens)
	r.CacheReadTokens = i64(m.Usage.CacheReadInputTokens)
	r.CacheCreationTokens = i64(m.Usage.CacheCreationInputTokens)
	r.ContextTokens = i64(m.Usage.InputTokens + m.Usage.CacheReadInputTokens + m.Usage.CacheCreationInputTokens)
	r.ServiceTier = optStr(m.Usage.ServiceTier)
	r.StopReason = optStr(m.StopReason)
	r.RequestID = optStr(ev.RequestID)
	r.NToolUse = i32(nTool)
	r.HasThinking = bp(hasThinking)
	s.rows = append(s.rows, r)
}

func (s *sessionParser) handleToolResult(seq int, ts time.Time, ev *rawEvent) {
	var tr toolResult
	if json.Unmarshal(ev.ToolUseResult, &tr) != nil {
		return
	}
	info := s.toolUse[toolResultID(ev.Message)] // zero value if uncorrelated

	// Commit (authoritative) wins over any file shape.
	if tr.GitOperation != nil && tr.GitOperation.Commit != nil && tr.GitOperation.Commit.Sha != "" {
		c := tr.GitOperation.Commit
		r := s.base("commit", seq, ts, ev)
		repo, _ := s.cl.classify(ev.Cwd)
		r.CommitSha = optStr(c.Sha)
		r.CommitKind = optStr(c.Kind)
		r.TargetRepo = optStr(repo)
		subject, files, ins, del := parseCommitStdout(tr.Stdout)
		r.CommitSubject = optStr(subject)
		r.CommitFilesChanged = files
		r.CommitInsertions = ins
		r.CommitDeletions = del
		s.rows = append(s.rows, r)
		return
	}

	// File op. Determine kind from the correlated tool name, then shape.
	filePath := firstNonEmpty(tr.FilePath, fileFieldPath(tr.File), info.filePath)
	kind := fileKind(info.name, &tr)
	if kind == "" || filePath == "" {
		return
	}
	r := s.base(kind, seq, ts, ev)
	r.ToolName = optStr(info.name)
	r.FilePath = optStr(filePath)
	repo, rel := s.cl.classify(filePath)
	r.TargetRepo = optStr(repo)
	r.FileRel = optStr(rel)
	r.FileExt = optStr(strings.ToLower(strings.TrimPrefix(filepath.Ext(filePath), ".")))
	switch kind {
	case "file_edit":
		add, rem := patchLineCounts(tr.StructuredPatch)
		r.LinesAdded, r.LinesRemoved = i32(add), i32(rem)
	case "file_write":
		r.LinesAdded = i32(countLines(rawToString(tr.Content)))
		r.LinesRemoved = i32(0)
	}
	s.rows = append(s.rows, r)
}

// mineRefCommits records commit shas that a bash command inspects (git
// show/log/diff/…), discriminated as commit_kind='referenced'. Best-effort and
// deduplicated per session; the authoritative committed rows come from
// gitOperation in handleToolResult.
func (s *sessionParser) mineRefCommits(seq int, ts time.Time, ev *rawEvent, command string) {
	if !gitInspectRe.MatchString(command) {
		return
	}
	repo, _ := s.cl.classify(ev.Cwd)
	for _, sha := range shaRe.FindAllString(command, -1) {
		if _, dup := s.seenRefSha[sha]; dup {
			continue
		}
		s.seenRefSha[sha] = struct{}{}
		r := s.base("commit", seq, ts, ev)
		r.CommitSha = optStr(sha)
		r.CommitKind = optStr("referenced")
		r.TargetRepo = optStr(repo)
		s.rows = append(s.rows, r)
	}
}

func (s *sessionParser) emitSessionRow() {
	repo, _ := s.cl.classify(s.sessCwd)
	r := eventRow{
		SessionID:   s.sessionID,
		Seq:         -1, // sorts before line 0 within the session
		Kind:        "session",
		Ts:          s.firstTs,
		ProjectDir:  s.projectDir,
		ProjectRepo: repo,
		Cwd:         optStr(s.sessCwd),
		GitBranch:   optStr(s.sessBranch),
		Version:     optStr(s.sessVersion),
		Title:       optStr(s.title),
	}
	s.rows = append(s.rows, r)
}

// base builds the identity/context fields common to every per-event row.
func (s *sessionParser) base(kind string, seq int, ts time.Time, ev *rawEvent) eventRow {
	repo := "other"
	if ev.Cwd != "" {
		repo, _ = s.cl.classify(ev.Cwd)
	}
	return eventRow{
		SessionID:   s.sessionID,
		Seq:         int32(seq),
		Kind:        kind,
		Ts:          ts,
		ProjectDir:  s.projectDir,
		ProjectRepo: repo,
		Cwd:         optStr(ev.Cwd),
		GitBranch:   optStr(ev.GitBranch),
		Version:     optStr(ev.Version),
		UUID:        optStr(ev.UUID),
		ParentUUID:  optStr(ev.ParentUUID),
		IsSidechain: bp(ev.IsSidechain),
	}
}

// fileKind maps a correlated tool name (falling back to the result shape) to a
// file event kind, or "" when the result is not a file operation.
func fileKind(toolName string, tr *toolResult) string {
	switch toolName {
	case "Read":
		return "file_read"
	case "Write":
		return "file_write"
	case "Edit", "MultiEdit", "NotebookEdit":
		return "file_edit"
	}
	// Uncorrelated: infer from the result shape.
	switch {
	case tr.Type == "create":
		return "file_write"
	case len(tr.StructuredPatch) > 0 || tr.OldString != "":
		return "file_edit"
	case tr.File != nil:
		return "file_read"
	}
	return ""
}

func patchLineCounts(hunks []patchHunk) (added, removed int32) {
	for _, h := range hunks {
		for _, ln := range h.Lines {
			if ln == "" {
				continue
			}
			switch ln[0] {
			case '+':
				added++
			case '-':
				removed++
			}
		}
	}
	return added, removed
}

func parseCommitStdout(stdout string) (subject string, files, ins, del *int32) {
	if stdout == "" {
		return "", nil, nil, nil
	}
	first, _, _ := strings.Cut(stdout, "\n")
	subject = commitHeadRe.ReplaceAllString(strings.TrimSpace(first), "")
	if m := commitStatRe.FindStringSubmatch(stdout); m != nil {
		files = atoiPtr(m[1])
		ins = atoiPtr(m[2])
		del = atoiPtr(m[3])
	}
	return subject, files, ins, del
}

func atoiPtr(s string) *int32 {
	if s == "" {
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return nil
	}
	return i32(int32(n))
}

func toolResultID(message json.RawMessage) string {
	var m struct {
		Content []contentBlock `json:"content"`
	}
	if json.Unmarshal(message, &m) != nil {
		return ""
	}
	for i := range m.Content {
		if m.Content[i].Type == "tool_result" && m.Content[i].ToolUseID != "" {
			return m.Content[i].ToolUseID
		}
	}
	return ""
}

// userText extracts the prompt text from a user message whose content is
// either a plain string (typed prompt) or an array of blocks.
func userText(message json.RawMessage) string {
	var asString string
	if json.Unmarshal(message, &asString) == nil {
		return strings.TrimSpace(asString)
	}
	var m struct {
		Content json.RawMessage `json:"content"`
	}
	if json.Unmarshal(message, &m) != nil {
		return ""
	}
	if json.Unmarshal(m.Content, &asString) == nil {
		return strings.TrimSpace(asString)
	}
	var blocks []contentBlock
	if json.Unmarshal(m.Content, &blocks) != nil {
		return ""
	}
	var sb strings.Builder
	for i := range blocks {
		if blocks[i].Type == "text" && blocks[i].Text != "" {
			if sb.Len() > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(blocks[i].Text)
		}
	}
	return strings.TrimSpace(sb.String())
}

func fileFieldPath(f *fileField) string {
	if f == nil {
		return ""
	}
	return f.FilePath
}

func rawToString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return ""
}

func countLines(s string) int32 {
	if s == "" {
		return 0
	}
	n := int32(strings.Count(s, "\n"))
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}

func parseTs(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

// pointer helpers
func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
func i32(i int32) *int32 { return &i }
func i64(i int64) *int64 { return &i }
func bp(b bool) *bool    { return &b }
