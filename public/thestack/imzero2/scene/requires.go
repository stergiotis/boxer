package scene

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/db/clickhouse/clickhouseenv"
	"github.com/stergiotis/boxer/public/extbin"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// requires.go is the registry of preconditions a scene may name (ADR-0248
// §SD6). A precondition that does not hold skips the scene: it says something
// about the machine, not about the app, and reporting it as a failure would
// teach people to ignore failures.

const (
	// RequireClickHouse holds when the configured ClickHouse answers.
	RequireClickHouse = "clickhouse"
	// RequireTablePrefix + a table name holds when that table has rows.
	RequireTablePrefix = "table:"
	// RequireExePrefix + the name of a program declared in the extbin registry
	// holds when that program resolves. Declared ones only: which programs a
	// scene may depend on is the same auditable list as everything else boxer
	// spawns.
	RequireExePrefix = "exe:"
)

func clickhouseQuery(sql string) (out string, err error) {
	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(clickhouseenv.URL.Get(), "text/plain", bytes.NewBufferString(sql))
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", eb.Build().Int("status", resp.StatusCode).Str("body", strings.TrimSpace(string(b))).Errorf("ClickHouse refused the query")
	}
	return strings.TrimSpace(string(b)), nil
}

// CheckRequire reports why a named precondition does not hold, or "" when it
// does. An unknown name is an error: a typo must not read as "holds".
func CheckRequire(name string) (unmet string, err error) {
	switch {
	case name == RequireClickHouse:
		if _, e := clickhouseQuery("SELECT 1"); e != nil {
			return "no ClickHouse at " + clickhouseenv.URL.Get(), nil
		}
		return "", nil
	case strings.HasPrefix(name, RequireTablePrefix):
		table := strings.TrimPrefix(name, RequireTablePrefix)
		// The name is interpolated, so hold it to what a table name can be.
		if table == "" || strings.ContainsAny(table, " ;'\"`()\n") {
			return "", eb.Build().Str("require", name).Errorf("not a table name")
		}
		out, e := clickhouseQuery("SELECT count() FROM " + table)
		if e != nil {
			return "table " + table + " is not readable", nil
		}
		if out == "0" {
			return "table " + table + " is empty", nil
		}
		return "", nil
	case strings.HasPrefix(name, RequireExePrefix):
		exe := strings.TrimPrefix(name, RequireExePrefix)
		for _, p := range extbin.Registry() {
			if p.Name == exe {
				if _, ok := p.Resolve(); !ok {
					return exe + " is not installed", nil
				}
				return "", nil
			}
		}
		return "", eb.Build().Str("require", name).Errorf("not a program the extbin registry declares")
	default:
		return "", eb.Build().Str("require", name).
			Errorf("unknown scene precondition (known: " + RequireClickHouse + ", " + RequireTablePrefix + "<name>, " + RequireExePrefix + "<name>)")
	}
}
