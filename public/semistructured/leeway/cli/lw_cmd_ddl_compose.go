package cli

import (
	"os"
	"strings"

	cli2 "github.com/stergiotis/boxer/public/hmi/cli"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	ddl2 "github.com/stergiotis/boxer/public/semistructured/leeway/ddl"
	"github.com/stergiotis/boxer/public/semistructured/leeway/ddl/clickhouse"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"
	"github.com/stergiotis/boxer/public/semistructured/leeway/naming"
	"github.com/stergiotis/boxer/public/semistructured/leeway/useaspects"
	"github.com/urfave/cli/v2"
)

// leeway ddl compose (ADR-0181 §SD6): a durable CREATE TABLE from the same
// column-spec tokens the LW_ constructor family carries, through the real
// generator — codecs, engine, ORDER BY. This is the path that applies what a
// CTAS over constructor-minted names can only record as intent (CTAS applies
// no CODEC clauses). Precedent for the shape: `leeway id udf`.

// composeDdlInput is the raw flag payload of one `leeway ddl compose` run,
// separated from the urfave plumbing so the composition is testable.
type composeDdlInput struct {
	Table          string
	Plain          []string // "<name> <ctype> item:<t> [enc:… sem:…]"
	Tagged         []string // "<section> <column> <ctype> [enc:… sem:… use:…]"
	Memberships    []string // "<section> <channel>"
	StreamGroups   []string // "<section> <key>"
	CoGroups       []string // "<section> <key>"
	Engine         string
	OrderBy        []string // "plain:<name>" | "tv:<section>:<column>" | "tvrole:<section>:<role>"
	PartitionBy    string
	Settings       []string
	IfNotExists    bool
	SkipIndexes    bool // DefaultSkipIndexPolicy-derived data-skipping indexes (ADR-0181 §SD4)
	TableRowConfig common.TableRowConfigE
}

func composeDdlName(kind string, s string) (n naming.StylableName, err error) {
	n, err = naming.MakeStylableName(s)
	if err != nil {
		err = eb.Build().Str(kind, s).Str("kind", kind).Errorf("invalid name: %w", err)
	}
	return
}

