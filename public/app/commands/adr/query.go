package adr

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/gov/adrcorpus"
	"github.com/stergiotis/boxer/public/keelson/data/chlocalpool"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// namedQuery is a titled canned query for the overview report.
type namedQuery struct {
	title string
	sql   string
}

// overviewQueries crosses the two axes — decision status vs code-evidence
// implementation degree — and surfaces the drift cases between them.
var overviewQueries = []namedQuery{
	{"Decision × implementation-evidence board (the two axes)",
		`SELECT status,
                countIf(impl_evidence='none')       AS none,
                countIf(impl_evidence='referenced') AS referenced,
                countIf(impl_evidence='broad')      AS broad,
                count()                             AS total
         FROM adr GROUP BY status ORDER BY total DESC`},
	{"Drift: accepted but NO code evidence (un-built, or implemented outside scanned source)",
		`SELECT num, last_date, plan_max_phase AS max_phase, title
         FROM adr WHERE status='accepted' AND code_refs=0 ORDER BY num`},
	{"Built ahead of decision: proposed but already referenced in code",
		`SELECT num, code_refs, code_pkgs, arrayStringConcat(code_qualifiers, ' ') AS qualifiers, title
         FROM adr WHERE status='proposed' AND code_refs>0 ORDER BY code_refs DESC`},
	{"Largest implementation footprint",
		`SELECT num, status, code_refs, code_files, code_pkgs, impl_evidence, title
         FROM adr ORDER BY code_refs DESC LIMIT 15`},
	{"Most recently touched (latest date mentioned in the ADR)",
		`SELECT num, status, impl_evidence, last_date, title
         FROM adr WHERE last_date != '' ORDER BY last_date DESC LIMIT 12`},
	{"Sub-item progress: what each ADR declares done of what it decomposed into",
		`SELECT num, status, subtasks_done AS done, subtasks_total AS total,
                round(100 * subtasks_done / subtasks_total) AS pct, title
         FROM adr WHERE subtasks_total > 0
         ORDER BY pct DESC, total DESC LIMIT 20`},
	{"Sub-item vocabulary: what the corpus actually decomposes into",
		`SELECT kind, count() AS declared, countIf(done) AS done,
                countIf(code_refs > 0) AS cited,
                countIf(shape='heading') AS as_heading, countIf(shape='list') AS as_list
         FROM subtask GROUP BY kind ORDER BY declared DESC`},
	{"Sub-item drift: code names it, but no ADR declares it done (the ✓ worklist)",
		`SELECT s.num AS num, s.marker AS marker, s.code_refs AS refs, substring(s.title, 1, 44) AS title
         FROM subtask s WHERE s.code_refs > 0 AND NOT s.done
         ORDER BY s.code_refs DESC, s.num LIMIT 20`},
}

// resolveChBinary reports whether clickhouse-local is reachable and, when it is
// via the bundled path, returns that path as an explicit override. An empty
// path with ok=true means "resolve on PATH at call time" (left to extbin).
func resolveChBinary() (path string, ok bool) {
	if _, err := os.Stat(chlocalpool.DefaultBinaryPath); err == nil {
		return chlocalpool.DefaultBinaryPath, true
	}
	// Command resolves the binary (PATH lookup) without running it; a nil error
	// means clickhouse-local is installed.
	if _, err := extbin.ClickHouseLocal.Command(context.Background(), extbin.Opts{}); err == nil {
		return "", true
	}
	return "", false
}

// chTablePrelude binds the three Arrow files to the table names `adr`,
// `coderef` and `subtask`. The files are referenced by basename and
// clickhouse-local is run with its working directory set to where they live
// (see RunQuery), so no absolute-path file()-access policy is involved.
func chTablePrelude(adrArrowBase, coderefArrowBase, subtaskArrowBase string) string {
	return fmt.Sprintf(
		"CREATE TEMPORARY TABLE adr AS SELECT * FROM file(%s, 'Arrow');\n"+
			"CREATE TEMPORARY TABLE coderef AS SELECT * FROM file(%s, 'Arrow');\n"+
			"CREATE TEMPORARY TABLE subtask AS SELECT * FROM file(%s, 'Arrow');\n",
		sqlQuote(adrArrowBase), sqlQuote(coderefArrowBase), sqlQuote(subtaskArrowBase))
}

// RunQuery executes one SQL statement against the adr/coderef/subtask tables
// via clickhouse-local. ok=false means the binary is unreachable; callers decide
// whether to error or fall back.
func RunQuery(adrArrow, coderefArrow, subtaskArrow, query, format string, stdout io.Writer) (ok bool, err error) {
	bin, found := resolveChBinary()
	if !found {
		return false, nil
	}
	if format == "" {
		format = "PrettyCompact"
	}
	script := chTablePrelude(filepath.Base(adrArrow), filepath.Base(coderefArrow), filepath.Base(subtaskArrow)) + query
	cmd, err := extbin.ClickHouseLocal.Command(context.Background(),
		extbin.Opts{Path: bin, Dir: filepath.Dir(adrArrow)},
		"--multiquery", "--output-format", format)
	if err != nil {
		return true, eb.Build().Str("query", query).Errorf("resolve clickhouse-local: %w", err)
	}
	cmd.Stdin = strings.NewReader(script)
	cmd.Stdout = stdout
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if runErr := cmd.Run(); runErr != nil {
		return true, eb.Build().Str("stderr", stderr.String()).Str("bin", cmd.Path).
			Str("query", query).Errorf("clickhouse-local: %w", runErr)
	}
	return true, nil
}

// RunOverview runs the canned overview queries, each under a heading. ok=false
// means clickhouse-local is unreachable (callers fall back to RenderBoardASCII).
func RunOverview(adrArrow, coderefArrow, subtaskArrow string, stdout io.Writer) (ok bool, err error) {
	if _, found := resolveChBinary(); !found {
		return false, nil
	}
	for _, q := range overviewQueries {
		if _, err = fmt.Fprintf(stdout, "\n── %s ──\n", q.title); err != nil {
			return true, err
		}
		if _, err = RunQuery(adrArrow, coderefArrow, subtaskArrow, q.sql, "PrettyCompact", stdout); err != nil {
			return true, err
		}
	}
	return true, nil
}

func sqlQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "\\'") + "'"
}

// RenderBoardASCII prints the two-axis board as a plain padded table. It is the
// no-clickhouse fallback so `boxer adr overview` is still useful where the
// binary is absent.
func RenderBoardASCII(adrs []adrcorpus.Adr, w io.Writer) error {
	sorted := make([]adrcorpus.Adr, len(adrs))
	copy(sorted, adrs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Num < sorted[j].Num })
	headers := []string{"num", "status", "evidence", "refs", "pkgs", "sub_done", "last_date", "title"}
	rows := make([][]string, 0, len(sorted))
	for _, a := range sorted {
		rows = append(rows, []string{
			strconv.Itoa(a.Num), a.Status, a.ImplEvidence,
			strconv.Itoa(a.CodeRefs), strconv.Itoa(a.CodePkgs),
			formatRollup(a.SubtasksDone, a.SubtasksTotal), a.LastDate, a.Title,
		})
	}
	return renderTable(headers, rows, w)
}

// formatRollup renders a sub-item rollup as "k/n", or "-" when the ADR declares
// no sub-items at all (distinct from "0/n", which means it declared some and
// has marked none done).
func formatRollup(done, total int) string {
	if total == 0 {
		return "-"
	}
	return strconv.Itoa(done) + "/" + strconv.Itoa(total)
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
