package storeexec

import (
	"context"
	"io"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/db/clickhouse/chclient"
	"github.com/stergiotis/boxer/public/db/clickhouse/chversion"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Connect builds the executor for the server cfg names and checks that the
// server answers. It is the whole of what a caller that only wants a record
// store's executor used to spell itself — a client, a ping, the wrap — and it
// keeps the client out of that caller: what comes back is the seam a store is
// written against, not the transport under it.
//
// A caller that needs the client as well (a second use of the connection, a
// custom HTTP client) builds it and calls [New].
//
// The check is a round trip that reads the server's release, and a server
// below the ClickHouse floor is refused there ([chversion.Check]) rather
// than on the first statement that needs a newer one.
func Connect(ctx context.Context, cfg chclient.Config) (inst *Executor, err error) {
	client := chclient.New(cfg, nil)
	body, err := client.Query(ctx, "SELECT "+chversion.SelectVersion)
	if err != nil {
		err = eb.Build().Str("url", env.RedactURL(cfg.URL)).Errorf("storeexec: ClickHouse not reachable: %w", err)
		return
	}
	raw, err := io.ReadAll(body)
	_ = body.Close()
	if err != nil {
		err = eb.Build().Str("url", env.RedactURL(cfg.URL)).Errorf("storeexec: read the server's version: %w", err)
		return
	}
	err = chversion.Check("server", string(raw))
	if err != nil {
		err = eb.Build().Str("url", env.RedactURL(cfg.URL)).Errorf("storeexec: %w", err)
		return
	}
	return New(client, nil)
}

// ConnectFromEnv is [Connect] to the server the CLICKHOUSE_* registry entries
// name ([chclient.ConfigFromEnv]).
func ConnectFromEnv(ctx context.Context) (inst *Executor, err error) {
	return Connect(ctx, chclient.ConfigFromEnv())
}
