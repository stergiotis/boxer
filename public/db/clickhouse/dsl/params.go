package dsl

import (
	"iter"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/containers"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/ast"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/parsing/antlr4utils"
	"golang.org/x/exp/maps"
)

type ParamBindEnv struct {
	bind     map[string]*grammar.SettingExprContext
	inputSql string
}

func NewParamBindEnv() *ParamBindEnv {
	return &ParamBindEnv{
		bind: make(map[string]*grammar.SettingExprContext, 32),
	}
}
func (inst *ParamBindEnv) Has(name string) (has bool) {
	_, has = inst.bind[name]
	return
}

var ErrParamAlreadyBound = eh.Errorf("parameter is already bound to a value")

func (inst *ParamBindEnv) IsEmpty() bool {
	return len(inst.bind) == 0
}
func (inst *ParamBindEnv) AddDistinct(p *grammar.SettingExprContext) (err error) {
	if p == nil {
		return
	}
	id := ast.Identifier{}
	id.LoadContext(p.Identifier().(*grammar.IdentifierContext))
	name := id.Name
	if inst.Has(name) {
		err = eb.Build().Str("param", name).Errorf("unable to add: %w", ErrParamAlreadyBound)
		return
	}
	inst.bind[name] = p
	return
}
func (inst *ParamBindEnv) Clear() {
	clear(inst.bind)
	inst.inputSql = ""
}
func (inst *ParamBindEnv) Set(p *grammar.SettingExprContext) {
	if p == nil {
		return
	}
	id := ast.Identifier{}
	id.LoadContext(p.Identifier().(*grammar.IdentifierContext))
	name := id.Name
	inst.bind[name] = p
	return
}
func (inst *ParamBindEnv) IterSql() iter.Seq2[string, string] {
	return func(yield func(string, string) bool) {
		for n, p := range inst.bind {
			if !yield(n, p.GetText()) {
				return
			}
		}
	}
}

type ParamSlotSet struct {
	typesLu          map[string][]*ast.ColumnType
	paramOccurrences int
}

func NewParamSlotsSet() *ParamSlotSet {
	return &ParamSlotSet{
		typesLu:          nil,
		paramOccurrences: 0,
	}
}

var ErrIncompatibleParam = eh.Errorf("a param with an incompatible type is already contained in param set")

func (inst *ParamSlotSet) Add2(id *ast.Identifier, ct *ast.ColumnType) (err error) {
	return inst.add2(id.Name, ct)
}
func (inst *ParamSlotSet) add2(id string, ct *ast.ColumnType) (err error) {
	if inst.typesLu == nil {
		inst.typesLu = make(map[string][]*ast.ColumnType, 128)
	}
	others := inst.typesLu[id]
	if others == nil {
		inst.typesLu[id] = []*ast.ColumnType{ct}
		inst.paramOccurrences++
		return
	}
	for _, o := range others {
		if !ct.IsCompatible(o) {
			err = eb.Build().Str("other", o.Sql).Str("this", ct.Sql).Errorf("unable to add param to paramset: incompatible with existing param: %w", ErrIncompatibleParam)
			return
		}
	}
	inst.typesLu[id] = append(others, ct)
	inst.paramOccurrences++
	return
}
func (inst *ParamSlotSet) Add(param *grammar.ParamSlotContext) (err error) {
	if param == nil {
		return
	}
	id := ast.Identifier{}
	id.LoadContext(param.Identifier().(*grammar.IdentifierContext))
	ct := ast.ColumnType{}
	ct.LoadContext(param.ColumnTypeExpr().(*grammar.ColumnTypeExprContext))
	return inst.Add2(&id, &ct)
}
func (inst *ParamSlotSet) UnionMod(other *ParamSlotSet) (err error) {
	for id, ps := range other.All() {
		for _, p := range ps {
			err = inst.add2(id, p)
			if err != nil {
				err = eh.Errorf("unable to union param sets: %w", err)
				return
			}
		}
	}
	return
}
func (inst *ParamSlotSet) TotalParamOccurrences() int {
	return inst.paramOccurrences
}
func (inst *ParamSlotSet) TotalDistinctParams() int {
	return len(inst.typesLu)
}
func (inst *ParamSlotSet) IsEmpty() bool {
	return len(inst.typesLu) == 0
}
func (inst *ParamSlotSet) All() iter.Seq2[string, []*ast.ColumnType] {
	return func(yield func(string, []*ast.ColumnType) bool) {
		for k, vs := range inst.typesLu {
			if !yield(k, vs) {
				return
			}
		}
	}
}
func (inst *ParamSlotSet) NamesAndTypes() iter.Seq2[string, *containers.HashSet[string]] {
	return func(yield func(string, *containers.HashSet[string]) bool) {
		types := containers.NewHashSet[string](32)
		defer types.Clear()
		for k, vs := range inst.typesLu {
			var _ = vs
			for _, v := range vs {
				types.Add(v.Sql)
			}
			if !yield(k, types) {
				return
			}
			types.Clear()
		}
	}
}
func (inst *ParamSlotSet) Clear() {
	if len(inst.typesLu) > 0 {
		maps.Clear(inst.typesLu)
	}
	inst.paramOccurrences = 0
}

func (inst *ParamSlotSet) AddSlotsFromParseTree(ast antlr.Tree) (err error) {
	for slot := range antlr4utils.IterateAllByType[*grammar.ParamSlotContext](ast) {
		err = inst.Add(slot)
		if err != nil {
			err = eh.Errorf("error while adding param slot: %w", err)
			return
		}
	}
	return
}
