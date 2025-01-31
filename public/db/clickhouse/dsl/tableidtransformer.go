//go:build disabled

package dsl

import (
	chparser "github.com/AfterShip/clickhouse-sql-parser/parser"
	"github.com/matoous/go-nanoid/v2"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"slices"
	"strconv"
)

type TableIdTransformer struct {
	chparser.DefaultASTVisitor
	plugins []TableIdTransformerPluginI

	tableIdentifiers []*chparser.TableIdentifier
	replacements     []chparser.Expr
	ctes             []*chparser.CTEStmt
}

func NewTableIdTransformer() *TableIdTransformer {
	return &TableIdTransformer{
		DefaultASTVisitor: chparser.DefaultASTVisitor{
			Visit: nil,
		},
		plugins: make([]TableIdTransformerPluginI, 0, 64),

		tableIdentifiers: make([]*chparser.TableIdentifier, 0, 128),
		replacements:     make([]chparser.Expr, 0, 128),
		ctes:             make([]*chparser.CTEStmt, 0, 128),
	}
}
func (inst *TableIdTransformer) AddPlugin(plugin TableIdTransformerPluginI) {
	if plugin != nil {
		inst.plugins = append(inst.plugins, plugin)
	}
}

func (inst *TableIdTransformer) Reset() (err error) {
	clear(inst.tableIdentifiers)
	inst.tableIdentifiers = inst.tableIdentifiers[:0]
	clear(inst.replacements)
	inst.replacements = inst.replacements[:0]
	clear(inst.ctes)
	inst.ctes = inst.ctes[:0]
	return
}

var ErrUnhandledAstType = eh.Errorf("unhandled ast type")

func (inst *TableIdTransformer) Apply(ast chparser.Expr) (err error) {
	err = inst.Reset()
	if err != nil {
		err = eh.Errorf("unable to reset transformer: %w", err)
		return
	}
	err = ast.Accept(inst)
	if err != nil {
		err = eh.Errorf("unable to apply ast visitor: %w", err)
		return
	}
	if len(inst.tableIdentifiers) > 0 {
		inst.replacements = slices.Grow(inst.replacements, len(inst.tableIdentifiers))
		for _, t := range inst.tableIdentifiers {
			var repl chparser.Expr
			repl, err = inst.transformTableIdentifier(t)
			if err != nil {
				err = eh.Errorf("unable to transform table identifier: %w", err)
				return
			}
			inst.replacements = append(inst.replacements, repl)
		}
	}
	err = inst.populateCtes()
	if err != nil {
		err = eh.Errorf("unable to populate CTEs: %w", err)
		return
	}
	switch astt := ast.(type) {
	case *chparser.SelectQuery:
		if len(inst.ctes) > 0 {
			if astt.With == nil {
				astt.With = &chparser.WithClause{
					WithPos: 0,
					EndPos:  0,
					CTEs:    inst.ctes,
				}
			} else {
				astt.With.CTEs = append(inst.ctes, astt.With.CTEs...)
			}
		}
		break
	default:
		err = eb.Build().Type("ast", ast).Errorf("unhandled type: %w", ErrUnhandledAstType)
	}
	return
}
func (inst *TableIdTransformer) populateCtes() (err error) {
	ctes := slices.Grow(inst.ctes, len(inst.tableIdentifiers))
	var key string
	key, err = gonanoid.Generate("abcdefghijklmnopqrstuvwxyz_", 24)
	if err != nil {
		err = eh.Errorf("unable to generate key for table identifiers: %w", err)
		return
	}
	var u uint64
	for i, t := range inst.tableIdentifiers {
		r := inst.replacements[i]
		if r != nil {
			n := key + "_" + strconv.FormatUint(u, 10)
			t.Database = nil
			t.Table.Name = n
			t.Table.NamePos = 0
			t.Table.NameEnd = 0
			t.Table.QuoteType = chparser.BackTicks

			ctes = append(ctes, &chparser.CTEStmt{
				CTEPos: 0,
				Expr: &chparser.Ident{
					Name:      n,
					QuoteType: chparser.BackTicks,
					NamePos:   0,
					NameEnd:   0,
				},
				Alias: r,
			})
			u++
		}
	}
	inst.ctes = ctes
	return
}
func (inst *TableIdTransformer) transformTableIdentifier(t *chparser.TableIdentifier) (replacement chparser.Expr, err error) {
	var db string
	if t.Database != nil {
		db = t.Database.Name
	}
	tbl := t.Table.Name
	for _, p := range inst.plugins {
		var repl *ParsedDqlQuery
		var isStaticReplacement bool
		var appl bool
		repl, isStaticReplacement, appl, err = p.Transform(db, tbl)
		if appl {
			name := p.Name()
			if repl == nil {
				log.Warn().Str("plugin", name).Msg("plugin returned nil for transformed expression, skipping")
			} else {
				if !isStaticReplacement {
					var other *ParsedDqlQuery
					other, err = repl.DeepCopy()
					if err != nil {
						err = eh.Errorf("unable to deep copy non-static replacement: %w", err)
						return
					}
					replacement = other.ast
				}
				log.Debug().Str("db", db).Str("tbl", tbl).Str("transformer", p.Name()).Bool("isStaticReplacement", isStaticReplacement).Msg("transforming table identifier")
				// first applicable transformer wins
				return
			}
		}
	}
	log.Debug().Str("db", db).Str("tbl", tbl).Msg("no transformer is applicable for table identifier")
	return
}

func (inst *TableIdTransformer) VisitTableIdentifier(expr *chparser.TableIdentifier) error {
	inst.tableIdentifiers = append(inst.tableIdentifiers, expr)
	if expr.Database == nil || expr.Table == nil {
		return nil
	}

	input := expr.Table
	log.Debug().Stringer("db", expr.Database).Stringer("input", input).Msg("transforming table identifier")
	return nil
}

var _ chparser.ASTVisitor = (*TableIdTransformer)(nil)
var _ TransfomerI = (*TableIdTransformer)(nil)
