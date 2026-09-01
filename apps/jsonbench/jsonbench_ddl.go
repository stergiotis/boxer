package main

import (
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/urfave/cli/v2"

	"github.com/stergiotis/boxer/public/keelson/data/chclient"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore/chstore"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

func ddlCommand() *cli.Command {
	return &cli.Command{
		Name:  "ddl",
		Usage: "emit or apply the boxer.facts DDL against a benchmark-local database",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "url", Value: "http://localhost:8123/"},
			&cli.StringFlag{Name: "user", Value: "default"},
			&cli.StringFlag{Name: "password"},
			&cli.StringFlag{Name: "database", Required: true},
			&cli.StringFlag{Name: "table", Value: "facts"},
			&cli.StringFlag{
				Name:  "engine",
				Usage: "MergeTree clause; empty selects the live store's own default (ORDER BY ts)",
			},
			&cli.BoolFlag{Name: "apply", Usage: "execute the DDL instead of printing it"},
			&cli.BoolFlag{Name: "drop", Usage: "DROP DATABASE first (benchmark databases only)"},
		},
		Action: runDdl,
	}
}

func runDdl(cCtx *cli.Context) (err error) {
	db := cCtx.String("database")
	// A guard, not a courtesy: this command drops databases, and the live
	// facts store must never be a target.
	if db == "boxer" {
		err = eb.Build().Str("db", db).Errorf("refusing to target the live facts database")
		return
	}
	cfg := chstore.Config{
		URL:      cCtx.String("url"),
		User:     cCtx.String("user"),
		Password: cCtx.String("password"),
		Database: db,
		Table:    cCtx.String("table"),
	}
	var sql string
	sql, err = chstore.ComposeSetupSQL(cfg, cCtx.String("engine"))
	if err != nil {
		return
	}
	if !cCtx.Bool("apply") {
		fmt.Println(sql)
		return
	}
	cli0 := chclient.New(chclient.Config{
		URL: cfg.URL, User: cfg.User, Password: cfg.Password,
	}, nil)
	ctx := cCtx.Context
	if cCtx.Bool("drop") {
		err = cli0.Exec(ctx, "DROP DATABASE IF EXISTS "+db)
		if err != nil {
			err = eb.Build().Str("db", db).Errorf("drop database: %w", err)
			return
		}
	}
	for stmt := range strings.SplitSeq(sql, ";") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		err = cli0.Exec(ctx, stmt)
		if err != nil {
			err = eh.Errorf("exec ddl: %w", err)
			return
		}
	}
	log.Info().Str("database", db).Str("table", cfg.Table).Msg("facts DDL applied")
	return
}
