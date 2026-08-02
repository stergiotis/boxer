package chpack

import (
	"context"
	"io"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Conn is the ClickHouse access Install needs. chclient.Client satisfies
// it; the interface keeps this package free of a client dependency.
type Conn interface {
	Exec(ctx context.Context, sql string) (err error)
	Query(ctx context.Context, sql string) (body io.ReadCloser, err error)
}

// featureProbeName is created and dropped by the install-time probe; it is
// never part of the roster.
const featureProbeName = "leewayPackFeatureProbe"

// Install reconciles the pack onto the server (ADR-0162 §SD5). Idempotent:
// every statement is CREATE OR REPLACE. Steps, each failing loudly rather
// than degrading silently:
//
//  1. feature probe — lambda-parameter SQL UDFs (the pack's floor was
//     verified on ClickHouse 26.7; older servers are untested),
//  2. collision check — no roster name may resolve to a non-UDF server
//     function (e.g. a builtin arriving with a server upgrade),
//  3. CREATE OR REPLACE the roster in dependency order,
//  4. verify leewayPackVersion() reports this build's Version.
func Install(ctx context.Context, conn Conn) (err error) {
	err = probeLambdaSupport(ctx, conn)
	if err != nil {
		return
	}
	err = checkCollisions(ctx, conn)
	if err != nil {
		return
	}
	for _, f := range Functions() {
		e := conn.Exec(ctx, Statement(f))
		if e != nil {
			err = eb.Build().Str("function", f.Name).Errorf("chpack: install: %w", e)
			return
		}
	}
	got, qErr := queryTrimmed(ctx, conn, "SELECT "+VersionFunctionName+"()")
	if qErr != nil {
		err = eh.Errorf("chpack: post-install verify: %w", qErr)
		return
	}
	want := strconv.Itoa(Version)
	if got != want {
		err = eb.Build().Str("got", got).Str("want", want).Errorf("chpack: version marker mismatch after install")
		return
	}
	return
}

// probeLambdaSupport creates, calls and drops a throwaway higher-order UDF.
// SQL UDFs predate lambda parameters; a server that rejects either step
// cannot host the pack, and the error says so instead of letting individual
// CREATEs fail with less attributable messages.
func probeLambdaSupport(ctx context.Context, conn Conn) (err error) {
	e := conn.Exec(ctx, "CREATE OR REPLACE FUNCTION "+featureProbeName+" AS (f, a) -> arrayMap(f, a)")
	if e == nil {
		var got string
		got, e = queryTrimmed(ctx, conn, "SELECT "+featureProbeName+"(x -> x + 1, [1])")
		if e == nil && got != "[2]" {
			e = eb.Build().Str("got", got).Errorf("unexpected probe result")
		}
	}
	// Best-effort: a leftover probe function is harmless and the next
	// install replaces it.
	_ = conn.Exec(ctx, "DROP FUNCTION IF EXISTS "+featureProbeName)
	if e != nil {
		err = eh.Errorf("chpack: server lacks lambda-parameter SQL UDF support (pack floor verified on ClickHouse 26.7): %w", e)
	}
	return
}

// checkCollisions refuses to install over any roster name that resolves to
// a function the pack does not own. The comparison is case-insensitive on
// purpose: pack names are case-sensitive, but some builtin lookups are not,
// so a same-name-different-case builtin still shadows (ADR-0162 §SD2/§SD5).
// Roster names are [a-zA-Z0-9] only (enforced by test), so splicing them
// into the query is safe.
func checkCollisions(ctx context.Context, conn Conn) (err error) {
	fns := Functions()
	quoted := make([]string, 0, len(fns))
	for _, f := range fns {
		quoted = append(quoted, "'"+strings.ToLower(f.Name)+"'")
	}
	sql := "SELECT name FROM system.functions WHERE lower(name) IN (" +
		strings.Join(quoted, ", ") + ") AND origin != 'SQLUserDefined' ORDER BY name"
	out, qErr := queryTrimmed(ctx, conn, sql)
	if qErr != nil {
		err = eh.Errorf("chpack: collision check: %w", qErr)
		return
	}
	if out != "" {
		err = eb.Build().Str("names", strings.Join(strings.Fields(out), ", ")).
			Errorf("chpack: pack names collide with server-owned functions; refusing to shadow (ADR-0162 §SD5)")
	}
	return
}

func queryTrimmed(ctx context.Context, conn Conn, sql string) (out string, err error) {
	body, err := conn.Query(ctx, sql)
	if err != nil {
		return
	}
	defer func() {
		_ = body.Close()
	}()
	b, err := io.ReadAll(body)
	if err != nil {
		return
	}
	out = strings.TrimSpace(string(b))
	return
}