// composeDdl builds the TableDesc from the specs (support and membership
// columns are machine-derived by the generator, never authored), validates
// it, and renders the CREATE TABLE.
func composeDdl(in composeDdlInput) (sql string, err error) {
	if in.Table == "" {
		err = eh.Errorf("--table is required")
		return
	}
	manip, err := common.NewTableManipulator()
	if err != nil {
		return
	}
	// The desc-level table name is metadata; the emitted statement carries
	// in.Table verbatim, so a qualified name ("db.t") stays possible.
	if descName, nameErr := naming.MakeStylableName(in.Table); nameErr == nil {
		manip.SetTableName(descName)
	} else {
		manip.SetTableName("composed")
	}
	parser := canonicaltypes.NewParser()

	// The manipulator upserts on a name collision (last type wins) with no
	// signal the CLI could surface, and it creates sections on first
	// mention — so duplicate specs and typo'd section names must be caught
	// here, before anything merges.
	plainSeen := make(map[string]string, len(in.Plain))
	tvSeen := make(map[string]string, len(in.Tagged))
	tvSections := make(map[string]string, 4)    // folded → display
	membSections := make(map[string]bool, 4)    // folded
	coGroupOf := make(map[string]naming.Key, 4) // folded section → co-group key

	for _, spec := range in.Plain {
		fields := strings.Fields(spec)
		if len(fields) < 3 {
			err = eb.Build().Str("plain", spec).Errorf("plain spec needs at least: <name> <canonical-type> item:<type>")
			return
		}
		var tokens lwsql.PlainSpecTokens
		tokens, err = lwsql.ParsePlainSpecTokens(fields[2:])
		if err != nil {
			err = eb.Build().Str("plain", spec).Errorf("invalid plain spec tokens: %w", err)
			return
		}
		var name naming.StylableName
		name, err = composeDdlName("column", fields[0])
		if err != nil {
			return
		}
		dupKey := tokens.Item.String() + "\x00" + string(name)
		if prior, dup := plainSeen[dupKey]; dup {
			err = eb.Build().Str("plain", spec).Str("prior", prior).Errorf("duplicate plain column spec — the second would silently overwrite the first")
			return
		}
		plainSeen[dupKey] = spec
		var ct canonicaltypes.PrimitiveAstNodeI
		ct, err = parser.ParsePrimitiveTypeAst(fields[1])
		if err != nil {
			err = eb.Build().Str("plain", spec).Str("canonicalType", fields[1]).Errorf("unable to parse canonical type: %w", err)
			return
		}
		manip.AddPlainValueItem(tokens.Item, name, ct, tokens.EncodingHints, tokens.ValueSemantics)
	}

	for _, spec := range in.Tagged {
		fields := strings.Fields(spec)
		if len(fields) < 3 {
			err = eb.Build().Str("tv", spec).Errorf("tagged spec needs at least: <section> <column> <canonical-type>")
			return
		}
		var tokens lwsql.TaggedSpecTokens
		tokens, err = lwsql.ParseTaggedSpecTokens(fields[3:])
		if err != nil {
			err = eb.Build().Str("tv", spec).Errorf("invalid tagged spec tokens: %w", err)
			return
		}
		var section, column naming.StylableName
		section, err = composeDdlName("section", fields[0])
		if err != nil {
			return
		}
		column, err = composeDdlName("column", fields[1])
		if err != nil {
			return
		}
		dupKey := string(section) + "\x00" + string(column)
		if prior, dup := tvSeen[dupKey]; dup {
			err = eb.Build().Str("tv", spec).Str("prior", prior).Errorf("duplicate tagged column spec — the second would silently overwrite the first")
			return
		}
		tvSeen[dupKey] = spec
		tvSections[string(section)] = fields[0]
		var ct canonicaltypes.PrimitiveAstNodeI
		ct, err = parser.ParsePrimitiveTypeAst(fields[2])
		if err != nil {
			err = eb.Build().Str("tv", spec).Str("canonicalType", fields[2]).Errorf("unable to parse canonical type: %w", err)
			return
		}
		manip.MergeTaggedValueColumn(section, column, ct, tokens.EncodingHints, tokens.ValueSemantics, tokens.UseAspects, common.MembershipSpecNone, "", "")
	}

	for _, spec := range in.Memberships {
		fields := strings.Fields(spec)
		if len(fields) != 2 {
			err = eb.Build().Str("memb", spec).Errorf("membership spec needs exactly: <section> <channel>")
			return
		}
		var section naming.StylableName
		section, err = composeDdlName("section", fields[0])
		if err != nil {
			return
		}
		var m common.MembershipSpecE
		m, err = lwsql.ParseMembershipSpec(fields[1])
		if err != nil {
			err = eb.Build().Str("memb", spec).Errorf("invalid membership spec: %w", err)
			return
		}
		membSections[string(section)] = true
		manip.MergeTaggedValueSection(section, useaspects.EmptyAspectSet, m, "", "")
	}

	applyGroup := func(specs []string, kind string, co bool) (err error) {
		for _, spec := range specs {
			fields := strings.Fields(spec)
			if len(fields) != 2 {
				err = eb.Build().Str(kind, spec).Errorf("%s spec needs exactly: <section> <key>", kind)
				return
			}
			var section naming.StylableName
			section, err = composeDdlName("section", fields[0])
			if err != nil {
				return
			}
			// A group flag never introduces a section — a typo would mint a
			// phantom one and silently leave the real section ungrouped.
			if _, known := tvSections[string(section)]; !known && !membSections[string(section)] {
				err = eb.Build().Str(kind, spec).Str("section", fields[0]).Str("kind", kind).Errorf("unknown section in spec — declare it via --tv or --memb first (typo?)")
				return
			}
			var key naming.Key
			key, err = naming.MakeKey(fields[1])
			if err != nil {
				err = eb.Build().Str(kind, spec).Errorf("invalid group key: %w", err)
				return
			}
			if co {
				coGroupOf[string(section)] = key
				manip.MergeTaggedValueSection(section, useaspects.EmptyAspectSet, common.MembershipSpecNone, key, "")
			} else {
				manip.MergeTaggedValueSection(section, useaspects.EmptyAspectSet, common.MembershipSpecNone, "", key)
			}
		}
		return
	}
	err = applyGroup(in.CoGroups, "co-group", true)
	if err != nil {
		return
	}
	err = applyGroup(in.StreamGroups, "stream-group", false)
	if err != nil {
		return
	}

	// A section with value lanes needs a membership channel — its own, or a
	// membership-carrying partner in its co-section group (the sharing
	// co-groups exist for). The generator would silently emit the
	// membership-less shape LwShapeCheck rejects.
	for folded, display := range tvSections {
		if membSections[folded] {
			continue
		}
		leaning := false
		if g := coGroupOf[folded]; g != "" {
			for other, og := range coGroupOf {
				if og == g && membSections[other] {
					leaning = true
					break
				}
			}
		}
		if !leaning {
			err = eb.Build().Str("section", display).Errorf("section has value lanes but no membership channel — add --memb '<section> <channel>' (or a membership-carrying co-group partner)")
			return
		}
	}

	table, err := manip.BuildTableDesc()
	if err != nil {
		err = eh.Errorf("unable to build table description: %w", err)
		return
	}
	validator := common.NewTableValidator()
	err = validator.ValidateTable(&table)
	if err != nil {
		err = eh.Errorf("composed table is invalid: %w", err)
		return
	}

	tech := clickhouse.NewTechnologySpecificCodeGenerator()
	ir := common.NewIntermediateTableRepresentation()
	err = ir.LoadFromTable(&table, tech)
	if err != nil {
		err = eh.Errorf("unable to build intermediate representation: %w", err)
		return
	}
	conv, err := ddl2.NewHumanReadableNamingConvention(lwsql.DefaultSeparator)
	if err != nil {
		return
	}

	orderBy := make([]clickhouse.ColumnRef, 0, len(in.OrderBy))
	for _, o := range in.OrderBy {
		var ref clickhouse.ColumnRef
		ref, err = parseComposeColumnRef(o)
		if err != nil {
			return
		}
		orderBy = append(orderBy, ref)
	}
	mode := clickhouse.CreateModePlain
	if in.IfNotExists {
		mode = clickhouse.CreateModeIfNotExists
	}
	// Derive here rather than through TableOptions.SkipIndexes: an
	// interactive flag that matches nothing should say so, not silently
	// emit an index-less table.
	var derived []clickhouse.IndexSpec
	if in.SkipIndexes {
		derived, err = clickhouse.DeriveSkipIndexes(ir, clickhouse.DefaultSkipIndexPolicy())
		if err != nil {
			return
		}
		if len(derived) == 0 {
			err = eh.Errorf("--skip-indexes matched no lanes: the schema carries no membership lanes (the bloom_filter targets)")
			return
		}
	}
	sql, err = clickhouse.ComposeCreateTable(in.Table, ir, in.TableRowConfig, conv, clickhouse.TableOptions{
		Mode:        mode,
		Engine:      in.Engine,
		OrderBy:     orderBy,
		PartitionBy: in.PartitionBy,
		Indexes:     derived,
		Settings:    in.Settings,
	})
	return
}

