package clickhouse

import (
	"os"

	"github.com/stergiotis/boxer/public/observability/eh"
)

func GetClickHouseBinaryPath() (path string, err error) {
	var found bool
	path, found = os.LookupEnv("clickhouse")
	if !found {
		path = os.ExpandEnv("$HOME/opt/clickhouse/clickhouse")
	}

	_, err = os.Stat(path)
	if err != nil {
		err = eh.Errorf("unable to locate clickhouse binary: %w", err)
		return
	}
	return
}
