//go:build llm_generated_opus47

// Package clickhouseenv centralises the ClickHouse-related environment
// variables consumed across boxer: the funccharacterize fuzzer,
// the spinnaker play HMI, and the live test harness. Each spec is
// registered with the boxer-wide registry (ADR-0058).
//
// Names match the ClickHouse client-tool convention (CLICKHOUSE_USER /
// CLICKHOUSE_PASSWORD / CLICKHOUSE_DATABASE / CLICKHOUSE_ENDPOINT /
// CLICKHOUSE_URL) and are not BOXER_*-prefixed: the names are shared
// with external tooling.
package clickhouseenv

import "github.com/stergiotis/boxer/public/config/env"

var (
	User = env.NewString(env.Spec{
		Name:        "CLICKHOUSE_USER",
		Default:     "default",
		Description: "ClickHouse user; defaults to the unauthenticated \"default\" account",
		Category:    env.CategoryDatabase,
	})

	Password = env.NewString(env.Spec{
		Name:        "CLICKHOUSE_PASSWORD",
		Description: "ClickHouse password; consumers typically omit the auth header when empty",
		Category:    env.CategoryDatabase,
		Sensitive:   true,
	})

	Database = env.NewString(env.Spec{
		Name:        "CLICKHOUSE_DATABASE",
		Description: "ClickHouse database; consumers omit the X-ClickHouse-Database header when empty",
		Category:    env.CategoryDatabase,
	})

	Endpoint = env.NewString(env.Spec{
		Name:        "CLICKHOUSE_ENDPOINT",
		Description: "ClickHouse HTTP endpoint (e.g. http://localhost:8123); tests skip when empty",
		Category:    env.CategoryDatabase,
	})

	// URL is a sibling of Endpoint historically used by the spinnaker
	// play HMI. The two will likely consolidate in a future cleanup;
	// keeping both declared makes the duplication explicit to the
	// registry.
	URL = env.NewString(env.Spec{
		Name:        "CLICKHOUSE_URL",
		Default:     "http://localhost:8123/",
		Description: "ClickHouse HTTP URL used by the spinnaker play HMI; defaults to localhost",
		Category:    env.CategoryDatabase,
	})
)