// parseComposeColumnRef parses the --order-by selector grammar. The three
// forms are explicit rather than guessed, because a value column and a role
// can share a spelling (`val`):
//
//	plain:<name>              a plain column by leeway name
//	tv:<section>:<column>     a tagged section's value column
//	tvrole:<section>:<role>   a tagged section's channel/support column
func parseComposeColumnRef(s string) (ref clickhouse.ColumnRef, err error) {
	parts := strings.Split(s, ":")
	switch {
	case len(parts) == 2 && parts[0] == "plain":
		var name naming.StylableName
		name, err = composeDdlName("column", parts[1])
		if err != nil {
			return
		}
		ref = clickhouse.ColumnRef{Plain: name}
	case len(parts) == 3 && parts[0] == "tv":
		var section, column naming.StylableName
		section, err = composeDdlName("section", parts[1])
		if err != nil {
			return
		}
		column, err = composeDdlName("column", parts[2])
		if err != nil {
			return
		}
		ref = clickhouse.ColumnRef{Section: section, Column: column}
	case len(parts) == 3 && parts[0] == "tvrole":
		var section naming.StylableName
		section, err = composeDdlName("section", parts[1])
		if err != nil {
			return
		}
		var role common.ColumnRoleE
		role, err = common.ParseColumnRole(parts[2])
		if err != nil {
			err = eb.Build().Str("selector", s).Errorf("invalid role: %w", err)
			return
		}
		ref = clickhouse.ColumnRef{Section: section, Role: role}
	default:
		err = eb.Build().Str("selector", s).Errorf("unknown column selector; expected plain:<name>, tv:<section>:<column>, or tvrole:<section>:<role>")
	}
	return
}

