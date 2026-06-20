package claudemine

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// namedQuery is a titled canned query for the overview report.
type namedQuery struct {
	title string
	sql   string
}

// overviewQueries demonstrate the dataset: token/context spend by repo and
// model, activity over time, and the cross-repo code references that are the
// reason the tool exists. `target_repo NOT IN ('other','claude-meta',”)`
// narrows to references that land inside one of the configured repositories,
// regardless of which session made them.
var overviewQueries = []namedQuery{
	{"Spend & footprint by repo the session worked in",
		`SELECT project_repo,
                uniqExact(session_id)                  AS sessions,
                countIf(kind='user_input')             AS prompts,
                countIf(kind='assistant')              AS responses,
                sum(output_tokens)                     AS out_tokens,
                max(context_tokens)                    AS peak_context
         FROM events GROUP BY project_repo ORDER BY out_tokens DESC`},
	{"Token spend by model",
		`SELECT model,
                count()                 AS responses,
                sum(input_tokens)       AS input_tokens,
                sum(cache_read_tokens)  AS cache_read,
                sum(output_tokens)      AS output_tokens
         FROM events WHERE kind='assistant' AND model IS NOT NULL
         GROUP BY model ORDER BY output_tokens DESC`},
	{"Daily output-token activity (last 14 active days)",
		`SELECT toDate(ts) AS day, count() AS responses, sum(output_tokens) AS out_tokens
         FROM events WHERE kind='assistant'
         GROUP BY day ORDER BY day DESC LIMIT 14`},
	{"Most-touched files inside the configured repos",
		`SELECT target_repo, file_path,
                count()             AS ops,
                sum(lines_added)    AS added,
                sum(lines_removed)  AS removed
         FROM events
         WHERE kind IN ('file_read','file_write','file_edit')
           AND target_repo NOT IN ('other','claude-meta','')
         GROUP BY target_repo, file_path ORDER BY ops DESC LIMIT 20`},
	{"Busiest sessions",
		`SELECT session_id,
                anyIf(title, kind='session')           AS title,
                max(project_repo)                      AS repo,
                sumIf(output_tokens, kind='assistant') AS out_tokens,
                countIf(kind='user_input')             AS prompts,
                countIf(kind LIKE 'file_%')            AS file_ops
         FROM events GROUP BY session_id ORDER BY out_tokens DESC LIMIT 15`},
	{"Commits by repo and day (authoritative gitOperation commits)",
		`SELECT toDate(ts) AS day, target_repo, count() AS commits
         FROM events WHERE kind='commit' AND commit_kind IN ('committed','amended')
         GROUP BY day, target_repo ORDER BY day DESC, commits DESC LIMIT 25`},
}

func resolveChBinary() (bin string, ok bool) {
	bin = chlocalpool.DefaultBinaryPath
	if _, err := os.Stat(bin); err == nil {
		return bin, true
	}
	if resolved, err := exec.LookPath("clickhouse-local"); err == nil {
		return resolved, true
	}
	return "", false
}

// chTablePrelude binds the Arrow file to the table name `events`. The file is
// referenced by basename and clickhouse-local runs with its working directory
// set to where it lives (see RunQuery), so no absolute-path file()-access
// policy is involved.
func chTablePrelude(eventsArrowBase string) string {
	return fmt.Sprintf(
		"CREATE TEMPORARY TABLE events AS SELECT * FROM file(%s, 'Arrow');\n",
		sqlQuote(eventsArrowBase))
}

