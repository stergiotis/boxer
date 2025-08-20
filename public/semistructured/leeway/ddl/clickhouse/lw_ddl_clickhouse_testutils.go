package clickhouse

import (
	"os"

	"github.com/stergiotis/boxer/public/observability/eh"
)

func GetClickHouseBinaryPath() (path string, err error) {
	var found bool
	path, found = os.LookupEnv("clickhouse")
	if found {
		return
	}

	path = os.ExpandEnv("$HOME/opt/clickhouse/clickhouse")
	if path != "" {
		return
	}
	err = eh.Errorf("unable to locate clickhouse binary")
	return
}
