package adr

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

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

// chTablePrelude binds the two Arrow files to the table names `adr` and
// `coderef`. The files are referenced by basename and clickhouse-local is run
// with its working directory set to where they live (see RunQuery), so no
// absolute-path file()-access policy is involved.
func chTablePrelude(adrArrowBase, coderefArrowBase string) string {
	return fmt.Sprintf(
		"CREATE TEMPORARY TABLE adr AS SELECT * FROM file(%s, 'Arrow');\n"+
			"CREATE TEMPORARY TABLE coderef AS SELECT * FROM file(%s, 'Arrow');\n",
		sqlQuote(adrArrowBase), sqlQuote(coderefArrowBase))
}

// RunQuery executes one SQL statement against the adr/coderef tables via
// clickhouse-local. ok=false means the binary is unreachable; callers decide
// whether to error or fall back.
func RunQuery(adrArrow, coderefArrow, query, format string, stdout io.Writer) (ok bool, err error) {
	bin, found := resolveChBinary()
	if !found {
		return false, nil
	}
	if format == "" {
		format = "PrettyCompact"
	}
	script := chTablePrelude(filepath.Base(adrArrow), filepath.Base(coderefArrow)) + query
	cmd := exec.Command(bin, "--multiquery", "--output-format", format)
	cmd.Dir = filepath.Dir(adrArrow)
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
// means clickhouse-local is unreachable (callers fall back to RenderBoardASCII).
func RunOverview(adrArrow, coderefArrow string, stdout io.Writer) (ok bool, err error) {
	if _, found := resolveChBinary(); !found {
		return false, nil
	}
	for _, q := range overviewQueries {
		if _, err = fmt.Fprintf(stdout, "\n── %s ──\n", q.title); err != nil {
			return true, err
		}
		if _, err = RunQuery(adrArrow, coderefArrow, q.sql, "PrettyCompact", stdout); err != nil {
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
func RenderBoardASCII(adrs []Adr, w io.Writer) error {
	sorted := make([]Adr, len(adrs))
	copy(sorted, adrs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Num < sorted[j].Num })
	headers := []string{"num", "status", "evidence", "refs", "pkgs", "last_date", "title"}
	rows := make([][]string, 0, len(sorted))
	for _, a := range sorted {
		rows = append(rows, []string{
			strconv.Itoa(a.Num), a.Status, a.ImplEvidence,
			strconv.Itoa(a.CodeRefs), strconv.Itoa(a.CodePkgs), a.LastDate, a.Title,
		})
	}
	return renderTable(headers, rows, w)
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