// RunQuery executes one SQL statement against the `events` table via
// clickhouse-local. ok=false means the binary is unreachable; callers decide
// whether to error or fall back.
func RunQuery(eventsArrow, query, format string, stdout io.Writer) (ok bool, err error) {
	bin, found := resolveChBinary()
	if !found {
		return false, nil
	}
	if format == "" {
		format = "PrettyCompact"
	}
	script := chTablePrelude(filepath.Base(eventsArrow)) + query
	cmd := exec.Command(bin, "--multiquery", "--output-format", format)
	cmd.Dir = filepath.Dir(eventsArrow)
	cmd.Stdin = strings.NewReader(script)
	cmd.Stdout = stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if runErr := cmd.Run(); runErr != nil {
		return true, eb.Build().Str("stderr", stderr.String()).Str("bin", bin).
			Str("query", query).Errorf("clickhouse-local: %w", runErr)
	}
	return true, nil
}

// RunOverview runs the canned overview queries, each under a heading. ok=false
// means clickhouse-local is unreachable (callers fall back to RenderSummaryASCII).
func RunOverview(eventsArrow string, stdout io.Writer) (ok bool, err error) {
	if _, found := resolveChBinary(); !found {
		return false, nil
	}
	for _, q := range overviewQueries {
		if _, err = fmt.Fprintf(stdout, "\n── %s ──\n", q.title); err != nil {
			return true, err
		}
		if _, err = RunQuery(eventsArrow, q.sql, "PrettyCompact", stdout); err != nil {
			return true, err
		}
	}
	return true, nil
}

func sqlQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "\\'") + "'"
}

// RenderSummaryASCII is the no-clickhouse fallback for `overview`: a compact
// Go-computed board so the command is still useful where the binary is absent.
func RenderSummaryASCII(rows []eventRow, w io.Writer) (err error) {
	type stat struct {
		sessions  map[string]struct{}
		prompts   int
		responses int
		outTokens int64
		fileOps   int
		commits   int
	}
	byRepo := map[string]*stat{}
	get := func(repo string) *stat {
		s := byRepo[repo]
		if s == nil {
			s = &stat{sessions: map[string]struct{}{}}
			byRepo[repo] = s
		}
		return s
	}
	for i := range rows {
		r := &rows[i]
		s := get(r.ProjectRepo)
		s.sessions[r.SessionID] = struct{}{}
		switch r.Kind {
		case "user_input":
			s.prompts++
		case "assistant":
			s.responses++
			if r.OutputTokens != nil {
				s.outTokens += *r.OutputTokens
			}
		case "file_read", "file_write", "file_edit":
			s.fileOps++
		case "commit":
			s.commits++
		}
	}
	repos := make([]string, 0, len(byRepo))
	for k := range byRepo {
		repos = append(repos, k)
	}
	sort.Slice(repos, func(i, j int) bool {
		return byRepo[repos[i]].outTokens > byRepo[repos[j]].outTokens
	})
	headers := []string{"project_repo", "sessions", "prompts", "responses", "out_tokens", "file_ops", "commits"}
	board := make([][]string, 0, len(repos))
	for _, repo := range repos {
		s := byRepo[repo]
		board = append(board, []string{
			repo,
			fmt.Sprint(len(s.sessions)), fmt.Sprint(s.prompts), fmt.Sprint(s.responses),
			fmt.Sprint(s.outTokens), fmt.Sprint(s.fileOps), fmt.Sprint(s.commits),
		})
	}
	return renderTable(headers, board, w)
}

func renderTable(headers []string, rows [][]string, w io.Writer) (err error) {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, r := range rows {
		for i, c := range r {
			if i < len(widths) && len(c) > widths[i] {
				widths[i] = len(c)
			}
		}
	}
	writeRow := func(cells []string) error {
		var b strings.Builder
		for i, c := range cells {
			if i > 0 {
				b.WriteString("  ")
			}
			b.WriteString(c)
			if i < len(cells)-1 {
				for p := len(c); p < widths[i]; p++ {
					b.WriteByte(' ')
				}
			}
		}
		b.WriteByte('\n')
		_, e := io.WriteString(w, b.String())
		return e
	}
	if err = writeRow(headers); err != nil {
		return err
	}
	for _, r := range rows {
		if err = writeRow(r); err != nil {
			return err
		}
	}
	return nil
}
