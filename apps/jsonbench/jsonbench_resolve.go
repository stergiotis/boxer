package main

import (
	"context"
	"fmt"
	"io"
	"iter"
	"os"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"
)

// resolveCommand expands leeway column handles to physical column names, so a
// query against a leeway table can be *written* as `symbol:value` instead of
// `tv:symbol:value:val:s:m:0:24:0::data`.
//
// This is ADR-0116's ResolveColumnNames pass with a live-endpoint resolver
// bound to it. play applies the same pass at StagePreExecute; running it on
// the command line gives a file of SQL the same treatment before it reaches a
// server that has never heard of handles.
//
// Handles are `section:column`, quoted — a colon cannot occur in a bare SQL
// identifier, so the syntax is unambiguous. Support columns resolve alongside
// value columns, which is what makes the membership lanes (`symbol:mrhp`,
// `symbol:lmrcard`, `stringArray:len`) reachable and this trial's queries
// legible.
func resolveCommand() *cli.Command {
	return &cli.Command{
		Name:      "resolve",
		Usage:     "expand leeway column handles in SQL read from stdin or a file",
		ArgsUsage: "[file]",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "url", Value: "http://localhost:8123/"},
			&cli.StringFlag{Name: "user", Value: "default"},
			&cli.StringFlag{Name: "password"},
			&cli.StringFlag{Name: "database", Required: true,
				Usage: "default database for unqualified table references"},
			&cli.BoolFlag{Name: "strict", Value: true,
				Usage: "fail on a handle that does not resolve, instead of passing it through"},
		},
		Action: runResolve,
	}
}

func runResolve(cCtx *cli.Context) (err error) {
	var src []byte
	if p := cCtx.Args().First(); p != "" {
		src, err = os.ReadFile(p)
	} else {
		src, err = io.ReadAll(os.Stdin)
	}
	if err != nil {
		err = eh.Errorf("read sql: %w", err)
		return
	}

	client := chclient.New(chclient.Config{
		URL:      cCtx.String("url"),
		User:     cCtx.String("user"),
		Password: cCtx.String("password"),
	}, nil)
	db := cCtx.String("database")
	resolver := lwsql.NewResolver(&chSchemaProvider{cli: client, ctx: cCtx.Context, fallbackDB: db})

	var bad []passes.ColumnDiagnostic
	pass := passes.ResolveColumnNames(resolver, db, func(d passes.ColumnDiagnostic) {
		bad = append(bad, d)
	})

	// One statement at a time: the pass is a body pass, and a file of
	// benchmark queries is a sequence of independent statements.
	out := make([]string, 0, 8)
	for _, stmt := range splitStatements(string(src)) {
		var resolved string
		resolved, err = pass.Run(stmt)
		if err != nil {
			err = eh.Errorf("resolve %.60s…: %w", stmt, err)
			return
		}
		out = append(out, resolved)
	}

	for _, d := range bad {
		log.Warn().Str("handle", d.Handle).Str("msg", d.Message).
			Strs("candidates", d.Candidates).Msg("unresolved column handle")
	}
	if len(bad) > 0 && cCtx.Bool("strict") {
		err = eh.Errorf("%d column handle(s) did not resolve", len(bad))
		return
	}
	fmt.Println(strings.Join(out, ";\n") + ";")
	return
}

// splitStatements strips comments first and only then splits on `;`. The order
// matters: a query file's header prose contains semicolons, and splitting
// first cuts a comment in half, leaving the remainder to be parsed as SQL.
// The benchmark files hold no semicolons inside literals, so a plain split is
// sufficient here; this is not a general SQL splitter.
func splitStatements(src string) (out []string) {
	for _, s := range strings.Split(stripSQLComments(src), ";") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return
}

// stripSQLComments removes `--` line comments, which carry the query files'
// prose and would otherwise ride into the resolved output.
func stripSQLComments(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// chSchemaProvider answers passes.SchemaProviderI from a live endpoint's
// system.columns — the same source ADR-0116 §SD3 specifies.
type chSchemaProvider struct {
	cli        *chclient.Client
	ctx        context.Context
	fallbackDB string
}

func (inst *chSchemaProvider) GetColumns(dbName string, tableName string) (columns iter.Seq[string], nColumns int, found bool) {
	if dbName == "" {
		dbName = inst.fallbackDB
	}
	body, err := inst.cli.Query(inst.ctx,
		"SELECT name FROM system.columns WHERE database = "+quoteLiteral(dbName)+
			" AND table = "+quoteLiteral(tableName)+" ORDER BY position FORMAT TSV")
	if err != nil {
		return
	}
	defer func() { _ = body.Close() }()
	raw, err := io.ReadAll(body)
	if err != nil {
		return
	}
	names := make([]string, 0, 64)
	for _, line := range strings.Split(string(raw), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			names = append(names, line)
		}
	}
	if len(names) == 0 {
		return
	}
	return func(yield func(string) bool) {
		for _, n := range names {
			if !yield(n) {
				return
			}
		}
	}, len(names), true
}

func quoteLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "\\'") + "'"
}

var _ passes.SchemaProviderI = (*chSchemaProvider)(nil)
var _ = nanopass.RegionBody // keep the nanopass import meaningful for readers