func newCliCommandDdlCompose() *cli.Command {
	tableRowConfigFlag, tableRowConfigGetter := cli2.BuildEnumStringFlag(common.AllTableRowConfigs, common.TableRowConfigMultiAttributesPerRow, "tableRowConfig")
	return &cli.Command{
		Name:  "compose",
		Usage: "compose a durable CREATE TABLE from LW_ constructor-style column specs (ADR-0181 §SD6) — codecs, engine, ORDER BY through the real generator",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "table", Usage: "target table name, emitted verbatim (qualify at will)", Required: true},
			&cli.StringSliceFlag{Name: "plain", Usage: "plain column spec: '<name> <canonical-type> item:<t> [enc:… sem:…]' (repeatable)"},
			&cli.StringSliceFlag{Name: "tv", Usage: "tagged value column spec: '<section> <column> <canonical-type> [enc:… sem:… use:…]' (repeatable)"},
			&cli.StringSliceFlag{Name: "memb", Usage: "section membership channel: '<section> <channel>' (repeatable; support columns are machine-derived)"},
			&cli.StringSliceFlag{Name: "stream-group", Usage: "section streaming group: '<section> <key>' (repeatable)"},
			&cli.StringSliceFlag{Name: "co-group", Usage: "section co-section group: '<section> <key>' (repeatable)"},
			&cli.StringFlag{Name: "engine", Value: "MergeTree()", Usage: "ENGINE clause value"},
			&cli.StringSliceFlag{Name: "order-by", Usage: "ORDER BY column selector: plain:<name> | tv:<section>:<column> | tvrole:<section>:<role> (repeatable)"},
			&cli.StringFlag{Name: "partition-by", Usage: "raw PARTITION BY expression"},
			&cli.StringSliceFlag{Name: "settings", Usage: "SETTINGS entry (repeatable)"},
			&cli.BoolFlag{Name: "if-not-exists", Usage: "emit CREATE TABLE IF NOT EXISTS"},
			&cli.BoolFlag{Name: "skip-indexes", Usage: "derive data-skipping indexes (bloom_filter on membership lanes, ADR-0181 §SD4 defaults)"},
			tableRowConfigFlag,
		},
		Action: func(context *cli.Context) error {
			sql, err := composeDdl(composeDdlInput{
				Table:          context.String("table"),
				Plain:          context.StringSlice("plain"),
				Tagged:         context.StringSlice("tv"),
				Memberships:    context.StringSlice("memb"),
				StreamGroups:   context.StringSlice("stream-group"),
				CoGroups:       context.StringSlice("co-group"),
				Engine:         context.String("engine"),
				OrderBy:        context.StringSlice("order-by"),
				PartitionBy:    context.String("partition-by"),
				Settings:       context.StringSlice("settings"),
				IfNotExists:    context.Bool("if-not-exists"),
				SkipIndexes:    context.Bool("skip-indexes"),
				TableRowConfig: tableRowConfigGetter(context),
			})
			if err != nil {
				return err
			}
			_, err = os.Stdout.WriteString(sql + "\n")
			return err
		},
	}
}
