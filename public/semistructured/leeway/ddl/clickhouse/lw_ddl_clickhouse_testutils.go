package clickhouse

import (
	"os"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// ClickHouseBinaryPath replaces the previously-lowercase "clickhouse"
// env var (renamed under ADR-0009 §6 — every other declared env name is
// uppercase). The default uses ~-expansion via the PathVar.
var ClickHouseBinaryPath = env.NewPath(env.Spec{
	Name:        "BOXER_CLICKHOUSE_BINARY_PATH",
	Default:     "~/opt/clickhouse/clickhouse",
	Description: "path to the clickhouse binary used by leeway DDL test utilities",
	Category:    env.CategoryTestIntegration,
})

func GetClickHouseBinaryPath() (path string, err error) {
	path = ClickHouseBinaryPath.Get()
	_, err = os.Stat(path)
	if err != nil {
		err = eh.Errorf("unable to locate clickhouse binary: %w", err)
		return
	}
	return
}
