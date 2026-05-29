//go:build llm_generated_opus47

// Package demo is the imzero2 demo carousel — the registry shell that
// resolves demo subcommands to their renderer functions and orchestrates
// switching between built-in apps under cmd/imzero2's `imzero2 demo`
// subcommand.
//
// Discovery and non-interactive launch:
//
//   - `--list` prints every registered application as an Arrow IPC
//     record streamed through `clickhouse-local --output-format
//     PrettyCompact` and exits before the client launches. The Arrow
//     stream itself can be captured with `--list-output <path>` for
//     downstream `clickhouse-local` queries, and `--list-format`
//     selects any ClickHouse output format (Pretty, Vertical, Markdown,
//     JSONEachRow, TSV, ...). When `clickhouse-local` is not on PATH
//     the command falls back to a plain ASCII table.
//   - `--launch <ref[,ref...]>` seeds initial windows. Each ref is
//     resolved by trying, in order: full manifest Id ("org.pebble2.play"),
//     legacy numeric code ("a005" / "5"), and SubjectAlias (the last
//     '/' segment of the Id — e.g. "play", "widgets", "hn_explorer").
//     Unresolved refs are logged and skipped — they do not abort startup.
//     Use `--list` to discover the alias / code / Id columns.
package demo
