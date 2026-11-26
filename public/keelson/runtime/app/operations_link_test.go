package app

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

type linkTestArgs struct{ Sql string }

// linkTestCodec spells one fixed argument value as one fixed tail; broken
// reads it back as another.
type linkTestCodec struct{ broken bool }

func (linkTestCodec) FormatLink(args []byte) (string, error) { return "?a", nil }
func (inst linkTestCodec) ParseLink(tail string) ([]byte, error) {
	if inst.broken {
		return []byte{0xa1}, nil
	}
	return []byte{0xa0}, nil
}
func (linkTestCodec) LinkExamples() [][]byte { return [][]byte{{0xa0}} }

type noExamplesCodec struct{ linkTestCodec }

func (noExamplesCodec) LinkExamples() [][]byte { return nil }

func linkCatalog(mutate func(ops []OperationSpec)) *OperationsCatalog {
	ops := []OperationSpec{
		{Name: "set_sql", Version: 1, Summary: "set", Class: OperationClassCommand, Effect: OperationEffectDocument,
			Writes: []string{"sql"}, Args: reflect.TypeFor[linkTestArgs](), Link: linkTestCodec{}},
		{Name: "run_sql", Version: 1, Summary: "run", Class: OperationClassCommand, Effect: OperationEffectRun,
			Writes: []string{"sql"}, Args: reflect.TypeFor[linkTestArgs](), Link: linkTestCodec{}, LinkStage: "set_sql"},
	}
	mutate(ops)
	return &OperationsCatalog{Resources: []ResourceSpec{{Name: "sql", Summary: "the statement"}}, Operations: ops}
}

func TestValidate_LinkRules(t *testing.T) {
	assert.NoError(t, linkCatalog(func([]OperationSpec) {}).Validate())
	assert.Error(t, linkCatalog(func(ops []OperationSpec) { ops[0].Effect = OperationEffectConsequential; ops[0].Writes = nil }).Validate(),
		"a consequential operation takes no codec")
	assert.Error(t, linkCatalog(func(ops []OperationSpec) { ops[0].Refs = []string{"sql"} }).Validate(),
		"an operation with references takes no codec")
	assert.Error(t, linkCatalog(func(ops []OperationSpec) { ops[1].LinkStage = "nosuch" }).Validate(), "a stage names a command")
	assert.Error(t, linkCatalog(func(ops []OperationSpec) { ops[1].LinkStage = "run_sql" }).Validate(), "a stage is a document command")
	assert.Error(t, linkCatalog(func(ops []OperationSpec) { ops[0].LinkStage = "run_sql" }).Validate(), "only a run names a stage")
	assert.Error(t, linkCatalog(func(ops []OperationSpec) { ops[1].Link = nil }).Validate(), "a stage needs a codec")
	assert.Error(t, linkCatalog(func(ops []OperationSpec) { ops[0].Link = linkTestCodec{broken: true} }).Validate(),
		"a codec that reads its example back as other arguments")
	assert.Error(t, linkCatalog(func(ops []OperationSpec) { ops[0].Link = noExamplesCodec{} }).Validate(), "a codec without examples")
	assert.Error(t, linkCatalog(func(ops []OperationSpec) {
		ops[0].Class, ops[0].Effect, ops[0].Reads, ops[0].Writes = OperationClassQuery, OperationEffectNone, []string{"sql"}, nil
		ops[1].LinkStage = ""
	}).Validate(), "a query takes no codec")
}
